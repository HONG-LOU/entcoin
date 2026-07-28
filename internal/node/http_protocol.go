package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"
)

const (
	maxHeadersPerSync             = 20_000
	maxChainSyncChunksPerRound    = 32
	maxMempoolSyncValidations     = 64
	maxMempoolResponseBytes       = int64(maxMempoolSyncValidations)*maxTransactionMessageBytes + 64<<10
	maxBlocksResponseBytes        = int64(maxBlockDownloadBatch)*maxBlockBodyBytes + 64<<10
	maxWalletResponseBytes        = int64(4 << 20)
	maxWalletUTXOs                = core.MaxTransactionInputs
	maxWalletHistory              = 50
	httpRequestsPerIPPerSecond    = float64(128)
	httpRequestsPerIPBurst        = float64(256)
	httpRequestRateStateRetention = 10 * time.Minute
	websocketHandshakesPerSecond  = float64(2)
	websocketHandshakeBurst       = float64(4)
	peerSyncRoundTimeout          = 2 * time.Minute
)

type requestRateState struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

func browserOriginAllowed(origin string) bool {
	if origin == "https://entcoin.xyz" || origin == "https://www.entcoin.xyz" || origin == "https://wallet.entcoin.xyz" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (s *Service) browserAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(writer, request)
			return
		}
		if !browserOriginAllowed(origin) {
			writeError(writer, http.StatusForbidden, fmt.Errorf("browser origin is not allowed"))
			return
		}
		writer.Header().Set("Access-Control-Allow-Origin", origin)
		writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		writer.Header().Set("Access-Control-Max-Age", "600")
		writer.Header().Set("Vary", "Origin")
		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

type resyncPause struct {
	tipHash string
	until   time.Time
}

const resyncCandidateBackoff = 30 * time.Minute

const entcoinClientIPHeader = "X-Entcoin-Client-IP"

type clientIPContextKey struct{}

func (r *requestRateState) allow(now time.Time) bool {
	return r.allowAtRate(now, httpRequestsPerIPPerSecond, httpRequestsPerIPBurst)
}

func (r *requestRateState) allowAtRate(now time.Time, rate, burst float64) bool {
	if r.updated.IsZero() {
		r.tokens = burst
		r.updated = now
	}
	elapsed := now.Sub(r.updated).Seconds()
	if elapsed > 0 {
		r.tokens = min(burst, r.tokens+elapsed*rate)
		r.updated = now
	}
	r.lastSeen = now
	if r.tokens < 1 {
		return false
	}
	r.tokens--
	return true
}

func (s *Service) registerProtocolHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /v2/status", s.handleStatus)
	mux.HandleFunc("GET /v2/consensus", s.handleConsensus)
	mux.HandleFunc("POST /v2/headers", s.handleHeaders)
	mux.HandleFunc("POST /v2/blocks", s.handleBlocks)
	mux.HandleFunc("GET /v2/mempool", s.handleMempool)
	mux.HandleFunc("GET /v2/peers", s.handlePeers)
	mux.HandleFunc("GET /v2/wallet/{address}", s.handleWallet)
	mux.HandleFunc("POST /v2/transactions", s.handleTransaction)
	mux.HandleFunc("POST /v2/block", s.handleBlock)
	mux.HandleFunc("GET /v2/p2p", s.handleWebSocket)
}

