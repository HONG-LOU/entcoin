package node

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"
	"github.com/HONG-LOU/entcoin/internal/store"
	"github.com/HONG-LOU/entcoin/internal/vault"
)

const DefaultListenAddress = "0.0.0.0:47821"

type Config struct {
	DataDirectory         string
	ListenAddress         string
	SyncInterval          time.Duration
	DisableDiscovery      bool
	SeedMode              bool
	PruneDepth            uint64
	PruneDepthSet         bool
	InitialPruneDepth     uint64
	FallbackPort          bool
	BootstrapPeers        []string
	BootstrapManifestURLs []string
	MaxOutboundPeers      int
	TrustLoopbackProxy    bool
}

type Service struct {
	mu                 sync.RWMutex
	peerMutationMu     sync.Mutex
	walletMutationMu   sync.Mutex
	ledger             *ledger.Ledger
	material           *vault.Material
	wallet             core.Wallet
	store              *store.Store
	peers              map[string]*peerState
	activeOutbound     map[string]struct{}
	sockets            map[*peerSocket]struct{}
	outboundSockets    map[string]*peerSocket
	listenAddress      string
	actualAddress      string
	client             *http.Client
	server             *http.Server
	cancel             context.CancelFunc
	rootContext        context.Context
	mining             bool
	miningJobs         int
	miningCancel       context.CancelFunc
	tipChanged         chan struct{}
	mineBlock          func(context.Context, core.Block) (core.Block, error)
	wait               sync.WaitGroup
	lastError          string
	directoryLock      *store.DirectoryLock
	syncing            map[string]bool
	dialing            map[string]bool
	requestSlots       chan struct{}
	websocketSlots     chan struct{}
	heavyRequestSlots  chan struct{}
	stagingSlot        chan struct{}
	requestsByIP       map[string]int
	websocketsByIP     map[string]int
	requestRatesByIP   map[string]*requestRateState
	websocketRatesByIP map[string]*requestRateState
	invalidGossipByIP  map[string]*invalidGossipState
	chainSyncSlot      chan struct{}
	resyncPauses       map[string]resyncPause
	mempoolOffsets     map[string]int
	resyncRequired     bool
	closing            bool
	syncInterval       time.Duration
	maxOutboundPeers   int
	disableDiscovery   bool
	fallbackPort       bool
	trustLoopbackProxy bool
	seedMode           bool
	bootstrapPeers     []string
	bootstrapURLs      []string
	bootstrapAttempt   time.Time
	bootstrapSuccess   time.Time
	bootstrapError     string
	peerExchangeNext   map[string]time.Time
	walletNeedsBackup  bool
	walletGeneration   uint64
	closeDone          chan struct{}
	pruneDepth         uint64
}

type Dashboard struct {
	Name                string          `json:"name"`
	Symbol              string          `json:"symbol"`
	Protocol            string          `json:"protocol"`
	ConsensusVersion    uint32          `json:"consensus_version"`
	ConsensusActivation uint64          `json:"consensus_activation"`
	Address             string          `json:"address"`
	ConfirmedBalance    string          `json:"confirmed_balance"`
	SpendableBalance    string          `json:"spendable_balance"`
	Height              uint64          `json:"height"`
	TipHash             string          `json:"tip_hash"`
	Difficulty          uint8           `json:"difficulty"`
	PendingCount        int             `json:"pending_count"`
	PeerCount           int             `json:"peer_count"`
	ConfiguredPeerCount int             `json:"configured_peer_count"`
	Peers               []PeerSummary   `json:"peers"`
	Mining              bool            `json:"mining"`
	Syncing             bool            `json:"syncing"`
	BestPeerHeight      uint64          `json:"best_peer_height"`
	ListenAddress       string          `json:"listen_address"`
	Issued              string          `json:"issued"`
	MaxSupply           string          `json:"max_supply"`
	TargetBlockSeconds  int             `json:"target_block_seconds"`
	EmissionBlocks      uint64          `json:"emission_blocks"`
	NextSubsidy         string          `json:"next_subsidy"`
	LastError           string          `json:"last_error"`
	DatabasePath        string          `json:"database_path"`
	DatabaseBytes       int64           `json:"database_bytes"`
	PrunedThrough       uint64          `json:"pruned_through"`
	PruneDepth          uint64          `json:"prune_depth"`
	ArchiveMode         bool            `json:"archive_mode"`
	WalletNeedsBackup   bool            `json:"wallet_needs_backup"`
	Wallets             []WalletProfile `json:"wallets"`
	BootstrapEnabled    bool            `json:"bootstrap_enabled"`
	BootstrapReady      bool            `json:"bootstrap_ready"`
	BootstrapLastUpdate int64           `json:"bootstrap_last_update,omitempty"`
	BootstrapError      string          `json:"bootstrap_error,omitempty"`
	RecentBlocks        []BlockSummary  `json:"recent_blocks"`
}

