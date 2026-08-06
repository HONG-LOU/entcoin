package entpay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
)

type MerchantConfig struct {
	MerchantAddress      string
	SigningKey           ed25519.PrivateKey
	NodeURL              string
	DatabasePath         string
	FulfillmentDirectory string
	InvoiceLifetime      time.Duration
	FulfillmentTimeout   time.Duration
	PublicEndpoint       string
	Products             []Product
	Logger               *slog.Logger
}

type Gateway struct {
	merchant           string
	signingKey         ed25519.PrivateKey
	node               *paymentNode
	store              *store
	fulfillments       *fulfillmentStore
	lifetime           time.Duration
	fulfillmentTimeout time.Duration
	publicEndpoint     string
	handoffKey         [32]byte
	products           map[string]Product
	descriptors        []ProductDescriptor
	logger             *slog.Logger
	limiter            *rateLimiter
	workerContext      context.Context
	cancelWorkers      context.CancelFunc
	workers            sync.WaitGroup
	closeOnce          sync.Once
	closeDone          chan struct{}
	closeErr           error
}

func NewGateway(config MerchantConfig) (*Gateway, error) {
	if err := core.ValidateAddress(config.MerchantAddress); err != nil {
		return nil, fmt.Errorf("merchant address: %w", err)
	}
	if len(config.SigningKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("Ed25519 signing key is invalid")
	}
	publicEndpoint := ""
	if strings.TrimSpace(config.PublicEndpoint) != "" {
		var err error
		publicEndpoint, err = validatePublicEndpoint(config.PublicEndpoint)
		if err != nil {
			return nil, fmt.Errorf("public endpoint: %w", err)
		}
	}
	if config.InvoiceLifetime == 0 {
		config.InvoiceLifetime = 15 * time.Minute
	}
	if config.InvoiceLifetime < time.Minute || config.InvoiceLifetime > time.Hour {
		return nil, fmt.Errorf("invoice lifetime must be between one minute and one hour")
	}
	if config.FulfillmentTimeout == 0 {
		config.FulfillmentTimeout = 3 * time.Minute
	}
	if config.FulfillmentTimeout < 10*time.Second || config.FulfillmentTimeout > 15*time.Minute {
		return nil, fmt.Errorf("fulfillment timeout must be between 10 seconds and 15 minutes")
	}
	if len(config.Products) == 0 || len(config.Products) > 32 {
		return nil, fmt.Errorf("one to 32 products are required")
	}
	products := make(map[string]Product, len(config.Products))
	descriptors := make([]ProductDescriptor, 0, len(config.Products))
	for _, product := range config.Products {
		if product == nil {
			return nil, fmt.Errorf("product is nil")
		}
		descriptor := product.Descriptor()
		if err := validateProductDescriptor(descriptor); err != nil {
			return nil, fmt.Errorf("product %q: %w", descriptor.ID, err)
		}
		if descriptor.Price > uint64(math.MaxInt64) {
			return nil, fmt.Errorf("product %q price exceeds storage range", descriptor.ID)
		}
		if _, exists := products[descriptor.ID]; exists {
			return nil, fmt.Errorf("product %q is duplicated", descriptor.ID)
		}
		products[descriptor.ID] = product
		descriptors = append(descriptors, descriptor)
	}
	node, err := newPaymentNode(config.NodeURL)
	if err != nil {
		return nil, fmt.Errorf("payment node: %w", err)
	}
	dataStore, err := openStore(config.DatabasePath)
	if err != nil {
		return nil, err
	}
	fulfillments, err := openFulfillmentStore(config.FulfillmentDirectory)
	if err != nil {
		_ = dataStore.Close()
		return nil, err
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	workerContext, cancelWorkers := context.WithCancel(context.Background())
	handoffKey := sha256.Sum256(append([]byte("entpay-v1-handoff-capsule\x00"), config.SigningKey.Seed()...))
	return &Gateway{
		merchant: config.MerchantAddress, signingKey: append(ed25519.PrivateKey(nil), config.SigningKey...),
		node: node, store: dataStore, fulfillments: fulfillments,
		lifetime: config.InvoiceLifetime, fulfillmentTimeout: config.FulfillmentTimeout,
		publicEndpoint: publicEndpoint, handoffKey: handoffKey,
		products: products, descriptors: descriptors, logger: logger, limiter: newRateLimiter(),
		workerContext: workerContext, cancelWorkers: cancelWorkers,
		closeDone: make(chan struct{}),
	}, nil
}

func (g *Gateway) Close(ctx context.Context) error {
	g.closeOnce.Do(func() {
		g.cancelWorkers()
		go func() {
			g.workers.Wait()
			g.closeErr = g.store.Close()
			close(g.closeDone)
		}()
	})
	select {
	case <-g.closeDone:
		return g.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", g.handleHome)
	mux.HandleFunc("GET /favicon.ico", g.handleFavicon)
	mux.HandleFunc("GET /app.js", g.handleJavaScript)
	mux.HandleFunc("GET /style.css", g.handleStyle)
	mux.HandleFunc("GET /healthz", g.handleHealth)
	mux.HandleFunc("GET /v1/info", g.handleInfo)
	mux.HandleFunc("POST /v1/invoices", g.handleCreateInvoice)
	mux.HandleFunc("POST /v1/handoffs/redeem", g.handleRedeemHandoff)
	mux.HandleFunc("GET /v1/invoices/{id}", g.handleInvoiceStatus)
	mux.HandleFunc("POST /v1/invoices/{id}/submit", g.handleSubmitPayment)
	mux.HandleFunc("POST /v1/invoices/{id}/claim", g.handleClaim)
	mux.HandleFunc("GET /v1/invoices/{id}/artifact", g.handleArtifact)
	return g.securityHeaders(g.requestLog(mux))
}

func (g *Gateway) handleHome(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(writer, indexHTML)
}

func (g *Gateway) handleFavicon(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Cache-Control", "public, max-age=86400")
	writer.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) handleJavaScript(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(writer, appJavaScript)
}

func (g *Gateway) handleStyle(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/css; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(writer, styleCSS)
}

func (g *Gateway) handleHealth(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()
	status, err := g.node.Status(ctx)
	if err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"status": false, "protocol": ProtocolVersion})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"status": true, "protocol": ProtocolVersion, "height": status.Height, "tip_hash": status.TipHash})
}

