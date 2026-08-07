package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HONG-LOU/entcoin/entpay"
)

const testHandoffCode = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestOpenEntPayLinkQueuesManualHandoff(t *testing.T) {
	app := NewApp()
	value := "entcoin://pay?merchant=https%3A%2F%2Fmerchant.example%2Fentpay%2F&v=1&handoff=" + testHandoffCode
	result, err := app.OpenEntPayLink(value)
	if err != nil || result.Message != "Payment request queued" {
		t.Fatalf("OpenEntPayLink() = %+v, %v", result, err)
	}
	select {
	case launch := <-app.handoffQueue:
		if launch.Merchant != "https://merchant.example/entpay/" || launch.Handoff != testHandoffCode {
			t.Fatalf("queued launch = %+v", launch)
		}
	default:
		t.Fatal("manual payment link was not queued")
	}
}

func TestOpenEntPayLinkRejectsIncompleteManualHandoff(t *testing.T) {
	for name, value := range map[string]string{
		"bootstrap only": "entcoin://pay?merchant=https%3A%2F%2Fmerchant.example%2Fentpay%2F&v=1",
		"short handoff":  "entcoin://pay?merchant=https%3A%2F%2Fmerchant.example%2Fentpay%2F&v=1&handoff=short",
		"wrong scheme":   "https://merchant.example/entpay/",
	} {
		t.Run(name, func(t *testing.T) {
			app := NewApp()
			if _, err := app.OpenEntPayLink(value); err == nil {
				t.Fatal("invalid manual payment link was accepted")
			}
			if len(app.handoffQueue) != 0 {
				t.Fatal("invalid manual payment link was queued")
			}
		})
	}
}

func TestEntPayRelayRequiresBootstrappedMerchantOrigin(t *testing.T) {
	merchant := "https://merchant.example/entpay/"
	app := NewApp(entpay.LaunchRequest{Merchant: merchant})

	preflight := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:47833/v1/handoffs", nil)
	preflight.Header.Set("Origin", "https://merchant.example")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set("Access-Control-Request-Headers", "content-type")
	preflight.Header.Set("Access-Control-Request-Private-Network", "true")
	preflightResult := httptest.NewRecorder()
	app.handleEntPayHandoffRelay(preflightResult, preflight)
	if preflightResult.Code != http.StatusNoContent || preflightResult.Header().Get("Access-Control-Allow-Origin") != "https://merchant.example" || preflightResult.Header().Get("Access-Control-Allow-Private-Network") != "true" {
		t.Fatalf("preflight = %d, %v", preflightResult.Code, preflightResult.Header())
	}

	body := `{"merchant":"https://merchant.example/entpay/","handoff":"` + testHandoffCode + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:47833/v1/handoffs", strings.NewReader(body))
	request.Header.Set("Origin", "https://merchant.example")
	request.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	app.handleEntPayHandoffRelay(result, request)
	if result.Code != http.StatusAccepted || result.Header().Get("Cache-Control") != "no-store" || strings.Contains(result.Body.String(), testHandoffCode) {
		t.Fatalf("relay = %d, headers=%v body=%q", result.Code, result.Header(), result.Body.String())
	}
	select {
	case launch := <-app.handoffQueue:
		if launch.Merchant != merchant || launch.Handoff != testHandoffCode {
			t.Fatalf("queued launch = %+v", launch)
		}
	default:
		t.Fatal("relay did not queue the handoff")
	}

	replay := httptest.NewRecorder()
	app.handleEntPayHandoffRelay(replay, request.Clone(request.Context()))
	if replay.Code != http.StatusForbidden {
		t.Fatalf("replay returned %d", replay.Code)
	}
}

func TestEntPayRelayServerLifecycle(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	merchant := "https://merchant.example/entpay/"
	app := NewApp(entpay.LaunchRequest{Merchant: merchant})
	ctx, cancel := context.WithCancel(context.Background())
	app.serveEntPayHandoffRelay(ctx, listener)

	body := `{"merchant":"` + merchant + `","handoff":"` + testHandoffCode + `"}`
	request, err := http.NewRequest(http.MethodPost, "http://"+listener.Addr().String()+"/v1/handoffs", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "https://merchant.example")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("relay returned %d", response.StatusCode)
	}

	cancel()
	done := make(chan struct{})
	go func() {
		app.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not stop after cancellation")
	}
	if _, err := http.Get("http://" + listener.Addr().String() + "/v1/handoffs"); err == nil {
		t.Fatal("relay still accepted connections after shutdown")
	}
}