func (s *Service) handleWallet(writer http.ResponseWriter, request *http.Request) {
	if !s.acquireHeavyRequest(request.Context()) {
		writeError(writer, http.StatusServiceUnavailable, fmt.Errorf("node is busy serving wallet data"))
		return
	}
	defer s.releaseHeavyRequest()
	address := request.PathValue("address")
	snapshot, err := s.ledger.ReadWalletSnapshot(request.Context(), address, maxWalletUTXOs, maxWalletHistory)
	if err != nil {
		status := http.StatusInternalServerError
		if core.ValidateAddress(address) != nil {
			status = http.StatusBadRequest
		}
		writeError(writer, status, err)
		return
	}
	utxos := make([]walletUTXO, 0, len(snapshot.UTXO))
	for outpoint, output := range snapshot.UTXO {
		utxos = append(utxos, walletUTXO{TxID: outpoint.TxID, OutputIndex: outpoint.Index, Amount: output.Amount, Address: output.Address})
	}
	sort.Slice(utxos, func(i, j int) bool {
		if utxos[i].TxID == utxos[j].TxID {
			return utxos[i].OutputIndex < utxos[j].OutputIndex
		}
		return utxos[i].TxID < utxos[j].TxID
	})
	writeBoundedJSON(writer, http.StatusOK, walletResponse{
		Protocol: ledger.ProtocolName, Height: snapshot.Tip.Height, TipHash: snapshot.Tip.Hash,
		ChainWork: snapshot.Tip.Work.String(), ConfirmedBalance: snapshot.ConfirmedBalance,
		SpendableBalance: snapshot.SpendableBalance, UTXOs: utxos,
		UTXOsTruncated: snapshot.UTXOTruncated, History: snapshot.History,
	}, maxWalletResponseBytes)
}

func (s *Service) handleStatus(writer http.ResponseWriter, request *http.Request) {
	tip, err := s.ledger.Tip(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, http.StatusOK, statusFromTip(tip, s.listenPort()))
}

func (s *Service) handleConsensus(writer http.ResponseWriter, request *http.Request) {
	tip, err := s.ledger.Tip(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, http.StatusOK, consensusStatus{
		Protocol:         ledger.ProtocolName,
		SupportedRules:   []uint32{core.LegacyBlockVersion, core.UpgradedBlockVersion},
		NextBlockRule:    core.BlockVersion(tip.Height + 1),
		ActivationHeight: core.ConsensusUpgradeHeight,
		AnchorHeight:     core.ASERTAnchorHeight,
		AnchorHash:       core.ASERTAnchorHash,
		Algorithm:        "integer-asert",
		HalfLifeSeconds:  core.ASERTHalfLifeSeconds,
	})
}

func (s *Service) handleHeaders(writer http.ResponseWriter, request *http.Request) {
	var query headersRequest
	if err := decodeLimitedJSON(request.Body, 16<<10, &query); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if query.Limit <= 0 || query.Limit > maxHeaderBatch {
		writeError(writer, http.StatusBadRequest, fmt.Errorf("header limit must be between 1 and %d", maxHeaderBatch))
		return
	}
	height, hash, err := s.ledger.FindLocator(request.Context(), query.Locator)
	if err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err)
		return
	}
	headers, err := s.ledger.HeadersFrom(request.Context(), height+1, query.Limit)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, http.StatusOK, headersResponse{
		Protocol:     ledger.ProtocolName,
		CommonHeight: height,
		CommonHash:   hash,
		Headers:      headers,
	})
}

func (s *Service) handleBlocks(writer http.ResponseWriter, request *http.Request) {
	if !s.acquireHeavyRequest(request.Context()) {
		writeError(writer, http.StatusServiceUnavailable, fmt.Errorf("node is busy serving block data"))
		return
	}
	defer s.releaseHeavyRequest()
	var query blocksRequest
	if err := decodeLimitedJSON(request.Body, 16<<10, &query); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if len(query.Hashes) == 0 || len(query.Hashes) > maxBlockBatch {
		writeError(writer, http.StatusBadRequest, fmt.Errorf("block request must contain between 1 and %d hashes", maxBlockBatch))
		return
	}
	blocks := make([]core.Block, 0, len(query.Hashes))
	seen := make(map[string]struct{}, len(query.Hashes))
	for _, hash := range query.Hashes {
		if _, exists := seen[hash]; exists {
			writeError(writer, http.StatusBadRequest, fmt.Errorf("block request contains duplicate hash %s", hash))
			return
		}
		seen[hash] = struct{}{}
		block, err := s.ledger.BlockByHash(request.Context(), hash)
		if err != nil {
			status := http.StatusNotFound
			if errors.Is(err, ledger.ErrBlockPruned) {
				status = http.StatusGone
			}
			writeError(writer, status, err)
			return
		}
		blocks = append(blocks, block)
	}
	writeBoundedJSON(writer, http.StatusOK, blocksResponse{Protocol: ledger.ProtocolName, Blocks: blocks}, maxBlocksResponseBytes)
}