func (g *Gateway) handleInfo(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, ServiceInfo{
		Protocol: ProtocolVersion, Network: core.NetworkID, Merchant: g.merchant,
		SigningKey: base64.RawURLEncoding.EncodeToString(g.signingKey.Public().(ed25519.PublicKey)),
		Products:   append([]ProductDescriptor(nil), g.descriptors...),
		Capabilities: []string{
			"input-bound-invoices", "payload-bound-receipts", "exact-output-verification",
			"confirmed-delivery", "idempotent-fulfillment", "authorized-artifacts",
		},
	})
}

func (g *Gateway) handleCreateInvoice(writer http.ResponseWriter, request *http.Request) {
	if !g.limiter.Allow(clientIP(request), 10, time.Minute) {
		writeError(writer, http.StatusTooManyRequests, "invoice rate limit exceeded")
		return
	}
	var input CreateInvoiceRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	input.Resource = strings.TrimSpace(input.Resource)
	product, exists := g.products[input.Resource]
	if !exists {
		writeError(writer, http.StatusUnprocessableEntity, "resource is not available")
		return
	}
	canonical, err := canonicalObject(input.Input)
	if err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err.Error())
		return
	}
	validationContext, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	if err := product.Validate(validationContext, canonical); err != nil {
		writeError(writer, http.StatusUnprocessableEntity, "resource input is invalid")
		return
	}
	record, token, err := g.store.Create(request.Context(), g.merchant, product.Descriptor(), canonical, time.Now().UTC().Add(g.lifetime))
	if err != nil {
		g.logger.Error("create invoice", "resource", input.Resource, "error", err)
		writeError(writer, http.StatusInternalServerError, "invoice could not be created")
		return
	}
	signInvoice(g.signingKey, &record.Invoice)
	created := CreateInvoiceResponse{Invoice: record.Invoice, ClaimToken: token}
	if g.publicEndpoint != "" {
		code, codeErr := newOpaqueToken()
		expiresAt := time.Now().UTC().Add(2 * time.Minute)
		if expiresAt.After(record.ExpiresAt) {
			expiresAt = record.ExpiresAt
		}
		capsule := HandoffCapsule{Endpoint: g.publicEndpoint, Input: canonical, Created: created}
		if codeErr != nil || g.store.CreateHandoff(request.Context(), code, record.ID, capsule, expiresAt, g.handoffKey) != nil {
			g.logger.Error("create handoff", "invoice", record.ID)
			writeError(writer, http.StatusInternalServerError, "payment handoff could not be created")
			return
		}
		created.Launch = &Launch{URL: buildLaunchURL(g.publicEndpoint), Handoff: code, ExpiresAt: expiresAt}
	}
	writeJSON(writer, http.StatusCreated, created)
}

