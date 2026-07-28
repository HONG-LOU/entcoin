package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"

	"github.com/gorilla/websocket"
)

func TestTwoNodesPropagateTransactionAndBlockIncrementally(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	nodeA := newTestNode(t)
	nodeB := newTestNode(t)
	startTestNode(t, ctx, nodeA)
	startTestNode(t, ctx, nodeB)
	peerA := "http://" + nodeA.ActualAddress()
	peerB := "http://" + nodeB.ActualAddress()
	if err := nodeA.AddPeer(peerB); err != nil {
		t.Fatal(err)
	}
	if err := nodeB.AddPeer(peerA); err != nil {
		t.Fatal(err)
	}

	block1, err := nodeA.MineOnce(ctx)
	if err != nil {
		t.Fatalf("node A mine: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool {
		dashboard, err := nodeB.Dashboard()
		return err == nil && dashboard.Height == 1
	})

	// Transaction relay is independent of coinbase maturity, which has its own
	// consensus tests. Make this one funding output ordinary only inside both
	// temporary test databases so no replayable mainnet prefix is published.
	for _, service := range []*Service{nodeA, nodeB} {
		database, err := sql.Open("sqlite", service.ledger.Path())
		if err != nil {
			t.Fatalf("open relay test database: %v", err)
		}
		result, updateErr := database.ExecContext(
			ctx,
			"UPDATE utxos SET coinbase = 0 WHERE tx_id = ?",
			block1.Transactions[0].ID,
		)
		closeErr := database.Close()
		if updateErr != nil {
			t.Fatalf("prepare relay funding UTXO: %v", updateErr)
		}
		if closeErr != nil {
			t.Fatalf("close relay test database: %v", closeErr)
		}
		rows, err := result.RowsAffected()
		if err != nil || rows != 1 {
			t.Fatalf("updated relay funding rows = %d, err %v", rows, err)
		}
	}

	slowPeer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
		writer.WriteHeader(http.StatusOK)
	}))
	defer slowPeer.Close()
	if err := nodeA.AddPeer(slowPeer.URL); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	transaction, fee, err := nodeA.SendRecommended(nodeB.Address(), "0.02")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if want := ledger.MinimumRelayFee(core.EncodedTransactionSize(transaction)); fee != want {
		t.Fatalf("automatic fee = %d, want %d", fee, want)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("local transaction acceptance waited on a slow peer for %s", elapsed)
	}
	waitFor(t, 3*time.Second, func() bool {
		dashboard, err := nodeB.Dashboard()
		return err == nil && dashboard.PendingCount == 1 && dashboard.SpendableBalance == "0.02000000"
	})

	if _, err := nodeB.MineOnce(ctx); err != nil {
		t.Fatalf("node B mine: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool {
		dashboard, err := nodeA.Dashboard()
		return err == nil && dashboard.Height == 2 && strings.HasPrefix(dashboard.ConfirmedBalance, "0.043")
	})
	history, err := nodeB.TransactionHistory(20)
	if err != nil {
		t.Fatal(err)
	}
	if !historyContains(history, transaction.ID, false) {
		t.Fatal("confirmed transaction was missing from wallet history")
	}
	for _, summary := range history {
		if summary.ID != transaction.ID {
			continue
		}
		if summary.BlockHeight == nil || summary.BlockHash == "" || summary.BlockPosition == nil || summary.Pruned {
			t.Fatalf("confirmed transaction metadata is incomplete: %+v", summary)
		}
		if len(summary.Inputs) != len(transaction.Inputs) || len(summary.Outputs) != len(transaction.Outputs) {
			t.Fatalf("transaction detail input/output count = %d/%d, want %d/%d", len(summary.Inputs), len(summary.Outputs), len(transaction.Inputs), len(transaction.Outputs))
		}
		if summary.Inputs[0].TransactionID != transaction.Inputs[0].TxID || summary.Outputs[0].Address != transaction.Outputs[0].Address {
			t.Fatalf("transaction detail does not match transaction: %+v", summary)
		}
		return
	}
	t.Fatal("confirmed transaction details were missing")
}

func TestConsensusStatusIsSeparateFromLegacyPeerStatus(t *testing.T) {
	service := newTestNode(t)
	recorder := httptest.NewRecorder()
	service.handleConsensus(recorder, httptest.NewRequest(http.MethodGet, "/v2/consensus", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v2/consensus returned HTTP %d", recorder.Code)
	}
	var status consensusStatus
	if err := json.NewDecoder(recorder.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Protocol != ledger.ProtocolName || status.NextBlockRule != core.LegacyBlockVersion ||
		status.ActivationHeight != core.ConsensusUpgradeHeight || status.AnchorHash != core.ASERTAnchorHash ||
		status.HalfLifeSeconds != core.ASERTHalfLifeSeconds {
		t.Fatalf("unexpected consensus status: %+v", status)
	}

	legacy, err := json.Marshal(statusFromTip(ledger.Tip{Height: 1}, 47821))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy), "consensus") || strings.Contains(string(legacy), "activation") {
		t.Fatalf("legacy peer status gained incompatible fields: %s", legacy)
	}
}

