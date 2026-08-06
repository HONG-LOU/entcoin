package entpay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestLocalAgent(t *testing.T, fixture gatewayFixture, pay func(context.Context, string, uint64) (Payment, error)) (*LocalAgent, *httptest.Server) {
	t.Helper()
	agent, err := NewLocalAgent(LocalAgentConfig{
		MaximumAmount: 10_000, PaymentTimeout: 5 * time.Second,
		ArtifactDirectory: t.TempDir(), Pay: pay,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(agent.Handler())
	t.Cleanup(server.Close)
	return agent, server
}

func postLocal(t *testing.T, endpoint string, body any) (*http.Response, []byte) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(endpoint, "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, contents
}

func createLocalHandoff(t *testing.T, serverURL string, fixture gatewayFixture, created CreateInvoiceResponse) LocalHandoffStatus {
	t.Helper()
	response, contents := postLocal(t, serverURL+"/v1/handoffs", LocalHandoffRequest{
		Endpoint: fixture.server.URL, Input: json.RawMessage(`{"prompt":"make a test image"}`), Created: created,
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("handoff returned %d: %s", response.StatusCode, contents)
	}
	if bytes.Contains(contents, []byte(created.ClaimToken)) || bytes.Contains(contents, []byte("claim_token")) {
		t.Fatal("handoff response exposed the claim token")
	}
	var status LocalHandoffStatus
	if err := json.Unmarshal(contents, &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestPreparedAgentUsesExistingInvoice(t *testing.T) {
	fixture := newGatewayFixture(t, true)
	fixture.node.mu.Lock()
	fixture.node.confirmations = 1
	fixture.node.mu.Unlock()
	created := createGatewayInvoice(t, fixture)
	result, err := RunPreparedAgent(context.Background(), PreparedAgentConfig{
		Endpoint: fixture.server.URL, Input: json.RawMessage(`{"prompt":"make a test image"}`),
		MaximumAmount: 10_000, PaymentTimeout: 5 * time.Second, Created: created,
		ArtifactOutput: t.TempDir() + "/result.jpg", Reasoner: approvingReasoner{},
		Pay: func(_ context.Context, _ string, amount uint64) (Payment, error) {
			transaction := fixture.transaction(t, amount)
			encoded, err := json.Marshal(transaction)
			return Payment{TransactionID: transaction.ID, Transaction: encoded}, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Invoice.ID != created.Invoice.ID || result.Delivery.Receipt.InvoiceID != created.Invoice.ID {
		t.Fatal("prepared Agent did not consume the supplied invoice")
	}
}

func TestInspectPreparedPaymentRejectsInvalidInvoice(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	input := json.RawMessage(`{"prompt":"make a test image"}`)
	base := createGatewayInvoice(t, fixture)
	tests := map[string]struct {
		created CreateInvoiceResponse
		input   json.RawMessage
		limit   uint64
	}{
		"bad signature": {created: func() CreateInvoiceResponse { value := base; value.Invoice.Signature = "invalid"; return value }(), input: input, limit: 10_000},
		"wrong input":   {created: base, input: json.RawMessage(`{"prompt":"different request"}`), limit: 10_000},
		"over limit":    {created: base, input: input, limit: 9_999},
		"missing token": {created: func() CreateInvoiceResponse { value := base; value.ClaimToken = ""; return value }(), input: input, limit: 10_000},
		"expired": {created: func() CreateInvoiceResponse {
			value := base
			value.Invoice.ExpiresAt = time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
			signInvoice(fixture.key, &value.Invoice)
			return value
		}(), input: input, limit: 10_000},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := InspectPreparedPayment(context.Background(), fixture.server.URL, test.input, test.limit, test.created); err == nil {
				t.Fatal("invalid prepared payment was accepted")
			}
		})
	}
}

func TestLocalAgentApprovalCompletesOnceAndSanitizesStatus(t *testing.T) {
	fixture := newGatewayFixture(t, true)
	fixture.node.mu.Lock()
	fixture.node.confirmations = 1
	fixture.node.mu.Unlock()
	created := createGatewayInvoice(t, fixture)
	var payments atomic.Uint64
	_, server := newTestLocalAgent(t, fixture, func(_ context.Context, _ string, amount uint64) (Payment, error) {
		payments.Add(1)
		transaction := fixture.transaction(t, amount)
		encoded, err := json.Marshal(transaction)
		return Payment{TransactionID: transaction.ID, Transaction: encoded}, err
	})
	status := createLocalHandoff(t, server.URL, fixture, created)
	statuses := make(chan int, 8)
	for range 8 {
		go func() {
			response, err := http.Post(server.URL+"/v1/handoffs/"+status.ID+"/approve", "application/json", strings.NewReader(`{}`))
			if err != nil {
				statuses <- 0
				return
			}
			response.Body.Close()
			statuses <- response.StatusCode
		}()
	}
	for range 8 {
		code := <-statuses
		if code != http.StatusAccepted && code != http.StatusOK {
			t.Fatalf("approve returned %d", code)
		}
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(server.URL + "/v1/handoffs/" + status.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		if status.Stage == "complete" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.Stage != "complete" || status.TransactionID == "" || status.Delivery == nil || status.ArtifactOutput == "" {
		t.Fatalf("incomplete local payment: %+v", status)
	}
	if payments.Load() != 1 {
		t.Fatalf("local Agent paid %d times", payments.Load())
	}
}

func TestLocalAgentRejectsWithoutPaymentAndHardensRequests(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	created := createGatewayInvoice(t, fixture)
	var paid atomic.Bool
	_, server := newTestLocalAgent(t, fixture, func(context.Context, string, uint64) (Payment, error) {
		paid.Store(true)
		return Payment{}, nil
	})
	status := createLocalHandoff(t, server.URL, fixture, created)
	response, contents := postLocal(t, server.URL+"/v1/handoffs/"+status.ID+"/reject", struct{}{})
	if response.StatusCode != http.StatusOK || !bytes.Contains(contents, []byte(`"stage":"rejected"`)) || paid.Load() {
		t.Fatalf("reject status=%d paid=%t body=%s", response.StatusCode, paid.Load(), contents)
	}
	health, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	health.Body.Close()
	if health.Header.Get("Content-Security-Policy") == "" || health.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("local security headers are missing")
	}
	bad, err := http.Post(server.URL+"/v1/handoffs", "application/json", strings.NewReader(`{} {}`))
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("trailing JSON returned %d", bad.StatusCode)
	}
}

func TestLocalAgentRejectsMissingWalletDirectoryAtStartup(t *testing.T) {
	_, err := NewLocalAgent(LocalAgentConfig{
		DataDirectory: "/path/to/Entropy/mainnet-v1", MaximumAmount: 10_000,
		PaymentTimeout: time.Minute,
	})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("placeholder wallet directory was accepted: %v", err)
	}
}

func TestLocalAgentRejectsCrossSiteAndFormApproval(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	created := createGatewayInvoice(t, fixture)
	_, server := newTestLocalAgent(t, fixture, func(context.Context, string, uint64) (Payment, error) {
		t.Fatal("cross-site request reached payment")
		return Payment{}, nil
	})
	status := createLocalHandoff(t, server.URL, fixture, created)
	for name, contentType := range map[string]string{"form": "application/x-www-form-urlencoded", "plain": "text/plain"} {
		t.Run(name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/handoffs/"+status.ID+"/approve", strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", contentType)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusUnsupportedMediaType {
				t.Fatalf("unsafe content type returned %d", response.StatusCode)
			}
		})
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/handoffs/"+status.ID+"/approve", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site approval returned %d", response.StatusCode)
	}
}

func TestLocalAgentAllowsCrossSiteTopLevelNavigation(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	_, server := newTestLocalAgent(t, fixture, func(context.Context, string, uint64) (Payment, error) {
		t.Fatal("page navigation reached payment")
		return Payment{}, nil
	})
	request, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	request.Header.Set("Sec-Fetch-Mode", "navigate")
	request.Header.Set("Sec-Fetch-Dest", "document")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cross-site top-level navigation returned %d", response.StatusCode)
	}
}

func TestLocalAgentRejectsCrossSiteSubresourceNavigation(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	_, server := newTestLocalAgent(t, fixture, func(context.Context, string, uint64) (Payment, error) {
		t.Fatal("subresource request reached payment")
		return Payment{}, nil
	})
	for _, destination := range []string{"iframe", "image", "script"} {
		t.Run(destination, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Sec-Fetch-Site", "cross-site")
			request.Header.Set("Sec-Fetch-Mode", "navigate")
			request.Header.Set("Sec-Fetch-Dest", destination)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("cross-site %s request returned %d", destination, response.StatusCode)
			}
		})
	}
}
