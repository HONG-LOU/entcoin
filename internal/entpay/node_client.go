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
	"sync"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"
)

type walletSnapshot struct {
	History []ledger.TransactionRecord `json:"history"`
}

type NodeClient struct {
	httpClient *http.Client
	localNode  string
	reportURLs []string
}

func NewNodeClient(localNode string, reportURLs []string) (*NodeClient, error) {
	local, err := normalizeNodeURL(localNode)
	if err != nil {
		return nil, fmt.Errorf("local node URL: %w", err)
	}
	normalized := make([]string, 0, len(reportURLs))
	for _, candidate := range reportURLs {
		nodeURL, err := normalizeNodeURL(candidate)
		if err != nil {
			return nil, fmt.Errorf("report node URL: %w", err)
		}
		normalized = append(normalized, nodeURL)
	}
	return &NodeClient{
		httpClient: &http.Client{Timeout: 8 * time.Second},
		localNode:  local,
		reportURLs: normalized,
	}, nil
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

func (c *NodeClient) AcceptTransaction(ctx context.Context, transaction core.Transaction, merchant string, amount uint64) error {
	body, err := json.Marshal(transaction)
	if err != nil {
		return fmt.Errorf("encode transaction: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.localNode+"/v2/transactions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("submit transaction to node: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusAccepted {
		return nil
	}
	if response.StatusCode == http.StatusConflict {
		found, _, observeErr := c.ObservePayment(ctx, merchant, transaction.ID, amount)
		if observeErr == nil && found {
			return nil
		}
	}
	message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	return fmt.Errorf("node rejected transaction with HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
}

func (c *NodeClient) ObservePayment(ctx context.Context, merchant, transactionID string, amount uint64) (bool, uint64, error) {
	var snapshot walletSnapshot
	if err := c.getJSON(ctx, c.localNode+"/v2/wallet/"+url.PathEscape(merchant), &snapshot); err != nil {
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

func (c *NodeClient) Report(ctx context.Context, query string) Report {
	statuses := make([]NodeStatus, len(c.reportURLs))
	var wait sync.WaitGroup
	for index, nodeURL := range c.reportURLs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			status := NodeStatus{URL: nodeURL}
			if err := c.getJSON(ctx, nodeURL+"/v2/status", &status); err != nil {
				status.Error = err.Error()
			}
			statuses[index] = status
		}()
	}
	wait.Wait()
	consistent := len(statuses) > 1
	for index, status := range statuses {
		if status.Error != "" || status.Protocol != core.NetworkID {
			consistent = false
		}
		if index > 0 && (status.Height != statuses[0].Height || status.TipHash != statuses[0].TipHash || status.ChainWork != statuses[0].ChainWork) {
			consistent = false
		}
	}
	return Report{GeneratedAt: time.Now().UTC(), Query: query, Consistent: consistent, Nodes: statuses}
}

func (c *NodeClient) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
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
