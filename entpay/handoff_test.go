package entpay

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseLaunchURL(t *testing.T) {
	code, err := newOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	valid := buildHandoffURL("https://merchant.example/entpay/", code)
	request, err := ParseLaunchURL(valid)
	if err != nil {
		t.Fatal(err)
	}
	if request.Merchant != "https://merchant.example/entpay/" || request.Handoff != code {
		t.Fatalf("unexpected launch request: %+v", request)
	}

	tests := map[string]string{
		"empty":             "",
		"too long":          valid + strings.Repeat("x", maxLaunchURLBytes),
		"wrong scheme":      strings.Replace(valid, "entcoin:", "https:", 1),
		"wrong host":        strings.Replace(valid, "//pay?", "//send?", 1),
		"path":              strings.Replace(valid, "//pay?", "//pay/path?", 1),
		"encoded path":      strings.Replace(valid, "//pay?", "//pay/%2F?", 1),
		"fragment":          valid + "#secret",
		"userinfo":          strings.Replace(valid, "//pay?", "//user@pay?", 1),
		"unknown parameter": valid + "&extra=true",
		"duplicate":         valid + "&v=1",
		"version":           strings.Replace(valid, "v=1", "v=2", 1),
		"short code":        strings.Replace(valid, code, "YWJj", 1),
		"padded code":       strings.Replace(valid, code, code+"=", 1),
		"http public":       buildHandoffURL("http://merchant.example/entpay/", code),
		"public port":       buildHandoffURL("https://merchant.example:8443/entpay/", code),
		"merchant query":    buildHandoffURL("https://merchant.example/entpay/?x=1", code),
		"merchant userinfo": buildHandoffURL("https://user@merchant.example/entpay/", code),
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseLaunchURL(value); err == nil {
				t.Fatal("invalid launch URL was accepted")
			}
		})
	}
	bootstrap, err := ParseLaunchURL(buildLaunchURL("https://merchant.example/entpay/"))
	if err != nil || bootstrap.Merchant != "https://merchant.example/entpay/" || bootstrap.Handoff != "" {
		t.Fatalf("bootstrap launch = %+v, %v", bootstrap, err)
	}
	windowsBootstrap, err := ParseLaunchURL(strings.Replace(buildLaunchURL("https://merchant.example/entpay/"), "pay?", "pay/?", 1))
	if err != nil || windowsBootstrap != bootstrap {
		t.Fatalf("canonical Windows bootstrap = %+v, %v", windowsBootstrap, err)
	}
}

func TestParseLaunchURLAllowsCanonicalLoopback(t *testing.T) {
	code, err := newOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://localhost:47832/entpay/", "http://127.0.0.1:47832/", "http://[::1]:47832/"} {
		parsed, err := ParseLaunchURL(buildHandoffURL(endpoint, code))
		if err != nil {
			t.Fatalf("parse %s: %v", endpoint, err)
		}
		if parsed.Merchant != endpoint {
			t.Fatalf("merchant changed: got %q want %q", parsed.Merchant, endpoint)
		}
	}
}

func TestGatewayHandoffRedeemIsEncryptedAndNonceBound(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	fixture.gateway.publicEndpoint = fixture.server.URL + "/"
	created := createGatewayInvoice(t, fixture)
	if created.Launch == nil || created.Launch.URL == "" || !created.Launch.ExpiresAt.Before(created.Invoice.ExpiresAt) {
		t.Fatalf("launch metadata is invalid: %+v", created.Launch)
	}
	bootstrap, err := ParseLaunchURL(created.Launch.URL)
	if err != nil || bootstrap.Merchant != fixture.gateway.publicEndpoint || bootstrap.Handoff != "" || created.Launch.Handoff == "" || strings.Contains(created.Launch.URL, created.Launch.Handoff) {
		t.Fatalf("gateway exposed handoff in launch URL: launch=%+v parsed=%+v err=%v", created.Launch, bootstrap, err)
	}
	launch := LaunchRequest{Merchant: fixture.gateway.publicEndpoint, Handoff: created.Launch.Handoff}
	nonce, err := newOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}

	redeem := func(t *testing.T, clientNonce string) (*http.Response, HandoffCapsule) {
		t.Helper()
		response, err := http.DefaultClient.Do(gatewayRequest(t, http.MethodPost, fixture.server.URL+"/v1/handoffs/redeem", "", RedeemHandoffRequest{Code: launch.Handoff, ClientNonce: clientNonce}))
		if err != nil {
			t.Fatal(err)
		}
		var capsule HandoffCapsule
		if response.StatusCode == http.StatusOK {
			if err := json.NewDecoder(response.Body).Decode(&capsule); err != nil {
				response.Body.Close()
				t.Fatal(err)
			}
		} else {
			_, _ = io.Copy(io.Discard, response.Body)
		}
		response.Body.Close()
		return response, capsule
	}

	response, capsule := redeem(t, nonce)
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("redeem returned %d with Cache-Control %q", response.StatusCode, response.Header.Get("Cache-Control"))
	}
	if capsule.Endpoint != fixture.gateway.publicEndpoint || !bytes.Equal(capsule.Input, []byte(`{"prompt":"make a test image"}`)) || capsule.Created.ClaimToken != created.ClaimToken || capsule.Created.Invoice.ID != created.Invoice.ID || capsule.Created.Launch != nil {
		t.Fatalf("redeemed capsule does not match: %+v", capsule)
	}
	if response, repeated := redeem(t, nonce); response.StatusCode != http.StatusOK || repeated.Created.ClaimToken != created.ClaimToken {
		t.Fatalf("idempotent redeem returned %d", response.StatusCode)
	}
	otherNonce, _ := newOpaqueToken()
	if response, _ := redeem(t, otherNonce); response.StatusCode != http.StatusNotFound {
		t.Fatalf("different nonce returned %d", response.StatusCode)
	}

	var codeHash, encrypted []byte
	if err := fixture.gateway.store.database.QueryRow(`SELECT code_hash, capsule FROM handoffs WHERE invoice_id = ?`, created.Invoice.ID).Scan(&codeHash, &encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(codeHash, []byte(launch.Handoff)) || bytes.Contains(encrypted, []byte(launch.Handoff)) || bytes.Contains(encrypted, []byte(created.ClaimToken)) || bytes.Contains(encrypted, capsule.Input) {
		t.Fatal("handoff secrets were stored in plaintext")
	}
}

