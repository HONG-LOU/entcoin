package entpay

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
)

const (
	ProtocolVersion = "entpay-v1"
	NetworkReport   = "network-report"
)

type Invoice struct {
	Protocol      string    `json:"protocol"`
	Network       string    `json:"network"`
	ID            string    `json:"id"`
	Merchant      string    `json:"merchant"`
	Amount        uint64    `json:"amount"`
	Resource      string    `json:"resource"`
	ExpiresAt     time.Time `json:"expires_at"`
	Confirmations uint64    `json:"confirmations"`
	Signature     string    `json:"signature"`
}

type CreateInvoiceRequest struct {
	Resource string `json:"resource"`
	Query    string `json:"query"`
}

type CreateInvoiceResponse struct {
	Invoice    Invoice `json:"invoice"`
	ClaimToken string  `json:"claim_token"`
}

type SubmitPaymentRequest struct {
	Transaction core.Transaction `json:"transaction"`
}

type PaymentStatus struct {
	InvoiceID     string `json:"invoice_id"`
	Status        string `json:"status"`
	TransactionID string `json:"transaction_id,omitempty"`
	Confirmations uint64 `json:"confirmations"`
	Required      uint64 `json:"required"`
}

type NodeStatus struct {
	URL       string `json:"url"`
	Protocol  string `json:"protocol,omitempty"`
	Name      string `json:"name,omitempty"`
	Symbol    string `json:"symbol,omitempty"`
	Height    uint64 `json:"height,omitempty"`
	TipHash   string `json:"tip_hash,omitempty"`
	ChainWork string `json:"chain_work,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Report struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Query       string       `json:"query"`
	Consistent  bool         `json:"consistent"`
	Nodes       []NodeStatus `json:"nodes"`
}

type Receipt struct {
	Protocol      string    `json:"protocol"`
	InvoiceID     string    `json:"invoice_id"`
	TransactionID string    `json:"transaction_id"`
	Resource      string    `json:"resource"`
	DeliveredAt   time.Time `json:"delivered_at"`
	Signature     string    `json:"signature"`
}

type Delivery struct {
	Receipt Receipt `json:"receipt"`
	Report  Report  `json:"report"`
}

type Info struct {
	Protocol      string   `json:"protocol"`
	Network       string   `json:"network"`
	Merchant      string   `json:"merchant"`
	Price         uint64   `json:"price"`
	Resource      string   `json:"resource"`
	Confirmations uint64   `json:"confirmations"`
	Nodes         []string `json:"nodes"`
	SigningKey    string   `json:"signing_key"`
}

func signInvoice(privateKey ed25519.PrivateKey, invoice Invoice) string {
	fields := []string{
		invoice.Protocol,
		invoice.Network,
		invoice.ID,
		invoice.Merchant,
		strconv.FormatUint(invoice.Amount, 10),
		invoice.Resource,
		strconv.FormatInt(invoice.ExpiresAt.Unix(), 10),
		strconv.FormatUint(invoice.Confirmations, 10),
	}
	return signFields(privateKey, fields...)
}

func signReceipt(privateKey ed25519.PrivateKey, receipt Receipt) string {
	return signFields(privateKey, receipt.Protocol, receipt.InvoiceID, receipt.TransactionID, receipt.Resource, strconv.FormatInt(receipt.DeliveredAt.Unix(), 10))
}

func verifyReceipt(publicKey ed25519.PublicKey, receipt Receipt) bool {
	signature, err := base64.RawURLEncoding.DecodeString(receipt.Signature)
	if err != nil {
		return false
	}
	return ed25519.Verify(publicKey, []byte(strings.Join([]string{
		receipt.Protocol,
		receipt.InvoiceID,
		receipt.TransactionID,
		receipt.Resource,
		strconv.FormatInt(receipt.DeliveredAt.Unix(), 10),
	}, "\n")), signature)
}

func signFields(privateKey ed25519.PrivateKey, fields ...string) string {
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(strings.Join(fields, "\n"))))
}

func verifyInvoice(publicKey ed25519.PublicKey, invoice Invoice) bool {
	signature, err := base64.RawURLEncoding.DecodeString(invoice.Signature)
	if err != nil {
		return false
	}
	unsigned := invoice
	unsigned.Signature = ""
	return ed25519.Verify(publicKey, []byte(strings.Join([]string{
		unsigned.Protocol,
		unsigned.Network,
		unsigned.ID,
		unsigned.Merchant,
		strconv.FormatUint(unsigned.Amount, 10),
		unsigned.Resource,
		strconv.FormatInt(unsigned.ExpiresAt.Unix(), 10),
		strconv.FormatUint(unsigned.Confirmations, 10),
	}, "\n")), signature)
}

func validatePayment(transaction core.Transaction, invoice Invoice) error {
	if transaction.Coinbase || transaction.ID == "" || transaction.ID != transaction.ComputeID() {
		return fmt.Errorf("payment transaction is invalid")
	}
	matching := 0
	for _, output := range transaction.Outputs {
		if core.AddressesEqual(output.Address, invoice.Merchant) && output.Amount == invoice.Amount {
			matching++
		}
	}
	if matching != 1 {
		return fmt.Errorf("payment must contain exactly one invoice output")
	}
	return nil
}
