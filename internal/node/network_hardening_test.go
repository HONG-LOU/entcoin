package node

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"

	"github.com/gorilla/websocket"
)

func TestHTTPClientRejectsCrossHostRedirectAndMarksPeerFailure(t *testing.T) {
	service := newTestNode(t)
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		writeJSON(writer, http.StatusOK, map[string]string{"unexpected": "redirect followed"})
	}))
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	crossHostTarget := "http://localhost:" + targetURL.Port() + "/v2/status"
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, crossHostTarget, http.StatusFound)
	}))
	defer redirect.Close()

	registerTestPeer(t, service, redirect.URL)
	service.syncPeer(redirect.URL)
	if targetRequests.Load() != 0 {
		t.Fatal("HTTP client followed a cross-host redirect")
	}
	service.mu.RLock()
	state := service.peers[redirect.URL]
	service.mu.RUnlock()
	if state == nil || state.Failures != 1 || state.NextAttempt.IsZero() {
		t.Fatalf("redirecting peer state = %#v", state)
	}
}

func TestInvalidHTTPMempoolBatchStopsAndMarksPeerFailure(t *testing.T) {
	service := newTestNode(t)
	peer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/status":
			tip, err := service.ledger.Tip(request.Context())
			if err != nil {
				writeError(writer, http.StatusInternalServerError, err)
				return
			}
			writeJSON(writer, http.StatusOK, statusFromTip(tip, 0))
		case "/v2/mempool":
			writeJSON(writer, http.StatusOK, transactionsResponse{
				Protocol: ledger.ProtocolName,
				Transactions: []core.Transaction{{
					ID: strings.Repeat("0", 64),
				}},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer peer.Close()
	registerTestPeer(t, service, peer.URL)

	service.syncPeer(peer.URL)
	service.mu.RLock()
	state := service.peers[peer.URL]
	service.mu.RUnlock()
	if state == nil || state.Failures != 1 || !strings.Contains(state.LastError, "invalid mempool batch") {
		t.Fatalf("invalid mempool peer state = %#v", state)
	}
}

func TestGossipTriggeredSyncHonorsPeerBackoff(t *testing.T) {
	service := newTestNode(t)
	peer := "http://127.0.0.1:49001"
	registerTestPeer(t, service, peer)
	service.mu.Lock()
	service.peers[peer].NextAttempt = time.Now().Add(time.Minute)
	service.mu.Unlock()
	if service.beginPeerSync(peer) {
		t.Fatal("peer sync bypassed active failure backoff")
	}
	service.mu.Lock()
	service.peers[peer].NextAttempt = time.Now().Add(-time.Second)
	service.mu.Unlock()
	if !service.beginPeerSync(peer) {
		t.Fatal("peer sync did not resume after backoff")
	}
	service.endPeerSync(peer)
}

func TestHTTPRateBudgetAndDuplicateBlockRequest(t *testing.T) {
	now := time.Now()
	rate := &requestRateState{}
	for index := 0; index < int(httpRequestsPerIPBurst); index++ {
		if !rate.allow(now) {
			t.Fatalf("burst request %d was rejected", index)
		}
	}
	if rate.allow(now) {
		t.Fatal("request beyond per-IP burst was accepted")
	}
	if !rate.allow(now.Add(time.Second)) {
		t.Fatal("request budget did not refill")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := newTestNode(t)
	startTestNode(t, ctx, service)
	genesisHash := core.GenesisBlock().Hash
	body, err := json.Marshal(blocksRequest{Hashes: []string{genesisHash, genesisHash}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post("http://"+service.ActualAddress()+"/v2/blocks", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate block request returned HTTP %d", response.StatusCode)
	}
}

func TestSyncRejectsOversizedHeaderPage(t *testing.T) {
	service := newTestNode(t)
	headers := make([]core.Block, maxHeaderBatch+1)
	peer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/headers" {
			http.NotFound(writer, request)
			return
		}
		writeJSON(writer, http.StatusOK, headersResponse{
			Protocol: ledger.ProtocolName, CommonHeight: 0,
			CommonHash: core.GenesisBlock().Hash, Headers: headers,
		})
	}))
	defer peer.Close()
	tip, err := service.ledger.Tip(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = service.syncRemoteChain(context.Background(), peer.URL, tip)
	if err == nil || !strings.Contains(err.Error(), "header page limit") {
		t.Fatalf("oversized header page error = %v", err)
	}
}

func TestPrunedSyncPausesOnlyValidatedStrongerCandidateBeforeBodies(t *testing.T) {
	service := newTestNode(t)
	setTestPrunedThrough(t, service, 1)
	genesis := core.GenesisBlock()

	weakPeer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/status":
			writeJSON(writer, http.StatusOK, protocolStatus{
				Protocol: ledger.ProtocolName, Name: core.ChainName, Symbol: core.ChainSymbol,
				Height: 0, TipHash: strings.Repeat("f", 64), ChainWork: "0",
			})
		case "/v2/headers":
			writeJSON(writer, http.StatusOK, headersResponse{
				Protocol: ledger.ProtocolName, CommonHeight: 0, CommonHash: genesis.Hash,
			})
		case "/v2/mempool":
			writeJSON(writer, http.StatusOK, transactionsResponse{Protocol: ledger.ProtocolName})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer weakPeer.Close()
	registerTestPeer(t, service, weakPeer.URL)
	service.syncPeer(weakPeer.URL)
	service.mu.RLock()
	_, weakPaused := service.resyncPauses[weakPeer.URL]
	service.mu.RUnlock()
	if weakPaused {
		t.Fatal("empty weak chain caused a pruned resync pause")
	}

	block := cachedValidTestBlock(t)
	header := block
	header.Transactions = nil
	var bodyRequests atomic.Int32
	strongPeer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/status":
			writeJSON(writer, http.StatusOK, protocolStatus{
				Protocol: ledger.ProtocolName, Name: core.ChainName, Symbol: core.ChainSymbol,
				Height: block.Height, TipHash: block.Hash,
				ChainWork: new(big.Int).Lsh(big.NewInt(1), uint(block.Difficulty)).String(),
			})
		case "/v2/headers":
			writeJSON(writer, http.StatusOK, headersResponse{
				Protocol: ledger.ProtocolName, CommonHeight: 0, CommonHash: genesis.Hash,
				Headers: []core.Block{header},
			})
		case "/v2/blocks":
			bodyRequests.Add(1)
			writeJSON(writer, http.StatusOK, blocksResponse{Protocol: ledger.ProtocolName, Blocks: []core.Block{block}})
		case "/v2/mempool":
			writeJSON(writer, http.StatusOK, transactionsResponse{Protocol: ledger.ProtocolName})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer strongPeer.Close()
	registerTestPeer(t, service, strongPeer.URL)
	service.syncPeer(strongPeer.URL)
	if bodyRequests.Load() != 0 {
		t.Fatal("pruned deep reorganization downloaded bodies before pausing")
	}
	if !service.chainSyncPaused(strongPeer.URL, block.Hash, time.Now()) {
		t.Fatal("validated stronger deep fork was not paused")
	}
	if service.chainSyncPaused(weakPeer.URL, block.Hash, time.Now()) {
		t.Fatal("one peer's deep fork paused an unrelated peer")
	}
	if service.chainSyncPaused(strongPeer.URL, strings.Repeat("e", 64), time.Now()) {
		t.Fatal("changed candidate tip remained paused")
	}
}

func TestSyncFoldsReplayedActivePrefixBeforePruneAndBodyDownload(t *testing.T) {
	service := newTestNode(t)
	first := cachedValidTestBlock(t)
	second := cachedValidTestExtension(t)
	state := core.NewState()
	state.Blocks = append(state.Blocks, first)
	if err := service.ledger.ImportState(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	setTestPrunedThrough(t, service, 1)
	firstHeader := first
	firstHeader.Transactions = nil
	secondHeader := second
	secondHeader.Transactions = nil
	requested := make(chan []string, 1)
	peer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/headers":
			writeJSON(writer, http.StatusOK, headersResponse{
				Protocol: ledger.ProtocolName, CommonHeight: 0, CommonHash: core.GenesisBlock().Hash,
				Headers: []core.Block{firstHeader, secondHeader},
			})
		case "/v2/blocks":
			var query blocksRequest
			if err := json.NewDecoder(request.Body).Decode(&query); err != nil {
				writeError(writer, http.StatusBadRequest, err)
				return
			}
			requested <- append([]string(nil), query.Hashes...)
			time.Sleep(100 * time.Millisecond)
			writeJSON(writer, http.StatusOK, blocksResponse{Protocol: ledger.ProtocolName, Blocks: []core.Block{second}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer peer.Close()
	localTip, err := service.ledger.Tip(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	syncContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.syncRemoteChain(syncContext, peer.URL, localTip); err != nil {
		t.Fatal(err)
	}
	select {
	case hashes := <-requested:
		if len(hashes) != 1 || hashes[0] != second.Hash {
			t.Fatalf("body request hashes = %v, want only extension %s", hashes, second.Hash)
		}
	default:
		t.Fatal("extension body was not requested")
	}
	tip, err := service.ledger.Tip(context.Background())
	if err != nil || tip.Height != 2 || tip.Hash != second.Hash {
		t.Fatalf("tip after folded-prefix sync = %#v, err %v", tip, err)
	}
}

func TestStagingSingleBudgetCleanupAndAtomicRollback(t *testing.T) {
	if maxBlockBatch != maxBlockDownloadBatch {
		t.Fatalf("served block batch %d does not match download batch %d", maxBlockBatch, maxBlockDownloadBatch)
	}
	if maxDirectExtensionBlocks != maxSyncHeaderBatch || maxDirectExtensionBlocks <= maxBlockBatch {
		t.Fatalf("direct extension chunk %d does not reuse a %d-header page across bounded %d-block requests", maxDirectExtensionBlocks, maxSyncHeaderBatch, maxBlockBatch)
	}
	if int64(maxDirectExtensionBlocks)*(maxBlockBodyBytes+4) > maxStagedForkBytes {
		t.Fatal("direct extension chunk can exceed the staging budget at legal per-block limits")
	}
	if maxBlocksResponseBytes < int64(maxBlockBatch)*maxBlockBodyBytes {
		t.Fatal("block response cap cannot carry a full legal block batch")
	}
	if maxMempoolResponseBytes < int64(maxMempoolSyncValidations)*maxTransactionMessageBytes {
		t.Fatal("mempool response cap cannot carry a full legal transaction batch")
	}
	service := newTestNode(t)
	valid := cachedValidTestBlock(t)
	invalid := core.Block{
		Version: valid.Version, Height: 2, Timestamp: valid.Timestamp + 10,
		PreviousHash: valid.Hash, MerkleRoot: strings.Repeat("0", 64),
		Difficulty: valid.Difficulty,
	}
	invalid.Hash = invalid.ComputeHash()
	bodies := map[string]core.Block{valid.Hash: valid, invalid.Hash: invalid}
	peer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/blocks" {
			http.NotFound(writer, request)
			return
		}
		var query blocksRequest
		if err := json.NewDecoder(request.Body).Decode(&query); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		blocks := make([]core.Block, 0, len(query.Hashes))
		for _, hash := range query.Hashes {
			blocks = append(blocks, bodies[hash])
		}
		writeJSON(writer, http.StatusOK, blocksResponse{Protocol: ledger.ProtocolName, Blocks: blocks})
	}))
	defer peer.Close()
	header := valid
	header.Transactions = nil

	first, err := service.stageBlocks(context.Background(), peer.URL, []core.Block{header})
	if err != nil {
		t.Fatal(err)
	}
	firstPath := first.path
	blockedContext, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := service.stageBlocks(blockedContext, peer.URL, []core.Block{header}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second simultaneous staging error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closed staging file still exists: %v", err)
	}

	if _, err := service.stageBlocksWithBudget(context.Background(), peer.URL, []core.Block{header}, 1); err == nil {
		t.Fatal("staging candidate exceeded a tiny total budget")
	}
	stale, err := filepath.Glob(filepath.Join(filepath.Dir(service.ledger.Path()), "incoming-chain-*.tmp"))
	if err != nil || len(stale) != 0 {
		t.Fatalf("failed staging left temporary files: %v, err %v", stale, err)
	}

	invalidHeader := invalid
	invalidHeader.Transactions = nil
	staged, err := service.stageBlocks(context.Background(), peer.URL, []core.Block{header, invalidHeader})
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.ledger.Tip(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ledger.ReplaceFromSource(context.Background(), 0, 2, staged.BlockAt); err == nil {
		t.Fatal("invalid staged replacement was accepted")
	}
	after, err := service.ledger.Tip(context.Background())
	if err != nil || after.Hash != before.Hash || after.Height != before.Height {
		t.Fatalf("failed staged replacement changed tip: before=%#v after=%#v err=%v", before, after, err)
	}
	stagedPath := staged.path
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stagedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement staging file still exists: %v", err)
	}
}

func TestStageBlocksReusesHeadersAcrossBoundedBodyBatches(t *testing.T) {
	service := newTestNode(t)
	count := maxBlockDownloadBatch + 1
	headers := make([]core.Block, 0, count)
	bodies := make(map[string]core.Block, count)
	for index := 0; index < count; index++ {
		block := core.Block{
			Version: 1, Height: uint64(index + 1), Hash: fmt.Sprintf("%064x", index+1),
		}
		headers = append(headers, block)
		bodies[block.Hash] = block
	}
	requested := make([]int, 0, 2)
	peer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var query blocksRequest
		if err := json.NewDecoder(request.Body).Decode(&query); err != nil {
			writeError(writer, http.StatusBadRequest, err)
			return
		}
		requested = append(requested, len(query.Hashes))
		blocks := make([]core.Block, 0, len(query.Hashes))
		for _, hash := range query.Hashes {
			blocks = append(blocks, bodies[hash])
		}
		writeJSON(writer, http.StatusOK, blocksResponse{Protocol: ledger.ProtocolName, Blocks: blocks})
	}))
	defer peer.Close()

	staged, err := service.stageBlocks(context.Background(), peer.URL, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if fmt.Sprint(requested) != fmt.Sprint([]int{maxBlockDownloadBatch, 1}) {
		t.Fatalf("body batch sizes = %v", requested)
	}
	for index, header := range headers {
		block, err := staged.BlockAt(index)
		if err != nil || block.Hash != header.Hash {
			t.Fatalf("staged block %d = %s, err %v", index, block.Hash, err)
		}
	}
}