func TestEntPayRelayOriginNormalizationAndExpiry(t *testing.T) {
	merchant := "https://merchant.example/entpay/"
	app := NewApp(entpay.LaunchRequest{Merchant: merchant})
	if !app.entPayOriginAuthorized("https://MERCHANT.example:443", merchant) {
		t.Fatal("canonical default-port origin was rejected")
	}
	app.mu.Lock()
	app.handoffOrigins[merchant] = time.Now().Add(-time.Second)
	app.mu.Unlock()
	if app.entPayOriginAuthorized("https://merchant.example", merchant) {
		t.Fatal("expired bootstrap authorization was accepted")
	}
}

func TestEntPayRelayRejectsInvalidTransportAndFullQueue(t *testing.T) {
	merchant := "https://merchant.example/entpay/"
	tests := map[string]struct {
		contentType string
		body        string
		status      int
	}{
		"missing content type": {body: `{}`, status: http.StatusUnsupportedMediaType},
		"wrong content type":   {contentType: "text/plain", body: `{}`, status: http.StatusUnsupportedMediaType},
		"oversized body":       {contentType: "application/json", body: strings.Repeat("x", 4097), status: http.StatusBadRequest},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			app := NewApp(entpay.LaunchRequest{Merchant: merchant})
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:47833/v1/handoffs", strings.NewReader(test.body))
			request.Header.Set("Origin", "https://merchant.example")
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			result := httptest.NewRecorder()
			app.handleEntPayHandoffRelay(result, request)
			if result.Code != test.status {
				t.Fatalf("returned %d, want %d", result.Code, test.status)
			}
		})
	}

	app := NewApp(entpay.LaunchRequest{Merchant: merchant})
	for index := 0; index < cap(app.handoffQueue); index++ {
		app.handoffQueue <- entpay.LaunchRequest{Merchant: merchant, Handoff: testHandoffCode}
	}
	body := `{"merchant":"` + merchant + `","handoff":"` + testHandoffCode + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:47833/v1/handoffs", strings.NewReader(body))
	request.Header.Set("Origin", "https://merchant.example")
	request.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	app.handleEntPayHandoffRelay(result, request)
	if result.Code != http.StatusServiceUnavailable {
		t.Fatalf("full queue returned %d", result.Code)
	}
}

func TestEntPayRelayRejectsInvalidPreflight(t *testing.T) {
	merchant := "https://merchant.example/entpay/"
	for name, headers := range map[string]map[string]string{
		"missing requested method": {"Access-Control-Request-Headers": "content-type"},
		"wrong requested method":   {"Access-Control-Request-Method": http.MethodPut, "Access-Control-Request-Headers": "content-type"},
		"unknown requested header": {"Access-Control-Request-Method": http.MethodPost, "Access-Control-Request-Headers": "content-type, authorization"},
	} {
		t.Run(name, func(t *testing.T) {
			app := NewApp(entpay.LaunchRequest{Merchant: merchant})
			request := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:47833/v1/handoffs", nil)
			request.Header.Set("Origin", "https://merchant.example")
			for key, value := range headers {
				request.Header.Set(key, value)
			}
			result := httptest.NewRecorder()
			app.handleEntPayHandoffRelay(result, request)
			if result.Code != http.StatusBadRequest {
				t.Fatalf("invalid preflight returned %d", result.Code)
			}
		})
	}
}

func TestEntPayRelayRejectsWrongOriginAndAmbiguousJSON(t *testing.T) {
	merchant := "https://merchant.example/entpay/"
	for name, test := range map[string]struct{ origin, body string }{
		"wrong origin": {"https://attacker.example", `{"merchant":"` + merchant + `","handoff":"` + testHandoffCode + `"}`},
		"duplicate":    {"https://merchant.example", `{"merchant":"` + merchant + `","handoff":"` + testHandoffCode + `","handoff":"` + testHandoffCode + `"}`},
		"unknown":      {"https://merchant.example", `{"merchant":"` + merchant + `","handoff":"` + testHandoffCode + `","claim_token":"leak"}`},
	} {
		t.Run(name, func(t *testing.T) {
			app := NewApp(entpay.LaunchRequest{Merchant: merchant})
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:47833/v1/handoffs", strings.NewReader(test.body))
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Content-Type", "application/json")
			result := httptest.NewRecorder()
			app.handleEntPayHandoffRelay(result, request)
			if result.Code < 400 || strings.Contains(result.Body.String(), testHandoffCode) {
				t.Fatalf("invalid relay returned %d, %q", result.Code, result.Body.String())
			}
		})
	}
}

func TestSystemLaunchNeverAcceptsHandoffCapability(t *testing.T) {
	app := NewApp()
	app.routeSystemLaunch(entpay.LaunchRequest{Merchant: "https://merchant.example/entpay/", Handoff: testHandoffCode})
	app.mu.RLock()
	defer app.mu.RUnlock()
	if !app.launchError || len(app.handoffOrigins) != 0 || len(app.handoffQueue) != 0 {
		t.Fatalf("secret-bearing system launch was accepted")
	}
}
