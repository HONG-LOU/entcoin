package entpay

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var (
	errInvoiceNotFound = errors.New("invoice not found")
	errUnauthorized    = errors.New("invoice authorization failed")
	errConflict        = errors.New("invoice state conflict")
	errHandoffNotFound = errors.New("handoff not found")
)

type invoiceRecord struct {
	Invoice
	Input        []byte
	Status       string
	TxID         string
	DeliveryJSON []byte
}

type deliveryJob struct {
	Status    string
	Attempts  uint64
	UpdatedAt time.Time
}

type store struct {
	database *sql.DB
}

func openStore(path string) (*store, error) {
	if path == "" {
		return nil, fmt.Errorf("invoice database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create invoice database directory: %w", err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open invoice database: %w", err)
	}
	database.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA trusted_schema=OFF",
	} {
		if _, err := database.Exec(pragma); err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("configure invoice database: %w", err)
		}
	}
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS invoices (
			id TEXT PRIMARY KEY,
			token_hash BLOB NOT NULL,
			merchant TEXT NOT NULL,
			amount INTEGER NOT NULL CHECK(amount > 0),
			resource TEXT NOT NULL,
			input BLOB NOT NULL,
			input_sha256 TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			confirmations INTEGER NOT NULL CHECK(confirmations > 0),
			status TEXT NOT NULL CHECK(status IN ('open', 'submitted', 'delivered')),
			tx_id TEXT UNIQUE,
			created_at INTEGER NOT NULL,
			delivered_at INTEGER,
			delivery BLOB
		);
		CREATE INDEX IF NOT EXISTS invoices_expiry ON invoices(status, expires_at);
		CREATE TABLE IF NOT EXISTS delivery_jobs (
			invoice_id TEXT PRIMARY KEY REFERENCES invoices(id) ON DELETE CASCADE,
			status TEXT NOT NULL CHECK(status IN ('queued', 'processing', 'failed', 'permanent', 'delivered')),
			attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
			updated_at INTEGER NOT NULL,
			last_error TEXT NOT NULL DEFAULT ''
		);
			CREATE INDEX IF NOT EXISTS delivery_jobs_status ON delivery_jobs(status, updated_at);
			CREATE TABLE IF NOT EXISTS handoffs (
				code_hash BLOB PRIMARY KEY,
				invoice_id TEXT NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
				capsule BLOB NOT NULL,
				expires_at INTEGER NOT NULL,
				nonce_hash BLOB,
				redeemed_at INTEGER
			);
			CREATE INDEX IF NOT EXISTS handoffs_expiry ON handoffs(expires_at);
	`); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("create invoice schema: %w", err)
	}
	return &store{database: database}, nil
}

func (s *store) CreateHandoff(ctx context.Context, code, invoiceID string, capsule HandoffCapsule, expiresAt time.Time, key [32]byte) error {
	encoded, err := json.Marshal(capsule)
	if err != nil {
		return fmt.Errorf("encode handoff capsule: %w", err)
	}
	sealed, err := sealHandoffCapsule(key, invoiceID, encoded)
	if err != nil {
		return err
	}
	digest := opaqueTokenDigest(code)
	_, err = s.database.ExecContext(ctx, `INSERT INTO handoffs(code_hash, invoice_id, capsule, expires_at) VALUES (?, ?, ?, ?)`, digest[:], invoiceID, sealed, expiresAt.UTC().Unix())
	if err != nil {
		return fmt.Errorf("store handoff: %w", err)
	}
	return nil
}

func (s *store) RedeemHandoff(ctx context.Context, code, nonce string, now time.Time, key [32]byte) (HandoffCapsule, string, error) {
	codeHash := opaqueTokenDigest(code)
	nonceHash := opaqueTokenDigest(nonce)
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return HandoffCapsule{}, "", fmt.Errorf("begin handoff redemption: %w", err)
	}
	defer tx.Rollback()
	var invoiceID string
	var sealed, existingNonce []byte
	var expiresAt int64
	var redeemedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT invoice_id, capsule, expires_at, nonce_hash, redeemed_at FROM handoffs WHERE code_hash = ?`, codeHash[:]).Scan(&invoiceID, &sealed, &expiresAt, &existingNonce, &redeemedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return HandoffCapsule{}, "", errHandoffNotFound
	}
	if err != nil {
		return HandoffCapsule{}, "", fmt.Errorf("read handoff: %w", err)
	}
	if now.Unix() > expiresAt || (redeemedAt.Valid && (now.Unix()-redeemedAt.Int64 > 30 || subtle.ConstantTimeCompare(existingNonce, nonceHash[:]) != 1)) {
		return HandoffCapsule{}, "", errHandoffNotFound
	}
	if !redeemedAt.Valid {
		result, updateErr := tx.ExecContext(ctx, `UPDATE handoffs SET nonce_hash = ?, redeemed_at = ? WHERE code_hash = ? AND redeemed_at IS NULL`, nonceHash[:], now.Unix(), codeHash[:])
		if updateErr != nil {
			return HandoffCapsule{}, "", fmt.Errorf("bind handoff nonce: %w", updateErr)
		}
		changed, updateErr := result.RowsAffected()
		if updateErr != nil || changed != 1 {
			return HandoffCapsule{}, "", errHandoffNotFound
		}
	}
	if err := tx.Commit(); err != nil {
		return HandoffCapsule{}, "", fmt.Errorf("commit handoff redemption: %w", err)
	}
	plaintext, err := openHandoffCapsule(key, invoiceID, sealed)
	if err != nil {
		return HandoffCapsule{}, "", err
	}
	var capsule HandoffCapsule
	if err := json.Unmarshal(plaintext, &capsule); err != nil {
		return HandoffCapsule{}, "", fmt.Errorf("decode handoff capsule: %w", err)
	}
	return capsule, invoiceID, nil
}

