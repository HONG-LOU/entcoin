package entpay

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/node"
)

type managerPaymentBackend struct {
	mu          sync.Mutex
	wallet      *core.Wallet
	utxo        core.UTXO
	prepareCall int
	commitCall  int
	failCommit  bool
}

func newManagerPaymentBackend(t *testing.T) *managerPaymentBackend {
	t.Helper()
	wallet, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	return &managerPaymentBackend{
		wallet: wallet,
		utxo: core.UTXO{
			{TxID: string(bytes.Repeat([]byte{'d'}, 64)), Index: 0}: {Address: wallet.Address, Amount: 100_000},
		},
	}
}

func (b *managerPaymentBackend) Address() string { return b.wallet.Address }

func (b *managerPaymentBackend) Dashboard() (node.Dashboard, error) {
	return node.Dashboard{Address: b.wallet.Address, PeerCount: 1, Height: 10, BestPeerHeight: 10}, nil
}

func (b *managerPaymentBackend) PreviewRecommendedPayment(expectedWallet, to string, amount uint64) (node.PaymentPreview, error) {
	if !core.AddressesEqual(expectedWallet, b.wallet.Address) {
		return node.PaymentPreview{}, node.ErrPaymentWalletChanged
	}
	return node.PaymentPreview{
		Wallet: b.wallet.Address, Recipient: to, Amount: amount, EstimatedFee: 1_000,
		MaximumFee: 2_000, TotalDebit: amount + 2_000, SpendableBalance: 100_000,
		HasSufficientFunds: true,
	}, nil
}

func (b *managerPaymentBackend) PrepareRecommendedPayment(expectedWallet, to string, amount, maximumFee uint64) (core.Transaction, uint64, error) {
	b.mu.Lock()
	b.prepareCall++
	b.mu.Unlock()
	transaction, err := core.BuildTransaction(b.wallet, to, amount, 1_000, b.utxo)
	return transaction, 1_000, err
}

func (b *managerPaymentBackend) CommitPreparedPayment(_ string, _ string, _ uint64, _ uint64, _ uint64, _ core.Transaction) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.commitCall++
	if b.failCommit {
		b.failCommit = false
		return errors.New("simulated interruption")
	}
	return nil
}

func newTestClientManager(t *testing.T, fixture gatewayFixture, backend *managerPaymentBackend) (*ClientManager, *ClientStore, string) {
	t.Helper()
	fixture.gateway.publicEndpoint = fixture.server.URL + "/"
	created := createGatewayInvoice(t, fixture)
	if created.Launch == nil {
		t.Fatal("gateway did not create a launch URL")
	}
	launch := LaunchRequest{Merchant: fixture.gateway.publicEndpoint, Handoff: created.Launch.Handoff}
	protector, _ := newXChaChaProtector(bytes.Repeat([]byte{0x55}, 32))
	path := filepath.Join(t.TempDir(), "entpay-client.db")
	store, err := OpenClientStore(path, protector)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := NewClientManager(context.Background(), store, backend, 10_000, filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.Receive(context.Background(), launch)
	if err != nil {
		t.Fatal(err)
	}
	if session.Stage != StageAwaitingApproval {
		t.Fatalf("received session stage = %s", session.Stage)
	}
	return manager, store, path
}

func TestClientManagerRequiresApprovalAndKeepsSecretsEncrypted(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	backend := newManagerPaymentBackend(t)
	_, store, path := newTestClientManager(t, fixture, backend)
	sessions, err := store.Sessions(context.Background(), 10)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %d, %v", len(sessions), err)
	}
	if backend.prepareCall != 0 || backend.commitCall != 0 {
		t.Fatal("receiving a handoff invoked payment")
	}
	capsule, err := store.Capsule(context.Background(), sessions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, capsule.Input) || bytes.Contains(contents, []byte(capsule.Created.ClaimToken)) {
		t.Fatal("client database contains redeemed secrets in plaintext")
	}
}

