package entpay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrInvoiceNotFound = errors.New("invoice not found")
	ErrUnauthorized    = errors.New("invoice authorization failed")
	ErrConflict        = errors.New("invoice state conflict")
)

type invoiceRecord struct {
	Invoice
	Query        string
	Status       string
	TxID         string
	DeliveryJSON []byte
}

type Store struct {
	database *sql.DB
}

func OpenStore(path string) (*Store, error) {
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
			query TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			confirmations INTEGER NOT NULL CHECK(confirmations > 0),
			status TEXT NOT NULL CHECK(status IN ('open', 'submitted', 'delivered')),
			tx_id TEXT UNIQUE,
			created_at INTEGER NOT NULL,
			delivered_at INTEGER,
			delivery BLOB
		);
		CREATE INDEX IF NOT EXISTS invoices_expiry ON invoices(status, expires_at);
	`); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("create invoice schema: %w", err)
	}
	return &Store{database: database}, nil
}

func (s *Store) Close() error {
	return s.database.Close()
}

func (s *Store) Create(ctx context.Context, merchant string, amount uint64, resource, query string, expiresAt time.Time, confirmations uint64) (invoiceRecord, string, error) {
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
			Merchant: merchant, Amount: amount, Resource: resource,
			ExpiresAt: expiresAt.UTC().Truncate(time.Second), Confirmations: confirmations,
		},
		Query: query, Status: "open",
	}
	_, err := s.database.ExecContext(ctx, `
		INSERT INTO invoices(id, token_hash, merchant, amount, resource, query, expires_at, confirmations, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'open', ?)
	`, id, tokenHash[:], merchant, amount, resource, query, record.ExpiresAt.Unix(), confirmations, time.Now().UTC().Unix())
	if err != nil {
		return invoiceRecord{}, "", fmt.Errorf("store invoice: %w", err)
	}
	return record, token, nil
}

func (s *Store) Authorized(ctx context.Context, id, token string) (invoiceRecord, error) {
	var record invoiceRecord
	var tokenHash []byte
	var expires int64
	err := s.database.QueryRowContext(ctx, `
		SELECT id, token_hash, merchant, amount, resource, query, expires_at, confirmations, status, COALESCE(tx_id, ''), COALESCE(delivery, '')
		FROM invoices WHERE id = ?
	`, id).Scan(&record.ID, &tokenHash, &record.Merchant, &record.Amount, &record.Resource, &record.Query, &expires, &record.Confirmations, &record.Status, &record.TxID, &record.DeliveryJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return invoiceRecord{}, ErrInvoiceNotFound
	}
	if err != nil {
		return invoiceRecord{}, fmt.Errorf("read invoice: %w", err)
	}
	presented := sha256.Sum256([]byte(token))
	if len(tokenHash) != len(presented) || subtle.ConstantTimeCompare(tokenHash, presented[:]) != 1 {
		return invoiceRecord{}, ErrUnauthorized
	}
	record.Protocol = ProtocolVersion
	record.Network = "entropy-mainnet-v1"
	record.ExpiresAt = time.Unix(expires, 0).UTC()
	return record, nil
}

func (s *Store) Submit(ctx context.Context, id, transactionID string) error {
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
	if changed != 1 {
		var existing string
		if err := s.database.QueryRowContext(ctx, `SELECT COALESCE(tx_id, '') FROM invoices WHERE id = ? AND status = 'submitted'`, id).Scan(&existing); err == nil && existing == transactionID {
			return nil
		}
		return ErrConflict
	}
	return nil
}

func (s *Store) Deliver(ctx context.Context, id string, deliveredAt time.Time, delivery []byte) error {
	result, err := s.database.ExecContext(ctx, `
		UPDATE invoices SET status = 'delivered', delivered_at = ?, delivery = ? WHERE id = ? AND status = 'submitted'
	`, deliveredAt.UTC().Unix(), delivery, id)
	if err != nil {
		return fmt.Errorf("mark invoice delivered: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read delivery update result: %w", err)
	}
	if changed != 1 {
		return ErrConflict
	}
	return nil
}
