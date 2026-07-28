package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"

	"github.com/gorilla/websocket"
)

func TestBootstrapManifestAutomaticallyJoinsLoopbackSeedAndSyncsBlock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seed := newTestNode(t)
	startTestNode(t, ctx, seed)
	minePublicNetworkTestBlock(t, seed)
	seedURL := "http://" + seed.ActualAddress()

	manifest := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, bootstrapManifest{
			Version:  bootstrapManifestVersion,
			Protocol: ledger.ProtocolName,
			Peers:    []string{seedURL},
		})
	}))
	defer manifest.Close()

	config := testConfig(t.TempDir())
	config.BootstrapManifestURLs = []string{manifest.URL}
	node := newPublicNetworkTestNode(t, config)
	startTestNode(t, ctx, node)

	waitFor(t, 10*time.Second, func() bool {
		tip, err := node.ledger.Tip(context.Background())
		if err != nil || tip.Height < 1 {
			return false
		}
		node.mu.RLock()
		state := node.peers[seedURL]
		ready := state != nil && state.Bootstrap && state.Online
		node.mu.RUnlock()
		return ready
	})

	dashboard, err := node.Dashboard()
	if err != nil {
		t.Fatal(err)
	}
	if !dashboard.BootstrapEnabled || !dashboard.BootstrapReady || dashboard.Height < 1 {
		t.Fatalf("dashboard after bootstrap sync = %+v", dashboard)
	}
}

func TestUnreachableBootstrapManifestDoesNotBlockStartAndRecordsError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	source := "http://" + listener.Addr().String() + "/mainnet.json"
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	config := testConfig(t.TempDir())
	config.BootstrapManifestURLs = []string{source}
	node := newPublicNetworkTestNode(t, config)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := time.Now()
	if err := node.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Start blocked for unreachable bootstrap manifest: %s", elapsed)
	}

	waitFor(t, 5*time.Second, func() bool {
		dashboard, dashboardErr := node.Dashboard()
		return dashboardErr == nil && dashboard.BootstrapEnabled && dashboard.BootstrapError != ""
	})
}