func TestClientManagerConcurrentApprovalCommitsOnce(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	backend := newManagerPaymentBackend(t)
	manager, store, _ := newTestClientManager(t, fixture, backend)
	sessions, _ := store.Sessions(context.Background(), 10)
	session := sessions[0]
	errorsFound := make([]error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for index := range errorsFound {
		go func(index int) {
			defer wait.Done()
			_, errorsFound[index] = manager.Approve(context.Background(), session.ID, session.Revision)
		}(index)
	}
	wait.Wait()
	successes := 0
	for _, err := range errorsFound {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrClientRevision) {
			t.Fatalf("approval error = %v", err)
		}
	}
	updated, err := store.Session(context.Background(), session.ID)
	if err != nil || successes != 1 || updated.Stage != StageBroadcast || updated.TransactionID == "" || backend.prepareCall != 1 || backend.commitCall != 1 {
		t.Fatalf("approval result: successes=%d session=%+v prepare=%d commit=%d err=%v", successes, updated, backend.prepareCall, backend.commitCall, err)
	}
}

func TestClientManagerReapprovalReusesJournaledTransaction(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	backend := newManagerPaymentBackend(t)
	backend.failCommit = true
	manager, store, _ := newTestClientManager(t, fixture, backend)
	sessions, _ := store.Sessions(context.Background(), 10)
	session, err := manager.Approve(context.Background(), sessions[0].ID, sessions[0].Revision)
	if err != nil || session.Stage != StageAwaitingApproval || session.TransactionID == "" {
		t.Fatalf("interrupted approval = %+v, %v", session, err)
	}
	journaledID := session.TransactionID
	session, err = manager.Approve(context.Background(), session.ID, session.Revision)
	if err != nil || session.Stage != StageBroadcast || session.TransactionID != journaledID {
		t.Fatalf("reapproval = %+v, %v", session, err)
	}
	if backend.prepareCall != 1 || backend.commitCall != 2 {
		t.Fatalf("prepare/commit calls = %d/%d", backend.prepareCall, backend.commitCall)
	}
}

func TestClientManagerRejectNeverPays(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	backend := newManagerPaymentBackend(t)
	manager, store, _ := newTestClientManager(t, fixture, backend)
	sessions, _ := store.Sessions(context.Background(), 10)
	session, err := manager.Reject(context.Background(), sessions[0].ID, sessions[0].Revision)
	if err != nil || session.Stage != StageRejected || session.TransactionID != "" || backend.prepareCall != 0 || backend.commitCall != 0 {
		t.Fatalf("rejection = %+v, prepare=%d commit=%d, %v", session, backend.prepareCall, backend.commitCall, err)
	}
	capsule, err := store.Capsule(context.Background(), session.ID)
	if err != nil || capsule.Created.ClaimToken != "" || capsule.HandoffCode != "" {
		t.Fatalf("rejected capsule = %+v, %v", capsule, err)
	}
}

func TestClientManagerCompletesVerifiedArtifactAndScrubsCapability(t *testing.T) {
	fixture := newGatewayFixture(t, true)
	backend := newManagerPaymentBackend(t)
	manager, store, _ := newTestClientManager(t, fixture, backend)
	sessions, _ := store.Sessions(context.Background(), 10)
	session, err := manager.Approve(context.Background(), sessions[0].ID, sessions[0].Revision)
	if err != nil || session.Stage != StageBroadcast {
		t.Fatalf("approval = %+v, %v", session, err)
	}
	fixture.node.mu.Lock()
	fixture.node.confirmations = 1
	fixture.node.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err = manager.Continue(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Stage != StageComplete || session.Receipt == nil || session.ArtifactPath == "" || session.ArtifactSHA256 == "" || session.CurrentConfirmations != 1 {
		t.Fatalf("completed session is incomplete: %+v", session)
	}
	contents, err := os.ReadFile(session.ArtifactPath)
	if err != nil || string(contents) != "test-image-contents" {
		t.Fatalf("artifact = %q, %v", contents, err)
	}
	capsule, err := store.Capsule(context.Background(), session.ID)
	if err != nil || capsule.Created.ClaimToken != "" || len(capsule.Input) == 0 {
		t.Fatalf("completed capsule = %+v, %v", capsule, err)
	}
}
