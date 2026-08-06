package entpay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/node"
)

type ClientPaymentBackend interface {
	Address() string
	Dashboard() (node.Dashboard, error)
	PreviewRecommendedPayment(expectedWallet, to string, amount uint64) (node.PaymentPreview, error)
	PrepareRecommendedPayment(expectedWallet, to string, amount, maximumFee uint64) (core.Transaction, uint64, error)
	CommitPreparedPayment(expectedWallet, to string, amount, fee, maximumFee uint64, transaction core.Transaction) error
}

type ClientManager struct {
	ctx               context.Context
	store             *ClientStore
	payments          ClientPaymentBackend
	maximumAmount     uint64
	artifactDirectory string
	httpClient        *http.Client
	mu                sync.Mutex
	active            map[string]struct{}
	wait              sync.WaitGroup
	listenerMu        sync.RWMutex
	listener          func(ClientSession)
	settingsMu        sync.RWMutex
}

type ClientSessionDetail struct {
	Session ClientSession   `json:"session"`
	Input   json.RawMessage `json:"input,omitempty"`
}

func (m *ClientManager) SetListener(listener func(ClientSession)) {
	m.listenerMu.Lock()
	m.listener = listener
	m.listenerMu.Unlock()
}

// Emit publishes a persisted public session snapshot to the desktop listener.
func (m *ClientManager) Emit(session ClientSession) {
	m.notify(session)
}

func (m *ClientManager) Sessions(ctx context.Context, limit int) ([]ClientSession, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	return m.store.Sessions(ctx, limit)
}

func (m *ClientManager) SessionDetail(ctx context.Context, id string) (ClientSessionDetail, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	session, err := m.store.Session(ctx, id)
	if err != nil {
		return ClientSessionDetail{}, err
	}
	detail := ClientSessionDetail{Session: session}
	if session.Stage == StageAwaitingApproval {
		capsule, capsuleErr := m.store.Capsule(ctx, id)
		if capsuleErr != nil {
			return ClientSessionDetail{}, capsuleErr
		}
		detail.Input = append(json.RawMessage(nil), capsule.Input...)
	}
	return detail, nil
}

func (m *ClientManager) StartContinuation(id string) {
	m.launch(func() { m.notifyResult(m.Continue(m.ctx, id)) })
}

func (m *ClientManager) StartRetry(id string, expectedRevision uint64) {
	m.launch(func() { m.notifyResult(m.Retry(m.ctx, id, expectedRevision)) })
}

func (m *ClientManager) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func NewClientManager(ctx context.Context, store *ClientStore, payments ClientPaymentBackend, maximumAmount uint64, artifactDirectory string) (*ClientManager, error) {
	if ctx == nil || store == nil || payments == nil || maximumAmount == 0 || strings.TrimSpace(artifactDirectory) == "" {
		return nil, fmt.Errorf("EntPay client manager configuration is incomplete")
	}
	settings, err := store.EnsureSettings(ctx, maximumAmount, artifactDirectory)
	if err != nil {
		return nil, err
	}
	return &ClientManager{
		ctx: ctx, store: store, payments: payments, maximumAmount: settings.MaximumAmount, artifactDirectory: settings.ArtifactDirectory,
		httpClient: &http.Client{Timeout: 20 * time.Second}, active: make(map[string]struct{}),
	}, nil
}

func (m *ClientManager) Settings(ctx context.Context) (ClientSettings, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	return m.store.Settings(ctx)
}

func (m *ClientManager) SaveSettings(ctx context.Context, settings ClientSettings, expectedRevision uint64) (ClientSettings, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	updated, err := m.store.SaveSettings(ctx, settings, expectedRevision)
	if err != nil {
		return ClientSettings{}, err
	}
	m.settingsMu.Lock()
	m.maximumAmount = updated.MaximumAmount
	m.artifactDirectory = updated.ArtifactDirectory
	m.settingsMu.Unlock()
	return updated, nil
}