func sealHandoffCapsule(key [32]byte, invoiceID string, plaintext []byte) ([]byte, error) {
	aead, err := handoffAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("create handoff nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintext, []byte("entpay-v1-handoff\x00"+invoiceID)), nil
}

func openHandoffCapsule(key [32]byte, invoiceID string, sealed []byte) ([]byte, error) {
	aead, err := handoffAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize()+aead.Overhead() {
		return nil, fmt.Errorf("handoff capsule is invalid")
	}
	plaintext, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte("entpay-v1-handoff\x00"+invoiceID))
	if err != nil {
		return nil, fmt.Errorf("decrypt handoff capsule: %w", err)
	}
	return plaintext, nil
}

func handoffAEAD(key [32]byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create handoff cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func (s *store) Close() error {
	return s.database.Close()
}

func (s *store) Create(ctx context.Context, merchant string, descriptor ProductDescriptor, input []byte, expiresAt time.Time) (invoiceRecord, string, error) {
	idBytes := make([]byte, 16)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return invoiceRecord{}, "", fmt.Errorf("create invoice ID: %w", err)
	}
	if _, err := rand.Read(tokenBytes); err != nil {
		return invoiceRecord{}, "", fmt.Errorf("create claim token: %w", err)
	}
	id := hex.EncodeToString(idBytes)
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	tokenHash := sha256.Sum256([]byte(token))
	record := invoiceRecord{
		Invoice: Invoice{
			Protocol: ProtocolVersion, Network: "entropy-mainnet-v1", ID: id,
			Merchant: merchant, Amount: descriptor.Price, Resource: descriptor.ID,
			InputSHA256: inputHash(descriptor.ID, input),
			ExpiresAt:   expiresAt.UTC().Truncate(time.Second), Confirmations: descriptor.Confirmations,
		},
		Input: append([]byte(nil), input...), Status: "open",
	}
	_, err := s.database.ExecContext(ctx, `
		INSERT INTO invoices(id, token_hash, merchant, amount, resource, input, input_sha256, expires_at, confirmations, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?)
	`, id, tokenHash[:], merchant, descriptor.Price, descriptor.ID, input, record.InputSHA256, record.ExpiresAt.Unix(), descriptor.Confirmations, time.Now().UTC().Unix())
	if err != nil {
		return invoiceRecord{}, "", fmt.Errorf("store invoice: %w", err)
	}
	return record, token, nil
}

func (s *store) Authorized(ctx context.Context, id, token string) (invoiceRecord, error) {
	var record invoiceRecord
	var tokenHash []byte
	var expires int64
	err := s.database.QueryRowContext(ctx, `
		SELECT id, token_hash, merchant, amount, resource, input, input_sha256, expires_at, confirmations,
		       status, COALESCE(tx_id, ''), COALESCE(delivery, '')
		FROM invoices WHERE id = ?
	`, id).Scan(
		&record.ID, &tokenHash, &record.Merchant, &record.Amount, &record.Resource,
		&record.Input, &record.InputSHA256, &expires, &record.Confirmations,
		&record.Status, &record.TxID, &record.DeliveryJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return invoiceRecord{}, errInvoiceNotFound
	}
	if err != nil {
		return invoiceRecord{}, fmt.Errorf("read invoice: %w", err)
	}
	presented := sha256.Sum256([]byte(token))
	if len(tokenHash) != len(presented) || subtle.ConstantTimeCompare(tokenHash, presented[:]) != 1 {
		return invoiceRecord{}, errUnauthorized
	}
	record.Protocol = ProtocolVersion
	record.Network = "entropy-mainnet-v1"
	record.ExpiresAt = time.Unix(expires, 0).UTC()
	return record, nil
}