func (s *Service) handleMempool(writer http.ResponseWriter, request *http.Request) {
	if !s.acquireHeavyRequest(request.Context()) {
		writeError(writer, http.StatusServiceUnavailable, fmt.Errorf("node is busy serving mempool data"))
		return
	}
	defer s.releaseHeavyRequest()
	limit := maxMempoolSyncValidations
	offset := 0
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > maxMempoolSyncValidations {
			writeError(writer, http.StatusBadRequest, fmt.Errorf("mempool limit must be between 1 and %d", maxMempoolSyncValidations))
			return
		}
		limit = parsed
	}
	if raw := request.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > core.MaxPendingTransactions {
			writeError(writer, http.StatusBadRequest, fmt.Errorf("mempool offset must be between 0 and %d", core.MaxPendingTransactions))
			return
		}
		offset = parsed
	}
	transactions, err := s.ledger.MempoolTransactionsPage(request.Context(), limit, offset)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	writeBoundedJSON(writer, http.StatusOK, transactionsResponse{Protocol: ledger.ProtocolName, Transactions: transactions}, maxMempoolResponseBytes)
}

func (s *Service) acquireHeavyRequest(ctx context.Context) bool {
	select {
	case s.heavyRequestSlots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	default:
		return false
	}
}

func (s *Service) releaseHeavyRequest() {
	<-s.heavyRequestSlots
}

func (s *Service) handleTransaction(writer http.ResponseWriter, request *http.Request) {
	var transaction core.Transaction
	if err := decodeLimitedJSON(request.Body, maxTransactionMessageBytes, &transaction); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if err := s.acceptTransaction(request.Context(), transaction, nil); err != nil {
		writeError(writer, http.StatusConflict, err)
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]string{"transaction_id": transaction.ID})
}

func (s *Service) handleBlock(writer http.ResponseWriter, request *http.Request) {
	var block core.Block
	if err := decodeLimitedJSON(request.Body, maxBlockBodyBytes, &block); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if err := s.acceptBlock(request.Context(), block, nil); err != nil {
		writeError(writer, http.StatusConflict, err)
		return
	}
	writeJSON(writer, http.StatusAccepted, map[string]string{"block_hash": block.Hash})
}

func (s *Service) acceptTransaction(ctx context.Context, transaction core.Transaction, source *peerSocket) error {
	if err := s.ledger.AddTransaction(ctx, transaction); err != nil {
		return err
	}
	s.broadcastTransaction(transaction, source)
	return nil
}

func (s *Service) acceptBlock(ctx context.Context, block core.Block, source *peerSocket) error {
	if err := s.ledger.ConnectBlock(ctx, block); err != nil {
		return err
	}
	s.notifyTipChanged()
	s.maybePrune(s.ledger)
	s.broadcastBlock(block, source)
	return nil
}

func (s *Service) syncPeer(peer string) {
	s.syncPeerMode(peer, true)
}

func (s *Service) syncPeerScheduled(peer string) {
	s.syncPeerMode(peer, false)
}

