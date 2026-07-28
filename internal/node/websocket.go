package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"

	"github.com/gorilla/websocket"
)

type peerSocket struct {
	service         *Service
	connection      *websocket.Conn
	baseURL         string
	outbound        bool
	send            chan queuedGossip
	done            chan struct{}
	closeOnce       sync.Once
	helloSeen       bool
	remoteIP        string
	expectedClose   atomic.Bool
	stateMu         sync.Mutex
	remoteStatus    *protocolStatus
	reconcile       bool
	reconciling     bool
	reconcileNext   time.Time
	reconcileErrors int
	reconcileLast   string
	mempoolOffset   int
	requestSeq      atomic.Uint64
	pending         map[string]*pendingSocketRequest
	sendMu          sync.Mutex
	queuedBytes     int64
	sendClosed      bool
}

type queuedGossip struct {
	data []byte
}

type pendingSocketRequest struct {
	expected  string
	responses chan gossipMessage
}

type remoteReconcileError struct {
	code    string
	message string
}

func (e *remoteReconcileError) Error() string {
	return "peer could not serve reconcile request: " + e.message
}

type invalidReconcileResponseError struct {
	err error
}

func (e *invalidReconcileResponseError) Error() string {
	return e.err.Error()
}

func (e *invalidReconcileResponseError) Unwrap() error {
	return e.err
}

const (
	webSocketInboundBytesPerSecond = float64(2 << 20)
	webSocketInboundByteBurst      = float64(maxBlockDownloadBatch) * float64(maxProtocolBytes)
	webSocketValidationPerSecond   = float64(512)
	webSocketValidationBurst       = float64(4_096)
	webSocketInvalidScoreLimit     = 12
	invalidTransactionScore        = 1
	invalidBlockScore              = 4
	invalidGossipScoreTTL          = 5 * time.Minute
	reconcileCapability            = "entropy-reconcile-v1"
	maxSocketPendingRequests       = 2
	maxSocketBlockBatch            = 2
	maxSocketMempoolBatch          = 8
	maxSocketMempoolPagesPerRound  = 8
	maxSocketReconcileErrorBytes   = 256
	socketReconcileRequestTimeout  = 20 * time.Second
	socketReconcileRoundTimeout    = 30 * time.Second
	socketReconcileMinimumInterval = 5 * time.Second
	maxSocketQueuedBytes           = 2*maxProtocolBytes + 64<<10
	socketReconcileMaximumBackoff  = 5 * time.Minute
	socketReconcilePrunedBackoff   = 30 * time.Minute
	reconcileErrorBusy             = "busy"
	reconcileErrorPruned           = "pruned"
	reconcileErrorNotFound         = "not_found"
	reconcileErrorTemporary        = "temporary"
	reconcileErrorInvalidRequest   = "invalid_request"
)

type invalidGossipState struct {
	score   int
	updated time.Time
}

type socketInboundBudget struct {
	last             time.Time
	byteTokens       float64
	validationTokens float64
}

func newSocketInboundBudget(now time.Time) *socketInboundBudget {
	return &socketInboundBudget{
		last:             now,
		byteTokens:       webSocketInboundByteBurst,
		validationTokens: webSocketValidationBurst,
	}
}

func (b *socketInboundBudget) allow(now time.Time, byteCost, validationCost int) bool {
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.byteTokens = min(webSocketInboundByteBurst, b.byteTokens+elapsed*webSocketInboundBytesPerSecond)
		b.validationTokens = min(webSocketValidationBurst, b.validationTokens+elapsed*webSocketValidationPerSecond)
		b.last = now
	}
	if float64(byteCost) > b.byteTokens || float64(validationCost) > b.validationTokens {
		return false
	}
	b.byteTokens -= float64(byteCost)
	b.validationTokens -= float64(validationCost)
	return true
}

var peerUpgrader = websocket.Upgrader{
	HandshakeTimeout: 5 * time.Second,
	ReadBufferSize:   16 << 10,
	WriteBufferSize:  16 << 10,
	CheckOrigin: func(request *http.Request) bool {
		return request.Header.Get("Origin") == ""
	},
}

func (s *Service) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	remoteIP := clientIPFromContext(request.Context())
	connection, err := peerUpgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	if remoteIP == "" {
		remoteIP = connectionRemoteIP(connection)
	}
	socket := &peerSocket{
		service:    s,
		connection: connection,
		send:       make(chan queuedGossip, 128),
		done:       make(chan struct{}),
		remoteIP:   remoteIP,
	}
	if !s.registerSocket(socket) {
		_ = connection.Close()
		return
	}
	socket.queue(s.helloMessage())
	socket.run(request.Context())
}

func connectionRemoteIP(connection *websocket.Conn) string {
	if connection == nil || connection.RemoteAddr() == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(connection.RemoteAddr().String())
	if err != nil {
		return ""
	}
	address := net.ParseIP(host)
	if address == nil {
		return ""
	}
	return address.String()
}