func (g *Gateway) handleRedeemHandoff(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if !g.limiter.Allow("handoff-ip:"+clientIP(request), 20, time.Minute) {
		writeError(writer, http.StatusTooManyRequests, "handoff rate limit exceeded")
		return
	}
	var input RedeemHandoffRequest
	if err := decodeJSON(request, &input); err != nil || validateOpaqueToken(input.Code) != nil || validateOpaqueToken(input.ClientNonce) != nil {
		writeError(writer, http.StatusNotFound, "payment handoff was not found")
		return
	}
	capsule, invoiceID, err := g.store.RedeemHandoff(request.Context(), input.Code, input.ClientNonce, time.Now().UTC(), g.handoffKey)
	if err != nil {
		if !errors.Is(err, errHandoffNotFound) {
			g.logger.Error("redeem handoff", "error", err)
		}
		writeError(writer, http.StatusNotFound, "payment handoff was not found")
		return
	}
	if !g.limiter.Allow("handoff-invoice:"+invoiceID, 5, time.Minute) {
		writeError(writer, http.StatusTooManyRequests, "handoff rate limit exceeded")
		return
	}
	writeJSON(writer, http.StatusOK, capsule)
}

func (g *Gateway) handleInvoiceStatus(writer http.ResponseWriter, request *http.Request) {
	record, ok := g.authorizedInvoice(writer, request)
	if !ok {
		return
	}
	status := PaymentStatus{InvoiceID: record.ID, Status: record.Status, TransactionID: record.TxID, Required: record.Confirmations}
	if record.TxID != "" {
		found, confirmations, err := g.node.ObservePayment(request.Context(), record.Merchant, record.TxID, record.Amount)
		if err != nil {
			writeError(writer, http.StatusBadGateway, "payment node is unavailable")
			return
		}
		if found {
			status.Confirmations = confirmations
		}
	}
	writeJSON(writer, http.StatusOK, status)
}

func (g *Gateway) handleSubmitPayment(writer http.ResponseWriter, request *http.Request) {
	if !g.limiter.Allow(clientIP(request), 30, time.Minute) {
		writeError(writer, http.StatusTooManyRequests, "payment rate limit exceeded")
		return
	}
	record, ok := g.authorizedInvoice(writer, request)
	if !ok {
		return
	}
	var input SubmitPaymentRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	var transaction core.Transaction
	if err := json.Unmarshal(input.Transaction, &transaction); err != nil {
		writeError(writer, http.StatusBadRequest, "transaction is invalid JSON")
		return
	}
	if err := validatePayment(transaction, record.Invoice); err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if record.Status == "submitted" && record.TxID == transaction.ID {
		writeJSON(writer, http.StatusAccepted, PaymentStatus{InvoiceID: record.ID, Status: "submitted", TransactionID: transaction.ID, Required: record.Confirmations})
		return
	}
	if time.Now().UTC().After(record.ExpiresAt) {
		writeError(writer, http.StatusConflict, "invoice is not payable")
		return
	}
	if record.Status != "open" {
		writeError(writer, http.StatusConflict, "invoice or transaction was already used")
		return
	}
	if err := g.node.AcceptTransaction(request.Context(), transaction, record.Merchant, record.Amount); err != nil {
		g.logger.Warn("reject invoice transaction", "invoice", record.ID, "transaction", transaction.ID, "error", err)
		writeError(writer, http.StatusConflict, "transaction was not accepted by the Entcoin node")
		return
	}
	if err := g.store.Submit(request.Context(), record.ID, transaction.ID); err != nil {
		if errors.Is(err, errConflict) {
			writeError(writer, http.StatusConflict, "invoice or transaction was already used")
			return
		}
		g.logger.Error("record invoice transaction", "invoice", record.ID, "error", err)
		writeError(writer, http.StatusInternalServerError, "payment could not be recorded")
		return
	}
	writeJSON(writer, http.StatusAccepted, PaymentStatus{InvoiceID: record.ID, Status: "submitted", TransactionID: transaction.ID, Required: record.Confirmations})
}