func TestGatewayHandoffConcurrentRedemptionBindsOneNonce(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	fixture.gateway.publicEndpoint = fixture.server.URL + "/"
	created := createGatewayInvoice(t, fixture)
	launch := LaunchRequest{Merchant: fixture.gateway.publicEndpoint, Handoff: created.Launch.Handoff}
	nonces := make([]string, 8)
	for index := range nonces {
		nonces[index], _ = newOpaqueToken()
	}
	statuses := make([]int, len(nonces))
	var wait sync.WaitGroup
	wait.Add(len(nonces))
	for index := range nonces {
		go func(index int) {
			defer wait.Done()
			body, _ := json.Marshal(RedeemHandoffRequest{Code: launch.Handoff, ClientNonce: nonces[index]})
			response, requestErr := http.Post(fixture.server.URL+"/v1/handoffs/redeem", "application/json", bytes.NewReader(body))
			if requestErr != nil {
				statuses[index] = -1
				return
			}
			statuses[index] = response.StatusCode
			response.Body.Close()
		}(index)
	}
	wait.Wait()
	successes := 0
	for _, status := range statuses {
		if status == http.StatusOK {
			successes++
		} else if status != http.StatusNotFound {
			t.Fatalf("unexpected statuses: %v", statuses)
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful nonce bindings: %v", successes, statuses)
	}
}

func TestGatewayHandoffExpiryAndMalformedRequestsReturnNotFound(t *testing.T) {
	fixture := newGatewayFixture(t, false)
	fixture.gateway.publicEndpoint = fixture.server.URL + "/"
	created := createGatewayInvoice(t, fixture)
	launch := LaunchRequest{Merchant: fixture.gateway.publicEndpoint, Handoff: created.Launch.Handoff}
	if _, err := fixture.gateway.store.database.Exec(`UPDATE handoffs SET expires_at = ? WHERE invoice_id = ?`, time.Now().Add(-time.Minute).Unix(), created.Invoice.ID); err != nil {
		t.Fatal(err)
	}
	nonce, _ := newOpaqueToken()
	for name, body := range map[string]string{
		"expired":       `{"code":"` + launch.Handoff + `","client_nonce":"` + nonce + `"}`,
		"unknown field": `{"code":"` + launch.Handoff + `","client_nonce":"` + nonce + `","claim_token":"leak"}`,
		"duplicate":     `{"code":"` + launch.Handoff + `","code":"` + launch.Handoff + `","client_nonce":"` + nonce + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response, err := http.Post(fixture.server.URL+"/v1/handoffs/redeem", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusNotFound || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("returned %d with Cache-Control %q", response.StatusCode, response.Header.Get("Cache-Control"))
			}
		})
	}
}

func TestBuildLaunchURLUsesEncodedQuery(t *testing.T) {
	code, err := newOpaqueToken()
	if err != nil {
		t.Fatal(err)
	}
	value := buildLaunchURL("https://merchant.example/entpay/")
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("merchant") != "https://merchant.example/entpay/" || strings.Contains(value, "merchant=https://") {
		t.Fatalf("merchant query was not encoded: %s", value)
	}
	if parsed.Query().Has("handoff") || strings.Contains(value, code) {
		t.Fatalf("bootstrap URL contains handoff secret: %s", value)
	}
}