func (s *Service) ensurePeerSocket(peer string) {
	s.mu.Lock()
	state := s.peers[peer]
	now := time.Now()
	if s.closing || s.outboundSockets[peer] != nil || s.dialing[peer] || state == nil || !state.ActiveOutbound ||
		(!state.NextAttempt.IsZero() && now.Before(state.NextAttempt)) ||
		(!state.NextSocket.IsZero() && now.Before(state.NextSocket)) ||
		len(s.outboundSockets)+len(s.dialing) >= s.maxOutboundPeers {
		s.mu.Unlock()
		return
	}
	s.dialing[peer] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.dialing, peer)
		s.mu.Unlock()
	}()
	parsed, err := url.Parse(peer)
	if err != nil {
		return
	}
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	parsed.Path = "/v2/p2p"
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second, Proxy: http.ProxyFromEnvironment}
	connection, response, err := dialer.DialContext(s.backgroundContext(), parsed.String(), nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		s.markPeerSocketFailure(peer, fmt.Errorf("open realtime channel: %w", err))
		return
	}
	socket := &peerSocket{
		service:    s,
		connection: connection,
		baseURL:    peer,
		outbound:   true,
		send:       make(chan queuedGossip, 128),
		done:       make(chan struct{}),
		remoteIP:   connectionRemoteIP(connection),
	}
	if !s.registerSocket(socket) {
		_ = connection.Close()
		return
	}
	s.mu.Lock()
	if state := s.peers[peer]; state != nil {
		state.SocketFailures = 0
		state.NextSocket = time.Time{}
	}
	s.mu.Unlock()
	socket.queue(s.helloMessage())
	go socket.run(s.backgroundContext())
}

func (s *Service) registerSocket(socket *peerSocket) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || len(s.sockets) >= maxPeerConnections {
		return false
	}
	if socket.outbound {
		if s.peers[socket.baseURL] == nil {
			return false
		}
		if _, active := s.activeOutbound[socket.baseURL]; !active {
			return false
		}
		if existing := s.outboundSockets[socket.baseURL]; existing != nil {
			return false
		}
		delete(s.dialing, socket.baseURL)
		if len(s.outboundSockets)+len(s.dialing) >= s.maxOutboundPeers {
			return false
		}
		s.outboundSockets[socket.baseURL] = socket
	}
	s.sockets[socket] = struct{}{}
	s.wait.Add(1)
	return true
}

func (s *Service) unregisterSocket(socket *peerSocket) {
	s.mu.Lock()
	delete(s.sockets, socket)
	if socket.outbound && s.outboundSockets[socket.baseURL] == socket {
		delete(s.outboundSockets, socket.baseURL)
	}
	s.mu.Unlock()
}

func (s *Service) helloMessage() gossipMessage {
	tip, err := s.ledger.Tip(context.Background())
	if err != nil {
		return gossipMessage{Type: "hello", Protocol: ledger.ProtocolName, NodeID: s.Address()}
	}
	status := statusFromTip(tip, s.listenPort())
	return gossipMessage{
		Type:       "hello",
		Protocol:   ledger.ProtocolName,
		NodeID:     s.Address(),
		ListenPort: status.ListenPort,
		Status:     &status,
	}
}

func (s *Service) sendStatusToOutbound(peer string) {
	tip, err := s.ledger.Tip(s.backgroundContext())
	if err != nil {
		return
	}
	s.mu.RLock()
	socket := s.outboundSockets[peer]
	s.mu.RUnlock()
	if socket != nil {
		status := statusFromTip(tip, s.listenPort())
		_ = socket.queue(gossipMessage{Type: "status", Protocol: ledger.ProtocolName, Status: &status})
	}
}

func (s *Service) listenPort() int {
	s.mu.RLock()
	address := s.actualAddress
	s.mu.RUnlock()
	_, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return 0
	}
	return port
}

func (p *peerSocket) noteRemoteStatus(status protocolStatus) {
	p.stateMu.Lock()
	copy := status
	p.remoteStatus = &copy
	p.stateMu.Unlock()
}

func (p *peerSocket) remoteStatusSnapshot() (protocolStatus, bool) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.remoteStatus == nil {
		return protocolStatus{}, false
	}
	return *p.remoteStatus, true
}

func (p *peerSocket) probeReconcile() bool {
	return p.queue(gossipMessage{Type: "ping", Protocol: ledger.ProtocolName, NodeID: reconcileCapability})
}

func (p *peerSocket) enableReconcile() {
	p.stateMu.Lock()
	p.reconcile = true
	p.stateMu.Unlock()
	p.maybeStartReconcile()
}

func (p *peerSocket) reconcileEnabled() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.reconcile
}

