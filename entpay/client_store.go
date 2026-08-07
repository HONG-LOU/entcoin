package entpay

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/node"

	_ "modernc.org/sqlite"
)

const clientStoreSchema = 1

type ClientStage string

const (
	StageReceived         ClientStage = "received"
	StageInspecting       ClientStage = "inspecting"
	StageAwaitingApproval ClientStage = "awaiting_approval"
	StagePreparingPayment ClientStage = "preparing_payment"
	StageBroadcast        ClientStage = "broadcast"
	StageSubmitting       ClientStage = "submitting"
	StageConfirming       ClientStage = "confirming"
	StageFulfilling       ClientStage = "fulfilling"
	StageVerifying        ClientStage = "verifying"
	StageComplete         ClientStage = "complete"
	StageInvalid          ClientStage = "invalid"
	StageExpired          ClientStage = "expired"
	StageRejected         ClientStage = "rejected"
	StageFailedRetryable  ClientStage = "failed_retryable"
	StageFailedTerminal   ClientStage = "failed_terminal"
)

var (
	ErrClientSessionNotFound = errors.New("EntPay session not found")
	ErrClientRevision        = errors.New("EntPay session changed; refresh before continuing")
	ErrClientStage           = errors.New("EntPay session transition is not allowed")
)

type ClientSession struct {
	ID                    string             `json:"id"`
	Revision              uint64             `json:"revision"`
	Endpoint              string             `json:"endpoint"`
	Merchant              string             `json:"merchant,omitempty"`
	SigningKey            string             `json:"signing_key,omitempty"`
	SigningKeyFingerprint string             `json:"signing_key_fingerprint,omitempty"`
	MerchantTrustState    string             `json:"merchant_trust_state,omitempty"`
	Invoice               *Invoice           `json:"invoice,omitempty"`
	Product               *ProductDescriptor `json:"product,omitempty"`
	WalletAddress         string             `json:"wallet_address,omitempty"`
	EstimatedFee          uint64             `json:"estimated_fee,omitempty"`
	FeeCeiling            uint64             `json:"fee_ceiling,omitempty"`
	SpendableBalance      uint64             `json:"spendable_balance,omitempty"`
	TransactionID         string             `json:"transaction_id,omitempty"`
	CurrentConfirmations  uint64             `json:"current_confirmations,omitempty"`
	Stage                 ClientStage        `json:"stage"`
	RetryStage            ClientStage        `json:"retry_stage,omitempty"`
	ErrorCode             string             `json:"error_code,omitempty"`
	ErrorMessage          string             `json:"error_message,omitempty"`
	Receipt               *Receipt           `json:"receipt,omitempty"`
	Payload               json.RawMessage    `json:"-"`
	Result                *ResultEnvelope    `json:"result,omitempty"`
	DeliveryArtifact      *ArtifactMetadata  `json:"delivery_artifact,omitempty"`
	ArtifactPath          string             `json:"artifact_path,omitempty"`
	ArtifactSHA256        string             `json:"artifact_sha256,omitempty"`
	ArtifactMediaType     string             `json:"artifact_media_type,omitempty"`
	ArtifactBytes         int64              `json:"artifact_bytes,omitempty"`
	CreatedAt             time.Time          `json:"created_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
	CompletedAt           *time.Time         `json:"completed_at,omitempty"`
}

type clientSessionCapsule struct {
	HandoffCode string                `json:"handoff_code,omitempty"`
	ClientNonce string                `json:"client_nonce,omitempty"`
	Input       json.RawMessage       `json:"input,omitempty"`
	Created     CreateInvoiceResponse `json:"created,omitempty"`
}

type ClientStore struct {
	database  *sql.DB
	protector clientProtector
}

type ClientSettings struct {
	Revision             uint64 `json:"revision"`
	RequireEveryApproval bool   `json:"require_every_approval"`
	MaximumAmount        uint64 `json:"maximum_amount"`
	ArtifactDirectory    string `json:"artifact_directory"`
	RequestRetentionDays int    `json:"request_retention_days"`
}

func (s *ClientStore) EnsureSettings(ctx context.Context, maximumAmount uint64, artifactDirectory string) (ClientSettings, error) {
	if maximumAmount == 0 || maximumAmount > core.MaxSupply || strings.TrimSpace(artifactDirectory) == "" {
		return ClientSettings{}, fmt.Errorf("EntPay settings defaults are invalid")
	}
	if _, err := s.database.ExecContext(ctx, `INSERT OR IGNORE INTO client_settings(id, revision, maximum_amount, artifact_directory, request_retention_days) VALUES (1, 1, ?, ?, 30)`, maximumAmount, artifactDirectory); err != nil {
		return ClientSettings{}, err
	}
	return s.Settings(ctx)
}

func (s *ClientStore) Settings(ctx context.Context) (ClientSettings, error) {
	settings := ClientSettings{RequireEveryApproval: true}
	if err := s.database.QueryRowContext(ctx, `SELECT revision, maximum_amount, artifact_directory, request_retention_days FROM client_settings WHERE id = 1`).Scan(&settings.Revision, &settings.MaximumAmount, &settings.ArtifactDirectory, &settings.RequestRetentionDays); err != nil {
		return ClientSettings{}, err
	}
	return settings, nil
}

func (s *ClientStore) SaveSettings(ctx context.Context, input ClientSettings, expectedRevision uint64) (ClientSettings, error) {
	if !input.RequireEveryApproval || input.MaximumAmount == 0 || input.MaximumAmount > core.MaxSupply || expectedRevision == 0 || (input.RequestRetentionDays != -1 && input.RequestRetentionDays != 0 && input.RequestRetentionDays != 7 && input.RequestRetentionDays != 30) {
		return ClientSettings{}, fmt.Errorf("EntPay settings are invalid")
	}
	directory, err := filepath.Abs(strings.TrimSpace(input.ArtifactDirectory))
	if err != nil || directory == "" {
		return ClientSettings{}, fmt.Errorf("EntPay artifact directory is invalid")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ClientSettings{}, fmt.Errorf("create EntPay artifact directory: %w", err)
	}
	if info, err := os.Lstat(directory); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ClientSettings{}, fmt.Errorf("EntPay artifact directory is unsafe")
	}
	result, err := s.database.ExecContext(ctx, `UPDATE client_settings SET revision = revision + 1, maximum_amount = ?, artifact_directory = ?, request_retention_days = ? WHERE id = 1 AND revision = ?`, input.MaximumAmount, directory, input.RequestRetentionDays, expectedRevision)
	if err != nil {
		return ClientSettings{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ClientSettings{}, ErrClientRevision
	}
	return s.Settings(ctx)
}

func (s *ClientStore) DeleteSession(ctx context.Context, id string) error {
	result, err := s.database.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND stage IN ('complete','invalid','expired','rejected','failed_terminal')`, id)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ErrClientStage
	}
	return nil
}