func TestPeersEndpointReturnsOnlyRecentActivePublicPeersWithinLimit(t *testing.T) {
	node := newTestNode(t)
	now := time.Now()
	eligible := make([]string, 0, maxPeerExchangePeers+4)

	node.mu.Lock()
	for index := 1; index <= maxPeerExchangePeers+4; index++ {
		peer := fmt.Sprintf("http://8.8.4.%d:47821", index)
		eligible = append(eligible, peer)
		node.peers[peer] = &peerState{
			URL: peer, Public: true, Online: true, ActiveOutbound: true, LastSeen: now,
		}
		node.activeOutbound[peer] = struct{}{}
	}
	excluded := map[string]*peerState{
		"http://10.0.0.1:47821": {
			URL: "http://10.0.0.1:47821", Online: true, ActiveOutbound: true, LastSeen: now,
		},
		"http://8.8.4.101:47821": {
			URL: "http://8.8.4.101:47821", Public: true, Online: true, ActiveOutbound: true,
			LastSeen: now.Add(-3 * time.Minute),
		},
		"http://8.8.4.102:47821": {
			URL: "http://8.8.4.102:47821", Public: true, Online: true, LastSeen: now,
		},
		"http://8.8.4.103:47821": {
			URL: "http://8.8.4.103:47821", Public: true, Online: true, ActiveOutbound: true,
			Failures: 1, LastSeen: now,
		},
		"http://8.8.4.104:47821": {
			URL: "http://8.8.4.104:47821", Public: true, ActiveOutbound: true, LastSeen: now,
		},
	}
	for peer, state := range excluded {
		node.peers[peer] = state
		if state.ActiveOutbound {
			node.activeOutbound[peer] = struct{}{}
		}
	}
	node.mu.Unlock()

	recorder := httptest.NewRecorder()
	node.handlePeers(recorder, httptest.NewRequest(http.MethodGet, "/v2/peers", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v2/peers returned HTTP %d", recorder.Code)
	}
	var response peersResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Protocol != ledger.ProtocolName {
		t.Fatalf("peer response protocol = %q", response.Protocol)
	}
	if len(response.Peers) != maxPeerExchangePeers {
		t.Fatalf("peer response count = %d, want %d", len(response.Peers), maxPeerExchangePeers)
	}
	sort.Strings(eligible)
	want := eligible[:maxPeerExchangePeers]
	if fmt.Sprint(response.Peers) != fmt.Sprint(want) {
		t.Fatalf("peer response = %v, want %v", response.Peers, want)
	}
	for _, peer := range response.Peers {
		if _, err := normalizePublicPeer(peer); err != nil {
			t.Fatalf("endpoint returned non-public peer %q: %v", peer, err)
		}
		if _, found := excluded[peer]; found {
			t.Fatalf("endpoint returned excluded peer %q", peer)
		}
	}
}

func TestStartupRemovesOnlyNeverReachableStaleDiscoveredPeers(t *testing.T) {
	directory := t.TempDir()
	initial, err := New(testConfig(directory))
	if err != nil {
		t.Fatal(err)
	}
	closeTestNode(t, initial)
	chain, err := ledger.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	stale := "http://203.0.113.10:47821"
	recent := "http://203.0.113.11:47821"
	manual := "http://203.0.113.12:47821"
	for _, peer := range []string{stale, recent} {
		if err := chain.UpsertPeer(context.Background(), peer, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := chain.UpsertPeer(context.Background(), manual, true); err != nil {
		t.Fatal(err)
	}
	for range staleDiscoveredFailures {
		if err := chain.RecordPeerFailure(context.Background(), stale, time.Now(), errors.New("dial timeout")); err != nil {
			t.Fatal(err)
		}
		if err := chain.RecordPeerFailure(context.Background(), manual, time.Now(), errors.New("dial timeout")); err != nil {
			t.Fatal(err)
		}
	}
	if err := chain.RecordPeerSuccess(context.Background(), recent, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := chain.Close(); err != nil {
		t.Fatal(err)
	}

	node := newPublicNetworkTestNode(t, testConfig(directory))
	node.mu.RLock()
	_, staleFound := node.peers[stale]
	_, recentFound := node.peers[recent]
	_, manualFound := node.peers[manual]
	node.mu.RUnlock()
	if staleFound || !recentFound || !manualFound {
		t.Fatalf("peer retention stale=%v recent=%v manual=%v", staleFound, recentFound, manualFound)
	}
	records, err := node.ledger.Peers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.URL == stale {
			t.Fatal("stale discovered peer remained in SQLite")
		}
	}
}

func TestTrustLoopbackProxyUsesOnlySingleDedicatedClientIPHeader(t *testing.T) {
	trustedConfig := testConfig(t.TempDir())
	trustedConfig.TrustLoopbackProxy = true
	trusted := newPublicNetworkTestNode(t, trustedConfig)
	untrusted := newTestNode(t)

	tests := []struct {
		name       string
		node       *Service
		remoteAddr string
		headers    []string
		wantStatus int
		wantIP     string
	}{
		{
			name: "trusted loopback proxy", node: trusted, remoteAddr: "127.0.0.1:40000",
			headers: []string{"8.8.8.8"}, wantStatus: http.StatusOK, wantIP: "8.8.8.8",
		},
		{
			name: "proxy trust disabled", node: untrusted, remoteAddr: "127.0.0.1:40001",
			headers: []string{"8.8.8.8"}, wantStatus: http.StatusOK, wantIP: "127.0.0.1",
		},
		{
			name: "non-loopback spoof ignored", node: trusted, remoteAddr: "198.51.100.20:40002",
			headers: []string{"8.8.8.8"}, wantStatus: http.StatusOK, wantIP: "198.51.100.20",
		},
		{
			name: "multiple values rejected", node: trusted, remoteAddr: "127.0.0.1:40003",
			headers: []string{"8.8.8.8", "1.1.1.1"}, wantStatus: http.StatusBadRequest,
		},
		{
			name: "comma list rejected", node: trusted, remoteAddr: "127.0.0.1:40004",
			headers: []string{"8.8.8.8, 1.1.1.1"}, wantStatus: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := test.node.limitRequests(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = writer.Write([]byte(clientIPFromContext(request.Context())))
			}))
			request := httptest.NewRequest(http.MethodGet, "/v2/status", nil)
			request.RemoteAddr = test.remoteAddr
			for _, value := range test.headers {
				request.Header.Add(entcoinClientIPHeader, value)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("HTTP status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if test.wantStatus == http.StatusOK && strings.TrimSpace(recorder.Body.String()) != test.wantIP {
				t.Fatalf("observed client IP = %q, want %q", recorder.Body.String(), test.wantIP)
			}
		})
	}
}

func TestFortyEightCandidatesRespectActiveAndOutboundLimits(t *testing.T) {
	servers := make([]*publicNetworkPeerServer, 0, 3)
	for range 3 {
		servers = append(servers, newPublicNetworkPeerServer(t))
	}

	config := testConfig(t.TempDir())
	config.MaxOutboundPeers = 3
	node := newPublicNetworkTestNode(t, config)
	now := time.Now()
	node.mu.Lock()
	for _, server := range servers {
		node.peers[server.URL()] = &peerState{
			URL: server.URL(), Online: true, LastSeen: now,
		}
	}
	for index := 0; len(node.peers) < 48; index++ {
		peer := fmt.Sprintf("http://127.0.0.1:%d", 40000+index)
		node.peers[peer] = &peerState{URL: peer}
	}
	node.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startTestNode(t, ctx, node)
	waitFor(t, 5*time.Second, func() bool {
		node.mu.RLock()
		defer node.mu.RUnlock()
		return len(node.activeOutbound) == config.MaxOutboundPeers &&
			len(node.outboundSockets) == config.MaxOutboundPeers
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		node.mu.RLock()
		candidateCount := len(node.peers)
		activeCount := len(node.activeOutbound)
		outboundCount := len(node.outboundSockets)
		dialingCount := len(node.dialing)
		node.mu.RUnlock()
		if candidateCount != 48 {
			t.Fatalf("candidate count = %d, want 48", candidateCount)
		}
		if activeCount > config.MaxOutboundPeers {
			t.Fatalf("active outbound count = %d, limit %d", activeCount, config.MaxOutboundPeers)
		}
		if outboundCount+dialingCount > config.MaxOutboundPeers {
			t.Fatalf("connected+dialing outbound count = %d, limit %d", outboundCount+dialingCount, config.MaxOutboundPeers)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHTTPOnlyPeerStillSyncsAndBecomesOnline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	remote := newTestNode(t)
	startTestNode(t, ctx, remote)
	minePublicNetworkTestBlock(t, remote)

	target, err := url.Parse("http://" + remote.ActualAddress())
	if err != nil {
		t.Fatal(err)
	}
	reverseProxy := httputil.NewSingleHostReverseProxy(target)
	var websocketAttempts int
	var attemptsMu sync.Mutex
	httpOnly := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v2/p2p" {
			attemptsMu.Lock()
			websocketAttempts++
			attemptsMu.Unlock()
			http.NotFound(writer, request)
			return
		}
		reverseProxy.ServeHTTP(writer, request)
	}))
	defer httpOnly.Close()

	local := newTestNode(t)
	startTestNode(t, ctx, local)
	if err := local.AddPeer(httpOnly.URL); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, func() bool {
		tip, tipErr := local.ledger.Tip(context.Background())
		local.mu.RLock()
		state := local.peers[httpOnly.URL]
		online := state != nil && state.Online && state.Failures == 0
		local.mu.RUnlock()
		return tipErr == nil && tip.Height >= 1 && online
	})
	attemptsMu.Lock()
	attempts := websocketAttempts
	attemptsMu.Unlock()
	if attempts == 0 {
		t.Fatal("HTTP-only peer was never probed for WebSocket compatibility")
	}
}