type BlockSummary struct {
	Height       uint64 `json:"height"`
	Hash         string `json:"hash"`
	Timestamp    int64  `json:"timestamp"`
	Transactions int    `json:"transactions"`
	Difficulty   uint8  `json:"difficulty"`
}

type PeerSummary struct {
	URL            string `json:"url"`
	Online         bool   `json:"online"`
	Height         uint64 `json:"height"`
	Failures       int    `json:"failures"`
	LastError      string `json:"last_error,omitempty"`
	Discovered     bool   `json:"discovered"`
	Public         bool   `json:"public"`
	Bootstrap      bool   `json:"bootstrap"`
	ActiveOutbound bool   `json:"active_outbound"`
	LastSeen       int64  `json:"last_seen,omitempty"`
}

type TransactionSummary struct {
	ID            string                     `json:"id"`
	BlockHeight   *uint64                    `json:"block_height,omitempty"`
	BlockHash     string                     `json:"block_hash,omitempty"`
	BlockPosition *int                       `json:"block_position,omitempty"`
	Pending       bool                       `json:"pending"`
	Coinbase      bool                       `json:"coinbase"`
	Pruned        bool                       `json:"pruned"`
	Confirmations uint64                     `json:"confirmations"`
	Timestamp     int64                      `json:"timestamp"`
	Received      string                     `json:"received"`
	Sent          string                     `json:"sent"`
	Inputs        []TransactionInputSummary  `json:"inputs"`
	Outputs       []TransactionOutputSummary `json:"outputs"`
}

type TransactionInputSummary struct {
	TransactionID string `json:"transaction_id"`
	OutputIndex   uint32 `json:"output_index"`
}

type TransactionOutputSummary struct {
	Index   uint32 `json:"index"`
	Address string `json:"address"`
	Amount  string `json:"amount"`
}

func New(config Config) (*Service, error) {
	return NewContext(context.Background(), config)
}