func validatePayment(transaction core.Transaction, expected Invoice) error {
	if transaction.Coinbase || transaction.ID == "" || transaction.ID != transaction.ComputeID() {
		return fmt.Errorf("payment transaction is invalid")
	}
	matches := 0
	for _, output := range transaction.Outputs {
		if core.AddressesEqual(output.Address, expected.Merchant) && output.Amount == expected.Amount {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("payment must contain exactly one invoice output")
	}
	return nil
}

func (g *Gateway) handleClaim(writer http.ResponseWriter, request *http.Request) {
	record, ok := g.authorizedInvoice(writer, request)
	if !ok {
		return
	}
	if record.Status == "delivered" {
		var result Delivery
		if json.Unmarshal(record.DeliveryJSON, &result) != nil || !g.validateStoredDelivery(record, result) {
			writeError(writer, http.StatusInternalServerError, "stored delivery failed integrity verification")
			return
		}
		writeJSON(writer, http.StatusOK, result)
		return
	}
	if record.Status != "submitted" || record.TxID == "" {
		writeError(writer, http.StatusPaymentRequired, "payment has not been submitted")
		return
	}
	found, confirmations, err := g.node.ObservePayment(request.Context(), record.Merchant, record.TxID, record.Amount)
	if err != nil {
		writeError(writer, http.StatusBadGateway, "payment node is unavailable")
		return
	}
	if !found || confirmations < record.Confirmations {
		writeJSON(writer, http.StatusAccepted, PaymentStatus{
			InvoiceID: record.ID, Status: "confirming", TransactionID: record.TxID,
			Confirmations: confirmations, Required: record.Confirmations,
		})
		return
	}
	now := time.Now().UTC()
	job, acquired, err := g.store.AcquireJob(request.Context(), record.ID, now, g.fulfillmentTimeout+time.Minute, 5*time.Second, 3)
	if err != nil {
		g.logger.Error("acquire fulfillment job", "invoice", record.ID, "error", err)
		writeError(writer, http.StatusInternalServerError, "fulfillment could not be queued")
		return
	}
	if acquired {
		g.startFulfillment(record)
	}
	if job.Status == "permanent" {
		writeError(writer, http.StatusBadGateway, "merchant rejected the fulfillment request")
		return
	}
	if job.Status == "failed" && job.Attempts >= 3 {
		writeError(writer, http.StatusServiceUnavailable, "fulfillment failed after retrying")
		return
	}
	writeJSON(writer, http.StatusAccepted, PaymentStatus{
		InvoiceID: record.ID, Status: "processing", TransactionID: record.TxID,
		Confirmations: confirmations, Required: record.Confirmations, Attempt: job.Attempts,
	})
}

func (g *Gateway) startFulfillment(record invoiceRecord) {
	g.workers.Add(1)
	go func() {
		defer g.workers.Done()
		ctx, cancel := context.WithTimeout(g.workerContext, g.fulfillmentTimeout)
		defer cancel()
		if err := g.fulfill(ctx, record); err != nil {
			permanent := isPermanentFailure(err)
			if storeErr := g.store.FailJob(context.Background(), record.ID, permanent, time.Now().UTC(), err.Error()); storeErr != nil {
				g.logger.Error("record fulfillment failure", "invoice", record.ID, "error", storeErr)
			}
			g.logger.Error("fulfill paid resource", "invoice", record.ID, "resource", record.Resource, "permanent", permanent, "error", err)
		}
	}()
}

func (g *Gateway) fulfill(ctx context.Context, record invoiceRecord) error {
	staged, exists, err := g.fulfillments.Existing(record.ID)
	if err != nil {
		return fmt.Errorf("read staged fulfillment: %w", err)
	}
	if !exists {
		product := g.products[record.Resource]
		if product == nil {
			return PermanentFailure(fmt.Errorf("resource is no longer configured"))
		}
		result, err := product.Fulfill(ctx, FulfillmentRequest{
			InvoiceID: record.ID, TransactionID: record.TxID,
			Input: append(json.RawMessage(nil), record.Input...),
		})
		if err != nil {
			return err
		}
		payload, err := validateFulfillment(result)
		if err != nil {
			return PermanentFailure(err)
		}
		staged, err = g.fulfillments.Stage(record.ID, result, payload)
		if err != nil {
			return err
		}
	}
	deliveredAt := time.Now().UTC().Truncate(time.Second)
	artifactHash := ""
	if staged.Artifact != nil {
		artifactHash = staged.Artifact.SHA256
	}
	receipt := Receipt{
		Protocol: ProtocolVersion, InvoiceID: record.ID, TransactionID: record.TxID,
		Resource: record.Resource, InputSHA256: record.InputSHA256,
		PayloadSHA256: contentHash(staged.Payload), ArtifactSHA256: artifactHash,
		DeliveredAt: deliveredAt,
	}
	signReceipt(g.signingKey, &receipt)
	delivery := Delivery{Receipt: receipt, Payload: staged.Payload, Artifact: staged.Artifact}
	encoded, err := json.Marshal(delivery)
	if err != nil {
		return fmt.Errorf("encode delivery: %w", err)
	}
	if err := g.store.DeliverJob(ctx, record.ID, deliveredAt, encoded); err != nil {
		return fmt.Errorf("persist delivery: %w", err)
	}
	g.fulfillments.RemoveStage(record.ID)
	return nil
}

func (g *Gateway) validateStoredDelivery(record invoiceRecord, result Delivery) bool {
	publicKey := g.signingKey.Public().(ed25519.PublicKey)
	if result.Receipt.InvoiceID != record.ID || result.Receipt.TransactionID != record.TxID || result.Receipt.Resource != record.Resource || result.Receipt.InputSHA256 != record.InputSHA256 || !verifyReceipt(publicKey, result.Receipt) {
		return false
	}
	payload, err := canonicalPayload(result.Payload)
	if err != nil || contentHash(payload) != result.Receipt.PayloadSHA256 {
		return false
	}
	if result.Artifact == nil {
		return result.Receipt.ArtifactSHA256 == ""
	}
	return result.Artifact.SHA256 == result.Receipt.ArtifactSHA256 && result.Artifact.DownloadPath == "v1/invoices/"+record.ID+"/artifact"
}

func (g *Gateway) handleArtifact(writer http.ResponseWriter, request *http.Request) {
	record, ok := g.authorizedInvoice(writer, request)
	if !ok {
		return
	}
	if record.Status != "delivered" {
		writeError(writer, http.StatusNotFound, "artifact was not found")
		return
	}
	var result Delivery
	if json.Unmarshal(record.DeliveryJSON, &result) != nil || !g.validateStoredDelivery(record, result) || result.Artifact == nil {
		writeError(writer, http.StatusNotFound, "artifact was not found")
		return
	}
	contents, err := g.fulfillments.ReadArtifact(record.ID)
	if err != nil || int64(len(contents)) != result.Artifact.Bytes || contentHash(contents) != result.Artifact.SHA256 {
		writeError(writer, http.StatusInternalServerError, "artifact failed integrity verification")
		return
	}
	writer.Header().Set("Content-Disposition", `inline; filename="`+result.Artifact.FileName+`"`)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", result.Artifact.FileName))
	writer.Header().Set("ETag", `"`+result.Artifact.SHA256+`"`)
	http.ServeContent(writer, request, result.Artifact.FileName, result.Receipt.DeliveredAt, bytes.NewReader(contents))
}

func (g *Gateway) authorizedInvoice(writer http.ResponseWriter, request *http.Request) (invoiceRecord, bool) {
	token := bearerToken(request)
	if token == "" {
		writeError(writer, http.StatusUnauthorized, "bearer claim token is required")
		return invoiceRecord{}, false
	}
	record, err := g.store.Authorized(request.Context(), request.PathValue("id"), token)
	if errors.Is(err, errInvoiceNotFound) || errors.Is(err, errUnauthorized) {
		writeError(writer, http.StatusNotFound, "invoice was not found")
		return invoiceRecord{}, false
	}
	if err != nil {
		g.logger.Error("authorize invoice", "error", err)
		writeError(writer, http.StatusInternalServerError, "invoice could not be read")
		return invoiceRecord{}, false
	}
	signInvoice(g.signingKey, &record.Invoice)
	return record, true
}

func bearerToken(request *http.Request) string {
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
}

func (g *Gateway) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self'; connect-src 'self' http://127.0.0.1:47833; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(writer, request)
	})
}

func (g *Gateway) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		next.ServeHTTP(writer, request)
		g.logger.Info("request", "method", request.Method, "path", request.URL.Path, "client_ip", clientIP(request), "duration_ms", time.Since(started).Milliseconds())
	})
}