func TestOutboundOnlyNodeReconcilesOfflineTransactionBacklogToSeed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	seed := newTestNode(t)
	outboundOnly := newTestNode(t)

	state := core.NewState()
	funding, err := state.Mine(ctx, outboundOnly.Address())
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range []*Service{seed, outboundOnly} {
		if err := service.ledger.ImportState(ctx, state); err != nil {
			t.Fatal(err)
		}
		makeFundingOrdinaryForPublicNetworkTest(t, ctx, service, funding.Transactions[0].ID)
	}
	transaction, err := outboundOnly.Send(seed.Address(), "0.02", "0.001")
	if err != nil {
		t.Fatal(err)
	}
	if count, err := seed.ledger.MempoolCount(ctx); err != nil || count != 0 {
		t.Fatalf("seed pending count before reconnect = %d, err %v", count, err)
	}

	startTestNode(t, ctx, seed)
	startTestNode(t, ctx, outboundOnly)
	callbackAttempts := advertiseUnusableCallback(t, outboundOnly)
	if err := outboundOnly.AddPeer("http://" + seed.ActualAddress()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, func() bool {
		transactions, queryErr := seed.ledger.MempoolTransactions(context.Background(), 8)
		return queryErr == nil && len(transactions) == 1 && transactions[0].ID == transaction.ID
	})
	if attempts := callbackAttempts.Load(); attempts != 0 {
		t.Fatalf("seed attempted %d NAT callbacks despite active reverse reconciliation", attempts)
	}
	seed.mu.RLock()
	callbackSockets := len(seed.outboundSockets)
	seed.mu.RUnlock()
	if callbackSockets != 0 {
		t.Fatalf("seed established %d callback sockets to the outbound-only node", callbackSockets)
	}
}

