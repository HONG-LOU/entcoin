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
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"
)

type testProduct struct {
	calls    atomic.Uint64
	artifact bool
}

func (p *testProduct) Descriptor() ProductDescriptor {
	return ProductDescriptor{
		ID: "test-resource", Name: "Test resource", Description: "A deterministic test merchant resource.",
		Price: 10_000, Confirmations: 1, Accent: "coral",
		Fields: []InputField{{ID: "prompt", Label: "Prompt", Type: "textarea", MinLength: 3, MaxLength: 200, Required: true}},
	}
}

func (p *testProduct) Validate(_ context.Context, input json.RawMessage) error {
	var value struct {
		Prompt string `json:"prompt"`
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || len(strings.TrimSpace(value.Prompt)) < 3 {
		return PermanentFailure(io.ErrUnexpectedEOF)
	}
	return nil
}

func (p *testProduct) Fulfill(_ context.Context, request FulfillmentRequest) (Fulfillment, error) {
	p.calls.Add(1)
	result := Fulfillment{Payload: json.RawMessage(`{"result":"delivered"}`)}
	if p.artifact {
		result.Artifact = &Artifact{Contents: []byte("test-image-contents"), MediaType: "image/jpeg", FileName: "result.jpg"}
	}
	return result, nil
}

type gatewayNode struct {
	mu            sync.Mutex
	transactions  map[string]core.Transaction
	confirmations uint64
}

func newGatewayNode(t *testing.T) (*gatewayNode, *httptest.Server) {
	t.Helper()
	node := &gatewayNode{transactions: make(map[string]core.Transaction)}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v2/status":
			writeJSON(writer, http.StatusOK, nodeStatus{Protocol: core.NetworkID, Height: 42, TipHash: strings.Repeat("a", 64)})
		case request.Method == http.MethodPost && request.URL.Path == "/v2/transactions":
			var transaction core.Transaction
			if json.NewDecoder(request.Body).Decode(&transaction) != nil {
				writeError(writer, http.StatusBadRequest, "invalid transaction")
				return
			}
			node.mu.Lock()
			_, exists := node.transactions[transaction.ID]
			node.transactions[transaction.ID] = transaction
			node.mu.Unlock()
			if exists {
				writeError(writer, http.StatusConflict, "duplicate transaction")
				return
			}
			writeJSON(writer, http.StatusAccepted, map[string]string{"id": transaction.ID})
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v2/wallet/"):
			node.mu.Lock()
			history := make([]ledger.TransactionRecord, 0, len(node.transactions))
			for _, transaction := range node.transactions {
				history = append(history, ledger.TransactionRecord{ID: transaction.ID, Pending: node.confirmations == 0, Confirmations: node.confirmations, Transaction: transaction})
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

type gatewayFixture struct {
	server   *httptest.Server
	gateway  *Gateway
	node     *gatewayNode
	product  *testProduct
	merchant *core.Wallet
	payer    *core.Wallet
	key      ed25519.PrivateKey
}

func newGatewayFixture(t *testing.T, artifact bool) gatewayFixture {
	t.Helper()
	merchant, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	payer, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	node, nodeServer := newGatewayNode(t)
	product := &testProduct{artifact: artifact}
	directory := t.TempDir()
	gateway, err := NewGateway(MerchantConfig{
		MerchantAddress: merchant.Address, SigningKey: key, NodeURL: nodeServer.URL,
		DatabasePath: directory + "/entpay.db", FulfillmentDirectory: directory + "/fulfillments",
		Products: []Product{product}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = gateway.Close(ctx)
	})
	server := httptest.NewServer(gateway.Handler())
	t.Cleanup(server.Close)
	return gatewayFixture{server: server, gateway: gateway, node: node, product: product, merchant: merchant, payer: payer, key: key}
}

func (f gatewayFixture) transaction(t *testing.T, amount uint64) core.Transaction {
	t.Helper()
	transaction, err := core.BuildTransaction(f.payer, f.merchant.Address, amount, 1_000, core.UTXO{{TxID: strings.Repeat("b", 64), Index: 0}: {Address: f.payer.Address, Amount: 100_000}})
	if err != nil {
		t.Fatal(err)
	}
	return transaction
}

func gatewayRequest(t *testing.T, method, endpoint, token string, value any) *http.Request {
	t.Helper()
	var body io.Reader
	if value != nil {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if value != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}

func createGatewayInvoice(t *testing.T, fixture gatewayFixture) CreateInvoiceResponse {
	t.Helper()
	request := gatewayRequest(t, http.MethodPost, fixture.server.URL+"/v1/invoices", "", CreateInvoiceRequest{Resource: "test-resource", Input: json.RawMessage(`{"prompt":"make a test image"}`)})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		contents, _ := io.ReadAll(response.Body)
		t.Fatalf("create invoice returned %d: %s", response.StatusCode, contents)
	}
	var created CreateInvoiceResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created
}

func TestGatewayPaymentConfirmationReceiptAndArtifact(t *testing.T) {
	fixture := newGatewayFixture(t, true)
	created := createGatewayInvoice(t, fixture)
	if !verifyInvoice(fixture.key.Public().(ed25519.PublicKey), created.Invoice) || created.ClaimToken == "" {
		t.Fatal("invoice signature or claim token is invalid")
	}
	transaction := fixture.transaction(t, created.Invoice.Amount)
	encoded, _ := json.Marshal(transaction)
	submit := gatewayRequest(t, http.MethodPost, fixture.server.URL+"/v1/invoices/"+created.Invoice.ID+"/submit", created.ClaimToken, SubmitPaymentRequest{Transaction: encoded})
	response, err := http.DefaultClient.Do(submit)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("submit returned %d", response.StatusCode)
	}
	claimEndpoint := fixture.server.URL + "/v1/invoices/" + created.Invoice.ID + "/claim"
	claim := gatewayRequest(t, http.MethodPost, claimEndpoint, created.ClaimToken, struct{}{})
	response, err = http.DefaultClient.Do(claim)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted || fixture.product.calls.Load() != 0 {
		t.Fatalf("unconfirmed claim status=%d calls=%d", response.StatusCode, fixture.product.calls.Load())
	}
	fixture.node.mu.Lock()
	fixture.node.confirmations = 1
	fixture.node.mu.Unlock()

	var delivery Delivery
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		claim = gatewayRequest(t, http.MethodPost, claimEndpoint, created.ClaimToken, struct{}{})
		response, err = http.DefaultClient.Do(claim)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&delivery); err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			break
		}
		response.Body.Close()
		time.Sleep(10 * time.Millisecond)
	}
	if delivery.Receipt.InvoiceID == "" || !verifyReceipt(fixture.key.Public().(ed25519.PublicKey), delivery.Receipt) || fixture.product.calls.Load() != 1 {
		t.Fatalf("invalid or duplicate delivery: calls=%d delivery=%+v", fixture.product.calls.Load(), delivery)
	}
	payload, _ := canonicalPayload(delivery.Payload)
	if contentHash(payload) != delivery.Receipt.PayloadSHA256 || delivery.Artifact == nil || delivery.Artifact.SHA256 != delivery.Receipt.ArtifactSHA256 {
		t.Fatal("delivery hashes are not bound to the receipt")
	}

	unauthorized := gatewayRequest(t, http.MethodGet, fixture.server.URL+"/v1/invoices/"+created.Invoice.ID+"/artifact", "wrong", nil)
	response, err = http.DefaultClient.Do(unauthorized)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unauthorized artifact returned %d", response.StatusCode)
	}
	rangeRequest := gatewayRequest(t, http.MethodGet, fixture.server.URL+"/v1/invoices/"+created.Invoice.ID+"/artifact", created.ClaimToken, nil)
	rangeRequest.Header.Set("Range", "bytes=0-3")
	response, err = http.DefaultClient.Do(rangeRequest)
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusPartialContent || string(contents) != "test" || response.Header.Get("ETag") == "" {
		t.Fatalf("range artifact status=%d body=%q", response.StatusCode, contents)
	}
}

