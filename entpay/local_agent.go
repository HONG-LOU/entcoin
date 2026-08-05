package entpay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
)

type LocalAgentConfig struct {
	DataDirectory     string
	WalletAddress     string
	MaximumAmount     uint64
	PaymentTimeout    time.Duration
	ArtifactDirectory string
	Pay               func(context.Context, string, uint64) (Payment, error)
}

type LocalHandoffRequest struct {
	Endpoint string                `json:"endpoint"`
	Input    json.RawMessage       `json:"input"`
	Created  CreateInvoiceResponse `json:"created"`
}

type LocalHandoffStatus struct {
	ID             string            `json:"id"`
	Stage          string            `json:"stage"`
	Message        string            `json:"message"`
	Invoice        Invoice           `json:"invoice"`
	Product        ProductDescriptor `json:"product"`
	Input          json.RawMessage   `json:"input"`
	MaximumAmount  uint64            `json:"maximum_amount"`
	TransactionID  string            `json:"transaction_id,omitempty"`
	ArtifactOutput string            `json:"artifact_output,omitempty"`
	Delivery       *Delivery         `json:"delivery,omitempty"`
	Error          string            `json:"error,omitempty"`
}

type localHandoff struct {
	status  LocalHandoffStatus
	request LocalHandoffRequest
	updated time.Time
}

type LocalAgent struct {
	config   LocalAgentConfig
	mu       sync.RWMutex
	handoffs map[string]*localHandoff
}

func NewLocalAgent(config LocalAgentConfig) (*LocalAgent, error) {
	if (config.Pay == nil && strings.TrimSpace(config.DataDirectory) == "") || config.MaximumAmount == 0 || config.PaymentTimeout < time.Second {
		return nil, fmt.Errorf("local Agent configuration is incomplete")
	}
	if config.Pay == nil {
		info, err := os.Stat(config.DataDirectory)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("local wallet data directory does not exist: %s", config.DataDirectory)
		}
		if _, err := os.Stat(filepath.Join(config.DataDirectory, "wallet.vault")); err != nil {
			return nil, fmt.Errorf("local wallet data directory has no wallet.vault: %s", config.DataDirectory)
		}
	}
	if strings.TrimSpace(config.WalletAddress) != "" {
		if err := core.ValidateAddress(config.WalletAddress); err != nil {
			return nil, fmt.Errorf("local Agent wallet address: %w", err)
		}
	}
	if strings.TrimSpace(config.ArtifactDirectory) != "" {
		info, err := filepath.Abs(config.ArtifactDirectory)
		if err != nil {
			return nil, fmt.Errorf("resolve artifact directory: %w", err)
		}
		config.ArtifactDirectory = info
	}
	return &LocalAgent{config: config, handoffs: make(map[string]*localHandoff)}, nil
}

func (a *LocalAgent) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.handleHome)
	mux.HandleFunc("GET /app.js", a.handleJavaScript)
	mux.HandleFunc("GET /style.css", a.handleStyle)
	mux.HandleFunc("GET /healthz", a.handleHealth)
	mux.HandleFunc("POST /v1/handoffs", a.handleHandoff)
	mux.HandleFunc("GET /v1/handoffs/{id}", a.handleHandoffStatus)
	mux.HandleFunc("POST /v1/handoffs/{id}/approve", a.handleApprove)
	mux.HandleFunc("POST /v1/handoffs/{id}/reject", a.handleReject)
	return a.securityHeaders(mux)
}

func (a *LocalAgent) handleHome(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(writer, localAgentHTML)
}

func (a *LocalAgent) handleJavaScript(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(writer, localAgentJavaScript)
}

func (a *LocalAgent) handleStyle(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/css; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(writer, localAgentCSS)
}

func (a *LocalAgent) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready", "protocol": ProtocolVersion})
}

func (a *LocalAgent) handleHandoff(writer http.ResponseWriter, request *http.Request) {
	var input LocalHandoffRequest
	if err := decodeLocalJSON(writer, request, &input); err != nil {
		return
	}
	approval, err := InspectPreparedPayment(request.Context(), input.Endpoint, input.Input, a.config.MaximumAmount, input.Created)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	id, err := localHandoffID()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "local Agent could not create the handoff")
		return
	}
	status := LocalHandoffStatus{
		ID: id, Stage: "approval", Message: "Waiting for your confirmation",
		Invoice: approval.Invoice, Product: approval.Product, Input: approval.Input,
		MaximumAmount: approval.MaximumAtoms,
	}
	a.mu.Lock()
	a.removeExpiredLocked(time.Now())
	a.handoffs[id] = &localHandoff{status: status, request: input, updated: time.Now()}
	a.mu.Unlock()
	writeJSON(writer, http.StatusCreated, status)
}