func TestOutboundOnlyNodeReconcilesOfflineStrongerForkToSeed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	seed := newTestNode(t)
	outboundOnly := newTestNode(t)

	common := core.NewState()
	if _, err := common.Mine(ctx, seed.Address()); err != nil {
		t.Fatal(err)
	}
	for _, service := range []*Service{seed, outboundOnly} {
		if err := service.ledger.ImportState(ctx, common); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := seed.MineOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := outboundOnly.MineOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := outboundOnly.MineOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := outboundOnly.MineOnce(ctx); err != nil {
		t.Fatal(err)
	}
	want, err := outboundOnly.ledger.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}

	startTestNode(t, ctx, seed)
	startTestNode(t, ctx, outboundOnly)
	callbackAttempts := advertiseUnusableCallback(t, outboundOnly)
	if err := outboundOnly.AddPeer("http://" + seed.ActualAddress()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, func() bool {
		got, tipErr := seed.ledger.Tip(context.Background())
		return tipErr == nil && got.Height == want.Height && got.Hash == want.Hash && got.Work.Cmp(want.Work) == 0
	})
	if attempts := callbackAttempts.Load(); attempts != 0 {
		t.Fatalf("seed attempted %d NAT callbacks despite active reverse reconciliation", attempts)
	}
	seed.mu.RLock()
	callbackSockets := len(seed.outboundSockets)
	seed.mu.RUnlock()
	if callbackSockets != 0 {
		t.Fatalf("seed established %d callback sockets to the outbound-only node", callbackSockets)
	}
}

