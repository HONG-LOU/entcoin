package entpay

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
)

const maxRequestBytes = 128 << 10

type Config struct {
	MerchantAddress string
	Price           uint64
	Confirmations   uint64
	InvoiceLifetime time.Duration
	SigningKey      ed25519.PrivateKey
	NodeClient      *NodeClient
	Store           *Store
	Logger          *slog.Logger
}

type Service struct {
	merchant       string
	price          uint64
	confirmations  uint64
	lifetime       time.Duration
	signingKey     ed25519.PrivateKey
	nodes          *NodeClient
	store          *Store
	logger         *slog.Logger
	requestLimiter *rateLimiter
}

func NewService(config Config) (*Service, error) {
	if err := core.ValidateAddress(config.MerchantAddress); err != nil {
		return nil, fmt.Errorf("merchant address: %w", err)
	}
	if config.Price == 0 || config.Price > uint64(math.MaxInt64) || config.Confirmations == 0 || config.Confirmations > uint64(math.MaxInt64) {
		return nil, fmt.Errorf("price and confirmations must be greater than zero")
	}
	if config.InvoiceLifetime < time.Minute || config.InvoiceLifetime > time.Hour {
		return nil, fmt.Errorf("invoice lifetime must be between one minute and one hour")
	}
	if len(config.SigningKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("Ed25519 signing key is invalid")
	}
	if config.NodeClient == nil || config.Store == nil {
		return nil, fmt.Errorf("node client and store are required")
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		merchant: config.MerchantAddress, price: config.Price,
		confirmations: config.Confirmations, lifetime: config.InvoiceLifetime,
		signingKey: append(ed25519.PrivateKey(nil), config.SigningKey...), nodes: config.NodeClient,
		store: config.Store, logger: logger, requestLimiter: newRateLimiter(),
	}, nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleHome)
	mux.HandleFunc("GET /favicon.ico", s.handleFavicon)
	mux.HandleFunc("GET /app.js", s.handleJavaScript)
	mux.HandleFunc("GET /style.css", s.handleStyle)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /v1/info", s.handleInfo)
	mux.HandleFunc("POST /v1/invoices", s.handleCreateInvoice)
	mux.HandleFunc("GET /v1/invoices/{id}", s.handleInvoiceStatus)
	mux.HandleFunc("POST /v1/invoices/{id}/submit", s.handleSubmitPayment)
	mux.HandleFunc("POST /v1/invoices/{id}/claim", s.handleClaim)
	return s.securityHeaders(s.requestLog(mux))
}

func (s *Service) handleHome(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(writer, indexHTML)
}

func (s *Service) handleFavicon(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Cache-Control", "public, max-age=86400")
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleJavaScript(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	writer.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = io.WriteString(writer, appJavaScript)
}

func (s *Service) handleStyle(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/css; charset=utf-8")
	writer.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = io.WriteString(writer, styleCSS)
}

func (s *Service) handleHealth(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
	defer cancel()
	report := s.nodes.Report(ctx, "health")
	status := http.StatusOK
	if len(report.Nodes) == 0 || report.Nodes[0].Error != "" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(writer, status, map[string]any{"status": status == http.StatusOK, "protocol": ProtocolVersion, "node": report.Nodes})
}

func (s *Service) handleInfo(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, Info{
		Protocol: ProtocolVersion, Network: core.NetworkID, Merchant: s.merchant,
		Price: s.price, Resource: NetworkReport, Confirmations: s.confirmations,
		Nodes:      append([]string(nil), s.nodes.reportURLs...),
		SigningKey: base64.RawURLEncoding.EncodeToString(s.signingKey.Public().(ed25519.PublicKey)),
	})
}

func (s *Service) handleCreateInvoice(writer http.ResponseWriter, request *http.Request) {
	if !s.requestLimiter.Allow(clientIP(request), 10, time.Minute) {
		writeError(writer, http.StatusTooManyRequests, "invoice rate limit exceeded")
		return
	}
	var input CreateInvoiceRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	input.Resource = strings.TrimSpace(input.Resource)
	input.Query = strings.TrimSpace(input.Query)
	if input.Resource != NetworkReport || len(input.Query) < 3 || len(input.Query) > 500 {
		writeError(writer, http.StatusUnprocessableEntity, "resource or query is invalid")
		return
	}
	record, token, err := s.store.Create(
		request.Context(), s.merchant, s.price, input.Resource, input.Query,
		time.Now().UTC().Add(s.lifetime), s.confirmations,
	)
	if err != nil {
		s.logger.Error("create invoice", "error", err)
		writeError(writer, http.StatusInternalServerError, "invoice could not be created")
		return
	}
	record.Signature = signInvoice(s.signingKey, record.Invoice)
	writeJSON(writer, http.StatusCreated, CreateInvoiceResponse{Invoice: record.Invoice, ClaimToken: token})
}