func (s *Service) syncPeerMode(peer string, force bool) {
	if !s.beginPeerSyncMode(peer, force) {
		return
	}
	defer s.endPeerSync(peer)
	ctx, cancel := context.WithTimeout(s.backgroundContext(), peerSyncRoundTimeout)
	defer cancel()
	var remote protocolStatus
	if err := s.getJSON(ctx, peer+"/v2/status", 64<<10, &remote); err != nil {
		s.markPeerFailure(peer, err)
		return
	}
	if err := validateRemoteStatus(remote); err != nil {
		s.markPeerFailure(peer, err)
		return
	}
	s.markPeerSuccess(peer, remote.Height)
	defer s.sendStatusToOutbound(peer)
	s.launch(func() {
		exchangeContext, exchangeCancel := context.WithTimeout(s.backgroundContext(), 3*time.Second)
		defer exchangeCancel()
		s.exchangePeerAddresses(exchangeContext, peer)
	})
	localTip, err := s.ledger.Tip(ctx)
	if err != nil {
		s.setError(err)
		return
	}
	if localTip.Hash != remote.TipHash {
		if s.chainSyncPaused(peer, remote.TipHash, time.Now()) {
			if err := s.syncRemoteMempool(ctx, peer); err != nil {
				s.markPeerFailure(peer, err)
			}
			return
		}
		for chunk := 0; chunk < maxChainSyncChunksPerRound && localTip.Hash != remote.TipHash; chunk++ {
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 5*time.Second {
				break
			}
			before := localTip.Hash
			if err := s.syncRemoteChain(ctx, peer, localTip); err != nil {
				if errors.Is(err, ledger.ErrReorgBeyondPrune) {
					s.pauseChainSyncForResync(peer, remote.TipHash, err)
					return
				}
				s.markPeerFailure(peer, err)
				return
			}
			localTip, err = s.ledger.Tip(ctx)
			if err != nil {
				s.setError(err)
				return
			}
			if localTip.Hash == before {
				break
			}
		}
	}
	if err := s.syncRemoteMempool(ctx, peer); err != nil {
		s.markPeerFailure(peer, err)
	}
}

func (s *Service) chainSyncPaused(peer, tipHash string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	pause, exists := s.resyncPauses[peer]
	if !exists {
		return false
	}
	if pause.tipHash != tipHash || !now.Before(pause.until) {
		delete(s.resyncPauses, peer)
		s.resyncRequired = len(s.resyncPauses) > 0
		return false
	}
	return true
}

func (s *Service) pauseChainSyncForResync(peer, tipHash string, err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	_, existed := s.resyncPauses[peer]
	s.resyncPauses[peer] = resyncPause{tipHash: tipHash, until: time.Now().Add(resyncCandidateBackoff)}
	s.resyncRequired = true
	s.lastError = err.Error()
	s.mu.Unlock()
	if !existed {
		s.recordHealth("resync_required", "critical", err.Error(), "Resync from an archive peer or rebuild the chain database")
	}
}