func TestGatewayConcurrentClaimsFulfillOnceAndTransactionCannotReplay(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	first := createGatewayInvoice(t, fixture)
	second := createGatewayInvoice(t, fixture)
	transaction := fixture.transaction(t, first.Invoice.Amount)
	encoded, _ := json.Marshal(transaction)
	for index, created := range []CreateInvoiceResponse{first, second} {
		request := gatewayRequest(t, http.MethodPost, fixture.server.URL+"/v1/invoices/"+created.Invoice.ID+"/submit", created.ClaimToken, SubmitPaymentRequest{Transaction: encoded})
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
			t.Fatalf("submit %d returned %d, want %d", index, response.StatusCode, expected)
		}
	}
	fixture.node.mu.Lock()
	fixture.node.confirmations = 1
	fixture.node.mu.Unlock()
	var wait sync.WaitGroup
	for range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			request := gatewayRequest(t, http.MethodPost, fixture.server.URL+"/v1/invoices/"+first.Invoice.ID+"/claim", first.ClaimToken, struct{}{})
			response, err := http.DefaultClient.Do(request)
			if err == nil {
				response.Body.Close()
			}
		}()
	}
	wait.Wait()
	deadline := time.Now().Add(time.Second)
	for fixture.product.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fixture.product.calls.Load() != 1 {
		t.Fatalf("concurrent claims fulfilled %d times", fixture.product.calls.Load())
	}
}

