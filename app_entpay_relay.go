package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/HONG-LOU/entcoin/entpay"
)

const entPayHandoffRelayAddress = "127.0.0.1:47833"

type entPayRelayRequest struct {
	Merchant string
	Handoff  string
}

func (a *App) routeSystemLaunch(launch entpay.LaunchRequest) {
	if entpay.ValidateLaunchIntent(launch) != nil {
		a.recordInvalidEntPayLaunch()
		return
	}
	a.mu.Lock()
	if !a.closing {
		a.handoffOrigins[launch.Merchant] = time.Now().UTC().Add(2 * time.Minute)
	}
	a.mu.Unlock()
	a.focusWindow()
}

func (a *App) startEntPayHandoffRelay(ctx context.Context) error {
	listener, err := net.Listen("tcp4", entPayHandoffRelayAddress)
	if err != nil {
		return fmt.Errorf("start Agent Pay handoff relay: %w", err)
	}
	a.serveEntPayHandoffRelay(ctx, listener)
	return nil
}

func (a *App) serveEntPayHandoffRelay(ctx context.Context, listener net.Listener) {
	server := &http.Server{
		Handler:           http.HandlerFunc(a.handleEntPayHandoffRelay),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       5 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	a.mu.Lock()
	a.relayServer = server
	a.relayStart = nil
	a.mu.Unlock()
	a.wait.Add(2)
	go func() {
		defer a.wait.Done()
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			a.mu.Lock()
			a.relayStart = fmt.Errorf("Agent Pay handoff relay stopped")
			a.launchError = true
			a.mu.Unlock()
		}
	}()
	go func() {
		defer a.wait.Done()
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
}

func (a *App) handleEntPayHandoffRelay(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.URL.Path != "/v1/handoffs" || request.URL.RawQuery != "" {
		http.NotFound(writer, request)
		return
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if !a.entPayOriginAuthorized(origin, "") {
		http.Error(writer, "origin is not authorized", http.StatusForbidden)
		return
	}
	writer.Header().Set("Access-Control-Allow-Origin", origin)
	writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	writer.Header().Set("Access-Control-Max-Age", "60")
	writer.Header().Set("Vary", "Origin")
	if strings.EqualFold(request.Header.Get("Access-Control-Request-Private-Network"), "true") {
		writer.Header().Set("Access-Control-Allow-Private-Network", "true")
	}
	if request.Method == http.MethodOptions {
		if request.Header.Get("Access-Control-Request-Method") != http.MethodPost || !entPayRelayPreflightHeadersValid(request.Header.Get("Access-Control-Request-Headers")) {
			http.Error(writer, "preflight is invalid", http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "POST, OPTIONS")
		http.Error(writer, "method is not allowed", http.StatusMethodNotAllowed)
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(writer, "content type is invalid", http.StatusUnsupportedMediaType)
		return
	}
	contents, err := io.ReadAll(io.LimitReader(request.Body, 4097))
	if err != nil || len(contents) == 0 || len(contents) > 4096 {
		http.Error(writer, "request is invalid", http.StatusBadRequest)
		return
	}
	input, err := decodeEntPayRelayRequest(contents)
	if err != nil || !a.entPayOriginAuthorized(origin, input.Merchant) || entpay.ValidateLaunchRequest(entpay.LaunchRequest{Merchant: input.Merchant, Handoff: input.Handoff}) != nil {
		http.Error(writer, "request is invalid", http.StatusBadRequest)
		return
	}
	if !a.enqueueHandoff(entpay.LaunchRequest{Merchant: input.Merchant, Handoff: input.Handoff}) {
		http.Error(writer, "Agent Pay request queue is full", http.StatusServiceUnavailable)
		return
	}
	a.mu.Lock()
	delete(a.handoffOrigins, input.Merchant)
	a.mu.Unlock()
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusAccepted)
	_, _ = writer.Write([]byte(`{"accepted":true}`))
}

func entPayRelayPreflightHeadersValid(value string) bool {
	parts := strings.Split(value, ",")
	if len(parts) != 1 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(parts[0]), "content-type")
}

func (a *App) entPayOriginAuthorized(origin, merchant string) bool {
	now := time.Now().UTC()
	a.mu.Lock()
	defer a.mu.Unlock()
	for endpoint, expiresAt := range a.handoffOrigins {
		if now.After(expiresAt) {
			delete(a.handoffOrigins, endpoint)
			continue
		}
		if merchant != "" && endpoint != merchant {
			continue
		}
		allowedOrigin, err := entPayEndpointOrigin(endpoint)
		requestOrigin, originErr := entPayEndpointOrigin(origin)
		if err == nil && originErr == nil && allowedOrigin == requestOrigin {
			return true
		}
	}
	return false
}

func entPayEndpointOrigin(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("endpoint origin is invalid")
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	}
	return scheme + "://" + host, nil
}

func decodeEntPayRelayRequest(contents []byte) (entPayRelayRequest, error) {
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return entPayRelayRequest{}, fmt.Errorf("request is invalid")
	}
	seen := make(map[string]struct{}, 2)
	var result entPayRelayRequest
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok {
			return entPayRelayRequest{}, fmt.Errorf("request is invalid")
		}
		if _, duplicate := seen[name]; duplicate {
			return entPayRelayRequest{}, fmt.Errorf("request field is duplicated")
		}
		seen[name] = struct{}{}
		switch name {
		case "merchant":
			err = decoder.Decode(&result.Merchant)
		case "handoff":
			err = decoder.Decode(&result.Handoff)
		default:
			return entPayRelayRequest{}, fmt.Errorf("request field is unknown")
		}
		if err != nil {
			return entPayRelayRequest{}, fmt.Errorf("request is invalid")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(seen) != 2 {
		return entPayRelayRequest{}, fmt.Errorf("request is incomplete")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return entPayRelayRequest{}, fmt.Errorf("request has trailing data")
	}
	return result, nil
}
