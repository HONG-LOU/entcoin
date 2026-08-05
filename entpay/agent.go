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
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/node"
)

type Decision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

type ApprovalRequest struct {
	Invoice      Invoice           `json:"invoice"`
	Product      ProductDescriptor `json:"product"`
	Input        json.RawMessage   `json:"input"`
	MaximumAtoms uint64            `json:"maximum_atoms"`
}

type Reasoner interface {
	Approve(context.Context, ApprovalRequest) (Decision, error)
	Analyze(context.Context, Delivery) (string, error)
}

type Payment struct {
	TransactionID string          `json:"transaction_id"`
	Transaction   json.RawMessage `json:"transaction"`
}

type AgentConfig struct {
	Endpoint       string
	DataDirectory  string
	WalletAddress  string
	Resource       string
	Input          json.RawMessage
	MaximumAmount  uint64
	PaymentTimeout time.Duration
	ArtifactOutput string
	Reasoner       Reasoner
	Pay            func(context.Context, string, uint64) (Payment, error)
}

type AgentResult struct {
	Decision       Decision `json:"decision"`
	Invoice        Invoice  `json:"invoice"`
	TransactionID  string   `json:"transaction_id"`
	Delivery       Delivery `json:"delivery"`
	ArtifactOutput string   `json:"artifact_output,omitempty"`
	Analysis       string   `json:"analysis"`
}

func RunAgent(ctx context.Context, config AgentConfig) (AgentResult, error) {
	endpoint, err := normalizeEntPayURL(config.Endpoint)
	if err != nil {
		return AgentResult{}, err
	}
	canonicalInput, err := canonicalObject(config.Input)
	if err != nil {
		return AgentResult{}, err
	}
	config.Resource = strings.TrimSpace(config.Resource)
	if (config.Pay == nil && strings.TrimSpace(config.DataDirectory) == "") || !validIdentifier(config.Resource) || config.MaximumAmount == 0 || config.PaymentTimeout < time.Second || config.Reasoner == nil {
		return AgentResult{}, fmt.Errorf("agent configuration is incomplete")
	}
	if config.ArtifactOutput != "" {
		if err := validateOutputPath(config.ArtifactOutput); err != nil {
			return AgentResult{}, err
		}
	}

	client := &http.Client{Timeout: 20 * time.Second}
	var info ServiceInfo
	if err := agentJSON(ctx, client, http.MethodGet, endpoint+"v1/info", "", nil, &info); err != nil {
		return AgentResult{}, err
	}
	product, err := advertisedProduct(info, config.Resource)
	if err != nil {
		return AgentResult{}, err
	}
	request := CreateInvoiceRequest{Resource: config.Resource, Input: canonicalInput}
	var created CreateInvoiceResponse
	if err := agentJSON(ctx, client, http.MethodPost, endpoint+"v1/invoices", "", request, &created); err != nil {
		return AgentResult{}, err
	}
	publicKey, err := validateRemoteInvoice(info, product, created.Invoice, canonicalInput, config.MaximumAmount)
	if err != nil {
		return AgentResult{}, err
	}
	decision, err := config.Reasoner.Approve(ctx, ApprovalRequest{Invoice: created.Invoice, Product: product, Input: canonicalInput, MaximumAtoms: config.MaximumAmount})
	if err != nil {
		return AgentResult{}, fmt.Errorf("AI payment decision: %w", err)
	}
	if !decision.Approved {
		return AgentResult{}, fmt.Errorf("AI rejected payment: %s", decision.Reason)
	}

	pay := config.Pay
	if pay == nil {
		pay = localWalletPayment(config.DataDirectory, config.WalletAddress)
	}
	payment, err := pay(ctx, created.Invoice.Merchant, created.Invoice.Amount)
	if err != nil {
		return AgentResult{}, fmt.Errorf("pay invoice: %w", err)
	}
	if err := validatePaymentPayload(payment); err != nil {
		return AgentResult{}, err
	}
	var submitted PaymentStatus
	if err := agentJSON(ctx, client, http.MethodPost, endpoint+"v1/invoices/"+created.Invoice.ID+"/submit", created.ClaimToken, SubmitPaymentRequest{Transaction: payment.Transaction}, &submitted); err != nil {
		return AgentResult{}, err
	}

	delivery, err := waitForDelivery(ctx, client, endpoint, created, config.PaymentTimeout)
	if err != nil {
		return AgentResult{}, err
	}
	if err := validateDelivery(publicKey, created.Invoice, payment.TransactionID, delivery); err != nil {
		return AgentResult{}, err
	}
	artifactOutput := ""
	if delivery.Artifact != nil && config.ArtifactOutput != "" {
		if err := downloadArtifact(ctx, client, endpoint, created, delivery.Artifact, config.ArtifactOutput); err != nil {
			return AgentResult{}, err
		}
		artifactOutput, _ = filepath.Abs(config.ArtifactOutput)
	}
	analysis, err := config.Reasoner.Analyze(ctx, delivery)
	if err != nil {
		return AgentResult{}, fmt.Errorf("AI delivery analysis: %w", err)
	}
	return AgentResult{Decision: decision, Invoice: created.Invoice, TransactionID: payment.TransactionID, Delivery: delivery, ArtifactOutput: artifactOutput, Analysis: strings.TrimSpace(analysis)}, nil
}