func (s *store) Submit(ctx context.Context, id, transactionID string) error {
	result, err := s.database.ExecContext(ctx, `
		UPDATE invoices SET status = 'submitted', tx_id = ?
		WHERE id = ? AND status = 'open'
		  AND NOT EXISTS (SELECT 1 FROM invoices used WHERE used.tx_id = ?)
	`, transactionID, id, transactionID)
	if err != nil {
		return fmt.Errorf("record invoice payment: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read invoice update result: %w", err)
	}
	if changed == 1 {
		return nil
	}
	var existing string
	if err := s.database.QueryRowContext(ctx, `SELECT COALESCE(tx_id, '') FROM invoices WHERE id = ? AND status = 'submitted'`, id).Scan(&existing); err == nil && existing == transactionID {
		return nil
	}
	return errConflict
}

func (s *store) AcquireJob(ctx context.Context, id string, now time.Time, staleAfter, retryAfter time.Duration, maxAttempts uint64) (deliveryJob, bool, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return deliveryJob{}, false, fmt.Errorf("begin delivery job: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO delivery_jobs(invoice_id, status, attempts, updated_at)
		SELECT id, 'queued', 0, ? FROM invoices WHERE id = ? AND status = 'submitted'
	`, now.UTC().Unix(), id); err != nil {
		return deliveryJob{}, false, fmt.Errorf("queue delivery job: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE delivery_jobs
		SET status = 'processing', attempts = attempts + 1, updated_at = ?, last_error = ''
		WHERE invoice_id = ? AND attempts < ? AND (
			status = 'queued'
			OR (status = 'failed' AND updated_at <= ?)
			OR (status = 'processing' AND updated_at <= ?)
		)
	`, now.UTC().Unix(), id, maxAttempts, now.Add(-retryAfter).UTC().Unix(), now.Add(-staleAfter).UTC().Unix())
	if err != nil {
		return deliveryJob{}, false, fmt.Errorf("acquire delivery job: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return deliveryJob{}, false, fmt.Errorf("read delivery job result: %w", err)
	}
	var job deliveryJob
	var updatedAt int64
	if err := tx.QueryRowContext(ctx, `SELECT status, attempts, updated_at FROM delivery_jobs WHERE invoice_id = ?`, id).Scan(&job.Status, &job.Attempts, &updatedAt); errors.Is(err, sql.ErrNoRows) {
		return deliveryJob{}, false, errConflict
	} else if err != nil {
		return deliveryJob{}, false, fmt.Errorf("read delivery job: %w", err)
	}
	job.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if err := tx.Commit(); err != nil {
		return deliveryJob{}, false, fmt.Errorf("commit delivery job: %w", err)
	}
	return job, changed == 1, nil
}

func (s *store) FailJob(ctx context.Context, id string, permanent bool, now time.Time, failure string) error {
	status := "failed"
	if permanent {
		status = "permanent"
	}
	if len(failure) > 200 {
		failure = failure[:200]
	}
	result, err := s.database.ExecContext(ctx, `
		UPDATE delivery_jobs SET status = ?, updated_at = ?, last_error = ?
		WHERE invoice_id = ? AND status = 'processing'
	`, status, now.UTC().Unix(), failure, id)
	if err != nil {
		return fmt.Errorf("fail delivery job: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read failed delivery job result: %w", err)
	}
	if changed != 1 {
		return errConflict
	}
	return nil
}

func (s *store) DeliverJob(ctx context.Context, id string, deliveredAt time.Time, delivery []byte) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin job delivery: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE invoices SET status = 'delivered', delivered_at = ?, delivery = ?
		WHERE id = ? AND status = 'submitted'
	`, deliveredAt.UTC().Unix(), delivery, id)
	if err != nil {
		return fmt.Errorf("mark invoice delivered: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read invoice delivery result: %w", err)
	}
	if changed != 1 {
		return errConflict
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE delivery_jobs SET status = 'delivered', updated_at = ?, last_error = ''
		WHERE invoice_id = ? AND status = 'processing'
	`, deliveredAt.UTC().Unix(), id)
	if err != nil {
		return fmt.Errorf("mark delivery job complete: %w", err)
	}
	changed, err = result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read delivery job result: %w", err)
	}
	if changed != 1 {
		return errConflict
	}
	return tx.Commit()
}