func (s *Service) backgroundContext() context.Context {
	s.mu.RLock()
	ctx := s.rootContext
	s.mu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func validateRemoteStatus(status protocolStatus) error {
	if status.Protocol != ledger.ProtocolName || status.Name != core.ChainName || status.Symbol != core.ChainSymbol {
		return fmt.Errorf("peer uses an incompatible Entcoin protocol")
	}
	decodedHash, err := hex.DecodeString(status.TipHash)
	if err != nil || len(decodedHash) != 32 {
		return fmt.Errorf("peer status has an invalid tip hash")
	}
	if len(status.ChainWork) == 0 || len(status.ChainWork) > 128 {
		return fmt.Errorf("peer status contains malformed chain work")
	}
	work, ok := new(big.Int).SetString(status.ChainWork, 10)
	if !ok || work.Sign() < 0 {
		return fmt.Errorf("peer status contains malformed chain work")
	}
	return nil
}

type remoteChainSource interface {
	remoteBlockSource
	requestHeaders(context.Context, headersRequest) (headersResponse, error)
}

func (s *Service) syncRemoteChain(ctx context.Context, peer string, localTip ledger.Tip) error {
	return s.syncRemoteChainFrom(ctx, httpRemoteSource{service: s, peer: peer}, localTip)
}

func (s *Service) syncRemoteChainFrom(ctx context.Context, source remoteChainSource, localTip ledger.Tip) error {
	return s.syncRemoteChainFromAtTime(ctx, source, localTip, time.Now().Unix())
}

func (s *Service) syncRemoteChainFromAtTime(
	ctx context.Context,
	source remoteChainSource,
	localTip ledger.Tip,
	validationTime int64,
) error {
	select {
	case s.chainSyncSlot <- struct{}{}:
		defer func() { <-s.chainSyncSlot }()
	case <-ctx.Done():
		return ctx.Err()
	}
	locator, err := s.ledger.BlockLocator(ctx)
	if err != nil {
		return err
	}
	localWork := new(big.Int).Set(localTip.Work)
	var (
		ancestorHeight    uint64
		effectiveAncestor uint64
		candidateWork     *big.Int
		priorHeaders      []core.Block
		previous          core.Block
		headers           []core.Block
		candidateHeaders  []core.Block
		diverged          bool
	)
	for len(headers) < maxHeadersPerSync {
		response, err := source.requestHeaders(ctx, headersRequest{Locator: locator, Limit: maxSyncHeaderBatch})
		if err != nil {
			return err
		}
		if response.Protocol != ledger.ProtocolName {
			return fmt.Errorf("peer returned incompatible headers")
		}
		if len(response.Headers) > maxSyncHeaderBatch {
			return fmt.Errorf("peer exceeded the header page limit")
		}
		if len(headers)+len(response.Headers) > maxHeadersPerSync {
			return fmt.Errorf("peer exceeded the total header sync limit")
		}
		if len(headers) == 0 {
			ancestorHeight = response.CommonHeight
			effectiveAncestor = ancestorHeight
			localHash, err := s.ledger.HashAt(ctx, ancestorHeight)
			if err != nil || localHash != response.CommonHash {
				return fmt.Errorf("peer selected a locator that is not on the local active chain")
			}
			candidateWork, err = s.ledger.WorkAt(ctx, ancestorHeight)
			if err != nil {
				return err
			}
			priorHeaders, err = s.ledger.HeaderWindow(ctx, ancestorHeight)
			if err != nil {
				return err
			}
			previous = priorHeaders[len(priorHeaders)-1]
		} else if response.CommonHeight != previous.Height || response.CommonHash != previous.Hash {
			return fmt.Errorf("peer changed chains while paginating headers")
		}
		if len(response.Headers) == 0 {
			break
		}
		for _, header := range response.Headers {
			if len(header.Transactions) != 0 {
				return fmt.Errorf("peer included block bodies in a header response")
			}
			if err := core.ValidateHeaderAtTime(header, previous, priorHeaders, validationTime); err != nil {
				return fmt.Errorf("validate peer header %d: %w", header.Height, err)
			}
			headers = append(headers, header)
			candidateWork.Add(candidateWork, new(big.Int).Lsh(big.NewInt(1), uint(header.Difficulty)))
			if !diverged && header.Height <= localTip.Height {
				localHash, err := s.ledger.HashAt(ctx, header.Height)
				if err != nil {
					return fmt.Errorf("compare peer header %d with active chain: %w", header.Height, err)
				}
				if localHash == header.Hash {
					effectiveAncestor = header.Height
				} else {
					diverged = true
					candidateHeaders = append(candidateHeaders, header)
				}
			} else {
				diverged = true
				candidateHeaders = append(candidateHeaders, header)
			}
			priorHeaders = append(priorHeaders, header)
			if len(priorHeaders) > core.FirstAdjustment {
				priorHeaders = priorHeaders[len(priorHeaders)-core.FirstAdjustment:]
			}
			previous = header
		}
		if candidateWork.Cmp(localWork) > 0 {
			if len(candidateHeaders) == 0 {
				return fmt.Errorf("peer claimed excess work without a candidate suffix")
			}
			prunedThrough, err := s.ledger.PrunedThrough(ctx)
			if err != nil {
				return err
			}
			if effectiveAncestor < prunedThrough {
				return fmt.Errorf("%w: ancestor %d is below retained height %d", ledger.ErrReorgBeyondPrune, effectiveAncestor, prunedThrough)
			}
			if effectiveAncestor == localTip.Height && len(candidateHeaders) > maxDirectExtensionBlocks {
				candidateHeaders = candidateHeaders[:maxDirectExtensionBlocks]
			}
			staged, err := s.stageBlocksFromSource(ctx, source, candidateHeaders)
			if err != nil {
				return err
			}
			defer staged.Close()
			if err := s.ledger.ReplaceFromSourceAtTime(ctx, effectiveAncestor, len(candidateHeaders), staged.BlockAt, validationTime); err != nil {
				return err
			}
			s.notifyTipChanged()
			s.maybePrune(s.ledger)
			return nil
		}
		if len(response.Headers) < maxSyncHeaderBatch {
			break
		}
		locator = []string{previous.Hash}
	}
	return nil
}

func sameBlockHeader(block, header core.Block) bool {
	return block.Version == header.Version && block.Height == header.Height &&
		block.Timestamp == header.Timestamp && block.PreviousHash == header.PreviousHash &&
		block.MerkleRoot == header.MerkleRoot && block.Difficulty == header.Difficulty &&
		block.Nonce == header.Nonce && block.Hash == header.Hash
}

func (s *Service) syncRemoteMempool(ctx context.Context, peer string) error {
	s.mu.RLock()
	offset := s.mempoolOffsets[peer]
	s.mu.RUnlock()
	var response transactionsResponse
	endpoint := peer + "/v2/mempool?limit=" + strconv.Itoa(maxMempoolSyncValidations) + "&offset=" + strconv.Itoa(offset)
	if err := s.getJSON(ctx, endpoint, maxMempoolResponseBytes, &response); err != nil {
		return err
	}
	if response.Protocol != ledger.ProtocolName {
		return fmt.Errorf("peer returned an incompatible mempool response")
	}
	if len(response.Transactions) > maxMempoolSyncValidations {
		return fmt.Errorf("peer exceeded the mempool validation limit")
	}
	for _, transaction := range response.Transactions {
		if err := s.ledger.AddTransaction(ctx, transaction); err != nil {
			if errors.Is(err, ledger.ErrTransactionAlreadyKnown) {
				continue
			}
			return fmt.Errorf("peer returned an invalid mempool batch: %w", err)
		}
		s.broadcastTransaction(transaction, nil)
	}
	s.mu.Lock()
	if s.peers[peer] != nil {
		nextOffset := offset + len(response.Transactions)
		if len(response.Transactions) == maxMempoolSyncValidations && nextOffset < core.MaxPendingTransactions {
			s.mempoolOffsets[peer] = nextOffset
		} else {
			s.mempoolOffsets[peer] = 0
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) getJSON(ctx context.Context, endpoint string, maximum int64, result any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("peer returned HTTP %d", response.StatusCode)
	}
	return decodeLimitedJSON(response.Body, maximum, result)
}

func (s *Service) postJSON(ctx context.Context, endpoint string, value any, maximum int64, result any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("peer returned HTTP %d", response.StatusCode)
	}
	return decodeLimitedJSON(response.Body, maximum, result)
}

func (s *Service) limitRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host, err := s.requestClientIP(request)
		if err != nil {
			writeError(writer, http.StatusBadRequest, fmt.Errorf("invalid remote address"))
			return
		}
		websocketRequest := request.URL.Path == "/v2/p2p"
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			writeError(writer, http.StatusServiceUnavailable, fmt.Errorf("node is shutting down"))
			return
		}
		now := time.Now()
		if !websocketRequest && !s.allowHTTPRequestLocked(host, now) {
			s.mu.Unlock()
			writeError(writer, http.StatusTooManyRequests, fmt.Errorf("peer request rate limit reached"))
			return
		}
		if websocketRequest && !s.allowWebSocketHandshakeLocked(host, now) {
			s.mu.Unlock()
			writeError(writer, http.StatusTooManyRequests, fmt.Errorf("peer websocket handshake rate limit reached"))
			return
		}
		counts := s.requestsByIP
		perIPLimit := 8
		slots := s.requestSlots
		if websocketRequest {
			counts = s.websocketsByIP
			perIPLimit = 4
			slots = s.websocketSlots
		}
		if counts[host] >= perIPLimit {
			s.mu.Unlock()
			writeError(writer, http.StatusTooManyRequests, fmt.Errorf("peer connection limit reached"))
			return
		}
		counts[host]++
		s.wait.Add(1)
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			counts[host]--
			if counts[host] == 0 {
				delete(counts, host)
			}
			s.mu.Unlock()
			s.wait.Done()
		}()
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			ctx := context.WithValue(request.Context(), clientIPContextKey{}, host)
			next.ServeHTTP(writer, request.WithContext(ctx))
		default:
			writeError(writer, http.StatusServiceUnavailable, fmt.Errorf("node is busy"))
		}
	})
}