func TestStartupRemovesOnlyRegularStaleStagingFiles(t *testing.T) {
	directory := t.TempDir()
	staleFile := filepath.Join(directory, "incoming-chain-stale.tmp")
	if err := os.WriteFile(staleFile, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	preservedDirectory := filepath.Join(directory, "incoming-chain-preserved.tmp")
	if err := os.Mkdir(preservedDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := New(testConfig(directory))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestNode(t, service)
	if _, err := os.Stat(staleFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("regular stale staging file still exists: %v", err)
	}
	if info, err := os.Stat(preservedDirectory); err != nil || !info.IsDir() {
		t.Fatalf("non-regular staging path was removed: info=%v err=%v", info, err)
	}
}

func TestWebSocketInvalidGossipThresholdDisconnectsAndBacksOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := newTestNode(t)
	startTestNode(t, ctx, service)
	connection, _, err := websocket.DefaultDialer.Dial("ws://"+service.ActualAddress()+"/v2/p2p", nil)
	if err != nil {
		t.Fatal(err)
	}
	remoteWallet, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(gossipMessage{
		Type: "hello", Protocol: ledger.ProtocolName, NodeID: remoteWallet.Address, ListenPort: 49000,
	}); err != nil {
		t.Fatal(err)
	}
	invalid := gossipMessage{
		Type: "transaction", Protocol: ledger.ProtocolName,
		Transaction: &core.Transaction{ID: strings.Repeat("0", 64)},
	}
	for range webSocketInvalidScoreLimit - 1 {
		if err := connection.WriteJSON(invalid); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, 3*time.Second, func() bool {
		service.mu.RLock()
		defer service.mu.RUnlock()
		state := service.invalidGossipByIP["127.0.0.1"]
		return state != nil && state.score == webSocketInvalidScoreLimit-1
	})
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	connection, _, err = websocket.DefaultDialer.Dial("ws://"+service.ActualAddress()+"/v2/p2p", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.WriteJSON(gossipMessage{
		Type: "hello", Protocol: ledger.ProtocolName, NodeID: remoteWallet.Address, ListenPort: 49000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(invalid); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		service.mu.RLock()
		defer service.mu.RUnlock()
		state := service.invalidGossipByIP["127.0.0.1"]
		return state != nil && state.score >= webSocketInvalidScoreLimit
	})
	service.mu.RLock()
	peerCount := len(service.peers)
	service.mu.RUnlock()
	if peerCount != 0 {
		t.Fatalf("inbound WebSocket polluted outbound peer table with %d entries", peerCount)
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := connection.ReadMessage(); err != nil {
			break
		}
	}
}

func TestCloseTimeoutReturnsAndFinishesCleanup(t *testing.T) {
	service := newTestNode(t)
	release := make(chan struct{})
	service.launch(func() { <-release })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := service.Close(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close timeout error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("close ignored its context deadline")
	}
	close(release)
	select {
	case <-service.closeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("asynchronous close cleanup did not finish")
	}
}

func TestTipChangeCancelsMiningJobAndContinuousMiningRebuilds(t *testing.T) {
	service := newTestNode(t)
	firstJob := make(chan struct{})
	rebuiltJob := make(chan struct{})
	var calls atomic.Int32
	service.mineBlock = func(ctx context.Context, _ core.Block) (core.Block, error) {
		switch calls.Add(1) {
		case 1:
			close(firstJob)
		case 2:
			close(rebuiltJob)
		}
		<-ctx.Done()
		return core.Block{}, ctx.Err()
	}
	if err := service.StartMining(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstJob:
	case <-time.After(2 * time.Second):
		t.Fatal("initial mining job did not start")
	}
	started := time.Now()
	if err := service.acceptBlock(context.Background(), cachedValidTestBlock(t), nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rebuiltJob:
		if time.Since(started) > time.Second {
			t.Fatal("continuous mining did not promptly rebuild after the tip changed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tip change did not cancel and rebuild the mining job")
	}
	service.StopMining()
	waitFor(t, 2*time.Second, func() bool {
		service.mu.RLock()
		defer service.mu.RUnlock()
		return !service.mining && service.miningJobs == 0
	})
}

func TestMiningTemplateRefreshesOnlyAfterActivatedDifficultyDrops(t *testing.T) {
	ideal := core.ASERTAnchorTimestamp + int64(core.ConsensusUpgradeHeight-core.ASERTAnchorHeight)*core.TargetBlockSeconds
	candidate := core.Block{
		Height:     core.ConsensusUpgradeHeight,
		Timestamp:  ideal,
		Difficulty: core.ASERTAnchorDifficulty,
	}
	if miningTemplateNeedsRefresh(candidate, ideal+core.ASERTHalfLifeSeconds/2-1) {
		t.Fatal("template refreshed before the next ASERT boundary")
	}
	if !miningTemplateNeedsRefresh(candidate, ideal+core.ASERTHalfLifeSeconds/2) {
		t.Fatal("template did not refresh at the next ASERT boundary")
	}
	candidate.Height = core.ConsensusUpgradeHeight - 1
	if miningTemplateNeedsRefresh(candidate, math.MaxInt64) {
		t.Fatal("legacy mining template used post-activation refresh rules")
	}
}

func TestHTTPIncrementalSyncCrossesConsensusActivation(t *testing.T) {
	service := newTestNode(t)
	activationTime := nodeUpgradeTestActivationTime()
	insertNodeUpgradeTestPrefix(t, service, activationTime-core.TargetBlockSeconds)
	ctx := context.Background()
	localTip, err := service.ledger.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	window, err := service.ledger.HeaderWindow(ctx, localTip.Height)
	if err != nil {
		t.Fatal(err)
	}
	coinbase, err := core.NewCoinbase(service.Address(), core.ConsensusUpgradeHeight, core.Subsidy(core.ConsensusUpgradeHeight))
	if err != nil {
		t.Fatal(err)
	}
	transactions := []core.Transaction{coinbase}
	block := core.Block{
		Version:      core.UpgradedBlockVersion,
		Height:       core.ConsensusUpgradeHeight,
		Timestamp:    activationTime,
		PreviousHash: localTip.Hash,
		MerkleRoot:   core.MerkleRoot(transactions),
		Difficulty:   core.ExpectedDifficultyAt(window, core.ConsensusUpgradeHeight, activationTime),
		Transactions: transactions,
	}
	block, err = core.MineBlockWithWorkers(ctx, block, 1)
	if err != nil {
		t.Fatal(err)
	}
	header := block
	header.Transactions = nil
	peer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/headers":
			writeJSON(writer, http.StatusOK, headersResponse{
				Protocol: ledger.ProtocolName, CommonHeight: localTip.Height,
				CommonHash: localTip.Hash, Headers: []core.Block{header},
			})
		case "/v2/blocks":
			writeJSON(writer, http.StatusOK, blocksResponse{Protocol: ledger.ProtocolName, Blocks: []core.Block{block}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer peer.Close()
	if err := service.syncRemoteChainFromAtTime(
		ctx,
		httpRemoteSource{service: service, peer: peer.URL},
		localTip,
		activationTime+core.MaxFutureSeconds,
	); err != nil {
		t.Fatalf("sync activation block: %v", err)
	}
	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Height != core.ConsensusUpgradeHeight || dashboard.TipHash != block.Hash ||
		dashboard.ConsensusVersion != core.UpgradedBlockVersion ||
		dashboard.ConfirmedBalance != core.FormatAmount(core.Subsidy(core.ConsensusUpgradeHeight)) {
		t.Fatalf("post-activation sync dashboard = %+v", dashboard)
	}
}

func nodeUpgradeTestActivationTime() int64 {
	ideal := core.ASERTAnchorTimestamp + int64(core.ConsensusUpgradeHeight-core.ASERTAnchorHeight)*core.TargetBlockSeconds
	return ideal + int64(core.ASERTAnchorDifficulty-core.MinimumDifficulty)*core.ASERTHalfLifeSeconds
}

func insertNodeUpgradeTestPrefix(t *testing.T, service *Service, tipTimestamp int64) {
	t.Helper()
	database, err := sql.Open("sqlite", service.ledger.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	work := new(big.Int).Lsh(big.NewInt(1), 40).Bytes()
	nonce := make([]byte, 8)
	insert := func(height uint64, hash, previous string, timestamp int64) {
		t.Helper()
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO blocks(
				height, hash, previous_hash, version, timestamp, merkle_root,
				difficulty, nonce, cumulative_work, data, encoded_size
			) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 0)
		`, int64(height), hash, previous, int64(core.LegacyBlockVersion), timestamp,
			core.MerkleRoot(nil), int64(core.ASERTAnchorDifficulty), nonce, work); err != nil {
			t.Fatalf("insert node activation header %d: %v", height, err)
		}
	}

	start := core.ConsensusUpgradeHeight - core.FirstAdjustment
	previous := fmt.Sprintf("%064x", start)
	for height := start; height < core.ConsensusUpgradeHeight; height++ {
		hash := fmt.Sprintf("%064x", height+1)
		timestamp := tipTimestamp - int64(core.ConsensusUpgradeHeight-1-height)*core.TargetBlockSeconds
		insert(height, hash, previous, timestamp)
		previous = hash
	}

	height := core.ConsensusUpgradeHeight - 1
	step := uint64(1)
	locatorCount := 0
	for height > 0 && locatorCount < 64 {
		if height < start {
			hash := fmt.Sprintf("%064x", height+1)
			insert(height, hash, fmt.Sprintf("%064x", height), tipTimestamp-int64(core.ConsensusUpgradeHeight-1-height)*core.TargetBlockSeconds)
		}
		locatorCount++
		if locatorCount > 10 {
			step *= 2
		}
		if step > height {
			height = 0
		} else {
			height -= step
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func registerTestPeer(t *testing.T, service *Service, rawURL string) {
	t.Helper()
	peer, err := normalizePeer(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ledger.UpsertPeer(context.Background(), peer, true); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.peers[peer] = &peerState{URL: peer}
	service.mu.Unlock()
}

func setTestPrunedThrough(t *testing.T, service *Service, height uint64) {
	t.Helper()
	database, err := sql.Open("sqlite", service.ledger.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, height)
	if _, err := database.Exec(`
		INSERT INTO meta(key, value) VALUES('pruned_through', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, encoded); err != nil {
		t.Fatal(err)
	}
}

var (
	validTestBlockOnce sync.Once
	validTestBlock     core.Block
	validTestExtension core.Block
	validTestBlockErr  error
)

func cachedValidTestBlock(t *testing.T) core.Block {
	t.Helper()
	validTestBlockOnce.Do(func() {
		wallet, err := core.NewWallet()
		if err != nil {
			validTestBlockErr = err
			return
		}
		state := core.NewState()
		validTestBlock, validTestBlockErr = state.Mine(context.Background(), wallet.Address)
		if validTestBlockErr == nil {
			validTestExtension, validTestBlockErr = state.Mine(context.Background(), wallet.Address)
		}
	})
	if validTestBlockErr != nil {
		t.Fatal(validTestBlockErr)
	}
	return validTestBlock
}

func cachedValidTestExtension(t *testing.T) core.Block {
	t.Helper()
	_ = cachedValidTestBlock(t)
	return validTestExtension
}