func (p *peerSocket) maybeStartReconcile() {
	if p.outbound {
		return
	}
	now := time.Now()
	p.stateMu.Lock()
	if !p.reconcile || p.reconciling || p.remoteStatus == nil || now.Before(p.reconcileNext) {
		p.stateMu.Unlock()
		return
	}
	p.reconciling = true
	p.reconcileNext = now.Add(socketReconcileMinimumInterval)
	p.stateMu.Unlock()
	p.service.launch(func() {
		more, err := p.service.reconcileSocket(p)
		p.stateMu.Lock()
		p.reconciling = false
		closePeer := false
		if err != nil {
			p.reconcileErrors++
			p.reconcileLast = err.Error()
			delay, closeConnection := reconcileFailureDisposition(err, p.reconcileErrors)
			closePeer = closeConnection
			p.reconcileNext = time.Now().Add(delay)
		} else {
			p.reconcileErrors = 0
			p.reconcileLast = ""
		}
		delay := max(time.Duration(0), time.Until(p.reconcileNext))
		p.stateMu.Unlock()
		if closePeer {
			p.penalize(err)
			p.close()
			return
		}
		if more || err != nil {
			p.scheduleReconcile(delay)
		}
	})
}

func (p *peerSocket) scheduleReconcile(delay time.Duration) {
	p.service.launch(func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			p.maybeStartReconcile()
		case <-p.done:
		case <-p.service.backgroundContext().Done():
		}
	})
}

func validReconcileRequestID(id string) bool {
	if len(id) == 0 || len(id) > 20 {
		return false
	}
	for _, character := range id {
		if character < '0' || character > '9' {
			return false
		}
	}
	return id != "0"
}

func isReconcileResponse(messageType string) bool {
	switch messageType {
	case "reconcile_headers_response", "reconcile_block", "reconcile_mempool_response", "reconcile_error":
		return true
	default:
		return false
	}
}

func invalidReconcileResponse(format string, arguments ...any) error {
	return &invalidReconcileResponseError{err: fmt.Errorf(format, arguments...)}
}

func reconcileResponseError(message gossipMessage) error {
	if message.ReconcileError == "" || len(message.ReconcileError) > maxSocketReconcileErrorBytes {
		return invalidReconcileResponse("peer returned a malformed reconcile error")
	}
	switch message.ReconcileErrorCode {
	case reconcileErrorBusy, reconcileErrorPruned, reconcileErrorNotFound,
		reconcileErrorTemporary, reconcileErrorInvalidRequest:
		return &remoteReconcileError{code: message.ReconcileErrorCode, message: message.ReconcileError}
	default:
		return invalidReconcileResponse("peer returned an unknown reconcile error code")
	}
}

func reconcileFailureDisposition(err error, failures int) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	var invalid *invalidReconcileResponseError
	if errors.As(err, &invalid) {
		return 0, true
	}
	if errors.Is(err, ledger.ErrReorgBeyondPrune) || errors.Is(err, ledger.ErrBlockPruned) {
		return socketReconcilePrunedBackoff, false
	}
	base := 5 * time.Second
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		base = 10 * time.Second
	} else {
		var remote *remoteReconcileError
		if !errors.As(err, &remote) {
			return 0, true
		}
		switch remote.code {
		case reconcileErrorBusy:
			base = 5 * time.Second
		case reconcileErrorPruned:
			return socketReconcilePrunedBackoff, false
		case reconcileErrorNotFound, reconcileErrorTemporary:
			base = 10 * time.Second
		case reconcileErrorInvalidRequest:
			return 0, true
		default:
			return 0, true
		}
	}
	shift := max(0, min(failures-1, 6))
	delay := base * time.Duration(1<<shift)
	return min(delay, socketReconcileMaximumBackoff), false
}

func (p *peerSocket) registerRequest(expected string, capacity int) (string, *pendingSocketRequest, error) {
	if capacity <= 0 || capacity > maxSocketBlockBatch {
		return "", nil, fmt.Errorf("reconcile response capacity is invalid")
	}
	id := strconv.FormatUint(p.requestSeq.Add(1), 10)
	request := &pendingSocketRequest{expected: expected, responses: make(chan gossipMessage, capacity)}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if !p.reconcile {
		return "", nil, fmt.Errorf("peer did not negotiate reconcile support")
	}
	if len(p.pending) >= maxSocketPendingRequests {
		return "", nil, fmt.Errorf("too many pending reconcile requests")
	}
	if p.pending == nil {
		p.pending = make(map[string]*pendingSocketRequest)
	}
	p.pending[id] = request
	return id, request, nil
}

func (p *peerSocket) unregisterRequest(id string, request *pendingSocketRequest) {
	p.stateMu.Lock()
	if p.pending[id] == request {
		delete(p.pending, id)
	}
	p.stateMu.Unlock()
}

func (p *peerSocket) routeReconcileResponse(message gossipMessage) error {
	if !validReconcileRequestID(message.RequestID) {
		return fmt.Errorf("peer reconcile response has an invalid request ID")
	}
	p.stateMu.Lock()
	request := p.pending[message.RequestID]
	p.stateMu.Unlock()
	if request == nil {
		return nil
	}
	if message.Type != request.expected && message.Type != "reconcile_error" {
		return fmt.Errorf("peer returned an unexpected reconcile response")
	}
	select {
	case request.responses <- message:
		return nil
	default:
		return fmt.Errorf("peer exceeded the reconcile response count")
	}
}