func (s *Service) requestClientIP(request *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return "", err
	}
	remote, err := netip.ParseAddr(host)
	if err != nil {
		return "", err
	}
	remote = remote.Unmap()
	if !s.trustLoopbackProxy || !remote.IsLoopback() {
		return remote.String(), nil
	}
	values := request.Header.Values(entcoinClientIPHeader)
	if len(values) == 0 {
		return remote.String(), nil
	}
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return "", fmt.Errorf("proxy client IP header must contain one address")
	}
	client, err := netip.ParseAddr(strings.TrimSpace(values[0]))
	if err != nil || client.Zone() != "" {
		return "", fmt.Errorf("proxy client IP header is invalid")
	}
	return client.Unmap().String(), nil
}

func clientIPFromContext(ctx context.Context) string {
	host, _ := ctx.Value(clientIPContextKey{}).(string)
	return host
}

func (s *Service) allowWebSocketHandshakeLocked(host string, now time.Time) bool {
	state := s.websocketRatesByIP[host]
	if state == nil {
		if len(s.websocketRatesByIP) >= 4_096 {
			for peerIP, candidate := range s.websocketRatesByIP {
				if now.Sub(candidate.lastSeen) > httpRequestRateStateRetention {
					delete(s.websocketRatesByIP, peerIP)
				}
			}
			if len(s.websocketRatesByIP) >= 4_096 {
				return false
			}
		}
		state = &requestRateState{}
		s.websocketRatesByIP[host] = state
	}
	return state.allowAtRate(now, websocketHandshakesPerSecond, websocketHandshakeBurst)
}