func TestWebSocketReconcileRequestsAreNegotiatedAndBounded(t *testing.T) {
	service := newTestNode(t)
	socket := &peerSocket{
		service: service, send: make(chan queuedGossip, 16), done: make(chan struct{}), helloSeen: true,
	}
	keep, _, err := service.handleGossip(context.Background(), socket, gossipMessage{
		Type: "reconcile_mempool_request", Protocol: ledger.ProtocolName,
		RequestID: "1", MempoolRequest: &mempoolRequest{Limit: 1},
	})
	if keep || err == nil || !strings.Contains(err.Error(), "before negotiation") {
		t.Fatalf("unnegotiated reconcile request = keep %v, err %v", keep, err)
	}
	socket.stateMu.Lock()
	socket.reconcile = true
	socket.stateMu.Unlock()

	hashes := make([]string, maxSocketBlockBatch+1)
	for index := range hashes {
		hashes[index] = fmt.Sprintf("%064x", index+1)
	}
	keep, _, err = service.handleGossip(context.Background(), socket, gossipMessage{
		Type: "reconcile_blocks_request", Protocol: ledger.ProtocolName,
		RequestID: "2", BlocksRequest: &blocksRequest{Hashes: hashes},
	})
	if keep || err == nil || !strings.Contains(err.Error(), "block request is invalid") {
		t.Fatalf("oversized reconcile block request = keep %v, err %v", keep, err)
	}
	keep, _, err = service.handleGossip(context.Background(), socket, gossipMessage{
		Type: "reconcile_mempool_request", Protocol: ledger.ProtocolName,
		RequestID: "3", MempoolRequest: &mempoolRequest{Limit: maxSocketMempoolBatch + 1},
	})
	if keep || err == nil || !strings.Contains(err.Error(), "mempool request is invalid") {
		t.Fatalf("oversized reconcile mempool request = keep %v, err %v", keep, err)
	}
	if maxSocketMempoolBatch*maxSocketMempoolPagesPerRound > maxMempoolSyncValidations {
		t.Fatal("one socket reconcile round can exceed the HTTP mempool validation budget")
	}
	if maxSocketBlockBatch >= maxBlockDownloadBatch {
		t.Fatalf("socket block batch %d must stay below internal download batch %d", maxSocketBlockBatch, maxBlockDownloadBatch)
	}
	if webSocketInboundByteBurst < float64(maxBlockDownloadBatch)*float64(maxProtocolBytes) {
		t.Fatal("socket byte burst cannot carry one full internal block download batch")
	}
	budget := newSocketInboundBudget(time.Unix(1, 0))
	for part := 0; part < maxBlockDownloadBatch; part++ {
		if !budget.allow(time.Unix(1, 0), int(maxProtocolBytes), 0) {
			t.Fatalf("legal maximum block frame %d exceeded the socket byte burst", part+1)
		}
	}
	if budget.allow(time.Unix(1, 0), 1, 0) {
		t.Fatal("socket byte burst accepted data beyond one full internal block download batch")
	}
	headerCost := gossipValidationCost(gossipMessage{
		Type:           "reconcile_headers_request",
		HeadersRequest: &headersRequest{Limit: maxHeaderBatch, Locator: []string{core.GenesisBlock().Hash}},
	})
	minimumHeaderCost := maxHeaderBatch / 8
	if headerCost < minimumHeaderCost {
		t.Fatalf("maximum header response cost = %d, want at least %d", headerCost, minimumHeaderCost)
	}
	headerPages := (maxHeadersPerSync + maxHeaderBatch - 1) / maxHeaderBatch
	if headerCost*headerPages > int(webSocketValidationBurst) {
		t.Fatalf("one legal header sync costs %d tokens, burst is %.0f", headerCost*headerPages, webSocketValidationBurst)
	}
	if _, closePeer := reconcileFailureDisposition(invalidReconcileResponse("invalid block"), 1); !closePeer {
		t.Fatal("invalid reconcile data did not close the peer")
	}
	if delay, closePeer := reconcileFailureDisposition(&remoteReconcileError{
		code: reconcileErrorBusy, message: "busy",
	}, 2); closePeer || delay <= socketReconcileMinimumInterval {
		t.Fatalf("busy reconcile disposition = delay %s, close %v", delay, closePeer)
	}
}

func TestWebSocketSendQueueHasStrictByteBudgetAndDrains(t *testing.T) {
	socket := &peerSocket{send: make(chan queuedGossip, 128), done: make(chan struct{})}
	payload := strings.Repeat("x", int(maxProtocolBytes/2))
	message := gossipMessage{
		Type: "reconcile_error", Protocol: ledger.ProtocolName,
		RequestID: "1", ReconcileError: payload,
	}
	queued := 0
	for socket.queue(message) {
		queued++
		if queued > 8 {
			t.Fatal("socket queue ignored its byte budget")
		}
	}
	if queued < 2 {
		t.Fatalf("socket queue accepted only %d bounded messages", queued)
	}
	socket.sendMu.Lock()
	queuedBytes := socket.queuedBytes
	socket.sendMu.Unlock()
	if queuedBytes <= 0 || queuedBytes > maxSocketQueuedBytes {
		t.Fatalf("queued socket bytes = %d, limit %d", queuedBytes, maxSocketQueuedBytes)
	}
	if len(socket.send) != queued {
		t.Fatalf("queued socket messages = %d, want %d", len(socket.send), queued)
	}
	socket.disableSendQueue()
	socket.sendMu.Lock()
	defer socket.sendMu.Unlock()
	if !socket.sendClosed || socket.queuedBytes != 0 || len(socket.send) != 0 {
		t.Fatalf("disabled socket queue = closed %v, bytes %d, messages %d", socket.sendClosed, socket.queuedBytes, len(socket.send))
	}
}