func TestIncrementalSyncReorgChoosesStrongerFork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	nodeA := newTestNode(t)
	nodeB := newTestNode(t)
	common := core.NewState()
	if _, err := common.Mine(ctx, nodeA.Address()); err != nil {
		t.Fatal(err)
	}
	if err := nodeA.ledger.ImportState(ctx, common); err != nil {
		t.Fatal(err)
	}
	if err := nodeB.ledger.ImportState(ctx, common); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeA.MineOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeB.MineOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeB.MineOnce(ctx); err != nil {
		t.Fatal(err)
	}

	startTestNode(t, ctx, nodeA)
	startTestNode(t, ctx, nodeB)
	if err := nodeA.AddPeer("http://" + nodeB.ActualAddress()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, func() bool {
		dashboard, err := nodeA.Dashboard()
		return err == nil && dashboard.Height == 3
	})
	nodeATip, err := nodeA.ledger.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nodeBTip, err := nodeB.ledger.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nodeATip.Hash != nodeBTip.Hash || nodeATip.Work.Cmp(nodeBTip.Work) != 0 {
		t.Fatalf("nodes did not converge after reorg: A %#v B %#v", nodeATip, nodeBTip)
	}
}

func TestRestartPreservesProtectedWalletAndSQLiteLedger(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	directory := t.TempDir()
	first, err := New(testConfig(directory))
	if err != nil {
		t.Fatal(err)
	}
	address := first.Address()
	block, err := first.MineOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	closeTestNode(t, first)
	second, err := New(testConfig(directory))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestNode(t, second)
	dashboard, err := second.Dashboard()
	if err != nil {
		t.Fatal(err)
	}
	if second.Address() != address || dashboard.Height != block.Height || dashboard.TipHash != block.Hash {
		t.Fatalf("restart changed node state: address=%s height=%d tip=%s", second.Address(), dashboard.Height, dashboard.TipHash)
	}
}

func TestDataDirectoryLockAndImmediateClose(t *testing.T) {
	directory := t.TempDir()
	first, err := New(testConfig(directory))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(testConfig(directory)); err == nil {
		t.Fatal("second node opened the same data directory")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := first.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	closeTestNode(t, first)
	third, err := New(testConfig(directory))
	if err != nil {
		t.Fatalf("data directory remained locked after close: %v", err)
	}
	closeTestNode(t, third)
}

func TestPrunePolicyPersistsAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	first, err := New(testConfig(directory))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.PruneLedger(1); err == nil {
		t.Fatal("unsafe prune depth was accepted")
	}
	if _, err := first.PruneLedger(120); err != nil {
		t.Fatal(err)
	}
	closeTestNode(t, first)
	second, err := New(testConfig(directory))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestNode(t, second)
	dashboard, err := second.Dashboard()
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.PruneDepth != 120 || dashboard.ArchiveMode {
		t.Fatalf("restored prune policy = depth %d archive %v", dashboard.PruneDepth, dashboard.ArchiveMode)
	}
	if _, err := second.PruneLedger(0); err != nil {
		t.Fatal(err)
	}
}

func TestV1FullStateEndpointIsRemoved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := newTestNode(t)
	startTestNode(t, ctx, service)
	response, err := http.Get("http://" + service.ActualAddress() + "/v1/state")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("legacy full-state endpoint returned HTTP %d", response.StatusCode)
	}
}

func TestRejectsUnsafePeerURLs(t *testing.T) {
	unsafe := []string{
		"http://<svg&Tab;onload=alert(1)>",
		"http://user:pass@localhost:47821",
		"http://localhost:0",
		"http://localhost:70000",
		"http://localhost:47821/path",
	}
	for _, peer := range unsafe {
		if _, err := normalizePeer(peer); err == nil {
			t.Fatalf("unsafe peer URL was accepted: %s", peer)
		}
	}
	for _, peer := range []string{"http://localhost:47821", "http://127.0.0.1:47821", "http://[::1]:47821"} {
		if _, err := normalizePeer(peer); err != nil {
			t.Fatalf("valid peer URL %s rejected: %v", peer, err)
		}
	}
}

func TestProtocolJSONRejectsDuplicateUnknownAndDeepFields(t *testing.T) {
	tests := []string{
		`{"locator":[],"limit":1,"limit":2}`,
		`{"locator":[],"limit":1,"unexpected":true}`,
		`{"locator":[],"limit":1} {}`,
		strings.Repeat("[", 130) + strings.Repeat("]", 130),
	}
	for _, body := range tests {
		var request headersRequest
		if err := decodeLimitedJSON(strings.NewReader(body), 16<<10, &request); err == nil {
			t.Fatalf("unsafe JSON was accepted: %.80s", body)
		}
	}
	var valid headersRequest
	if err := decodeLimitedJSON(strings.NewReader(`{"locator":["hash"],"limit":1}`), 16<<10, &valid); err != nil {
		t.Fatalf("valid JSON was rejected: %v", err)
	}
}

func TestWebSocketPerIPConnectionLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := newTestNode(t)
	startTestNode(t, ctx, service)
	endpoint := "ws://" + service.ActualAddress() + "/v2/p2p"
	connections := make([]*websocket.Conn, 0, 4)
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()
	for range 4 {
		connection, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
	}
	connection, response, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("fifth websocket = response %v, err %v", response, err)
	}
	_ = response.Body.Close()
}

func newTestNode(t *testing.T) *Service {
	t.Helper()
	service, err := New(testConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestNode(t, service) })
	return service
}

func testConfig(directory string) Config {
	return Config{
		DataDirectory:    directory,
		ListenAddress:    "127.0.0.1:0",
		SyncInterval:     100 * time.Millisecond,
		DisableDiscovery: true,
	}
}

func startTestNode(t *testing.T, ctx context.Context, service *Service) {
	t.Helper()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
}

func closeTestNode(t *testing.T, service *Service) {
	t.Helper()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.Close(shutdown); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("close node: %v", err)
	}
}

func historyContains(history []TransactionSummary, id string, pending bool) bool {
	for _, transaction := range history {
		if transaction.ID == id && transaction.Pending == pending {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(fmt.Errorf("condition not met within %s", timeout))
}