func (s *ClientStore) PurgeExpiredRequestDetails(ctx context.Context) error {
	settings, err := s.Settings(ctx)
	if err != nil || settings.RequestRetentionDays < 0 {
		return err
	}
	cutoff := time.Now().UTC().Unix()
	if settings.RequestRetentionDays > 0 {
		cutoff = time.Now().UTC().AddDate(0, 0, -settings.RequestRetentionDays).Unix()
	}
	rows, err := s.database.QueryContext(ctx, `SELECT id FROM sessions WHERE stage IN ('complete','invalid','expired','rejected','failed_terminal') AND updated_at <= ?`, cutoff)
	if err != nil {
		return err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		capsule, err := s.Capsule(ctx, id)
		if err != nil {
			return err
		}
		if len(capsule.Input) == 0 {
			continue
		}
		capsule.Input = nil
		encoded, err := json.Marshal(capsule)
		if err != nil {
			return err
		}
		encrypted, err := s.protector.Seal(id, clientCapsuleSchema, encoded)
		clear(encoded)
		if err != nil {
			return err
		}
		if _, err := s.database.ExecContext(ctx, `UPDATE sessions SET encrypted_capsule = ? WHERE id = ?`, encrypted, id); err != nil {
			return err
		}
	}
	return nil
}

func OpenClientStore(path string, protector clientProtector) (*ClientStore, error) {
	if strings.TrimSpace(path) == "" || protector == nil {
		return nil, fmt.Errorf("EntPay client store configuration is incomplete")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create EntPay client directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("EntPay client database is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect EntPay client database: %w", err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open EntPay client database: %w", err)
	}
	database.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON", "PRAGMA trusted_schema=OFF",
	} {
		if _, err := database.Exec(pragma); err != nil {
			database.Close()
			return nil, fmt.Errorf("configure EntPay client database: %w", err)
		}
	}
	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		database.Close()
		return nil, fmt.Errorf("read EntPay client schema: %w", err)
	}
	if version > clientStoreSchema {
		database.Close()
		return nil, fmt.Errorf("EntPay client database schema %d is newer than supported schema %d", version, clientStoreSchema)
	}
	if version == 0 {
		if err := createClientSchema(database); err != nil {
			database.Close()
			return nil, err
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		database.Close()
		return nil, fmt.Errorf("protect EntPay client database: %w", err)
	}
	return &ClientStore{database: database, protector: protector}, nil
}