func TestInvoiceAndReceiptSignaturesDetectTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	invoice := Invoice{Protocol: ProtocolVersion, Network: core.NetworkID, ID: strings.Repeat("a", 32), Merchant: "merchant", Amount: 10, Resource: "resource-id", InputSHA256: strings.Repeat("b", 64), ExpiresAt: time.Now().UTC().Truncate(time.Second), Confirmations: 1}
	signInvoice(privateKey, &invoice)
	if !verifyInvoice(publicKey, invoice) {
		t.Fatal("valid invoice signature was rejected")
	}
	invoice.Amount++
	if verifyInvoice(publicKey, invoice) {
		t.Fatal("tampered invoice signature was accepted")
	}
	receipt := Receipt{Protocol: ProtocolVersion, InvoiceID: strings.Repeat("c", 32), TransactionID: "tx", Resource: "resource-id", InputSHA256: strings.Repeat("d", 64), PayloadSHA256: strings.Repeat("e", 64), DeliveredAt: time.Now().UTC().Truncate(time.Second)}
	signReceipt(privateKey, &receipt)
	if !verifyReceipt(publicKey, receipt) {
		t.Fatal("valid receipt signature was rejected")
	}
	receipt.PayloadSHA256 = strings.Repeat("f", 64)
	if verifyReceipt(publicKey, receipt) {
		t.Fatal("tampered receipt signature was accepted")
	}
}

func TestGatewayRejectsAmbiguousJSONAndInvalidInput(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	deep := strings.Repeat(`{"x":`, 33) + `0` + strings.Repeat(`}`, 33)
	cases := map[string]string{
		"duplicate": `{"resource":"test-resource","resource":"test-resource","input":{"prompt":"valid prompt"}}`,
		"unknown":   `{"resource":"test-resource","input":{"prompt":"valid prompt"},"admin":true}`,
		"nested":    `{"resource":"test-resource","input":` + deep + `}`,
		"oversized": `{"resource":"test-resource","input":{"prompt":"` + strings.Repeat("x", maxRequestBytes) + `"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			response, err := http.Post(fixture.server.URL+"/v1/invoices", "application/json", strings.NewReader(body))
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

func TestGatewayHomeIsResponsiveAndHardened(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	response, err := http.Get(fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := io.ReadAll(response.Body)
	response.Body.Close()
	for _, expected := range []string{"Merchant Workspace", "min-height:100dvh", "@media(max-width:780px)"} {
		if expected == "Merchant Workspace" && !bytes.Contains(contents, []byte(expected)) {
			t.Fatalf("home does not contain %q", expected)
		}
		if expected != "Merchant Workspace" && !strings.Contains(styleCSS, expected) {
			t.Fatalf("responsive CSS does not contain %q", expected)
		}
	}
	if response.Header.Get("Content-Security-Policy") == "" || response.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers are missing")
	}
}

type approvingReasoner struct{}

func (approvingReasoner) Approve(_ context.Context, request ApprovalRequest) (Decision, error) {
	if request.Product.ID != "test-resource" || request.Invoice.InputSHA256 == "" || !bytes.Contains(request.Input, []byte("make a test image")) {
		return Decision{}, io.ErrUnexpectedEOF
	}
	return Decision{Approved: true, Reason: "product and input match"}, nil
}

func (approvingReasoner) Analyze(_ context.Context, delivery Delivery) (string, error) {
	if delivery.Receipt.Signature == "" {
		return "", io.ErrUnexpectedEOF
	}
	return "Verified merchant delivery.", nil
}

func TestAgentCompletesGenericPaymentAndVerifiedDownload(t *testing.T) {
	fixture := newGatewayFixture(t, true)
	fixture.node.mu.Lock()
	fixture.node.confirmations = 1
	fixture.node.mu.Unlock()
	output := t.TempDir() + "/generated.jpg"
	result, err := RunAgent(context.Background(), AgentConfig{
		Endpoint: fixture.server.URL, Resource: "test-resource", Input: json.RawMessage(`{"prompt":"make a test image"}`),
		MaximumAmount: 10_000, PaymentTimeout: 5 * time.Second, ArtifactOutput: output, Reasoner: approvingReasoner{},
		Pay: func(_ context.Context, merchant string, amount uint64) (Payment, error) {
			if merchant != fixture.merchant.Address || amount != 10_000 {
				t.Fatalf("unexpected payment target: %s %d", merchant, amount)
			}
			transaction := fixture.transaction(t, amount)
			encoded, err := json.Marshal(transaction)
			return Payment{TransactionID: transaction.ID, Transaction: encoded}, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "test-image-contents" || result.ArtifactOutput == "" || result.TransactionID == "" || result.Analysis == "" {
		t.Fatalf("agent result or artifact is incomplete: %+v", result)
	}
}

func TestAgentRejectsExistingArtifactOutputBeforePayment(t *testing.T) {
	directory := t.TempDir()
	output := directory + "/existing.jpg"
	if err := os.WriteFile(output, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	paid := false
	_, err := RunAgent(context.Background(), AgentConfig{
		Endpoint: "http://127.0.0.1:1", Resource: "test-resource", Input: json.RawMessage(`{"prompt":"valid prompt"}`),
		MaximumAmount: 10_000, PaymentTimeout: time.Second, ArtifactOutput: output, Reasoner: approvingReasoner{},
		Pay: func(context.Context, string, uint64) (Payment, error) { paid = true; return Payment{}, nil },
	})
	if err == nil || paid {
		t.Fatalf("existing output was not rejected before payment: err=%v paid=%t", err, paid)
	}
	contents, _ := os.ReadFile(output)
	if string(contents) != "keep" {
		t.Fatal("existing artifact output was modified")
	}
}