func (p *peerSocket) requestOne(
	ctx context.Context,
	message gossipMessage,
	expected string,
) (gossipMessage, error) {
	requestContext, cancel := context.WithTimeout(ctx, socketReconcileRequestTimeout)
	defer cancel()
	id, pending, err := p.registerRequest(expected, 1)
	if err != nil {
		return gossipMessage{}, err
	}
	defer p.unregisterRequest(id, pending)
	message.RequestID = id
	if !p.queue(message) {
		return gossipMessage{}, fmt.Errorf("queue reconcile request: peer channel is unavailable")
	}
	select {
	case response := <-pending.responses:
		if response.Type == "reconcile_error" {
			return gossipMessage{}, reconcileResponseError(response)
		}
		return response, nil
	case <-p.done:
		return gossipMessage{}, fmt.Errorf("peer channel closed during reconcile")
	case <-requestContext.Done():
		return gossipMessage{}, requestContext.Err()
	}
}

func (p *peerSocket) requestBlockBatch(ctx context.Context, request blocksRequest) (blocksResponse, error) {
	if len(request.Hashes) == 0 || len(request.Hashes) > maxSocketBlockBatch {
		return blocksResponse{}, fmt.Errorf("reconcile block request count is invalid")
	}
	requestContext, cancel := context.WithTimeout(ctx, socketReconcileRequestTimeout)
	defer cancel()
	id, pending, err := p.registerRequest("reconcile_block", len(request.Hashes))
	if err != nil {
		return blocksResponse{}, err
	}
	defer p.unregisterRequest(id, pending)
	if !p.queue(gossipMessage{
		Type: "reconcile_blocks_request", Protocol: ledger.ProtocolName,
		RequestID: id, BlocksRequest: &request,
	}) {
		return blocksResponse{}, fmt.Errorf("queue reconcile block request: peer channel is unavailable")
	}
	blocks := make([]core.Block, len(request.Hashes))
	seen := make([]bool, len(request.Hashes))
	for received := 0; received < len(request.Hashes); received++ {
		select {
		case response := <-pending.responses:
			if response.Type == "reconcile_error" {
				return blocksResponse{}, reconcileResponseError(response)
			}
			if response.Block == nil || response.Parts != len(request.Hashes) ||
				response.Part < 0 || response.Part >= len(request.Hashes) || seen[response.Part] {
				return blocksResponse{}, invalidReconcileResponse("peer returned a malformed reconcile block stream")
			}
			seen[response.Part] = true
			blocks[response.Part] = *response.Block
		case <-p.done:
			return blocksResponse{}, fmt.Errorf("peer channel closed during block reconcile")
		case <-requestContext.Done():
			return blocksResponse{}, requestContext.Err()
		}
	}
	return blocksResponse{Protocol: ledger.ProtocolName, Blocks: blocks}, nil
}

type socketRemoteSource struct {
	socket *peerSocket
}

func (s socketRemoteSource) requestHeaders(ctx context.Context, request headersRequest) (headersResponse, error) {
	response, err := s.socket.requestOne(ctx, gossipMessage{
		Type: "reconcile_headers_request", Protocol: ledger.ProtocolName, HeadersRequest: &request,
	}, "reconcile_headers_response")
	if err != nil {
		return headersResponse{}, err
	}
	if response.HeadersResponse == nil {
		return headersResponse{}, invalidReconcileResponse("peer reconcile headers response is missing")
	}
	return *response.HeadersResponse, nil
}

func (s socketRemoteSource) requestBlocks(ctx context.Context, request blocksRequest) (blocksResponse, error) {
	if len(request.Hashes) == 0 || len(request.Hashes) > maxBlockDownloadBatch {
		return blocksResponse{}, fmt.Errorf("staged reconcile block request count is invalid")
	}
	blocks := make([]core.Block, 0, len(request.Hashes))
	for start := 0; start < len(request.Hashes); start += maxSocketBlockBatch {
		end := min(start+maxSocketBlockBatch, len(request.Hashes))
		response, err := s.socket.requestBlockBatch(ctx, blocksRequest{Hashes: request.Hashes[start:end]})
		if err != nil {
			return blocksResponse{}, err
		}
		if response.Protocol != ledger.ProtocolName || len(response.Blocks) != end-start {
			return blocksResponse{}, invalidReconcileResponse("peer returned an incomplete reconcile block sub-batch")
		}
		blocks = append(blocks, response.Blocks...)
	}
	return blocksResponse{Protocol: ledger.ProtocolName, Blocks: blocks}, nil
}

func (p *peerSocket) requestMempool(
	ctx context.Context,
	request mempoolRequest,
) (transactionsResponse, error) {
	response, err := p.requestOne(ctx, gossipMessage{
		Type: "reconcile_mempool_request", Protocol: ledger.ProtocolName, MempoolRequest: &request,
	}, "reconcile_mempool_response")
	if err != nil {
		return transactionsResponse{}, err
	}
	if response.TransactionsResponse == nil {
		return transactionsResponse{}, invalidReconcileResponse("peer reconcile mempool response is missing")
	}
	return *response.TransactionsResponse, nil
}