func (m *ClientManager) DeleteSession(ctx context.Context, id string) error {
	if ctx == nil {
		ctx = m.ctx
	}
	return m.store.DeleteSession(ctx, id)
}

func (m *ClientManager) Continue(ctx context.Context, id string) (ClientSession, error) {
	if !m.begin(id) {
		return ClientSession{}, ErrClientRevision
	}
	defer m.end(id)
	if ctx == nil {
		ctx = m.ctx
	}
	session, err := m.store.Session(ctx, id)
	if err != nil {
		return ClientSession{}, err
	}
	if session.TransactionID == "" || session.Invoice == nil {
		return ClientSession{}, ErrClientStage
	}
	if session.Stage == StageBroadcast {
		session, err = m.store.transition(ctx, id, session.Revision, StageSubmitting, "", "")
		if err != nil {
			return ClientSession{}, err
		}
		m.notify(session)
	}
	if session.Stage == StageSubmitting {
		if err := m.submit(ctx, session); err != nil {
			return m.fail(ctx, session, StageFailedRetryable, "submit_unavailable", "Payment was sent. Merchant submission will be retried.", err)
		}
		session, err = m.store.transition(ctx, id, session.Revision, StageConfirming, "", "")
		if err != nil {
			return ClientSession{}, err
		}
		m.notify(session)
	}
	if session.Stage != StageConfirming && session.Stage != StageFulfilling {
		return ClientSession{}, ErrClientStage
	}
	for {
		delivery, status, err := m.claim(ctx, session)
		if err != nil {
			return m.fail(ctx, session, StageFailedRetryable, "delivery_unavailable", "Payment was sent. Delivery checking will resume automatically.", err)
		}
		if delivery != nil {
			session, err = m.store.SaveDelivery(ctx, id, session.Revision, *delivery)
			if err != nil {
				return ClientSession{}, err
			}
			return m.verifyDelivery(ctx, session, *delivery)
		}
		if status.TransactionID != session.TransactionID || status.InvoiceID != session.Invoice.ID || status.Required != session.Invoice.Confirmations {
			return m.fail(ctx, session, StageFailedTerminal, "merchant_status_invalid", "Payment was sent. The merchant returned an invalid payment status.", nil)
		}
		session, err = m.store.SaveConfirmations(ctx, id, session.Revision, status.Confirmations)
		if err != nil {
			return ClientSession{}, err
		}
		m.notify(session)
		if status.Status == "processing" && session.Stage == StageConfirming {
			session, err = m.store.transition(ctx, id, session.Revision, StageFulfilling, "", "")
			if err != nil {
				return ClientSession{}, err
			}
			m.notify(session)
		} else if status.Status != "confirming" && status.Status != "processing" {
			return m.fail(ctx, session, StageFailedTerminal, "merchant_status_invalid", "Payment was sent. The merchant returned an unknown delivery state.", nil)
		}
		select {
		case <-ctx.Done():
			return session, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (m *ClientManager) submit(ctx context.Context, session ClientSession) error {
	capsule, err := m.store.Capsule(ctx, session.ID)
	if err != nil {
		return err
	}
	transaction, err := m.store.Transaction(ctx, session.ID)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(transaction)
	if err != nil {
		return err
	}
	var status PaymentStatus
	if err := agentJSON(ctx, m.httpClient, http.MethodPost, session.Endpoint+"v1/invoices/"+session.Invoice.ID+"/submit", capsule.Created.ClaimToken, SubmitPaymentRequest{Transaction: encoded}, &status); err != nil {
		return err
	}
	if status.InvoiceID != session.Invoice.ID || status.TransactionID != session.TransactionID || status.Status != "submitted" || status.Required != session.Invoice.Confirmations {
		return fmt.Errorf("merchant submission response is inconsistent")
	}
	return nil
}

func (m *ClientManager) claim(ctx context.Context, session ClientSession) (*Delivery, PaymentStatus, error) {
	capsule, err := m.store.Capsule(ctx, session.ID)
	if err != nil {
		return nil, PaymentStatus{}, err
	}
	var raw json.RawMessage
	statusCode, err := agentJSONStatus(ctx, m.httpClient, http.MethodPost, session.Endpoint+"v1/invoices/"+session.Invoice.ID+"/claim", capsule.Created.ClaimToken, struct{}{}, &raw)
	if err != nil {
		return nil, PaymentStatus{}, err
	}
	if statusCode == http.StatusOK {
		var delivery Delivery
		if err := json.Unmarshal(raw, &delivery); err != nil {
			return nil, PaymentStatus{}, fmt.Errorf("decode EntPay delivery: %w", err)
		}
		return &delivery, PaymentStatus{}, nil
	}
	if statusCode != http.StatusAccepted {
		return nil, PaymentStatus{}, fmt.Errorf("unexpected EntPay claim status %d", statusCode)
	}
	var status PaymentStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil, PaymentStatus{}, fmt.Errorf("decode EntPay payment status: %w", err)
	}
	return nil, status, nil
}

func (m *ClientManager) verifyDelivery(ctx context.Context, session ClientSession, delivery Delivery) (ClientSession, error) {
	publicKey, err := base64.RawURLEncoding.DecodeString(session.SigningKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || validateDelivery(ed25519.PublicKey(publicKey), *session.Invoice, session.TransactionID, delivery) != nil {
		return m.fail(ctx, session, StageFailedRetryable, "delivery_invalid", "Payment was sent. The delivery failed Receipt or payload verification. Retry will request a fresh delivery.", nil)
	}
	artifactPath := ""
	if delivery.Artifact != nil {
		_, artifactDirectory := m.configuration()
		artifactPath, err = downloadClientArtifact(ctx, m.httpClient, session.Endpoint, session.Invoice.ID, artifactDirectory, session.ID, delivery, func() (string, error) {
			capsule, err := m.store.Capsule(ctx, session.ID)
			return capsule.Created.ClaimToken, err
		})
		if err != nil {
			return m.fail(ctx, session, StageFailedRetryable, "artifact_unavailable", "Payment was sent. The verified artifact could not be saved and will be retried.", err)
		}
	}
	return m.store.Complete(ctx, session.ID, session.Revision, artifactPath, delivery.Artifact)
}

func (m *ClientManager) Receive(ctx context.Context, launch LaunchRequest) (ClientSession, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	nonce, err := newOpaqueToken()
	if err != nil {
		return ClientSession{}, err
	}
	session, err := m.store.CreateReceived(ctx, launch, nonce)
	if err != nil || session.Stage != StageReceived {
		return session, err
	}
	session, err = m.store.transition(ctx, session.ID, session.Revision, StageInspecting, "", "")
	if err != nil {
		return ClientSession{}, err
	}
	m.notify(session)
	return m.inspect(ctx, session)
}

func (m *ClientManager) ReceiveQueued(ctx context.Context, launch LaunchRequest) (ClientSession, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	nonce, err := newOpaqueToken()
	if err != nil {
		return ClientSession{}, err
	}
	session, err := m.store.CreateReceived(ctx, launch, nonce)
	if err != nil {
		return ClientSession{}, err
	}
	switch session.Stage {
	case StageReceived:
		session, err = m.store.transition(ctx, session.ID, session.Revision, StageInspecting, "", "")
		if err != nil {
			return ClientSession{}, err
		}
		m.notify(session)
		current := session
		m.launch(func() { m.notifyResult(m.inspect(m.ctx, current)) })
	case StageInspecting:
		current := session
		m.launch(func() { m.notifyResult(m.inspect(m.ctx, current)) })
	}
	return session, nil
}

func (m *ClientManager) inspect(ctx context.Context, session ClientSession) (ClientSession, error) {
	capsule, err := m.store.Capsule(ctx, session.ID)
	if err != nil {
		return m.fail(ctx, session, StageFailedRetryable, "secret_unavailable", "Payment has not been sent. The protected request could not be opened.", err)
	}
	if capsule.Created.ClaimToken == "" {
		redeemed, err := m.redeem(ctx, session.Endpoint, capsule.HandoffCode, capsule.ClientNonce)
		if err != nil {
			return m.fail(ctx, session, StageFailedRetryable, "handoff_unavailable", "Payment has not been sent. The payment link could not be redeemed.", err)
		}
		if redeemed.Endpoint != session.Endpoint {
			return m.fail(ctx, session, StageInvalid, "endpoint_mismatch", "Payment has not been sent. The merchant endpoint did not match the payment link.", nil)
		}
		session, err = m.store.SaveRedeemedCapsule(ctx, session.ID, session.Revision, redeemed)
		if err != nil {
			return ClientSession{}, err
		}
		capsule = clientSessionCapsule{Input: redeemed.Input, Created: redeemed.Created}
	}
	maximumAmount, _ := m.configuration()
	inspection, err := InspectPreparedPaymentDetails(ctx, session.Endpoint, capsule.Input, maximumAmount, capsule.Created)
	if err != nil {
		return m.fail(ctx, session, terminalInspectionStage(capsule.Created.Invoice), "invoice_invalid", "Payment has not been sent. The signed invoice failed verification.", err)
	}
	wallet := m.payments.Address()
	preview, err := m.payments.PreviewRecommendedPayment(wallet, inspection.Approval.Invoice.Merchant, inspection.Approval.Invoice.Amount)
	if err != nil {
		return m.fail(ctx, session, StageFailedRetryable, "preview_unavailable", "Payment has not been sent. The wallet preview is unavailable.", err)
	}
	return m.store.SaveReview(ctx, session.ID, session.Revision, inspection.Service, inspection.Approval.Product, inspection.Approval.Invoice, preview)
}

func (m *ClientManager) Approve(ctx context.Context, id string, expectedRevision uint64) (ClientSession, error) {
	if !m.begin(id) {
		return ClientSession{}, ErrClientRevision
	}
	defer m.end(id)
	if ctx == nil {
		ctx = m.ctx
	}
	session, err := m.store.Session(ctx, id)
	if err != nil {
		return ClientSession{}, err
	}
	if session.Revision != expectedRevision {
		return ClientSession{}, ErrClientRevision
	}
	if session.Stage != StageAwaitingApproval || session.Invoice == nil || session.Product == nil {
		return ClientSession{}, ErrClientStage
	}
	if session.MerchantTrustState == "changed" {
		return ClientSession{}, fmt.Errorf("merchant identity changed; review and re-establish trust before payment")
	}
	if time.Now().After(session.Invoice.ExpiresAt) {
		return m.store.transition(ctx, id, expectedRevision, StageExpired, "invoice_expired", "Payment has not been sent. The invoice expired before approval.")
	}
	dashboard, err := m.payments.Dashboard()
	if err != nil || dashboard.PeerCount < 1 || dashboard.Syncing || dashboard.BestPeerHeight > dashboard.Height || dashboard.WalletNeedsBackup {
		return ClientSession{}, fmt.Errorf("payment readiness check failed: connect and sync the node, then secure the wallet recovery material")
	}
	preview, err := m.payments.PreviewRecommendedPayment(session.WalletAddress, session.Invoice.Merchant, session.Invoice.Amount)
	if err != nil {
		return ClientSession{}, err
	}
	if preview.Wallet != session.WalletAddress || preview.WalletNeedsBackup || !preview.HasSufficientFunds || preview.MaximumFee > session.FeeCeiling {
		return ClientSession{}, fmt.Errorf("payment terms changed after review; refresh the payment request")
	}
	session, err = m.store.transition(ctx, id, expectedRevision, StagePreparingPayment, "", "")
	if err != nil {
		return ClientSession{}, err
	}
	m.notify(session)
	var transaction core.Transaction
	fee := session.EstimatedFee
	if session.TransactionID == "" {
		transaction, fee, err = m.payments.PrepareRecommendedPayment(session.WalletAddress, session.Invoice.Merchant, session.Invoice.Amount, session.FeeCeiling)
		if err != nil {
			return m.store.transition(ctx, id, session.Revision, StageAwaitingApproval, "payment_preparation_failed", "Payment has not been sent. The transaction could not be prepared.")
		}
		session, err = m.store.JournalPayment(ctx, id, session.Revision, transaction, fee)
		if err != nil {
			return ClientSession{}, err
		}
		m.notify(session)
	} else {
		transaction, err = m.store.Transaction(ctx, id)
		if err != nil {
			return ClientSession{}, err
		}
	}
	if err := m.payments.CommitPreparedPayment(session.WalletAddress, session.Invoice.Merchant, session.Invoice.Amount, fee, session.FeeCeiling, transaction); err != nil {
		return m.store.transition(ctx, id, session.Revision, StageAwaitingApproval, "payment_not_broadcast", "Payment has not been sent. The prepared transaction was not broadcast.")
	}
	return m.store.transition(ctx, id, session.Revision, StageBroadcast, "", "")
}

func (m *ClientManager) Reject(ctx context.Context, id string, expectedRevision uint64) (ClientSession, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	return m.store.TerminateWithoutPayment(ctx, id, expectedRevision, StageRejected, "rejected", "Payment rejected; no transaction was created.")
}

func (m *ClientManager) ReestablishMerchantTrust(ctx context.Context, id string, expectedRevision uint64) (ClientSession, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	return m.store.ReestablishMerchantTrust(ctx, id, expectedRevision)
}

func (m *ClientManager) Retry(ctx context.Context, id string, expectedRevision uint64) (ClientSession, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	session, err := m.store.Session(ctx, id)
	if err != nil {
		return ClientSession{}, err
	}
	if session.Revision != expectedRevision || session.Stage != StageFailedRetryable || !validClientStage(session.RetryStage) {
		return ClientSession{}, ErrClientRevision
	}
	retryStage := session.RetryStage
	if session.ErrorCode == "delivery_invalid" {
		retryStage = StageFulfilling
	}
	session, err = m.store.transition(ctx, id, expectedRevision, retryStage, "", "")
	if err != nil {
		return ClientSession{}, err
	}
	switch session.Stage {
	case StageInspecting:
		return m.inspect(ctx, session)
	case StageSubmitting, StageConfirming, StageFulfilling:
		return m.Continue(ctx, id)
	case StageVerifying:
		if session.Invoice == nil || session.Receipt == nil {
			return ClientSession{}, fmt.Errorf("stored EntPay delivery is incomplete")
		}
		return m.verifyDelivery(ctx, session, Delivery{Receipt: *session.Receipt, Payload: session.Payload, Artifact: session.DeliveryArtifact})
	default:
		return ClientSession{}, ErrClientStage
	}
}

func (m *ClientManager) Recover() error {
	if err := m.store.PurgeExpiredRequestDetails(m.ctx); err != nil {
		return err
	}
	sessions, err := m.store.Sessions(m.ctx, 500)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		switch session.Stage {
		case StageReceived:
			updated, transitionErr := m.store.transition(m.ctx, session.ID, session.Revision, StageInspecting, "", "")
			if transitionErr != nil {
				return transitionErr
			}
			m.notify(updated)
			m.launch(func() { m.notifyResult(m.inspect(m.ctx, updated)) })
		case StageInspecting:
			current := session
			m.launch(func() { m.notifyResult(m.inspect(m.ctx, current)) })
		case StagePreparingPayment:
			if updated, transitionErr := m.store.transition(m.ctx, session.ID, session.Revision, StageAwaitingApproval, "approval_interrupted", "Payment has not been sent. Review the request again before broadcasting the saved transaction."); transitionErr != nil {
				return transitionErr
			} else {
				m.notify(updated)
			}
		case StageBroadcast, StageSubmitting, StageConfirming, StageFulfilling:
			id := session.ID
			m.launch(func() { m.notifyResult(m.Continue(m.ctx, id)) })
		case StageVerifying:
			current := session
			m.launch(func() {
				if current.Invoice != nil && current.Receipt != nil {
					m.notifyResult(m.verifyDelivery(m.ctx, current, Delivery{Receipt: *current.Receipt, Payload: current.Payload, Artifact: current.DeliveryArtifact}))
				}
			})
		case StageFailedRetryable:
			if session.TransactionID != "" {
				current := session
				m.launch(func() { m.notifyResult(m.Retry(m.ctx, current.ID, current.Revision)) })
			}
		}
	}
	return nil
}

func (m *ClientManager) launch(operation func()) {
	m.wait.Add(1)
	go func() {
		defer m.wait.Done()
		operation()
	}()
}

func (m *ClientManager) notify(session ClientSession) {
	if session.ID == "" {
		return
	}
	m.listenerMu.RLock()
	listener := m.listener
	m.listenerMu.RUnlock()
	if listener != nil {
		listener(session)
	}
}

func (m *ClientManager) notifyResult(session ClientSession, _ error) {
	m.notify(session)
}

func (m *ClientManager) redeem(ctx context.Context, endpoint, code, nonce string) (HandoffCapsule, error) {
	if validateOpaqueToken(code) != nil || validateOpaqueToken(nonce) != nil {
		return HandoffCapsule{}, fmt.Errorf("handoff secret is invalid")
	}
	encoded, err := json.Marshal(RedeemHandoffRequest{Code: code, ClientNonce: nonce})
	if err != nil {
		return HandoffCapsule{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"v1/handoffs/redeem", bytes.NewReader(encoded))
	if err != nil {
		return HandoffCapsule{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := m.httpClient.Do(request)
	if err != nil {
		return HandoffCapsule{}, fmt.Errorf("redeem EntPay handoff: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, maxRequestBytes+1))
	if err != nil || len(contents) > maxRequestBytes {
		return HandoffCapsule{}, fmt.Errorf("EntPay handoff response is invalid")
	}
	if response.StatusCode != http.StatusOK {
		return HandoffCapsule{}, fmt.Errorf("EntPay handoff returned HTTP %d", response.StatusCode)
	}
	if !strings.EqualFold(strings.TrimSpace(response.Header.Get("Cache-Control")), "no-store") {
		return HandoffCapsule{}, fmt.Errorf("EntPay handoff response is cacheable")
	}
	if err := rejectDuplicateJSONKeys(contents); err != nil {
		return HandoffCapsule{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var capsule HandoffCapsule
	if err := decoder.Decode(&capsule); err != nil {
		return HandoffCapsule{}, fmt.Errorf("decode EntPay handoff: %w", err)
	}
	return capsule, nil
}

func (m *ClientManager) fail(ctx context.Context, session ClientSession, stage ClientStage, code, message string, cause error) (ClientSession, error) {
	var updated ClientSession
	var err error
	if (stage == StageInvalid || stage == StageExpired) && session.TransactionID == "" {
		updated, err = m.store.TerminateWithoutPayment(ctx, session.ID, session.Revision, stage, code, message)
	} else {
		updated, err = m.store.transition(ctx, session.ID, session.Revision, stage, code, message)
	}
	if err != nil {
		return ClientSession{}, errors.Join(cause, err)
	}
	return updated, cause
}

func terminalInspectionStage(invoice Invoice) ClientStage {
	if !invoice.ExpiresAt.IsZero() && time.Now().After(invoice.ExpiresAt) {
		return StageExpired
	}
	return StageInvalid
}

func (m *ClientManager) begin(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.active[id]; exists {
		return false
	}
	m.active[id] = struct{}{}
	return true
}

func (m *ClientManager) end(id string) {
	m.mu.Lock()
	delete(m.active, id)
	m.mu.Unlock()
}

func (m *ClientManager) configuration() (uint64, string) {
	m.settingsMu.RLock()
	defer m.settingsMu.RUnlock()
	return m.maximumAmount, m.artifactDirectory
}