func createClientSchema(database *sql.DB) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
			endpoint TEXT NOT NULL,
			handoff_digest BLOB NOT NULL UNIQUE,
			merchant TEXT NOT NULL DEFAULT '', signing_key TEXT NOT NULL DEFAULT '',
			signing_key_fingerprint TEXT NOT NULL DEFAULT '', merchant_trust_state TEXT NOT NULL DEFAULT '',
			invoice_json BLOB, product_json BLOB, encrypted_capsule BLOB NOT NULL,
			wallet_address TEXT NOT NULL DEFAULT '', estimated_fee INTEGER NOT NULL DEFAULT 0,
			fee_ceiling INTEGER NOT NULL DEFAULT 0, spendable_balance INTEGER NOT NULL DEFAULT 0,
			transaction_id TEXT UNIQUE, transaction_json BLOB, current_confirmations INTEGER NOT NULL DEFAULT 0,
			stage TEXT NOT NULL CHECK(stage IN (
				'received','inspecting','awaiting_approval','preparing_payment','broadcast','submitting',
				'confirming','fulfilling','verifying','complete','invalid','expired','rejected',
				'failed_retryable','failed_terminal')),
			retry_stage TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '',
			receipt_json BLOB, payload_json BLOB, artifact_json BLOB,
			artifact_path TEXT NOT NULL DEFAULT '', artifact_sha256 TEXT NOT NULL DEFAULT '',
			artifact_media_type TEXT NOT NULL DEFAULT '', artifact_bytes INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, completed_at INTEGER
		);
		CREATE INDEX sessions_stage_updated ON sessions(stage, updated_at DESC);
		CREATE TABLE session_transitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			from_stage TEXT, to_stage TEXT NOT NULL, revision INTEGER NOT NULL, created_at INTEGER NOT NULL
		);
		CREATE INDEX session_transitions_session ON session_transitions(session_id, id);
		CREATE TABLE merchant_identities (
			endpoint TEXT PRIMARY KEY, merchant TEXT NOT NULL, signing_key TEXT NOT NULL,
			fingerprint TEXT NOT NULL, trusted_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		);
		CREATE TABLE client_settings (
			id INTEGER PRIMARY KEY CHECK(id = 1), revision INTEGER NOT NULL,
			maximum_amount INTEGER NOT NULL CHECK(maximum_amount > 0), artifact_directory TEXT NOT NULL,
			request_retention_days INTEGER NOT NULL CHECK(request_retention_days IN (-1,0,7,30))
		);
		PRAGMA user_version = 1;
	`)
	if err != nil {
		return fmt.Errorf("create EntPay client schema: %w", err)
	}
	return tx.Commit()
}

func (s *ClientStore) Close() error {
	if s == nil || s.database == nil {
		return nil
	}
	_, checkpointErr := s.database.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return errors.Join(checkpointErr, s.database.Close())
}

func (s *ClientStore) CreateReceived(ctx context.Context, launch LaunchRequest, clientNonce string) (ClientSession, error) {
	if ValidateLaunchRequest(launch) != nil || validateOpaqueToken(clientNonce) != nil {
		return ClientSession{}, fmt.Errorf("EntPay launch request is invalid")
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return ClientSession{}, err
	}
	id := hex.EncodeToString(idBytes)
	capsule, err := json.Marshal(clientSessionCapsule{HandoffCode: launch.Handoff, ClientNonce: clientNonce})
	if err != nil {
		return ClientSession{}, err
	}
	defer clear(capsule)
	encrypted, err := s.protector.Seal(id, clientCapsuleSchema, capsule)
	if err != nil {
		return ClientSession{}, err
	}
	digest := sha256.Sum256([]byte(launch.Merchant + "\x00" + launch.Handoff))
	now := time.Now().UTC().Truncate(time.Second)
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return ClientSession{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO sessions(id, endpoint, handoff_digest, encrypted_capsule, stage, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'received', ?, ?)
	`, id, launch.Merchant, digest[:], encrypted, now.Unix(), now.Unix())
	if err != nil {
		return ClientSession{}, fmt.Errorf("create EntPay session: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return ClientSession{}, err
	}
	if changed == 0 {
		if err := tx.Rollback(); err != nil {
			return ClientSession{}, err
		}
		return s.sessionByHandoffDigest(ctx, digest[:])
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO session_transitions(session_id, from_stage, to_stage, revision, created_at) VALUES (?, NULL, 'received', 1, ?)`, id, now.Unix()); err != nil {
		return ClientSession{}, fmt.Errorf("record EntPay session creation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ClientSession{}, err
	}
	return s.Session(ctx, id)
}

func (s *ClientStore) Session(ctx context.Context, id string) (ClientSession, error) {
	if err := validateSessionID(id); err != nil {
		return ClientSession{}, ErrClientSessionNotFound
	}
	return scanClientSession(s.database.QueryRowContext(ctx, clientSessionSelect+" WHERE id = ?", id))
}

func (s *ClientStore) sessionByHandoffDigest(ctx context.Context, digest []byte) (ClientSession, error) {
	return scanClientSession(s.database.QueryRowContext(ctx, clientSessionSelect+" WHERE handoff_digest = ?", digest))
}

func (s *ClientStore) Sessions(ctx context.Context, limit int) ([]ClientSession, error) {
	if limit <= 0 || limit > 500 {
		return nil, fmt.Errorf("EntPay session limit must be between 1 and 500")
	}
	rows, err := s.database.QueryContext(ctx, clientSessionSelect+" ORDER BY CASE stage WHEN 'awaiting_approval' THEN 0 WHEN 'preparing_payment' THEN 1 WHEN 'broadcast' THEN 1 WHEN 'submitting' THEN 1 WHEN 'confirming' THEN 1 WHEN 'fulfilling' THEN 1 WHEN 'verifying' THEN 1 WHEN 'failed_retryable' THEN 2 ELSE 3 END, updated_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := make([]ClientSession, 0)
	for rows.Next() {
		session, err := scanClientSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *ClientStore) Capsule(ctx context.Context, id string) (clientSessionCapsule, error) {
	if err := validateSessionID(id); err != nil {
		return clientSessionCapsule{}, ErrClientSessionNotFound
	}
	var encrypted []byte
	if err := s.database.QueryRowContext(ctx, `SELECT encrypted_capsule FROM sessions WHERE id = ?`, id).Scan(&encrypted); errors.Is(err, sql.ErrNoRows) {
		return clientSessionCapsule{}, ErrClientSessionNotFound
	} else if err != nil {
		return clientSessionCapsule{}, err
	}
	plaintext, err := s.protector.Open(id, clientCapsuleSchema, encrypted)
	if err != nil {
		return clientSessionCapsule{}, err
	}
	defer clear(plaintext)
	var capsule clientSessionCapsule
	if err := json.Unmarshal(plaintext, &capsule); err != nil {
		return clientSessionCapsule{}, fmt.Errorf("decode EntPay session capsule: %w", err)
	}
	return capsule, nil
}

func (s *ClientStore) SaveRedeemedCapsule(ctx context.Context, id string, expectedRevision uint64, redeemed HandoffCapsule) (ClientSession, error) {
	if expectedRevision == 0 || redeemed.Endpoint == "" || len(redeemed.Input) == 0 || redeemed.Created.ClaimToken == "" {
		return ClientSession{}, fmt.Errorf("redeemed EntPay capsule is incomplete")
	}
	encoded, err := json.Marshal(clientSessionCapsule{Input: redeemed.Input, Created: redeemed.Created})
	if err != nil {
		return ClientSession{}, err
	}
	defer clear(encoded)
	encrypted, err := s.protector.Seal(id, clientCapsuleSchema, encoded)
	if err != nil {
		return ClientSession{}, err
	}
	result, err := s.database.ExecContext(ctx, `UPDATE sessions SET encrypted_capsule = ?, revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ? AND stage = 'inspecting'`, encrypted, time.Now().UTC().Unix(), id, expectedRevision)
	if err != nil {
		return ClientSession{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ClientSession{}, ErrClientRevision
	}
	return s.Session(ctx, id)
}

func (s *ClientStore) SaveReview(ctx context.Context, id string, expectedRevision uint64, info ServiceInfo, product ProductDescriptor, invoice Invoice, preview node.PaymentPreview) (ClientSession, error) {
	invoiceJSON, err := json.Marshal(invoice)
	if err != nil {
		return ClientSession{}, err
	}
	productJSON, err := json.Marshal(product)
	if err != nil {
		return ClientSession{}, err
	}
	key, err := base64.RawURLEncoding.DecodeString(info.SigningKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return ClientSession{}, fmt.Errorf("merchant signing key is invalid")
	}
	fingerprint := sha256.Sum256(key)
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return ClientSession{}, err
	}
	defer tx.Rollback()
	trust, err := merchantTrustState(ctx, tx, id, info.Merchant, info.SigningKey, hex.EncodeToString(fingerprint[:]))
	if err != nil {
		return ClientSession{}, err
	}
	now := time.Now().UTC().Unix()
	result, err := tx.ExecContext(ctx, `
		UPDATE sessions SET merchant = ?, signing_key = ?, signing_key_fingerprint = ?, merchant_trust_state = ?,
			invoice_json = ?, product_json = ?, wallet_address = ?, estimated_fee = ?, fee_ceiling = ?,
			spendable_balance = ?, stage = 'awaiting_approval', revision = revision + 1,
			error_code = '', error_message = '', updated_at = ?
		WHERE id = ? AND revision = ? AND stage = 'inspecting'
	`, info.Merchant, info.SigningKey, hex.EncodeToString(fingerprint[:]), trust, invoiceJSON, productJSON,
		preview.Wallet, preview.EstimatedFee, preview.MaximumFee, preview.SpendableBalance, now, id, expectedRevision)
	if err != nil {
		return ClientSession{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ClientSession{}, ErrClientRevision
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO session_transitions(session_id, from_stage, to_stage, revision, created_at) VALUES (?, 'inspecting', 'awaiting_approval', ?, ?)`, id, expectedRevision+1, now); err != nil {
		return ClientSession{}, err
	}
	if trust == "first" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO merchant_identities(endpoint, merchant, signing_key, fingerprint, trusted_at, updated_at) SELECT endpoint, ?, ?, ?, ?, ? FROM sessions WHERE id = ?`, info.Merchant, info.SigningKey, hex.EncodeToString(fingerprint[:]), now, now, id); err != nil {
			return ClientSession{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ClientSession{}, err
	}
	return s.Session(ctx, id)
}

func merchantTrustState(ctx context.Context, tx *sql.Tx, sessionID, merchant, signingKey, fingerprint string) (string, error) {
	var endpoint string
	if err := tx.QueryRowContext(ctx, `SELECT endpoint FROM sessions WHERE id = ?`, sessionID).Scan(&endpoint); errors.Is(err, sql.ErrNoRows) {
		return "", ErrClientSessionNotFound
	} else if err != nil {
		return "", err
	}
	var knownMerchant, knownKey, knownFingerprint string
	err := tx.QueryRowContext(ctx, `SELECT merchant, signing_key, fingerprint FROM merchant_identities WHERE endpoint = ?`, endpoint).Scan(&knownMerchant, &knownKey, &knownFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return "first", nil
	}
	if err != nil {
		return "", err
	}
	if core.AddressesEqual(knownMerchant, merchant) && knownKey == signingKey && knownFingerprint == fingerprint {
		return "previous", nil
	}
	return "changed", nil
}

func (s *ClientStore) ReestablishMerchantTrust(ctx context.Context, id string, expectedRevision uint64) (ClientSession, error) {
	if expectedRevision == 0 {
		return ClientSession{}, ErrClientRevision
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return ClientSession{}, err
	}
	defer tx.Rollback()
	var endpoint, merchant, signingKey, fingerprint string
	var revision uint64
	var stage ClientStage
	var trust string
	if err := tx.QueryRowContext(ctx, `SELECT endpoint, merchant, signing_key, signing_key_fingerprint, revision, stage, merchant_trust_state FROM sessions WHERE id = ?`, id).Scan(&endpoint, &merchant, &signingKey, &fingerprint, &revision, &stage, &trust); errors.Is(err, sql.ErrNoRows) {
		return ClientSession{}, ErrClientSessionNotFound
	} else if err != nil {
		return ClientSession{}, err
	}
	if revision != expectedRevision || stage != StageAwaitingApproval || trust != "changed" || endpoint == "" || merchant == "" || signingKey == "" || fingerprint == "" {
		return ClientSession{}, ErrClientRevision
	}
	now := time.Now().UTC().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO merchant_identities(endpoint, merchant, signing_key, fingerprint, trusted_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(endpoint) DO UPDATE SET merchant = excluded.merchant, signing_key = excluded.signing_key, fingerprint = excluded.fingerprint, trusted_at = excluded.trusted_at, updated_at = excluded.updated_at`, endpoint, merchant, signingKey, fingerprint, now, now); err != nil {
		return ClientSession{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET merchant_trust_state = 'previous', revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ? AND stage = 'awaiting_approval' AND merchant_trust_state = 'changed'`, now, id, expectedRevision)
	if err != nil {
		return ClientSession{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ClientSession{}, ErrClientRevision
	}
	if err := tx.Commit(); err != nil {
		return ClientSession{}, err
	}
	return s.Session(ctx, id)
}

func (s *ClientStore) JournalPayment(ctx context.Context, id string, expectedRevision uint64, transaction core.Transaction, fee uint64) (ClientSession, error) {
	if transaction.ID == "" || transaction.ID != transaction.ComputeID() || fee == 0 {
		return ClientSession{}, fmt.Errorf("prepared EntPay transaction is invalid")
	}
	encoded, err := json.Marshal(transaction)
	if err != nil {
		return ClientSession{}, err
	}
	now := time.Now().UTC().Unix()
	result, err := s.database.ExecContext(ctx, `
		UPDATE sessions SET transaction_id = ?, transaction_json = ?, estimated_fee = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revision = ? AND stage = 'preparing_payment' AND transaction_id IS NULL
	`, transaction.ID, encoded, fee, now, id, expectedRevision)
	if err != nil {
		return ClientSession{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ClientSession{}, ErrClientRevision
	}
	return s.Session(ctx, id)
}

func (s *ClientStore) Transaction(ctx context.Context, id string) (core.Transaction, error) {
	var encoded []byte
	if err := s.database.QueryRowContext(ctx, `SELECT transaction_json FROM sessions WHERE id = ? AND transaction_id IS NOT NULL`, id).Scan(&encoded); errors.Is(err, sql.ErrNoRows) {
		return core.Transaction{}, ErrClientSessionNotFound
	} else if err != nil {
		return core.Transaction{}, err
	}
	var transaction core.Transaction
	if err := json.Unmarshal(encoded, &transaction); err != nil || transaction.ID == "" || transaction.ID != transaction.ComputeID() {
		return core.Transaction{}, fmt.Errorf("stored EntPay transaction failed integrity validation")
	}
	return transaction, nil
}

func (s *ClientStore) SaveConfirmations(ctx context.Context, id string, expectedRevision, confirmations uint64) (ClientSession, error) {
	result, err := s.database.ExecContext(ctx, `UPDATE sessions SET current_confirmations = ?, revision = revision + 1, updated_at = ? WHERE id = ? AND revision = ? AND stage IN ('confirming','fulfilling') AND current_confirmations != ?`, confirmations, time.Now().UTC().Unix(), id, expectedRevision, confirmations)
	if err != nil {
		return ClientSession{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return ClientSession{}, err
	}
	if changed == 0 {
		current, err := s.Session(ctx, id)
		if err != nil {
			return ClientSession{}, err
		}
		if current.Revision != expectedRevision || (current.Stage != StageConfirming && current.Stage != StageFulfilling) {
			return ClientSession{}, ErrClientRevision
		}
		return current, nil
	}
	return s.Session(ctx, id)
}

func (s *ClientStore) SaveDelivery(ctx context.Context, id string, expectedRevision uint64, delivery Delivery) (ClientSession, error) {
	receiptJSON, err := json.Marshal(delivery.Receipt)
	if err != nil {
		return ClientSession{}, err
	}
	payload, err := canonicalPayload(delivery.Payload)
	if err != nil {
		return ClientSession{}, err
	}
	var artifactJSON []byte
	if delivery.Artifact != nil {
		artifactJSON, err = json.Marshal(delivery.Artifact)
		if err != nil {
			return ClientSession{}, err
		}
	}
	now := time.Now().UTC().Unix()
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return ClientSession{}, err
	}
	defer tx.Rollback()
	var current ClientStage
	var revision uint64
	if err := tx.QueryRowContext(ctx, `SELECT stage, revision FROM sessions WHERE id = ?`, id).Scan(&current, &revision); err != nil {
		return ClientSession{}, err
	}
	if revision != expectedRevision || (current != StageConfirming && current != StageFulfilling) {
		return ClientSession{}, ErrClientRevision
	}
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET receipt_json = ?, payload_json = ?, artifact_json = ?, stage = 'verifying', revision = revision + 1, error_code = '', error_message = '', updated_at = ? WHERE id = ? AND revision = ? AND stage = ?`, receiptJSON, payload, artifactJSON, now, id, expectedRevision, current)
	if err != nil {
		return ClientSession{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ClientSession{}, ErrClientRevision
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO session_transitions(session_id, from_stage, to_stage, revision, created_at) VALUES (?, ?, 'verifying', ?, ?)`, id, current, expectedRevision+1, now); err != nil {
		return ClientSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return ClientSession{}, err
	}
	return s.Session(ctx, id)
}

func (s *ClientStore) Complete(ctx context.Context, id string, expectedRevision uint64, artifactPath string, metadata *ArtifactMetadata) (ClientSession, error) {
	capsule, err := s.Capsule(ctx, id)
	if err != nil {
		return ClientSession{}, err
	}
	capsule.HandoffCode = ""
	capsule.ClientNonce = ""
	capsule.Created.ClaimToken = ""
	if settings, settingsErr := s.Settings(ctx); settingsErr == nil && settings.RequestRetentionDays == 0 {
		capsule.Input = nil
	}
	encoded, err := json.Marshal(capsule)
	if err != nil {
		return ClientSession{}, err
	}
	defer clear(encoded)
	encrypted, err := s.protector.Seal(id, clientCapsuleSchema, encoded)
	if err != nil {
		return ClientSession{}, err
	}
	artifactSHA, mediaType := "", ""
	artifactBytes := int64(0)
	if metadata != nil {
		artifactSHA, mediaType, artifactBytes = metadata.SHA256, metadata.MediaType, metadata.Bytes
	}
	now := time.Now().UTC().Unix()
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return ClientSession{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET encrypted_capsule = ?, artifact_path = ?, artifact_sha256 = ?, artifact_media_type = ?, artifact_bytes = ?, stage = 'complete', revision = revision + 1, error_code = '', error_message = '', updated_at = ?, completed_at = ? WHERE id = ? AND revision = ? AND stage = 'verifying'`, encrypted, artifactPath, artifactSHA, mediaType, artifactBytes, now, now, id, expectedRevision)
	if err != nil {
		return ClientSession{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ClientSession{}, ErrClientRevision
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO session_transitions(session_id, from_stage, to_stage, revision, created_at) VALUES (?, 'verifying', 'complete', ?, ?)`, id, expectedRevision+1, now); err != nil {
		return ClientSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return ClientSession{}, err
	}
	return s.Session(ctx, id)
}

func (s *ClientStore) TerminateWithoutPayment(ctx context.Context, id string, expectedRevision uint64, stage ClientStage, errorCode, errorMessage string) (ClientSession, error) {
	if stage != StageRejected && stage != StageInvalid && stage != StageExpired {
		return ClientSession{}, ErrClientStage
	}
	capsule, err := s.Capsule(ctx, id)
	if err != nil {
		return ClientSession{}, err
	}
	capsule.HandoffCode = ""
	capsule.ClientNonce = ""
	capsule.Created.ClaimToken = ""
	if settings, settingsErr := s.Settings(ctx); settingsErr == nil && settings.RequestRetentionDays == 0 {
		capsule.Input = nil
	}
	encoded, err := json.Marshal(capsule)
	if err != nil {
		return ClientSession{}, err
	}
	defer clear(encoded)
	encrypted, err := s.protector.Seal(id, clientCapsuleSchema, encoded)
	if err != nil {
		return ClientSession{}, err
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return ClientSession{}, err
	}
	defer tx.Rollback()
	var current ClientStage
	var revision uint64
	if err := tx.QueryRowContext(ctx, `SELECT stage, revision FROM sessions WHERE id = ?`, id).Scan(&current, &revision); err != nil {
		return ClientSession{}, err
	}
	if revision != expectedRevision || !validClientTransition(current, stage) {
		return ClientSession{}, ErrClientRevision
	}
	now := time.Now().UTC().Unix()
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET encrypted_capsule = ?, stage = ?, revision = revision + 1, error_code = ?, error_message = ?, updated_at = ? WHERE id = ? AND revision = ? AND stage = ? AND transaction_id IS NULL`, encrypted, stage, errorCode, errorMessage, now, id, expectedRevision, current)
	if err != nil {
		return ClientSession{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ClientSession{}, ErrClientRevision
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO session_transitions(session_id, from_stage, to_stage, revision, created_at) VALUES (?, ?, ?, ?, ?)`, id, current, stage, expectedRevision+1, now); err != nil {
		return ClientSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return ClientSession{}, err
	}
	return s.Session(ctx, id)
}

func (s *ClientStore) transition(ctx context.Context, id string, expectedRevision uint64, to ClientStage, errorCode, errorMessage string) (ClientSession, error) {
	if !validClientStage(to) || expectedRevision == 0 || len(errorCode) > 80 || len(errorMessage) > 500 {
		return ClientSession{}, ErrClientStage
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return ClientSession{}, err
	}
	defer tx.Rollback()
	var current ClientStage
	var revision uint64
	if err := tx.QueryRowContext(ctx, `SELECT stage, revision FROM sessions WHERE id = ?`, id).Scan(&current, &revision); errors.Is(err, sql.ErrNoRows) {
		return ClientSession{}, ErrClientSessionNotFound
	} else if err != nil {
		return ClientSession{}, err
	}
	if revision != expectedRevision {
		return ClientSession{}, ErrClientRevision
	}
	if !validClientTransition(current, to) {
		return ClientSession{}, fmt.Errorf("%w: %s to %s", ErrClientStage, current, to)
	}
	now := time.Now().UTC().Unix()
	completed := any(nil)
	if to == StageComplete {
		completed = now
	}
	retryStage := ""
	if to == StageFailedRetryable {
		retryStage = string(current)
	}
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET stage = ?, retry_stage = ?, revision = revision + 1, error_code = ?, error_message = ?, updated_at = ?, completed_at = COALESCE(?, completed_at) WHERE id = ? AND revision = ?`, to, retryStage, errorCode, errorMessage, now, completed, id, expectedRevision)
	if err != nil {
		return ClientSession{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return ClientSession{}, ErrClientRevision
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO session_transitions(session_id, from_stage, to_stage, revision, created_at) VALUES (?, ?, ?, ?, ?)`, id, current, to, expectedRevision+1, now); err != nil {
		return ClientSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return ClientSession{}, err
	}
	return s.Session(ctx, id)
}

func validClientTransition(from, to ClientStage) bool {
	allowed := map[ClientStage][]ClientStage{
		StageReceived:         {StageInspecting, StageInvalid, StageExpired},
		StageInspecting:       {StageAwaitingApproval, StageInvalid, StageExpired, StageFailedRetryable},
		StageAwaitingApproval: {StagePreparingPayment, StageRejected, StageExpired},
		StagePreparingPayment: {StageBroadcast, StageAwaitingApproval, StageFailedTerminal},
		StageBroadcast:        {StageSubmitting, StageFailedRetryable},
		StageSubmitting:       {StageConfirming, StageFulfilling, StageFailedRetryable, StageFailedTerminal},
		StageConfirming:       {StageFulfilling, StageVerifying, StageFailedRetryable, StageFailedTerminal},
		StageFulfilling:       {StageVerifying, StageFailedRetryable, StageFailedTerminal},
		StageVerifying:        {StageComplete, StageFailedRetryable, StageFailedTerminal},
		StageFailedRetryable:  {StageInspecting, StageSubmitting, StageConfirming, StageFulfilling, StageVerifying},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

func validClientStage(stage ClientStage) bool {
	return strings.Contains(" received inspecting awaiting_approval preparing_payment broadcast submitting confirming fulfilling verifying complete invalid expired rejected failed_retryable failed_terminal ", " "+string(stage)+" ")
}

const clientSessionSelect = `SELECT id, revision, endpoint, merchant, signing_key, signing_key_fingerprint,
	merchant_trust_state, invoice_json, product_json, wallet_address, estimated_fee, fee_ceiling,
	spendable_balance, COALESCE(transaction_id, ''), current_confirmations, stage, retry_stage, error_code, error_message, receipt_json,
	payload_json, artifact_json, artifact_path, artifact_sha256, artifact_media_type, artifact_bytes,
	created_at, updated_at, completed_at FROM sessions`

type clientRowScanner interface {
	Scan(...any) error
}

func scanClientSession(row clientRowScanner) (ClientSession, error) {
	var session ClientSession
	var invoiceJSON, productJSON, receiptJSON, payloadJSON, artifactJSON []byte
	var createdAt, updatedAt int64
	var completedAt sql.NullInt64
	err := row.Scan(
		&session.ID, &session.Revision, &session.Endpoint, &session.Merchant, &session.SigningKey,
		&session.SigningKeyFingerprint, &session.MerchantTrustState, &invoiceJSON, &productJSON,
		&session.WalletAddress, &session.EstimatedFee, &session.FeeCeiling, &session.SpendableBalance,
		&session.TransactionID, &session.CurrentConfirmations, &session.Stage, &session.RetryStage, &session.ErrorCode, &session.ErrorMessage, &receiptJSON,
		&payloadJSON, &artifactJSON, &session.ArtifactPath, &session.ArtifactSHA256, &session.ArtifactMediaType,
		&session.ArtifactBytes, &createdAt, &updatedAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ClientSession{}, ErrClientSessionNotFound
	}
	if err != nil {
		return ClientSession{}, err
	}
	if len(invoiceJSON) > 0 {
		session.Invoice = &Invoice{}
		if json.Unmarshal(invoiceJSON, session.Invoice) != nil {
			return ClientSession{}, fmt.Errorf("stored EntPay invoice is invalid")
		}
	}
	if len(productJSON) > 0 {
		session.Product = &ProductDescriptor{}
		if json.Unmarshal(productJSON, session.Product) != nil {
			return ClientSession{}, fmt.Errorf("stored EntPay product is invalid")
		}
	}
	if len(receiptJSON) > 0 {
		session.Receipt = &Receipt{}
		if json.Unmarshal(receiptJSON, session.Receipt) != nil {
			return ClientSession{}, fmt.Errorf("stored EntPay receipt is invalid")
		}
	}
	if len(payloadJSON) > 0 {
		session.Payload = append(json.RawMessage(nil), payloadJSON...)
		result, err := decodeResult(session.Payload)
		if err == nil {
			session.Result = &result
		} else {
			legacy, canonicalErr := canonicalPayload(session.Payload)
			if canonicalErr != nil {
				return ClientSession{}, fmt.Errorf("stored EntPay payload is invalid")
			}
			session.Result = &ResultEnvelope{
				Schema: LegacyResultSchema, Summary: "Legacy merchant delivery", Data: legacy,
			}
		}
	}
	if len(artifactJSON) > 0 {
		session.DeliveryArtifact = &ArtifactMetadata{}
		if json.Unmarshal(artifactJSON, session.DeliveryArtifact) != nil {
			return ClientSession{}, fmt.Errorf("stored EntPay artifact metadata is invalid")
		}
	}
	session.CreatedAt = time.Unix(createdAt, 0).UTC()
	session.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if completedAt.Valid {
		value := time.Unix(completedAt.Int64, 0).UTC()
		session.CompletedAt = &value
	}
	return session, nil
}