func localWalletPayment(dataDirectory, walletAddress string) func(context.Context, string, uint64) (Payment, error) {
	return func(ctx context.Context, merchant string, amount uint64) (Payment, error) {
		walletNode, err := node.NewContext(ctx, node.Config{DataDirectory: dataDirectory, ListenAddress: "127.0.0.1:0", DisableDiscovery: true, BootstrapManifestURLs: []string{}})
		if err != nil {
			return Payment{}, fmt.Errorf("open agent wallet: %w", err)
		}
		dashboard, err := walletNode.Dashboard()
		if err != nil {
			_ = walletNode.Close(context.Background())
			return Payment{}, err
		}
		originalWallet := dashboard.Address
		switched := walletAddress != "" && !core.AddressesEqual(walletAddress, originalWallet)
		if switched {
			if err := walletNode.SwitchWallet(walletAddress); err != nil {
				_ = walletNode.Close(context.Background())
				return Payment{}, err
			}
		}
		transaction, _, sendErr := walletNode.SendRecommended(merchant, core.FormatAmount(amount))
		var restoreErr error
		if switched {
			restoreErr = walletNode.SwitchWallet(originalWallet)
		}
		closeErr := walletNode.Close(context.Background())
		if err := errors.Join(sendErr, restoreErr, closeErr); err != nil {
			return Payment{}, err
		}
		encoded, err := json.Marshal(transaction)
		if err != nil {
			return Payment{}, err
		}
		return Payment{TransactionID: transaction.ID, Transaction: encoded}, nil
	}
}

func advertisedProduct(info ServiceInfo, resource string) (ProductDescriptor, error) {
	if info.Protocol != ProtocolVersion || info.Network != core.NetworkID || core.ValidateAddress(info.Merchant) != nil || len(info.Products) == 0 {
		return ProductDescriptor{}, fmt.Errorf("EntPay service information violates agent policy")
	}
	required := []string{"input-bound-invoices", "payload-bound-receipts", "exact-output-verification", "confirmed-delivery", "idempotent-fulfillment", "authorized-artifacts"}
	for _, capability := range required {
		if !slices.Contains(info.Capabilities, capability) {
			return ProductDescriptor{}, fmt.Errorf("EntPay service is missing capability %q", capability)
		}
	}
	for _, product := range info.Products {
		if product.ID == resource {
			if err := validateProductDescriptor(product); err != nil {
				return ProductDescriptor{}, fmt.Errorf("advertised product is invalid: %w", err)
			}
			return product, nil
		}
	}
	return ProductDescriptor{}, fmt.Errorf("resource %q is not advertised", resource)
}