func (s *Service) handleInvoiceStatus(writer http.ResponseWriter, request *http.Request) {
	record, ok := s.authorizedInvoice(writer, request)
	if !ok {
		return
	}
	status := PaymentStatus{InvoiceID: record.ID, Status: record.Status, TransactionID: record.TxID, Required: record.Confirmations}
	if record.TxID != "" {
		found, confirmations, err := s.nodes.ObservePayment(request.Context(), record.Merchant, record.TxID, record.Amount)
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

func (s *Service) handleSubmitPayment(writer http.ResponseWriter, request *http.Request) {
	if !s.requestLimiter.Allow(clientIP(request), 30, time.Minute) {
		writeError(writer, http.StatusTooManyRequests, "payment rate limit exceeded")
		return
	}
	record, ok := s.authorizedInvoice(writer, request)
	if !ok {
		return
	}
	if time.Now().UTC().After(record.ExpiresAt) {
		writeError(writer, http.StatusConflict, "invoice is not payable")
		return
	}
	var input SubmitPaymentRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePayment(input.Transaction, record.Invoice); err != nil {
		writeError(writer, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if record.Status == "submitted" && record.TxID == input.Transaction.ID {
		writeJSON(writer, http.StatusAccepted, PaymentStatus{
			InvoiceID: record.ID, Status: "submitted", TransactionID: input.Transaction.ID,
			Required: record.Confirmations,
		})
		return
	}
	if record.Status != "open" {
		writeError(writer, http.StatusConflict, "invoice or transaction was already used")
		return
	}
	if err := s.nodes.AcceptTransaction(request.Context(), input.Transaction, record.Merchant, record.Amount); err != nil {
		s.logger.Warn("reject invoice transaction", "invoice", record.ID, "transaction", input.Transaction.ID, "error", err)
		writeError(writer, http.StatusConflict, "transaction was not accepted by the Entcoin node")
		return
	}
	if err := s.store.Submit(request.Context(), record.ID, input.Transaction.ID); err != nil {
		if errors.Is(err, ErrConflict) {
			writeError(writer, http.StatusConflict, "invoice or transaction was already used")
			return
		}
		s.logger.Error("record invoice transaction", "invoice", record.ID, "error", err)
		writeError(writer, http.StatusInternalServerError, "payment could not be recorded")
		return
	}
	writeJSON(writer, http.StatusAccepted, PaymentStatus{
		InvoiceID: record.ID, Status: "submitted", TransactionID: input.Transaction.ID,
		Required: record.Confirmations,
	})
}

func (s *Service) handleClaim(writer http.ResponseWriter, request *http.Request) {
	record, ok := s.authorizedInvoice(writer, request)
	if !ok {
		return
	}
	if record.Status == "delivered" {
		var delivery Delivery
		if len(record.DeliveryJSON) == 0 || json.Unmarshal(record.DeliveryJSON, &delivery) != nil {
			writeError(writer, http.StatusInternalServerError, "stored delivery is invalid")
			return
		}
		writeJSON(writer, http.StatusOK, delivery)
		return
	}
	if record.Status != "submitted" || record.TxID == "" {
		writeError(writer, http.StatusPaymentRequired, "payment has not been submitted")
		return
	}
	found, confirmations, err := s.nodes.ObservePayment(request.Context(), record.Merchant, record.TxID, record.Amount)
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
	reportContext, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	report := s.nodes.Report(reportContext, record.Query)
	deliveredAt := time.Now().UTC().Truncate(time.Second)
	receipt := Receipt{
		Protocol: ProtocolVersion, InvoiceID: record.ID, TransactionID: record.TxID,
		Resource: record.Resource, DeliveredAt: deliveredAt,
	}
	receipt.Signature = signReceipt(s.signingKey, receipt)
	delivery := Delivery{Receipt: receipt, Report: report}
	deliveryJSON, err := json.Marshal(delivery)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "resource delivery could not be encoded")
		return
	}
	if err := s.store.Deliver(request.Context(), record.ID, deliveredAt, deliveryJSON); err != nil {
		latest, readErr := s.store.Authorized(request.Context(), record.ID, strings.TrimSpace(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")))
		if readErr == nil && latest.Status == "delivered" && json.Unmarshal(latest.DeliveryJSON, &delivery) == nil {
			writeJSON(writer, http.StatusOK, delivery)
			return
		}
		writeError(writer, http.StatusConflict, "resource delivery was already claimed")
		return
	}
	writeJSON(writer, http.StatusOK, delivery)
}

func (s *Service) authorizedInvoice(writer http.ResponseWriter, request *http.Request) (invoiceRecord, bool) {
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		writeError(writer, http.StatusUnauthorized, "bearer claim token is required")
		return invoiceRecord{}, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	if token == "" {
		writeError(writer, http.StatusUnauthorized, "bearer claim token is required")
		return invoiceRecord{}, false
	}
	record, err := s.store.Authorized(request.Context(), request.PathValue("id"), token)
	if errors.Is(err, ErrInvoiceNotFound) || errors.Is(err, ErrUnauthorized) {
		writeError(writer, http.StatusNotFound, "invoice was not found")
		return invoiceRecord{}, false
	}
	if err != nil {
		s.logger.Error("authorize invoice", "error", err)
		writeError(writer, http.StatusInternalServerError, "invoice could not be read")
		return invoiceRecord{}, false
	}
	record.Signature = signInvoice(s.signingKey, record.Invoice)
	return record, true
}