func (s *Service) reconcileSocket(socket *peerSocket) (bool, error) {
	ctx, cancel := context.WithTimeout(s.backgroundContext(), socketReconcileRoundTimeout)
	defer cancel()
	remote, ok := socket.remoteStatusSnapshot()
	if !ok {
		return false, nil
	}
	localTip, err := s.ledger.Tip(ctx)
	if err != nil {
		s.setError(err)
		return false, nil
	}
	chainProgressed := false
	var chainErr error
	if localTip.Hash != remote.TipHash {
		source := socketRemoteSource{socket: socket}
		for chunk := 0; chunk < maxChainSyncChunksPerRound && localTip.Hash != remote.TipHash; chunk++ {
			if deadline, hasDeadline := ctx.Deadline(); hasDeadline && time.Until(deadline) < 5*time.Second {
				break
			}
			before := localTip.Hash
			if err := s.syncRemoteChainFrom(ctx, source, localTip); err != nil {
				chainErr = err
				if _, closePeer := reconcileFailureDisposition(err, 1); closePeer {
					return false, err
				}
				break
			}
			localTip, err = s.ledger.Tip(ctx)
			if err != nil {
				s.setError(err)
				return false, nil
			}
			if localTip.Hash == before {
				break
			}
			chainProgressed = true
		}
	}
	chainMore := chainProgressed && localTip.Hash != remote.TipHash
	for page := 0; page < maxSocketMempoolPagesPerRound; page++ {
		if err := s.syncSocketMempoolPage(ctx, socket); err != nil {
			return chainMore, err
		}
		socket.stateMu.Lock()
		finished := socket.mempoolOffset == 0
		socket.stateMu.Unlock()
		if finished {
			return chainMore, chainErr
		}
	}
	return true, chainErr
}

func (s *Service) syncSocketMempoolPage(ctx context.Context, socket *peerSocket) error {
	socket.stateMu.Lock()
	offset := socket.mempoolOffset
	socket.stateMu.Unlock()
	response, err := socket.requestMempool(ctx, mempoolRequest{Limit: maxSocketMempoolBatch, Offset: offset})
	if err != nil {
		return err
	}
	if response.Protocol != ledger.ProtocolName || len(response.Transactions) > maxSocketMempoolBatch {
		return invalidReconcileResponse("peer returned an invalid reconcile mempool response")
	}
	for _, transaction := range response.Transactions {
		if err := s.ledger.AddTransaction(ctx, transaction); err != nil {
			if errors.Is(err, ledger.ErrTransactionAlreadyKnown) {
				continue
			}
			return invalidReconcileResponse("peer returned an invalid reconcile transaction: %w", err)
		}
		s.broadcastTransaction(transaction, socket)
	}
	nextOffset := 0
	if len(response.Transactions) == maxSocketMempoolBatch && offset+len(response.Transactions) < core.MaxPendingTransactions {
		nextOffset = offset + len(response.Transactions)
	}
	socket.stateMu.Lock()
	socket.mempoolOffset = nextOffset
	socket.stateMu.Unlock()
	return nil
}

func (p *peerSocket) run(ctx context.Context) {
	defer p.service.wait.Done()
	defer p.service.unregisterSocket(p)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		p.writeLoop(ctx)
	}()
	p.readLoop(ctx)
	p.close()
	<-writerDone
	if p.outbound && ctx.Err() == nil && !p.expectedClose.Load() {
		p.service.markPeerSocketFailure(p.baseURL, fmt.Errorf("realtime channel closed"))
	}
}

func (p *peerSocket) readLoop(ctx context.Context) {
	p.connection.SetReadLimit(maxProtocolBytes)
	_ = p.connection.SetReadDeadline(time.Now().Add(45 * time.Second))
	p.connection.SetPongHandler(func(string) error {
		return p.connection.SetReadDeadline(time.Now().Add(45 * time.Second))
	})
	budget := newSocketInboundBudget(time.Now())
	for {
		messageType, data, err := p.connection.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			p.penalize(fmt.Errorf("peer sent a non-text gossip message"))
			return
		}
		if !budget.allow(time.Now(), len(data), 0) {
			p.penalize(fmt.Errorf("peer exceeded the realtime byte budget"))
			return
		}
		var message gossipMessage
		if err := decodeLimitedJSON(bytes.NewReader(data), maxProtocolBytes, &message); err != nil {
			p.penalize(fmt.Errorf("decode realtime message: %w", err))
			return
		}
		if message.Protocol != ledger.ProtocolName {
			p.penalize(fmt.Errorf("peer sent an incompatible realtime protocol"))
			return
		}
		if !budget.allow(time.Now(), 0, gossipValidationCost(message)) {
			p.penalize(fmt.Errorf("peer exceeded the realtime validation budget"))
			return
		}
		if isReconcileResponse(message.Type) {
			if !p.helloSeen || !p.reconcileEnabled() {
				p.penalize(fmt.Errorf("peer sent a reconcile response before negotiation"))
				return
			}
			if err := p.routeReconcileResponse(message); err != nil {
				p.penalize(err)
				return
			}
			continue
		}
		keep, invalidScore, handleErr := p.service.handleGossip(ctx, p, message)
		if !keep {
			if handleErr != nil {
				p.penalize(handleErr)
			}
			return
		}
		if invalidScore > 0 {
			if p.service.addInvalidGossipScore(p, invalidScore, time.Now()) >= webSocketInvalidScoreLimit {
				p.penalize(fmt.Errorf("peer exceeded the invalid gossip threshold: %w", handleErr))
				return
			}
		}
	}
}

