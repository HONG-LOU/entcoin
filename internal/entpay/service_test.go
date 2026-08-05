package entpay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"
)

type fakeNode struct {
	mu            sync.Mutex
	transactions  map[string]core.Transaction
	confirmations uint64
	status        NodeStatus
}

func newFakeNode(t *testing.T) (*fakeNode, *httptest.Server) {
	t.Helper()
	node := &fakeNode{
		transactions: make(map[string]core.Transaction),
		status: NodeStatus{
			Protocol: core.NetworkID, Name: core.ChainName, Symbol: core.ChainSymbol,
			Height: 193_100, TipHash: strings.Repeat("0", 56) + "feedcafe", ChainWork: "863816090910720",
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v2/status":
			writeJSON(writer, http.StatusOK, node.status)
		case request.Method == http.MethodPost && request.URL.Path == "/v2/transactions":
			var transaction core.Transaction
			if err := json.NewDecoder(request.Body).Decode(&transaction); err != nil {
				writeError(writer, http.StatusBadRequest, "invalid transaction")
				return
			}
			node.mu.Lock()
			_, duplicate := node.transactions[transaction.ID]
			node.transactions[transaction.ID] = transaction
			node.mu.Unlock()
			if duplicate {
				writeError(writer, http.StatusConflict, "duplicate")
				return
			}
			writeJSON(writer, http.StatusAccepted, map[string]string{"id": transaction.ID})
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v2/wallet/"):
			node.mu.Lock()
			history := make([]ledger.TransactionRecord, 0, len(node.transactions))
			for _, transaction := range node.transactions {
				history = append(history, ledger.TransactionRecord{
					ID: transaction.ID, Pending: node.confirmations == 0,
					Confirmations: node.confirmations, Transaction: transaction,
				})
			}
			node.mu.Unlock()
			writeJSON(writer, http.StatusOK, walletSnapshot{History: history})
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	return node, server
}

type serviceFixture struct {
	service  *httptest.Server
	node     *fakeNode
	merchant *core.Wallet
	payer    *core.Wallet
	key      ed25519.PrivateKey
	store    *Store
}

func newServiceFixture(t *testing.T) serviceFixture {
	t.Helper()
	merchant, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	payer, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	_, signingKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	node, nodeServer := newFakeNode(t)
	client, err := NewNodeClient(nodeServer.URL, []string{nodeServer.URL, nodeServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(t.TempDir() + "/entpay.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, err := NewService(Config{
		MerchantAddress: merchant.Address, Price: 10_000, Confirmations: 1,
		InvoiceLifetime: 15 * time.Minute, SigningKey: signingKey,
		NodeClient: client, Store: store, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(service.Handler())
	t.Cleanup(httpServer.Close)
	return serviceFixture{service: httpServer, node: node, merchant: merchant, payer: payer, key: signingKey, store: store}
}

func (f serviceFixture) transaction(t *testing.T, amount uint64) core.Transaction {
	t.Helper()
	transaction, err := core.BuildTransaction(f.payer, f.merchant.Address, amount, 1_000, core.UTXO{
		{TxID: strings.Repeat("a", 64), Index: 0}: {Address: f.payer.Address, Amount: 100_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	return transaction
}

func createInvoice(t *testing.T, fixture serviceFixture) CreateInvoiceResponse {
	t.Helper()
	requestBody := `{"resource":"network-report","query":"compare both public nodes"}`
	response, err := http.Post(fixture.service.URL+"/v1/invoices", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("create invoice returned %d: %s", response.StatusCode, body)
	}
	var created CreateInvoiceResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created
}

func authorizedRequest(t *testing.T, method, endpoint, token string, body any) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func TestInvoicePaymentConfirmationAndIdempotentDelivery(t *testing.T) {
	fixture := newServiceFixture(t)
	created := createInvoice(t, fixture)
	if created.Invoice.Signature == "" || created.ClaimToken == "" || !verifyInvoice(fixture.key.Public().(ed25519.PublicKey), created.Invoice) {
		t.Fatal("invoice was not signed or authorized")
	}
	transaction := fixture.transaction(t, created.Invoice.Amount)
	submit := authorizedRequest(t, http.MethodPost, fixture.service.URL+"/v1/invoices/"+created.Invoice.ID+"/submit", created.ClaimToken, SubmitPaymentRequest{Transaction: transaction})
	response, err := http.DefaultClient.Do(submit)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("submit returned %d", response.StatusCode)
	}

	for range 2 {
		retry := authorizedRequest(t, http.MethodPost, fixture.service.URL+"/v1/invoices/"+created.Invoice.ID+"/submit", created.ClaimToken, SubmitPaymentRequest{Transaction: transaction})
		response, err = http.DefaultClient.Do(retry)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("idempotent submit returned %d", response.StatusCode)
		}
	}

	claim := authorizedRequest(t, http.MethodPost, fixture.service.URL+"/v1/invoices/"+created.Invoice.ID+"/claim", created.ClaimToken, struct{}{})
	response, err = http.DefaultClient.Do(claim)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unconfirmed claim returned %d", response.StatusCode)
	}
	fixture.node.mu.Lock()
	fixture.node.confirmations = 1
	fixture.node.mu.Unlock()

	var first Delivery
	for index := range 2 {
		claim = authorizedRequest(t, http.MethodPost, fixture.service.URL+"/v1/invoices/"+created.Invoice.ID+"/claim", created.ClaimToken, struct{}{})
		response, err = http.DefaultClient.Do(claim)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("claim %d returned %d", index, response.StatusCode)
		}
		var delivery Delivery
		if err := json.NewDecoder(response.Body).Decode(&delivery); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if index == 0 {
			first = delivery
		} else if delivery.Receipt != first.Receipt {
			t.Fatal("retried claim changed the signed receipt")
		}
	}
	if !first.Report.Consistent || !verifyReceipt(fixture.key.Public().(ed25519.PublicKey), first.Receipt) {
		t.Fatal("delivery report or receipt is invalid")
	}
}

func TestRejectsUnauthorizedAndWrongPayment(t *testing.T) {
	fixture := newServiceFixture(t)
	created := createInvoice(t, fixture)
	request := authorizedRequest(t, http.MethodGet, fixture.service.URL+"/v1/invoices/"+created.Invoice.ID, "wrong", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unauthorized status returned %d", response.StatusCode)
	}
	wrong := fixture.transaction(t, created.Invoice.Amount+1)
	request = authorizedRequest(t, http.MethodPost, fixture.service.URL+"/v1/invoices/"+created.Invoice.ID+"/submit", created.ClaimToken, SubmitPaymentRequest{Transaction: wrong})
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("wrong payment returned %d", response.StatusCode)
	}
}

func TestTransactionCannotPayTwoInvoices(t *testing.T) {
	fixture := newServiceFixture(t)
	first := createInvoice(t, fixture)
	second := createInvoice(t, fixture)
	transaction := fixture.transaction(t, first.Invoice.Amount)
	for index, created := range []CreateInvoiceResponse{first, second} {
		request := authorizedRequest(t, http.MethodPost, fixture.service.URL+"/v1/invoices/"+created.Invoice.ID+"/submit", created.ClaimToken, SubmitPaymentRequest{Transaction: transaction})
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		expected := http.StatusAccepted
		if index == 1 {
			expected = http.StatusConflict
		}
		if response.StatusCode != expected {
			t.Fatalf("invoice %d returned %d, want %d", index, response.StatusCode, expected)
		}
	}
}

func TestRejectsExpiredInvoice(t *testing.T) {
	fixture := newServiceFixture(t)
	created := createInvoice(t, fixture)
	if _, err := fixture.store.database.Exec(`UPDATE invoices SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Second).Unix(), created.Invoice.ID); err != nil {
		t.Fatal(err)
	}
	request := authorizedRequest(t, http.MethodPost, fixture.service.URL+"/v1/invoices/"+created.Invoice.ID+"/submit", created.ClaimToken, SubmitPaymentRequest{Transaction: fixture.transaction(t, created.Invoice.Amount)})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expired payment returned %d", response.StatusCode)
	}
}

func TestHomeIsResponsiveAndHardened(t *testing.T) {
	fixture := newServiceFixture(t)
	response, err := http.Get(fixture.service.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("EntPay Agent")) || response.Header.Get("Content-Security-Policy") == "" || response.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("unexpected home response: status=%d", response.StatusCode)
	}
}

func TestFaviconDoesNotProduceBrowserError(t *testing.T) {
	fixture := newServiceFixture(t)
	response, err := http.Get(fixture.service.URL + "/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("favicon returned %d", response.StatusCode)
	}
}

func TestCreateInvoiceRejectsAmbiguousAndOversizedJSON(t *testing.T) {
	fixture := newServiceFixture(t)
	for name, body := range map[string]string{
		"duplicate": `{"resource":"network-report","resource":"network-report","query":"compare nodes"}`,
		"unknown":   `{"resource":"network-report","query":"compare nodes","admin":true}`,
		"oversized": `{"resource":"network-report","query":"` + strings.Repeat("x", maxRequestBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response, err := http.Post(fixture.service.URL+"/v1/invoices", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("invalid JSON returned %d", response.StatusCode)
			}
		})
	}
}

type fakeReasoner struct{}

func (fakeReasoner) Approve(context.Context, Invoice, Info, string, uint64) (Decision, error) {
	return Decision{Approved: true, Reason: "invoice matches the enforced policy"}, nil
}

func (fakeReasoner) Analyze(_ context.Context, delivery Delivery) (string, error) {
	return "两个节点一致，高度相同。", nil
}

func TestAgentCompletesSignedPaymentFlow(t *testing.T) {
	fixture := newServiceFixture(t)
	fixture.node.mu.Lock()
	fixture.node.confirmations = 1
	fixture.node.mu.Unlock()
	result, err := RunAgent(context.Background(), AgentConfig{
		Endpoint: fixture.service.URL, Query: "compare both public nodes",
		MaximumAmount: 10_000, PaymentTimeout: 5 * time.Second, Reasoner: fakeReasoner{},
		Pay: func(_ context.Context, merchant string, amount uint64) (core.Transaction, error) {
			if merchant != fixture.merchant.Address || amount != 10_000 {
				t.Fatalf("unexpected payment: %s %d", merchant, amount)
			}
			return fixture.transaction(t, amount), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Decision.Approved || result.Transaction == "" || result.Analysis == "" || !result.Delivery.Report.Consistent {
		t.Fatalf("incomplete agent result: %+v", result)
	}
}

func TestAgentWaitsForConfirmation(t *testing.T) {
	fixture := newServiceFixture(t)
	go func() {
		time.Sleep(250 * time.Millisecond)
		fixture.node.mu.Lock()
		fixture.node.confirmations = 1
		fixture.node.mu.Unlock()
	}()
	result, err := RunAgent(context.Background(), AgentConfig{
		Endpoint: fixture.service.URL, Query: "compare both public nodes",
		MaximumAmount: 10_000, PaymentTimeout: 5 * time.Second, Reasoner: fakeReasoner{},
		Pay: func(_ context.Context, _ string, amount uint64) (core.Transaction, error) {
			return fixture.transaction(t, amount), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Delivery.Receipt.InvoiceID == "" || result.Delivery.Receipt.TransactionID != result.Transaction {
		t.Fatalf("agent returned an invalid delayed delivery: %+v", result.Delivery)
	}
}