func NewContext(ctx context.Context, config Config) (*Service, error) {
	if ctx == nil {
		return nil, fmt.Errorf("node initialization context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.DataDirectory == "" {
		var err error
		config.DataDirectory, err = store.DefaultDirectory()
		if err != nil {
			return nil, err
		}
	}
	if config.ListenAddress == "" {
		config.ListenAddress = DefaultListenAddress
	}
	if config.SyncInterval <= 0 {
		config.SyncInterval = defaultPeerSyncInterval
	}
	if config.MaxOutboundPeers <= 0 {
		config.MaxOutboundPeers = defaultMaxOutboundPeers
	}
	if config.MaxOutboundPeers > maxPeerConnections {
		return nil, fmt.Errorf("maximum outbound peers must not exceed %d", maxPeerConnections)
	}
	if config.SeedMode {
		if config.PruneDepthSet && config.PruneDepth != 0 {
			return nil, fmt.Errorf("seed mode requires archive storage with prune depth 0")
		}
		config.PruneDepth = 0
		config.PruneDepthSet = true
		config.InitialPruneDepth = 0
	}
	bootstrapPeers, err := normalizeBootstrapPeers(config.BootstrapPeers)
	if err != nil {
		return nil, err
	}
	bootstrapURLs, err := normalizeBootstrapManifestURLs(config.BootstrapManifestURLs)
	if err != nil {
		return nil, err
	}
	existingData, err := nodeDataExists(config.DataDirectory)
	if err != nil {
		return nil, err
	}
	directoryLock, err := store.LockDirectory(config.DataDirectory)
	if err != nil {
		return nil, err
	}
	keepResources := false
	var chain *ledger.Ledger
	var material *vault.Material
	defer func() {
		if keepResources {
			return
		}
		if chain != nil {
			_ = chain.Close()
		}
		if material != nil {
			material.Clear()
		}
		_ = directoryLock.Close()
	}()
	if err := cleanupStaleStagingFiles(config.DataDirectory); err != nil {
		return nil, err
	}

	storage := store.New(config.DataDirectory)
	var walletState walletLoadState
	if config.SeedMode {
		material, walletState, err = loadSeedMaterial(storage)
	} else {
		material, walletState, err = loadWalletMaterial(storage, existingData)
	}
	if err != nil {
		return nil, err
	}
	chain, err = ledger.Open(ctx, config.DataDirectory)
	if err != nil {
		return nil, err
	}
	if config.SeedMode {
		prunedThrough, pruneErr := chain.PrunedThrough(ctx)
		if pruneErr != nil {
			return nil, pruneErr
		}
		if prunedThrough > 0 {
			return nil, fmt.Errorf("seed mode requires unpruned history; this ledger was pruned through block %d", prunedThrough)
		}
	}
	if err := migrateLegacyState(ctx, storage, chain); err != nil {
		return nil, err
	}
	if err := migrateLegacyPeers(ctx, storage, chain); err != nil {
		return nil, err
	}
	pruneDepth, err := chain.PruneDepth(ctx)
	if err != nil {
		return nil, err
	}
	if config.PruneDepthSet || config.PruneDepth > 0 {
		if err := chain.SetPruneDepth(ctx, config.PruneDepth); err != nil {
			return nil, err
		}
		pruneDepth = config.PruneDepth
	} else if !existingData && config.InitialPruneDepth > 0 {
		if err := chain.SetPruneDepth(ctx, config.InitialPruneDepth); err != nil {
			return nil, err
		}
		pruneDepth = config.InitialPruneDepth
	}
	storedPeers, err := chain.Peers(ctx)
	if err != nil {
		return nil, err
	}
	service := &Service{
		ledger:             chain,
		material:           material,
		wallet:             material.Wallet,
		store:              storage,
		peers:              make(map[string]*peerState),
		activeOutbound:     make(map[string]struct{}),
		sockets:            make(map[*peerSocket]struct{}),
		outboundSockets:    make(map[string]*peerSocket),
		listenAddress:      config.ListenAddress,
		client:             newHTTPClient(),
		syncing:            make(map[string]bool),
		dialing:            make(map[string]bool),
		requestSlots:       make(chan struct{}, 32),
		websocketSlots:     make(chan struct{}, 32),
		heavyRequestSlots:  make(chan struct{}, 4),
		stagingSlot:        make(chan struct{}, 1),
		requestsByIP:       make(map[string]int),
		websocketsByIP:     make(map[string]int),
		requestRatesByIP:   make(map[string]*requestRateState),
		websocketRatesByIP: make(map[string]*requestRateState),
		invalidGossipByIP:  make(map[string]*invalidGossipState),
		chainSyncSlot:      make(chan struct{}, 1),
		resyncPauses:       make(map[string]resyncPause),
		mempoolOffsets:     make(map[string]int),
		directoryLock:      directoryLock,
		syncInterval:       config.SyncInterval,
		maxOutboundPeers:   config.MaxOutboundPeers,
		disableDiscovery:   config.DisableDiscovery,
		fallbackPort:       config.FallbackPort,
		trustLoopbackProxy: config.TrustLoopbackProxy,
		seedMode:           config.SeedMode,
		bootstrapPeers:     bootstrapPeers,
		bootstrapURLs:      bootstrapURLs,
		peerExchangeNext:   make(map[string]time.Time),
		walletNeedsBackup:  !config.SeedMode && (walletState.Created || !walletRecoveryConfirmed(storage, material.Wallet.Address)),
		walletGeneration:   1,
		closeDone:          make(chan struct{}),
		pruneDepth:         pruneDepth,
		tipChanged:         make(chan struct{}),
		mineBlock:          core.MineBlock,
	}
	discoveredPeers := 0
	for _, record := range storedPeers {
		if len(service.peers) >= maxPeerConnections {
			break
		}
		if !record.Manual && discoveredPeers >= maxDiscoveredPeers {
			continue
		}
		peer, normalizeErr := normalizePeer(record.URL)
		if normalizeErr == nil {
			_, publicErr := normalizePublicPeer(peer)
			service.peers[peer] = &peerState{
				URL:         peer,
				Failures:    record.Failures,
				LastError:   record.LastError,
				LastSeen:    record.LastSeen,
				NextAttempt: record.NextAttempt,
				Discovered:  !record.Manual,
				Public:      publicErr == nil,
			}
			if !record.Manual {
				discoveredPeers++
			}
		}
	}
	for _, peer := range bootstrapPeers {
		state := service.peers[peer]
		if state == nil {
			if len(service.peers) >= maxPeerConnections || service.discoveredPeerCountLocked() >= maxDiscoveredPeers {
				return nil, fmt.Errorf("bootstrap peer limit reached")
			}
			_, publicErr := normalizePublicPeer(peer)
			state = &peerState{URL: peer, Discovered: true, Public: publicErr == nil, Bootstrap: true}
			service.peers[peer] = state
		} else if state.Discovered {
			state.Bootstrap = true
		}
		if err := chain.UpsertPeer(ctx, peer, false); err != nil {
			return nil, err
		}
	}
	if err := service.removeStaleDiscoveredPeers(ctx); err != nil {
		return nil, fmt.Errorf("remove stale discovered peers: %w", err)
	}
	keepResources = true
	return service, nil
}

func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 4 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   4 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (s *Service) Start(parent context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return fmt.Errorf("node is already running")
	}
	if s.closing {
		return fmt.Errorf("node has been closed")
	}
	listener, fallback, err := listenNode(s.listenAddress, s.fallbackPort)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.listenAddress, err)
	}
	if fallback {
		s.lastError = fmt.Sprintf("default listener %s was busy; using an automatic port", s.listenAddress)
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.rootContext = ctx
	s.actualAddress = listener.Addr().String()
	mux := http.NewServeMux()
	s.registerProtocolHandlers(mux)
	server := &http.Server{
		Handler:           s.limitRequests(s.browserAccess(mux)),
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 4 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	s.server = server
	s.wait.Add(2)
	go func() {
		defer s.wait.Done()
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.setError(err)
		}
	}()
	go s.syncLoop(ctx)
	if len(s.bootstrapURLs) > 0 {
		s.wait.Add(1)
		go s.bootstrapLoop(ctx)
	}
	if !s.disableDiscovery {
		s.wait.Add(1)
		go s.discoveryLoop(ctx)
	}
	return nil
}

