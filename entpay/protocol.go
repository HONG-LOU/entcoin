package entpay

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	ProtocolVersion  = "entpay-v1"
	maxArtifactBytes = 20 << 20
)

type InputField struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Placeholder string `json:"placeholder,omitempty"`
	Default     string `json:"default,omitempty"`
	MinLength   int    `json:"min_length,omitempty"`
	MaxLength   int    `json:"max_length,omitempty"`
	Required    bool   `json:"required"`
}

type ProductDescriptor struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	Price         uint64       `json:"price"`
	Confirmations uint64       `json:"confirmations"`
	Accent        string       `json:"accent"`
	Fields        []InputField `json:"fields"`
}

type Product interface {
	Descriptor() ProductDescriptor
	Validate(context.Context, json.RawMessage) error
	Fulfill(context.Context, FulfillmentRequest) (Fulfillment, error)
}

type FulfillmentRequest struct {
	InvoiceID     string          `json:"invoice_id"`
	TransactionID string          `json:"transaction_id"`
	Input         json.RawMessage `json:"input"`
}

type Fulfillment struct {
	Payload  json.RawMessage
	Artifact *Artifact
}

type Artifact struct {
	Contents  []byte
	MediaType string
	FileName  string
}

type Invoice struct {
	Protocol      string    `json:"protocol"`
	Network       string    `json:"network"`
	ID            string    `json:"id"`
	Merchant      string    `json:"merchant"`
	Amount        uint64    `json:"amount"`
	Resource      string    `json:"resource"`
	InputSHA256   string    `json:"input_sha256"`
	ExpiresAt     time.Time `json:"expires_at"`
	Confirmations uint64    `json:"confirmations"`
	Signature     string    `json:"signature"`
}

type Receipt struct {
	Protocol       string    `json:"protocol"`
	InvoiceID      string    `json:"invoice_id"`
	TransactionID  string    `json:"transaction_id"`
	Resource       string    `json:"resource"`
	InputSHA256    string    `json:"input_sha256"`
	PayloadSHA256  string    `json:"payload_sha256"`
	ArtifactSHA256 string    `json:"artifact_sha256,omitempty"`
	DeliveredAt    time.Time `json:"delivered_at"`
	Signature      string    `json:"signature"`
}

type ArtifactMetadata struct {
	DownloadPath string `json:"download_path"`
	SHA256       string `json:"sha256"`
	MediaType    string `json:"media_type"`
	FileName     string `json:"file_name"`
	Bytes        int64  `json:"bytes"`
}

type Delivery struct {
	Receipt  Receipt           `json:"receipt"`
	Payload  json.RawMessage   `json:"payload"`
	Artifact *ArtifactMetadata `json:"artifact,omitempty"`
}

type ServiceInfo struct {
	Protocol     string              `json:"protocol"`
	Network      string              `json:"network"`
	Merchant     string              `json:"merchant"`
	SigningKey   string              `json:"signing_key"`
	Products     []ProductDescriptor `json:"products"`
	Capabilities []string            `json:"capabilities"`
}

type CreateInvoiceRequest struct {
	Resource string          `json:"resource"`
	Input    json.RawMessage `json:"input"`
}

type CreateInvoiceResponse struct {
	Invoice    Invoice `json:"invoice"`
	ClaimToken string  `json:"claim_token"`
}

type SubmitPaymentRequest struct {
	Transaction json.RawMessage `json:"transaction"`
}

type PaymentStatus struct {
	InvoiceID     string `json:"invoice_id"`
	Status        string `json:"status"`
	TransactionID string `json:"transaction_id,omitempty"`
	Confirmations uint64 `json:"confirmations"`
	Required      uint64 `json:"required"`
	Attempt       uint64 `json:"attempt,omitempty"`
}

var errPermanentFulfillment = errors.New("permanent fulfillment failure")

func PermanentFailure(err error) error {
	if err == nil {
		return errPermanentFulfillment
	}
	return fmt.Errorf("%w: %v", errPermanentFulfillment, err)
}

func isPermanentFailure(err error) bool {
	return errors.Is(err, errPermanentFulfillment)
}

func signInvoice(privateKey ed25519.PrivateKey, value *Invoice) {
	value.Signature = signFields(privateKey,
		value.Protocol, value.Network, value.ID, value.Merchant,
		strconv.FormatUint(value.Amount, 10), value.Resource, value.InputSHA256,
		strconv.FormatInt(value.ExpiresAt.Unix(), 10), strconv.FormatUint(value.Confirmations, 10),
	)
}

func verifyInvoice(publicKey ed25519.PublicKey, value Invoice) bool {
	return verifyFields(publicKey, value.Signature,
		value.Protocol, value.Network, value.ID, value.Merchant,
		strconv.FormatUint(value.Amount, 10), value.Resource, value.InputSHA256,
		strconv.FormatInt(value.ExpiresAt.Unix(), 10), strconv.FormatUint(value.Confirmations, 10),
	)
}

func signReceipt(privateKey ed25519.PrivateKey, value *Receipt) {
	value.Signature = signFields(privateKey,
		value.Protocol, value.InvoiceID, value.TransactionID, value.Resource,
		value.InputSHA256, value.PayloadSHA256, value.ArtifactSHA256,
		strconv.FormatInt(value.DeliveredAt.Unix(), 10),
	)
}

func verifyReceipt(publicKey ed25519.PublicKey, value Receipt) bool {
	return verifyFields(publicKey, value.Signature,
		value.Protocol, value.InvoiceID, value.TransactionID, value.Resource,
		value.InputSHA256, value.PayloadSHA256, value.ArtifactSHA256,
		strconv.FormatInt(value.DeliveredAt.Unix(), 10),
	)
}

func signFields(privateKey ed25519.PrivateKey, fields ...string) string {
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(strings.Join(fields, "\n"))))
}

func verifyFields(publicKey ed25519.PublicKey, signatureText string, fields ...string) bool {
	signature, err := base64.RawURLEncoding.DecodeString(signatureText)
	return err == nil && ed25519.Verify(publicKey, []byte(strings.Join(fields, "\n")), signature)
}

func contentHash(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func inputHash(resource string, canonicalInput []byte) string {
	return contentHash([]byte(resource + "\n" + string(canonicalInput)))
}
