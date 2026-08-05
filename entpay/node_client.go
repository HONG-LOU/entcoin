package entpay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"
)

type walletSnapshot struct {
	History []ledger.TransactionRecord `json:"history"`
}

type nodeStatus struct {
	Protocol string `json:"protocol"`
	Height   uint64 `json:"height"`
	TipHash  string `json:"tip_hash"`
}

type paymentNode struct {
	httpClient *http.Client
	baseURL    string
}

func newPaymentNode(baseURL string) (*paymentNode, error) {
	normalized, err := normalizeNodeURL(baseURL)
	if err != nil {
		return nil, err
	}
	return &paymentNode{httpClient: &http.Client{Timeout: 8 * time.Second}, baseURL: normalized}, nil
}

func normalizeNodeURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid HTTP node URL")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("node URL must not contain a path")
	}
	parsed.Path = ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func (n *paymentNode) Status(ctx context.Context) (nodeStatus, error) {
	var status nodeStatus
	if err := n.getJSON(ctx, n.baseURL+"/v2/status", &status); err != nil {
		return nodeStatus{}, err
	}
	if status.Protocol != core.NetworkID || status.TipHash == "" {
		return nodeStatus{}, fmt.Errorf("payment node status is invalid")
	}
	return status, nil
}

func (n *paymentNode) AcceptTransaction(ctx context.Context, transaction core.Transaction, merchant string, amount uint64) error {
	body, err := json.Marshal(transaction)
	if err != nil {
		return fmt.Errorf("encode transaction: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+"/v2/transactions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := n.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("submit transaction to node: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusAccepted {
		return nil
	}
	if response.StatusCode == http.StatusConflict {
		found, _, observeErr := n.ObservePayment(ctx, merchant, transaction.ID, amount)
		if observeErr == nil && found {
			return nil
		}
	}
	return fmt.Errorf("node rejected transaction with HTTP %d", response.StatusCode)
}

func (n *paymentNode) ObservePayment(ctx context.Context, merchant, transactionID string, amount uint64) (bool, uint64, error) {
	var snapshot walletSnapshot
	if err := n.getJSON(ctx, n.baseURL+"/v2/wallet/"+url.PathEscape(merchant), &snapshot); err != nil {
		return false, 0, err
	}
	for _, record := range snapshot.History {
		if record.ID != transactionID {
			continue
		}
		matching := 0
		for _, output := range record.Transaction.Outputs {
			if core.AddressesEqual(output.Address, merchant) && output.Amount == amount {
				matching++
			}
		}
		if matching == 1 {
			return true, record.Confirmations, nil
		}
	}
	return false, 0, nil
}

func (n *paymentNode) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := n.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request node: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("node returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode node response: %w", err)
	}
	return nil
}