func listenNode(address string, fallback bool) (net.Listener, bool, error) {
	listener, err := net.Listen("tcp", address)
	if err == nil || !fallback || !isAddressInUse(err) {
		return listener, false, err
	}
	host, _, splitErr := net.SplitHostPort(address)
	if splitErr != nil {
		return nil, false, err
	}
	listener, fallbackErr := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if fallbackErr != nil {
		return nil, false, errors.Join(err, fallbackErr)
	}
	return listener, true, nil
}

func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closing {
		done := s.closeDone
		s.mu.Unlock()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.closing = true
	if s.miningCancel != nil {
		s.miningCancel()
		s.miningCancel = nil
	}
	if s.tipChanged != nil {
		close(s.tipChanged)
		s.tipChanged = nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	server := s.server
	s.server = nil
	sockets := make([]*peerSocket, 0, len(s.sockets))
	for socket := range s.sockets {
		sockets = append(sockets, socket)
	}
	directoryLock := s.directoryLock
	s.directoryLock = nil
	chain := s.ledger
	material := s.material
	done := s.closeDone
	s.mu.Unlock()

	for _, socket := range sockets {
		socket.stop()
	}
	var shutdownErr error
	if server != nil {
		shutdownErr = server.Shutdown(ctx)
		if shutdownErr != nil {
			shutdownErr = errors.Join(shutdownErr, server.Close())
		}
	}
	if transport, ok := s.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	waitDone := make(chan struct{})
	go func() {
		s.wait.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
		return errors.Join(shutdownErr, s.finishClose(chain, material, directoryLock, done))
	case <-ctx.Done():
		if server != nil {
			_ = server.Close()
		}
		go func() {
			<-waitDone
			if err := s.finishClose(chain, material, directoryLock, done); err != nil {
				s.setError(err)
			}
		}()
		return errors.Join(shutdownErr, ctx.Err())
	}
}

func (s *Service) finishClose(chain *ledger.Ledger, material *vault.Material, directoryLock *store.DirectoryLock, done chan struct{}) error {
	var closeErr error
	if chain != nil {
		closeErr = errors.Join(closeErr, chain.Close())
	}
	if material != nil {
		material.Clear()
	}
	if directoryLock != nil {
		closeErr = errors.Join(closeErr, directoryLock.Close())
	}
	s.mu.Lock()
	s.ledger = nil
	s.material = nil
	s.wallet.PrivateKey = ""
	s.mu.Unlock()
	close(done)
	return closeErr
}

func (s *Service) Address() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wallet.Address
}

func (s *Service) ActualAddress() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.actualAddress
}

func (s *Service) DataDirectory() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.store == nil {
		return ""
	}
	return s.store.Directory()
}