func advertiseUnusableCallback(t *testing.T, service *Service) *atomic.Int32 {
	t.Helper()
	attempts := &atomic.Int32{}
	callback := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		writeError(writer, http.StatusServiceUnavailable, fmt.Errorf("callback is unavailable"))
	}))
	t.Cleanup(callback.Close)
	parsed, err := url.Parse(callback.URL)
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.actualAddress = parsed.Host
	service.mu.Unlock()
	return attempts
}

func makeFundingOrdinaryForPublicNetworkTest(t *testing.T, ctx context.Context, service *Service, txID string) {
	t.Helper()
	database, err := sql.Open("sqlite", service.ledger.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	result, err := database.ExecContext(ctx, "UPDATE utxos SET coinbase = 0 WHERE tx_id = ?", txID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		t.Fatalf("updated relay funding rows = %d, err %v", rows, err)
	}
}

func newPublicNetworkTestNode(t *testing.T, config Config) *Service {
	t.Helper()
	node, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestNode(t, node) })
	return node
}

func minePublicNetworkTestBlock(t *testing.T, node *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := node.MineOnce(ctx); err != nil {
		t.Fatal(err)
	}
}

type publicNetworkPeerServer struct {
	server        *httptest.Server
	status        protocolStatus
	nodeID        string
	connectionsMu sync.Mutex
	connections   map[*websocket.Conn]struct{}
}

func newPublicNetworkPeerServer(t *testing.T) *publicNetworkPeerServer {
	t.Helper()
	wallet, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	genesis := core.GenesisBlock()
	peer := &publicNetworkPeerServer{
		status: protocolStatus{
			Protocol:  ledger.ProtocolName,
			Name:      core.ChainName,
			Symbol:    core.ChainSymbol,
			Height:    0,
			TipHash:   genesis.Hash,
			ChainWork: "0",
		},
		nodeID:      wallet.Address,
		connections: make(map[*websocket.Conn]struct{}),
	}
	peer.server = httptest.NewServer(http.HandlerFunc(peer.handle))
	t.Cleanup(peer.close)
	return peer
}

func (p *publicNetworkPeerServer) URL() string {
	return p.server.URL
}

func (p *publicNetworkPeerServer) handle(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/v2/p2p":
		connection, err := peerUpgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		p.connectionsMu.Lock()
		p.connections[connection] = struct{}{}
		p.connectionsMu.Unlock()
		defer func() {
			p.connectionsMu.Lock()
			delete(p.connections, connection)
			p.connectionsMu.Unlock()
			_ = connection.Close()
		}()
		if err := connection.WriteJSON(gossipMessage{
			Type: "hello", Protocol: ledger.ProtocolName, NodeID: p.nodeID, Status: &p.status,
		}); err != nil {
			return
		}
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	case "/v2/status":
		writeJSON(writer, http.StatusOK, p.status)
	case "/v2/mempool":
		writeJSON(writer, http.StatusOK, transactionsResponse{Protocol: ledger.ProtocolName})
	default:
		http.NotFound(writer, request)
	}
}

func (p *publicNetworkPeerServer) close() {
	p.connectionsMu.Lock()
	connections := make([]*websocket.Conn, 0, len(p.connections))
	for connection := range p.connections {
		connections = append(connections, connection)
	}
	p.connectionsMu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
	p.server.Close()
}
