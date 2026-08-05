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
	"strings"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/node"
)

type Decision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

type AgentResult struct {
	Decision    Decision `json:"decision"`
	Invoice     Invoice  `json:"invoice"`
	Transaction string   `json:"transaction_id"`
	Delivery    Delivery `json:"delivery"`
	Analysis    string   `json:"analysis"`
}

type Reasoner interface {
	Approve(context.Context, Invoice, Info, string, uint64) (Decision, error)
	Analyze(context.Context, Delivery) (string, error)
}

type AgentConfig struct {
	Endpoint       string
	DataDirectory  string
	WalletAddress  string
	Query          string
	MaximumAmount  uint64
	PaymentTimeout time.Duration
	Reasoner       Reasoner
	Pay            func(context.Context, string, uint64) (core.Transaction, error)
}

func RunAgent(ctx context.Context, config AgentConfig) (AgentResult, error) {
	endpoint, err := normalizeEntPayURL(config.Endpoint)
	if err != nil {
		return AgentResult{}, err
	}
	if (config.Pay == nil && strings.TrimSpace(config.DataDirectory) == "") || config.MaximumAmount == 0 || config.PaymentTimeout < time.Second || config.Reasoner == nil {
		return AgentResult{}, fmt.Errorf("agent configuration is incomplete")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	var info Info
	if err := agentJSON(ctx, client, http.MethodGet, endpoint+"v1/info", "", nil, &info); err != nil {
		return AgentResult{}, err
	}
	request := CreateInvoiceRequest{Resource: NetworkReport, Query: strings.TrimSpace(config.Query)}
	var created CreateInvoiceResponse
	if err := agentJSON(ctx, client, http.MethodPost, endpoint+"v1/invoices", "", request, &created); err != nil {
		return AgentResult{}, err
	}
	if err := validateRemoteInvoice(info, created.Invoice, config.MaximumAmount); err != nil {
		return AgentResult{}, err
	}
	decision, err := config.Reasoner.Approve(ctx, created.Invoice, info, request.Query, config.MaximumAmount)
	if err != nil {
		return AgentResult{}, fmt.Errorf("AI payment decision: %w", err)
	}
	if !decision.Approved {
		return AgentResult{}, fmt.Errorf("AI rejected payment: %s", decision.Reason)
	}
	if created.Invoice.Amount > config.MaximumAmount {
		return AgentResult{}, fmt.Errorf("invoice amount exceeds the enforced payment limit")
	}
	pay := config.Pay
	if pay == nil {
		pay = func(ctx context.Context, merchant string, amount uint64) (core.Transaction, error) {
			walletNode, err := node.NewContext(ctx, node.Config{
				DataDirectory: config.DataDirectory, ListenAddress: "127.0.0.1:0",
				DisableDiscovery: true, BootstrapManifestURLs: []string{},
			})
			if err != nil {
				return core.Transaction{}, fmt.Errorf("open agent wallet: %w", err)
			}
			dashboard, err := walletNode.Dashboard()
			if err != nil {
				_ = walletNode.Close(context.Background())
				return core.Transaction{}, err
			}
			originalWallet := dashboard.Address
			switched := config.WalletAddress != "" && !core.AddressesEqual(config.WalletAddress, originalWallet)
			if switched {
				if err := walletNode.SwitchWallet(config.WalletAddress); err != nil {
					_ = walletNode.Close(context.Background())
					return core.Transaction{}, err
				}
			}
			transaction, _, sendErr := walletNode.SendRecommended(merchant, core.FormatAmount(amount))
			var restoreErr error
			if switched {
				restoreErr = walletNode.SwitchWallet(originalWallet)
			}
			closeErr := walletNode.Close(context.Background())
			if err := errors.Join(sendErr, restoreErr, closeErr); err != nil {
				return core.Transaction{}, err
			}
			return transaction, nil
		}
	}
	transaction, err := pay(ctx, created.Invoice.Merchant, created.Invoice.Amount)
	if err != nil {
		return AgentResult{}, fmt.Errorf("pay invoice: %w", err)
	}
	var submitted PaymentStatus
	if err := agentJSON(ctx, client, http.MethodPost, endpoint+"v1/invoices/"+created.Invoice.ID+"/submit", created.ClaimToken, SubmitPaymentRequest{Transaction: transaction}, &submitted); err != nil {
		return AgentResult{}, err
	}
	deadline := time.Now().Add(config.PaymentTimeout)
	var delivery Delivery
	for {
		claimContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		var status int
		status, err = agentJSONStatus(claimContext, client, http.MethodPost, endpoint+"v1/invoices/"+created.Invoice.ID+"/claim", created.ClaimToken, struct{}{}, &delivery)
		cancel()
		if err != nil {
			return AgentResult{}, err
		}
		if status == http.StatusOK {
			break
		}
		if status != http.StatusAccepted {
			return AgentResult{}, fmt.Errorf("EntPay returned unexpected claim status %d", status)
		}
		if time.Now().After(deadline) {
			return AgentResult{}, fmt.Errorf("payment was not confirmed before the deadline")
		}
		select {
		case <-ctx.Done():
			return AgentResult{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	publicKey, _ := base64.RawURLEncoding.DecodeString(info.SigningKey)
	if delivery.Receipt.InvoiceID != created.Invoice.ID || delivery.Receipt.TransactionID != transaction.ID || delivery.Receipt.Resource != created.Invoice.Resource || !verifyReceipt(ed25519.PublicKey(publicKey), delivery.Receipt) {
		return AgentResult{}, fmt.Errorf("delivery receipt is invalid")
	}
	analysis, err := config.Reasoner.Analyze(ctx, delivery)
	if err != nil {
		return AgentResult{}, fmt.Errorf("AI delivery analysis: %w", err)
	}
	return AgentResult{
		Decision: decision, Invoice: created.Invoice, Transaction: transaction.ID,
		Delivery: delivery, Analysis: strings.TrimSpace(analysis),
	}, nil
}

func validateRemoteInvoice(info Info, invoice Invoice, maximum uint64) error {
	if info.Protocol != ProtocolVersion || info.Network != core.NetworkID || info.Resource != NetworkReport || info.Price == 0 || info.Price > maximum {
		return fmt.Errorf("EntPay service information violates agent policy")
	}
	if invoice.Protocol != info.Protocol || invoice.Network != info.Network || invoice.Merchant != info.Merchant || invoice.Amount != info.Price || invoice.Resource != info.Resource || invoice.Confirmations != info.Confirmations || time.Now().After(invoice.ExpiresAt) {
		return fmt.Errorf("invoice does not match the advertised service")
	}
	if err := core.ValidateAddress(invoice.Merchant); err != nil {
		return fmt.Errorf("invoice merchant is invalid: %w", err)
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(info.SigningKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || !verifyInvoice(ed25519.PublicKey(publicKey), invoice) {
		return fmt.Errorf("invoice signature is invalid")
	}
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