func (s *Service) Dashboard() (Dashboard, error) {
	s.walletMutationMu.Lock()
	s.mu.Lock()
	if s.closing || s.ledger == nil {
		s.mu.Unlock()
		s.walletMutationMu.Unlock()
		return Dashboard{}, fmt.Errorf("node is closed")
	}
	s.wait.Add(1)
	defer s.wait.Done()
	chain := s.ledger
	address := s.wallet.Address
	peers := s.peerSummariesLocked()
	mining := s.mining
	peerSyncActive := len(s.syncing) > 0
	listenAddress := s.actualAddress
	lastError := s.lastError
	walletNeedsBackup := s.walletNeedsBackup
	seedMode := s.seedMode
	pruneDepth := s.pruneDepth
	bootstrapEnabled := len(s.bootstrapPeers) > 0 || len(s.bootstrapURLs) > 0
	bootstrapReady := len(s.bootstrapPeers) > 0 || !s.bootstrapSuccess.IsZero()
	bootstrapLastUpdate := int64(0)
	if !s.bootstrapSuccess.IsZero() {
		bootstrapLastUpdate = s.bootstrapSuccess.Unix()
	}
	bootstrapError := s.bootstrapError
	s.mu.Unlock()
	wallets := make([]WalletProfile, 0)
	if !seedMode {
		var err error
		wallets, err = listWalletProfiles(s.store, address)
		if err != nil {
			s.walletMutationMu.Unlock()
			return Dashboard{}, err
		}
	}
	s.walletMutationMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tip, err := chain.Tip(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	confirmed, spendable, err := chain.Balances(ctx, address)
	if err != nil {
		return Dashboard{}, err
	}
	pending, err := chain.MempoolCount(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	recentBlocks, err := chain.RecentBlocks(ctx, 8)
	if err != nil {
		return Dashboard{}, err
	}
	prunedThrough, err := chain.PrunedThrough(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	recent := make([]BlockSummary, 0, len(recentBlocks))
	for _, block := range recentBlocks {
		recent = append(recent, BlockSummary{
			Height:       block.Height,
			Hash:         block.Hash,
			Timestamp:    block.Timestamp,
			Transactions: len(block.Transactions),
			Difficulty:   block.Difficulty,
		})
	}
	online := 0
	bestPeerHeight := uint64(0)
	recentPeerCutoff := time.Now().Add(-max(3*s.syncInterval, time.Minute)).Unix()
	for _, peer := range peers {
		if peer.Online && peer.ActiveOutbound && peer.LastSeen >= recentPeerCutoff {
			online++
		}
		if peer.Height > bestPeerHeight {
			bestPeerHeight = peer.Height
		}
	}
	return Dashboard{
		Name:                core.ProductName,
		Symbol:              core.ChainSymbol,
		Protocol:            ledger.ProtocolName,
		ConsensusVersion:    core.BlockVersion(tip.Height + 1),
		ConsensusActivation: core.ConsensusUpgradeHeight,
		Address:             address,
		ConfirmedBalance:    core.FormatAmount(confirmed),
		SpendableBalance:    core.FormatAmount(spendable),
		Height:              tip.Height,
		TipHash:             tip.Hash,
		Difficulty:          tip.Difficulty,
		PendingCount:        pending,
		PeerCount:           online,
		ConfiguredPeerCount: len(peers),
		Peers:               peers,
		Mining:              mining,
		Syncing:             chainSyncInProgress(peerSyncActive, tip.Height, bestPeerHeight),
		BestPeerHeight:      bestPeerHeight,
		ListenAddress:       listenAddress,
		Issued:              core.FormatAmount(core.MintedThrough(tip.Height)),
		MaxSupply:           core.FormatAmount(core.MaxSupply),
		TargetBlockSeconds:  core.TargetBlockSeconds,
		EmissionBlocks:      core.EmissionBlocks,
		NextSubsidy:         core.FormatAmount(core.Subsidy(tip.Height + 1)),
		LastError:           lastError,
		DatabasePath:        chain.Path(),
		DatabaseBytes:       databaseSize(chain.Path()),
		PrunedThrough:       prunedThrough,
		PruneDepth:          pruneDepth,
		ArchiveMode:         prunedThrough == 0 && pruneDepth == 0,
		WalletNeedsBackup:   walletNeedsBackup,
		Wallets:             wallets,
		BootstrapEnabled:    bootstrapEnabled,
		BootstrapReady:      bootstrapReady,
		BootstrapLastUpdate: bootstrapLastUpdate,
		BootstrapError:      bootstrapError,
		RecentBlocks:        recent,
	}, nil
}

func chainSyncInProgress(peerSyncActive bool, localHeight, bestPeerHeight uint64) bool {
	return peerSyncActive && bestPeerHeight > localHeight
}

func databaseSize(path string) int64 {
	var total int64
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if info, err := os.Stat(candidate); err == nil {
			total += info.Size()
		}
	}
	return total
}

func (s *Service) Send(to, amountText, feeText string) (core.Transaction, error) {
	amount, err := core.ParseAmount(amountText)
	if err != nil {
		return core.Transaction{}, err
	}
	fee, err := core.ParseAmount(feeText)
	if err != nil {
		return core.Transaction{}, err
	}
	transaction, _, err := s.sendTransaction(to, amount, &fee)
	return transaction, err
}

func (s *Service) SendRecommended(to, amountText string) (core.Transaction, uint64, error) {
	amount, err := core.ParseAmount(amountText)
	if err != nil {
		return core.Transaction{}, 0, err
	}
	return s.sendTransaction(to, amount, nil)
}

func (s *Service) sendTransaction(to string, amount uint64, fixedFee *uint64) (core.Transaction, uint64, error) {
	s.walletMutationMu.Lock()
	defer s.walletMutationMu.Unlock()
	s.mu.Lock()
	if s.closing || s.ledger == nil {
		s.mu.Unlock()
		return core.Transaction{}, 0, fmt.Errorf("node is closed")
	}
	if s.seedMode {
		s.mu.Unlock()
		return core.Transaction{}, 0, ErrSeedModeWalletUnavailable
	}
	s.wait.Add(1)
	chain := s.ledger
	wallet := s.wallet
	s.mu.Unlock()
	defer s.wait.Done()
	utxo, err := chain.SpendableUTXO(context.Background(), wallet.Address)
	if err != nil {
		return core.Transaction{}, 0, err
	}
	fee := ledger.MinimumRelayFee(1)
	if fixedFee != nil {
		fee = *fixedFee
	}
	var transaction core.Transaction
	for attempt := 0; ; attempt++ {
		transaction, err = core.BuildTransaction(&wallet, to, amount, fee, utxo)
		if err != nil {
			return core.Transaction{}, 0, err
		}
		if fixedFee != nil {
			break
		}
		required := ledger.MinimumRelayFee(core.EncodedTransactionSize(transaction))
		if fee >= required {
			break
		}
		if attempt >= core.MaxTransactionInputs {
			return core.Transaction{}, 0, fmt.Errorf("automatic fee calculation did not converge")
		}
		fee = required
	}
	if err := chain.AddTransaction(context.Background(), transaction); err != nil {
		return core.Transaction{}, 0, err
	}
	s.broadcastTransaction(transaction, nil)
	return transaction, fee, nil
}

func (s *Service) MineOnce(ctx context.Context) (core.Block, error) {
	s.mu.Lock()
	if s.closing || s.ledger == nil {
		s.mu.Unlock()
		return core.Block{}, fmt.Errorf("node is closed")
	}
	if s.seedMode {
		s.mu.Unlock()
		return core.Block{}, ErrSeedModeWalletUnavailable
	}
	chain := s.ledger
	address := s.wallet.Address
	tipChanged := s.tipChanged
	mineBlock := s.mineBlock
	s.miningJobs++
	s.wait.Add(1)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.miningJobs--
		s.mu.Unlock()
		s.wait.Done()
	}()
	for {
		jobContext, cancelJob := context.WithCancelCause(ctx)
		candidate, expectedTip, err := chain.BuildMiningCandidate(jobContext, address)
		if err != nil {
			cancelJob(context.Canceled)
			return core.Block{}, miningJobError(jobContext, err)
		}
		watchDone := watchMiningTemplate(jobContext, cancelJob, tipChanged, candidate)
		block, err := mineBlock(jobContext, candidate)
		cause := context.Cause(jobContext)
		cancelJob(context.Canceled)
		<-watchDone
		if errors.Is(cause, errMiningDifficultyChanged) {
			continue
		}
		if errors.Is(cause, errMiningTipChanged) {
			return core.Block{}, errMiningTipChanged
		}
		if err != nil {
			return core.Block{}, miningJobError(jobContext, err)
		}
		if err := chain.CommitMinedBlock(ctx, block, expectedTip); err != nil {
			return core.Block{}, err
		}
		s.notifyTipChanged()
		s.maybePrune(chain)
		s.broadcastBlock(block, nil)
		return block, nil
	}
}

var (
	errMiningTipChanged        = errors.New("mining template became stale after a chain tip change")
	errMiningDifficultyChanged = errors.New("mining template became stale after a difficulty decrease")
)

const miningDifficultyPollInterval = 5 * time.Second

func watchMiningTemplate(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	tipChanged <-chan struct{},
	candidate core.Block,
) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if core.RulesAtHeight(candidate.Height).Difficulty != core.IntegerASERTDifficulty {
			select {
			case <-tipChanged:
				cancel(errMiningTipChanged)
			case <-ctx.Done():
			}
			return
		}
		ticker := time.NewTicker(miningDifficultyPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-tipChanged:
				cancel(errMiningTipChanged)
				return
			case now := <-ticker.C:
				if miningTemplateNeedsRefresh(candidate, now.Unix()) {
					cancel(errMiningDifficultyChanged)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return done
}

func miningTemplateNeedsRefresh(candidate core.Block, now int64) bool {
	if core.RulesAtHeight(candidate.Height).Difficulty != core.IntegerASERTDifficulty || candidate.Difficulty <= core.MinimumDifficulty {
		return false
	}
	timestamp := max(now, candidate.Timestamp)
	return core.ExpectedDifficultyAt(nil, candidate.Height, timestamp) < candidate.Difficulty
}

func miningJobError(ctx context.Context, err error) error {
	if err != nil {
		cause := context.Cause(ctx)
		if errors.Is(cause, errMiningTipChanged) || errors.Is(cause, errMiningDifficultyChanged) {
			return cause
		}
	}
	return err
}

func (s *Service) notifyTipChanged() {
	s.mu.Lock()
	if s.tipChanged != nil {
		close(s.tipChanged)
		s.tipChanged = make(chan struct{})
	}
	s.mu.Unlock()
}

func (s *Service) PruneLedger(retainRecent uint64) (uint64, error) {
	s.mu.Lock()
	if s.closing || s.ledger == nil {
		s.mu.Unlock()
		return 0, fmt.Errorf("node is closed")
	}
	s.wait.Add(1)
	chain := s.ledger
	s.mu.Unlock()
	defer s.wait.Done()
	if err := chain.SetPruneDepth(context.Background(), retainRecent); err != nil {
		return 0, err
	}
	height, err := chain.PrunedThrough(context.Background())
	if err != nil {
		return 0, err
	}
	if retainRecent > 0 {
		height, err = chain.Prune(context.Background(), retainRecent)
		if err != nil {
			return 0, err
		}
	}
	s.mu.Lock()
	s.pruneDepth = retainRecent
	s.mu.Unlock()
	return height, nil
}

func (s *Service) maybePrune(chain *ledger.Ledger) {
	s.mu.RLock()
	depth := s.pruneDepth
	s.mu.RUnlock()
	if depth == 0 {
		return
	}
	if _, err := chain.Prune(context.Background(), depth); err != nil {
		s.setError(err)
		s.recordHealth("prune_failed", "error", err.Error(), "Check free disk space and database health")
	}
}

func (s *Service) HealthEvents(activeOnly bool, limit int) ([]ledger.HealthEvent, error) {
	s.mu.Lock()
	if s.closing || s.ledger == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("node is closed")
	}
	s.wait.Add(1)
	chain := s.ledger
	s.mu.Unlock()
	defer s.wait.Done()
	return chain.HealthEvents(context.Background(), activeOnly, limit)
}

func (s *Service) ResolveHealthEvent(id int64) error {
	s.mu.Lock()
	if s.closing || s.ledger == nil {
		s.mu.Unlock()
		return fmt.Errorf("node is closed")
	}
	s.wait.Add(1)
	chain := s.ledger
	s.mu.Unlock()
	defer s.wait.Done()
	if err := chain.ResolveHealthEvent(context.Background(), id); err != nil {
		return err
	}
	s.mu.Lock()
	clear(s.resyncPauses)
	s.resyncRequired = false
	s.mu.Unlock()
	return nil
}

func (s *Service) recordHealth(code, severity, message, action string) {
	s.mu.RLock()
	chain := s.ledger
	closing := s.closing
	s.mu.RUnlock()
	if closing || chain == nil {
		return
	}
	s.launch(func() {
		events, err := chain.HealthEvents(context.Background(), true, 100)
		if err == nil {
			for _, event := range events {
				if event.Code == code && event.Message == message {
					return
				}
			}
		}
		_, _ = chain.AddHealthEvent(context.Background(), ledger.HealthEvent{
			Code: code, Severity: severity, Message: message, Action: action,
		})
	})
}

func (s *Service) StartMining() error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return fmt.Errorf("node is closed")
	}
	if s.seedMode {
		s.mu.Unlock()
		return ErrSeedModeWalletUnavailable
	}
	if s.mining {
		s.mu.Unlock()
		return nil
	}
	parent := s.rootContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	s.mining = true
	s.miningCancel = cancel
	s.wait.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wait.Done()
		defer func() {
			s.mu.Lock()
			s.mining = false
			s.miningCancel = nil
			s.mu.Unlock()
		}()
		for {
			if _, err := s.MineOnce(ctx); err != nil {
				if errors.Is(err, errMiningTipChanged) {
					continue
				}
				if errors.Is(err, context.Canceled) {
					return
				}
				if !errors.Is(err, ledger.ErrStaleTip) {
					s.setError(err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(250 * time.Millisecond):
				}
			}
		}
	}()
	return nil
}