func (a *LocalAgent) handleHandoffStatus(writer http.ResponseWriter, request *http.Request) {
	status, ok := a.status(request.PathValue("id"))
	if !ok {
		writeError(writer, http.StatusNotFound, "payment request was not found")
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (a *LocalAgent) handleApprove(writer http.ResponseWriter, request *http.Request) {
	if err := decodeLocalJSON(writer, request, &struct{}{}); err != nil {
		return
	}
	id := request.PathValue("id")
	a.mu.Lock()
	handoff, ok := a.handoffs[id]
	if !ok {
		a.mu.Unlock()
		writeError(writer, http.StatusNotFound, "payment request was not found")
		return
	}
	if handoff.status.Stage != "approval" {
		status := handoff.status
		a.mu.Unlock()
		writeJSON(writer, http.StatusOK, status)
		return
	}
	handoff.status.Stage = "paying"
	handoff.status.Message = "Opening the local wallet"
	handoff.updated = time.Now()
	requestCopy := handoff.request
	product := handoff.status.Product
	a.mu.Unlock()

	go a.runPayment(id, requestCopy, product)
	status, _ := a.status(id)
	writeJSON(writer, http.StatusAccepted, status)
}

func (a *LocalAgent) handleReject(writer http.ResponseWriter, request *http.Request) {
	if err := decodeLocalJSON(writer, request, &struct{}{}); err != nil {
		return
	}
	id := request.PathValue("id")
	a.mu.Lock()
	defer a.mu.Unlock()
	handoff, ok := a.handoffs[id]
	if !ok {
		writeError(writer, http.StatusNotFound, "payment request was not found")
		return
	}
	if handoff.status.Stage == "approval" {
		handoff.status.Stage = "rejected"
		handoff.status.Message = "Payment rejected locally; no transaction was created"
		handoff.updated = time.Now()
	}
	writeJSON(writer, http.StatusOK, handoff.status)
}

func (a *LocalAgent) runPayment(id string, handoff LocalHandoffRequest, product ProductDescriptor) {
	output := ""
	if a.config.ArtifactDirectory != "" {
		extension := ".bin"
		if product.ID == "generated-photo" {
			extension = ".jpg"
		}
		output = filepath.Join(a.config.ArtifactDirectory, product.ID+"-"+handoff.Created.Invoice.ID+extension)
	}
	result, err := RunPreparedAgent(context.Background(), PreparedAgentConfig{
		Endpoint: handoff.Endpoint, DataDirectory: a.config.DataDirectory, WalletAddress: a.config.WalletAddress,
		Input: handoff.Input, MaximumAmount: a.config.MaximumAmount, PaymentTimeout: a.config.PaymentTimeout,
		ArtifactOutput: output, Created: handoff.Created, Reasoner: localApprovalReasoner{}, Pay: a.config.Pay,
		Progress: func(progress AgentProgress) { a.updateProgress(id, progress) },
	})
	a.mu.Lock()
	defer a.mu.Unlock()
	current, ok := a.handoffs[id]
	if !ok {
		return
	}
	current.updated = time.Now()
	if err != nil {
		current.status.Stage = "error"
		current.status.Message = "Payment stopped"
		current.status.Error = err.Error()
		return
	}
	current.status.Stage = "complete"
	current.status.Message = "Payment and delivery verified"
	current.status.TransactionID = result.TransactionID
	current.status.ArtifactOutput = result.ArtifactOutput
	current.status.Delivery = &result.Delivery
}

func (a *LocalAgent) updateProgress(id string, progress AgentProgress) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if handoff, ok := a.handoffs[id]; ok {
		handoff.status.Stage = progress.Stage
		handoff.status.Message = progress.Message
		handoff.updated = time.Now()
	}
}

func (a *LocalAgent) status(id string) (LocalHandoffStatus, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	handoff, ok := a.handoffs[id]
	if !ok {
		return LocalHandoffStatus{}, false
	}
	return handoff.status, true
}

func (a *LocalAgent) removeExpiredLocked(now time.Time) {
	for id, handoff := range a.handoffs {
		if now.Sub(handoff.updated) > 2*time.Hour {
			delete(a.handoffs, id)
		}
	}
}

func (a *LocalAgent) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; object-src 'none'")
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		if !validLocalBrowserRequest(request) {
			writeError(writer, http.StatusForbidden, "local Agent accepts only same-origin loopback requests")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func validLocalBrowserRequest(request *http.Request) bool {
	host, _, err := net.SplitHostPort(request.Host)
	if err != nil {
		host = request.Host
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil || !address.IsLoopback() {
		return false
	}
	if site := request.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme == "http" && parsed.Host == request.Host
}

func decodeLocalJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "request must use application/json")
		return fmt.Errorf("local request content type is not JSON")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(writer, http.StatusBadRequest, "request must be strict JSON")
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(writer, http.StatusBadRequest, "request must contain one JSON value")
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func localHandoffID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

type localApprovalReasoner struct{}

func (localApprovalReasoner) Approve(context.Context, ApprovalRequest) (Decision, error) {
	return Decision{Approved: true, Reason: "Approved by the user in the local Agent"}, nil
}

func (localApprovalReasoner) Analyze(context.Context, Delivery) (string, error) {
	return "", nil
}