func validateRemoteInvoice(info ServiceInfo, product ProductDescriptor, invoice Invoice, input []byte, maximum uint64) (ed25519.PublicKey, error) {
	if product.Price > maximum || invoice.Protocol != info.Protocol || invoice.Network != info.Network || !core.AddressesEqual(invoice.Merchant, info.Merchant) || invoice.Amount != product.Price || invoice.Resource != product.ID || invoice.InputSHA256 != inputHash(product.ID, input) || invoice.Confirmations != product.Confirmations || time.Now().After(invoice.ExpiresAt) || invoice.ExpiresAt.After(time.Now().Add(time.Hour+time.Minute)) {
		return nil, fmt.Errorf("invoice does not match the advertised service or agent policy")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(info.SigningKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || !verifyInvoice(ed25519.PublicKey(publicKey), invoice) {
		return nil, fmt.Errorf("invoice signature is invalid")
	}
	return ed25519.PublicKey(publicKey), nil
}

func validatePaymentPayload(payment Payment) error {
	if strings.TrimSpace(payment.TransactionID) == "" || len(payment.Transaction) == 0 || len(payment.Transaction) > 1<<20 {
		return fmt.Errorf("payment callback returned an invalid transaction")
	}
	var transaction core.Transaction
	if json.Unmarshal(payment.Transaction, &transaction) != nil || transaction.ID != payment.TransactionID || transaction.ID != transaction.ComputeID() {
		return fmt.Errorf("payment callback returned an inconsistent transaction")
	}
	return nil
}

func waitForDelivery(ctx context.Context, client *http.Client, endpoint string, created CreateInvoiceResponse, timeout time.Duration) (Delivery, error) {
	deadline := time.Now().Add(timeout)
	for {
		claimContext, cancel := context.WithTimeout(ctx, 20*time.Second)
		var raw json.RawMessage
		status, err := agentJSONStatus(claimContext, client, http.MethodPost, endpoint+"v1/invoices/"+created.Invoice.ID+"/claim", created.ClaimToken, struct{}{}, &raw)
		cancel()
		if err != nil {
			return Delivery{}, err
		}
		if status == http.StatusOK {
			var delivery Delivery
			if err := json.Unmarshal(raw, &delivery); err != nil {
				return Delivery{}, fmt.Errorf("decode EntPay delivery: %w", err)
			}
			return delivery, nil
		}
		if status != http.StatusAccepted {
			return Delivery{}, fmt.Errorf("EntPay returned unexpected claim status %d", status)
		}
		if time.Now().After(deadline) {
			return Delivery{}, fmt.Errorf("payment was not confirmed before the deadline")
		}
		select {
		case <-ctx.Done():
			return Delivery{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func validateDelivery(publicKey ed25519.PublicKey, invoice Invoice, transactionID string, delivery Delivery) error {
	receipt := delivery.Receipt
	if receipt.Protocol != ProtocolVersion || receipt.InvoiceID != invoice.ID || receipt.TransactionID != transactionID || receipt.Resource != invoice.Resource || receipt.InputSHA256 != invoice.InputSHA256 || !verifyReceipt(publicKey, receipt) {
		return fmt.Errorf("delivery receipt is invalid")
	}
	payload, err := canonicalPayload(delivery.Payload)
	if err != nil || contentHash(payload) != receipt.PayloadSHA256 {
		return fmt.Errorf("delivery payload failed integrity verification")
	}
	if delivery.Artifact == nil {
		if receipt.ArtifactSHA256 != "" {
			return fmt.Errorf("delivery artifact metadata is missing")
		}
		return nil
	}
	artifact := delivery.Artifact
	if artifact.SHA256 == "" || artifact.SHA256 != receipt.ArtifactSHA256 || artifact.Bytes <= 0 || artifact.Bytes > maxArtifactBytes || !validMediaType(artifact.MediaType) || artifact.DownloadPath != "v1/invoices/"+invoice.ID+"/artifact" {
		return fmt.Errorf("delivery artifact metadata is invalid")
	}
	return nil
}

func downloadArtifact(ctx context.Context, client *http.Client, endpoint string, created CreateInvoiceResponse, metadata *ArtifactMetadata, output string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+metadata.DownloadPath, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+created.ClaimToken)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download fulfillment artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("artifact download returned HTTP %d", response.StatusCode)
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, maxArtifactBytes+1))
	if err != nil || int64(len(contents)) != metadata.Bytes || contentHash(contents) != metadata.SHA256 {
		return fmt.Errorf("downloaded artifact failed integrity verification")
	}
	if contentType := strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]); contentType != metadata.MediaType {
		return fmt.Errorf("downloaded artifact media type is invalid")
	}
	if err := writeAtomic(output, contents, 0o600); err != nil {
		return fmt.Errorf("save fulfillment artifact: %w", err)
	}
	return nil
}

func validateOutputPath(path string) error {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "." || strings.TrimSpace(path) == "" {
		return fmt.Errorf("artifact output path is invalid")
	}
	if _, err := os.Lstat(cleaned); err == nil {
		return fmt.Errorf("artifact output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect artifact output: %w", err)
	}
	parent := filepath.Dir(cleaned)
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("artifact output directory does not exist")
	}
	probe, err := os.CreateTemp(parent, ".entpay-output-check-*")
	if err != nil {
		return fmt.Errorf("artifact output directory is not writable: %w", err)
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)
	return nil
}

type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("EntPay returned HTTP %d: %s", e.Status, e.Body)
}

func agentJSON(ctx context.Context, client *http.Client, method, endpoint, token string, input, output any) error {
	_, err := agentJSONStatus(ctx, client, method, endpoint, token, input, output)
	return err
}

func agentJSONStatus(ctx context.Context, client *http.Client, method, endpoint, token string, input, output any) (int, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, err
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Accept", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("request EntPay: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return response.StatusCode, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, &HTTPError{Status: response.StatusCode, Body: strings.TrimSpace(string(contents))}
	}
	if output != nil {
		if err := json.Unmarshal(contents, output); err != nil {
			return response.StatusCode, fmt.Errorf("decode EntPay response: %w", err)
		}
	}
	return response.StatusCode, nil
}

func normalizeEntPayURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("EntPay endpoint must be an HTTPS URL")
	}
	if parsed.Scheme != "https" {
		address, addressErr := netip.ParseAddr(parsed.Hostname())
		if parsed.Scheme != "http" || (parsed.Hostname() != "localhost" && (addressErr != nil || !address.IsLoopback())) {
			return "", fmt.Errorf("EntPay endpoint must be HTTPS except on loopback")
		}
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/"
	return parsed.String(), nil
}