func (s *Service) StopMining() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.miningCancel != nil {
		s.miningCancel()
	}
}

func (s *Service) TransactionHistory(limit int) ([]TransactionSummary, error) {
	return s.FilteredTransactionHistory(limit, string(ledger.TransactionHistoryAll))
}

func (s *Service) FilteredTransactionHistory(limit int, filter string) ([]TransactionSummary, error) {
	if limit <= 0 || limit > 500 {
		return nil, fmt.Errorf("history limit must be between 1 and 500")
	}
	s.mu.Lock()
	if s.closing || s.ledger == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("node is closed")
	}
	s.wait.Add(1)
	chain := s.ledger
	address := s.wallet.Address
	s.mu.Unlock()
	defer s.wait.Done()
	records, err := chain.FilteredTransactionHistory(
		context.Background(),
		address,
		limit,
		ledger.TransactionHistoryFilter(filter),
	)
	if err != nil {
		return nil, err
	}
	result := make([]TransactionSummary, 0, len(records))
	for _, record := range records {
		inputs := make([]TransactionInputSummary, 0, len(record.Transaction.Inputs))
		for _, input := range record.Transaction.Inputs {
			inputs = append(inputs, TransactionInputSummary{TransactionID: input.TxID, OutputIndex: input.OutputIndex})
		}
		outputs := make([]TransactionOutputSummary, 0, len(record.Transaction.Outputs))
		for index, output := range record.Transaction.Outputs {
			outputs = append(outputs, TransactionOutputSummary{
				Index: uint32(index), Address: output.Address, Amount: core.FormatAmount(output.Amount),
			})
		}
		var blockPosition *int
		if record.BlockHeight != nil {
			position := record.Position
			blockPosition = &position
		}
		result = append(result, TransactionSummary{
			ID:            record.ID,
			BlockHeight:   record.BlockHeight,
			BlockHash:     record.BlockHash,
			BlockPosition: blockPosition,
			Pending:       record.Pending,
			Coinbase:      record.Coinbase,
			Pruned:        record.Pruned,
			Confirmations: record.Confirmations,
			Timestamp:     record.Timestamp,
			Received:      core.FormatAmount(record.Received),
			Sent:          core.FormatAmount(record.Sent),
			Inputs:        inputs,
			Outputs:       outputs,
		})
	}
	return result, nil
}

func (s *Service) setError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.lastError = err.Error()
	s.mu.Unlock()
}

func (s *Service) launch(task func()) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.wait.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wait.Done()
		task()
	}()
}

func (s *Service) peerSummariesLocked() []PeerSummary {
	peers := make([]PeerSummary, 0, len(s.peers))
	for _, peer := range s.peers {
		lastSeen := int64(0)
		if !peer.LastSeen.IsZero() {
			lastSeen = peer.LastSeen.Unix()
		}
		peers = append(peers, PeerSummary{
			URL:            peer.URL,
			Online:         peer.Online,
			Height:         peer.Height,
			Failures:       peer.Failures,
			LastError:      peer.LastError,
			Discovered:     peer.Discovered,
			Public:         peer.Public,
			Bootstrap:      peer.Bootstrap,
			ActiveOutbound: peer.ActiveOutbound,
			LastSeen:       lastSeen,
		})
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].URL < peers[j].URL })
	return peers
}