func (s *Service) addInvalidGossipScore(socket *peerSocket, delta int, now time.Time) int {
	if delta <= 0 || socket == nil || socket.connection == nil {
		return 0
	}
	scoreKey := socket.remoteIP
	if socket.outbound && socket.baseURL != "" {
		scoreKey = socket.baseURL
	}
	if scoreKey == "" {
		return webSocketInvalidScoreLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.invalidGossipByIP[scoreKey]
	if state == nil || now.Sub(state.updated) > invalidGossipScoreTTL {
		state = &invalidGossipState{}
		s.invalidGossipByIP[scoreKey] = state
	}
	state.score += delta
	state.updated = now
	if len(s.invalidGossipByIP) > 4_096 {
		for peerIP, candidate := range s.invalidGossipByIP {
			if peerIP != scoreKey && now.Sub(candidate.updated) > invalidGossipScoreTTL {
				delete(s.invalidGossipByIP, peerIP)
			}
		}
	}
	return state.score
}

func gossipValidationCost(message gossipMessage) int {
	// Reconcile responses are accepted only for a locally registered bounded
	// request. Byte limits still apply, while the requested objects are fully
	// validated by the chain or mempool synchronizer.
	if isReconcileResponse(message.Type) {
		return 1
	}
	cost := 1
	if message.Transaction != nil {
		cost += len(message.Transaction.Inputs) + len(message.Transaction.Outputs)
	}
	if message.Block != nil {
		cost += 16 + len(message.Block.Transactions)
		for _, transaction := range message.Block.Transactions {
			cost += len(transaction.Inputs) + len(transaction.Outputs)
			if cost > int(webSocketValidationBurst) {
				return cost
			}
		}
	}
	if message.HeadersRequest != nil {
		limitCost := (max(0, message.HeadersRequest.Limit) + 7) / 8
		cost += 16 + len(message.HeadersRequest.Locator) + limitCost
	}
	if message.HeadersResponse != nil {
		cost += 16 + len(message.HeadersResponse.Headers)
	}
	if message.BlocksRequest != nil {
		cost += 64 * len(message.BlocksRequest.Hashes)
	}
	if message.MempoolRequest != nil {
		cost += 16 + message.MempoolRequest.Limit
	}
	if message.TransactionsResponse != nil {
		cost += 16
		for _, transaction := range message.TransactionsResponse.Transactions {
			cost += len(transaction.Inputs) + len(transaction.Outputs)
		}
	}
	return cost
}

func (p *peerSocket) penalize(err error) {
	if err != nil && p.baseURL != "" {
		p.service.markPeerFailure(p.baseURL, err)
	}
}

func (p *peerSocket) writeLoop(ctx context.Context) {
	defer p.disableSendQueue()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.done:
			return
		case message := <-p.send:
			_ = p.connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := p.connection.WriteMessage(websocket.TextMessage, message.data)
			p.releaseQueuedBytes(int64(len(message.data)))
			if err != nil {
				return
			}
		case <-ping.C:
			_ = p.connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := p.connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}

func (p *peerSocket) queue(message gossipMessage) bool {
	encoded, err := json.Marshal(message)
	if err != nil || int64(len(encoded)) > maxProtocolBytes {
		return false
	}
	size := int64(len(encoded))
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	if p.sendClosed || p.queuedBytes > maxSocketQueuedBytes-size {
		return false
	}
	p.queuedBytes += size
	select {
	case p.send <- queuedGossip{data: encoded}:
		return true
	default:
		p.queuedBytes -= size
		return false
	}
}

func (p *peerSocket) releaseQueuedBytes(size int64) {
	if size <= 0 {
		return
	}
	p.sendMu.Lock()
	p.queuedBytes -= size
	if p.queuedBytes < 0 {
		p.queuedBytes = 0
	}
	p.sendMu.Unlock()
}

func (p *peerSocket) disableSendQueue() {
	p.sendMu.Lock()
	p.sendClosed = true
	p.sendMu.Unlock()
	for {
		select {
		case message := <-p.send:
			p.releaseQueuedBytes(int64(len(message.data)))
		default:
			return
		}
	}
}

func (p *peerSocket) close() {
	p.closeOnce.Do(func() {
		p.disableSendQueue()
		close(p.done)
		_ = p.connection.Close()
	})
}

func (p *peerSocket) stop() {
	p.expectedClose.Store(true)
	p.close()
}

func (p *peerSocket) queueReconcile(message gossipMessage) bool {
	return p.queue(message)
}

func (p *peerSocket) queueReconcileError(requestID, code string, err error) bool {
	message := "reconcile request failed"
	if err != nil {
		message = err.Error()
	}
	if len(message) > maxSocketReconcileErrorBytes {
		message = message[:maxSocketReconcileErrorBytes]
	}
	switch code {
	case reconcileErrorBusy, reconcileErrorPruned, reconcileErrorNotFound,
		reconcileErrorTemporary, reconcileErrorInvalidRequest:
	default:
		code = reconcileErrorTemporary
	}
	return p.queueReconcile(gossipMessage{
		Type: "reconcile_error", Protocol: ledger.ProtocolName,
		RequestID: requestID, ReconcileError: message, ReconcileErrorCode: code,
	})
}

func validateReconcileRequest(socket *peerSocket, requestID string) error {
	if !socket.reconcileEnabled() {
		return fmt.Errorf("peer sent a reconcile request before negotiation")
	}
	if !validReconcileRequestID(requestID) {
		return fmt.Errorf("peer reconcile request has an invalid request ID")
	}
	return nil
}

func (s *Service) serveReconcileHeaders(socket *peerSocket, message gossipMessage) (bool, error) {
	if err := validateReconcileRequest(socket, message.RequestID); err != nil {
		return false, err
	}
	request := message.HeadersRequest
	if request == nil || request.Limit <= 0 || request.Limit > maxHeaderBatch {
		return false, fmt.Errorf("peer reconcile header request is invalid")
	}
	height, hash, err := s.ledger.FindLocator(s.backgroundContext(), request.Locator)
	if err != nil {
		return socket.queueReconcileError(message.RequestID, reconcileErrorInvalidRequest, err), nil
	}
	headers, err := s.ledger.HeadersFrom(s.backgroundContext(), height+1, request.Limit)
	if err != nil {
		return socket.queueReconcileError(message.RequestID, reconcileErrorTemporary, err), nil
	}
	response := headersResponse{
		Protocol: ledger.ProtocolName, CommonHeight: height, CommonHash: hash, Headers: headers,
	}
	if !socket.queueReconcile(gossipMessage{
		Type: "reconcile_headers_response", Protocol: ledger.ProtocolName,
		RequestID: message.RequestID, HeadersResponse: &response,
	}) {
		return false, fmt.Errorf("reconcile header response exceeds the bounded peer queue")
	}
	return true, nil
}

func (s *Service) serveReconcileBlocks(socket *peerSocket, message gossipMessage) (bool, error) {
	if err := validateReconcileRequest(socket, message.RequestID); err != nil {
		return false, err
	}
	request := message.BlocksRequest
	if request == nil || len(request.Hashes) == 0 || len(request.Hashes) > maxSocketBlockBatch {
		return false, fmt.Errorf("peer reconcile block request is invalid")
	}
	seen := make(map[string]struct{}, len(request.Hashes))
	for _, hash := range request.Hashes {
		if _, duplicate := seen[hash]; duplicate {
			return false, fmt.Errorf("peer reconcile block request contains duplicate hashes")
		}
		seen[hash] = struct{}{}
	}
	if !s.acquireHeavyRequest(s.backgroundContext()) {
		return socket.queueReconcileError(
			message.RequestID, reconcileErrorBusy, fmt.Errorf("node is busy serving block data"),
		), nil
	}
	defer s.releaseHeavyRequest()
	for index, hash := range request.Hashes {
		block, err := s.ledger.BlockByHash(s.backgroundContext(), hash)
		if err != nil {
			code := reconcileErrorTemporary
			if errors.Is(err, ledger.ErrBlockPruned) {
				code = reconcileErrorPruned
			} else if errors.Is(err, ledger.ErrBlockNotFound) {
				code = reconcileErrorNotFound
			}
			return socket.queueReconcileError(message.RequestID, code, err), nil
		}
		if !socket.queueReconcile(gossipMessage{
			Type: "reconcile_block", Protocol: ledger.ProtocolName,
			RequestID: message.RequestID, Part: index, Parts: len(request.Hashes), Block: &block,
		}) {
			return false, fmt.Errorf("reconcile block response exceeds the bounded peer queue")
		}
	}
	return true, nil
}

func (s *Service) serveReconcileMempool(socket *peerSocket, message gossipMessage) (bool, error) {
	if err := validateReconcileRequest(socket, message.RequestID); err != nil {
		return false, err
	}
	request := message.MempoolRequest
	if request == nil || request.Limit <= 0 || request.Limit > maxSocketMempoolBatch ||
		request.Offset < 0 || request.Offset > core.MaxPendingTransactions {
		return false, fmt.Errorf("peer reconcile mempool request is invalid")
	}
	if !s.acquireHeavyRequest(s.backgroundContext()) {
		return socket.queueReconcileError(
			message.RequestID, reconcileErrorBusy, fmt.Errorf("node is busy serving mempool data"),
		), nil
	}
	defer s.releaseHeavyRequest()
	transactions, err := s.ledger.MempoolTransactionsPage(s.backgroundContext(), request.Limit, request.Offset)
	if err != nil {
		return socket.queueReconcileError(message.RequestID, reconcileErrorTemporary, err), nil
	}
	response := transactionsResponse{Protocol: ledger.ProtocolName, Transactions: transactions}
	if !socket.queueReconcile(gossipMessage{
		Type: "reconcile_mempool_response", Protocol: ledger.ProtocolName,
		RequestID: message.RequestID, TransactionsResponse: &response,
	}) {
		return false, fmt.Errorf("reconcile mempool response exceeds the bounded peer queue")
	}
	return true, nil
}

func (s *Service) handleGossip(ctx context.Context, socket *peerSocket, message gossipMessage) (bool, int, error) {
	if message.Type != "hello" && !socket.helloSeen {
		return false, 0, fmt.Errorf("peer sent gossip before completing hello")
	}
	switch message.Type {
	case "hello":
		if socket.helloSeen || core.ValidateAddress(message.NodeID) != nil || message.NodeID == s.Address() {
			return false, 0, fmt.Errorf("peer hello contains an invalid node ID")
		}
		if !socket.outbound && (message.ListenPort <= 0 || message.ListenPort > 65535) {
			return false, 0, fmt.Errorf("inbound peer hello contains an invalid listen port")
		}
		if message.Status != nil {
			if err := validateRemoteStatus(*message.Status); err != nil {
				return false, 0, err
			}
			socket.noteRemoteStatus(*message.Status)
		}
		if message.Status != nil && socket.baseURL != "" {
			s.markPeerSuccess(socket.baseURL, message.Status.Height)
		}
		socket.helloSeen = true
		if !socket.probeReconcile() {
			return false, 0, fmt.Errorf("peer channel could not queue capability probe")
		}
		return true, 0, nil
	case "status":
		if message.Status == nil {
			return false, 0, fmt.Errorf("peer status is missing")
		}
		if err := validateRemoteStatus(*message.Status); err != nil {
			return false, 0, err
		}
		socket.noteRemoteStatus(*message.Status)
		if socket.baseURL != "" {
			s.markPeerSuccess(socket.baseURL, message.Status.Height)
		}
		socket.maybeStartReconcile()
		return true, 0, nil
	case "transaction":
		if message.Transaction == nil {
			return false, 0, fmt.Errorf("peer transaction is missing")
		}
		if err := s.acceptTransaction(ctx, *message.Transaction, socket); err != nil {
			if errors.Is(err, ledger.ErrTransactionAlreadyKnown) {
				return true, 0, nil
			}
			return true, invalidTransactionScore, err
		}
		return true, 0, nil
	case "block":
		if message.Block == nil {
			return false, 0, fmt.Errorf("peer block is missing")
		}
		tip, tipErr := s.ledger.Tip(ctx)
		err := s.acceptBlock(ctx, *message.Block, socket)
		if err != nil {
			if socket.baseURL != "" && (socket.outbound || !socket.reconcileEnabled()) {
				peer := socket.baseURL
				s.launch(func() { s.syncPeer(peer) })
			}
			socket.maybeStartReconcile()
			if tipErr == nil && message.Block.Height == tip.Height+1 && message.Block.PreviousHash == tip.Hash {
				return true, invalidBlockScore, err
			}
		}
		return true, 0, nil
	case "ping":
		response := gossipMessage{Type: "pong", Protocol: ledger.ProtocolName}
		if message.NodeID == reconcileCapability {
			response.NodeID = reconcileCapability
		}
		return socket.queue(response), 0, nil
	case "pong":
		if message.NodeID == reconcileCapability {
			socket.enableReconcile()
		}
		return true, 0, nil
	case "reconcile_headers_request":
		keep, err := s.serveReconcileHeaders(socket, message)
		return keep, 0, err
	case "reconcile_blocks_request":
		keep, err := s.serveReconcileBlocks(socket, message)
		return keep, 0, err
	case "reconcile_mempool_request":
		keep, err := s.serveReconcileMempool(socket, message)
		return keep, 0, err
	default:
		return false, 0, fmt.Errorf("peer sent an unknown gossip message type")
	}
}

func (s *Service) broadcastTransaction(transaction core.Transaction, source *peerSocket) {
	s.broadcastGossip(gossipMessage{Type: "transaction", Protocol: ledger.ProtocolName, Transaction: &transaction}, source)
	s.broadcastHTTP("/v2/transactions", transaction)
}

func (s *Service) broadcastBlock(block core.Block, source *peerSocket) {
	s.broadcastGossip(gossipMessage{Type: "block", Protocol: ledger.ProtocolName, Block: &block}, source)
	s.broadcastHTTP("/v2/block", block)
}

func (s *Service) broadcastGossip(message gossipMessage, source *peerSocket) {
	s.mu.RLock()
	sockets := make([]*peerSocket, 0, len(s.sockets))
	for socket := range s.sockets {
		if socket != source {
			sockets = append(sockets, socket)
		}
	}
	s.mu.RUnlock()
	for _, socket := range sockets {
		_ = socket.queue(message)
	}
}

func (s *Service) broadcastHTTP(path string, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		return
	}
	for _, peer := range s.activePeerURLs() {
		peer := peer
		s.launch(func() {
			ctx, cancel := context.WithTimeout(s.backgroundContext(), 8*time.Second)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, peer+path, bytes.NewReader(body))
			if err != nil {
				return
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := s.client.Do(request)
			if err != nil {
				return
			}
			_ = response.Body.Close()
		})
	}
}