func (s *Service) allowHTTPRequestLocked(host string, now time.Time) bool {
	state := s.requestRatesByIP[host]
	if state == nil {
		if len(s.requestRatesByIP) >= 4_096 {
			for peerIP, candidate := range s.requestRatesByIP {
				if now.Sub(candidate.lastSeen) > httpRequestRateStateRetention {
					delete(s.requestRatesByIP, peerIP)
				}
			}
			if len(s.requestRatesByIP) >= 4_096 {
				return false
			}
		}
		state = &requestRateState{}
		s.requestRatesByIP[host] = state
	}
	return state.allow(now)
}

func decodeLimitedJSON(reader io.Reader, limit int64, value any) error {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return fmt.Errorf("read JSON: %w", err)
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("JSON exceeds size limit")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("JSON contains trailing data")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 128 {
			return fmt.Errorf("JSON nesting exceeds limit")
		}
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("scan JSON: %w", err)
		}
		delimiter, nested := token.(json.Delim)
		if !nested {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return fmt.Errorf("scan JSON object key: %w", err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("JSON object key is not a string")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("JSON contains duplicate key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("JSON contains an invalid delimiter")
		}
		closing, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("scan JSON closing delimiter: %w", err)
		}
		if closeDelimiter, ok := closing.(json.Delim); !ok ||
			(delimiter == '{' && closeDelimiter != '}') || (delimiter == '[' && closeDelimiter != ']') {
			return fmt.Errorf("JSON delimiters do not match")
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("JSON contains trailing data")
		}
		return fmt.Errorf("scan trailing JSON: %w", err)
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeBoundedJSON(writer http.ResponseWriter, status int, value any, maximum int64) {
	data, err := json.Marshal(value)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err)
		return
	}
	if int64(len(data)) > maximum {
		writeError(writer, http.StatusRequestEntityTooLarge, fmt.Errorf("response exceeds %d-byte protocol limit", maximum))
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
	_, _ = writer.Write(data)
}

func writeError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]string{"error": err.Error()})
}
