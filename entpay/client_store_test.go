package entpay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestClientStoreEncryptsAndDeduplicatesReceivedHandoff(t *testing.T) {
	protector, err := newXChaChaProtector(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "entpay-client.db")
	store, err := OpenClientStore(path, protector)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := newOpaqueToken()
	nonce, _ := newOpaqueToken()
	launch := LaunchRequest{Merchant: "https://merchant.example/entpay/", Handoff: code}
	first, err := store.CreateReceived(context.Background(), launch, nonce)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateReceived(context.Background(), launch, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Stage != StageReceived || first.Revision != 1 {
		t.Fatalf("deduplicated sessions differ: %+v %+v", first, second)
	}
	capsule, err := store.Capsule(context.Background(), first.ID)
	if err != nil || capsule.HandoffCode != code || capsule.ClientNonce != nonce {
		t.Fatalf("capsule = %+v, %v", capsule, err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte(code)) || bytes.Contains(contents, []byte(nonce)) {
		t.Fatal("client database contains handoff secret plaintext")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("client database permissions = %v", info.Mode().Perm())
	}
}

func TestClientStoreTransitionRevisionAndLegality(t *testing.T) {
	protector, _ := newXChaChaProtector(bytes.Repeat([]byte{9}, 32))
	store, err := OpenClientStore(filepath.Join(t.TempDir(), "client.db"), protector)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := newOpaqueToken()
	nonce, _ := newOpaqueToken()
	session, err := store.CreateReceived(context.Background(), LaunchRequest{Merchant: "https://merchant.example/", Handoff: code}, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.transition(context.Background(), session.ID, session.Revision, StageBroadcast, "", ""); !errors.Is(err, ErrClientStage) {
		t.Fatalf("illegal transition error = %v", err)
	}
	session, err = store.transition(context.Background(), session.ID, session.Revision, StageInspecting, "", "")
	if err != nil || session.Revision != 2 || session.Stage != StageInspecting {
		t.Fatalf("inspect transition = %+v, %v", session, err)
	}
	if _, err := store.transition(context.Background(), session.ID, 1, StageAwaitingApproval, "", ""); !errors.Is(err, ErrClientRevision) {
		t.Fatalf("stale revision error = %v", err)
	}
}

func TestClientStoreDeletesOnlySafeRequestStages(t *testing.T) {
	protector, _ := newXChaChaProtector(bytes.Repeat([]byte{6}, 32))
	store, err := OpenClientStore(filepath.Join(t.TempDir(), "client.db"), protector)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	create := func(stage ClientStage, transactionID string) ClientSession {
		code, _ := newOpaqueToken()
		nonce, _ := newOpaqueToken()
		session, createErr := store.CreateReceived(context.Background(), LaunchRequest{Merchant: "https://merchant.example/", Handoff: code}, nonce)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, updateErr := store.database.Exec(`UPDATE sessions SET stage = ?, transaction_id = ? WHERE id = ?`, stage, transactionID, session.ID); updateErr != nil {
			t.Fatal(updateErr)
		}
		return session
	}

	awaiting := create(StageAwaitingApproval, "")
	if err := store.DeleteSession(context.Background(), awaiting.ID); err != nil {
		t.Fatalf("delete awaiting approval: %v", err)
	}
	retryable := create(StageFailedRetryable, "")
	if err := store.DeleteSession(context.Background(), retryable.ID); err != nil {
		t.Fatalf("delete unpaid retryable request: %v", err)
	}
	paid := create(StageFailedRetryable, "tx1")
	if err := store.DeleteSession(context.Background(), paid.ID); !errors.Is(err, ErrClientStage) {
		t.Fatalf("delete paid retryable request error = %v", err)
	}
	active := create(StageConfirming, "tx2")
	if err := store.DeleteSession(context.Background(), active.ID); !errors.Is(err, ErrClientStage) {
		t.Fatalf("delete active request error = %v", err)
	}
}

func TestClientStorePresentsLegacyPayloadAsGenericResult(t *testing.T) {
	protector, _ := newXChaChaProtector(bytes.Repeat([]byte{8}, 32))
	store, err := OpenClientStore(filepath.Join(t.TempDir(), "client.db"), protector)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := newOpaqueToken()
	nonce, _ := newOpaqueToken()
	session, err := store.CreateReceived(context.Background(), LaunchRequest{Merchant: "https://merchant.example/", Handoff: code}, nonce)
	if err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"merchant_field":"value"}`)
	if _, err := store.database.Exec(`UPDATE sessions SET payload_json = ? WHERE id = ?`, legacy, session.ID); err != nil {
		t.Fatal(err)
	}
	session, err = store.Session(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Result == nil || session.Result.Schema != LegacyResultSchema || session.Result.Summary != "Legacy merchant delivery" || string(session.Result.Data) != string(legacy) {
		t.Fatalf("legacy result = %+v", session.Result)
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"payload"`)) || !bytes.Contains(encoded, []byte(`"result"`)) {
		t.Fatalf("client session DTO exposed the wrong delivery fields: %s", encoded)
	}
}

func TestClientStoreRejectsNewerSchemaAndSymlink(t *testing.T) {
	protector, _ := newXChaChaProtector(bytes.Repeat([]byte{3}, 32))
	directory := t.TempDir()
	target := filepath.Join(directory, "target.db")
	if err := os.WriteFile(target, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "client.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenClientStore(link, protector); err == nil {
		t.Fatal("symlink client database was accepted")
	}
}

func TestClientStoreReestablishesChangedMerchantTrustSeparatelyFromApproval(t *testing.T) {
	protector, _ := newXChaChaProtector(bytes.Repeat([]byte{5}, 32))
	store, err := OpenClientStore(filepath.Join(t.TempDir(), "client.db"), protector)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := newOpaqueToken()
	nonce, _ := newOpaqueToken()
	session, err := store.CreateReceived(context.Background(), LaunchRequest{Merchant: "https://merchant.example/", Handoff: code}, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.database.Exec(`UPDATE sessions SET merchant = 'ent1new', signing_key = 'new-key', signing_key_fingerprint = 'new-fingerprint', merchant_trust_state = 'changed', stage = 'awaiting_approval' WHERE id = ?`, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.database.Exec(`INSERT INTO merchant_identities(endpoint, merchant, signing_key, fingerprint, trusted_at, updated_at) VALUES ('https://merchant.example/', 'ent1old', 'old-key', 'old-fingerprint', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	updated, err := store.ReestablishMerchantTrust(context.Background(), session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Stage != StageAwaitingApproval || updated.MerchantTrustState != "previous" || updated.Revision != session.Revision+1 || updated.TransactionID != "" {
		t.Fatalf("reestablished session = %+v", updated)
	}
	if _, err := store.ReestablishMerchantTrust(context.Background(), session.ID, session.Revision); !errors.Is(err, ErrClientRevision) {
		t.Fatalf("stale re-trust error = %v", err)
	}
	var merchant, signingKey string
	if err := store.database.QueryRow(`SELECT merchant, signing_key FROM merchant_identities WHERE endpoint = 'https://merchant.example/'`).Scan(&merchant, &signingKey); err != nil {
		t.Fatal(err)
	}
	if merchant != "ent1new" || signingKey != "new-key" {
		t.Fatalf("trusted identity = %q, %q", merchant, signingKey)
	}
}

func TestClientStoreSettingsRevisionAndRequestRetention(t *testing.T) {
	protector, _ := newXChaChaProtector(bytes.Repeat([]byte{6}, 32))
	store, err := OpenClientStore(filepath.Join(t.TempDir(), "client.db"), protector)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	directory := filepath.Join(t.TempDir(), "artifacts")
	settings, err := store.EnsureSettings(context.Background(), 1_000_000, directory)
	if err != nil || settings.RequestRetentionDays != 30 || !settings.RequireEveryApproval {
		t.Fatalf("default settings = %+v, %v", settings, err)
	}
	settings.RequestRetentionDays = 0
	updated, err := store.SaveSettings(context.Background(), settings, settings.Revision)
	if err != nil || updated.Revision != settings.Revision+1 {
		t.Fatalf("saved settings = %+v, %v", updated, err)
	}
	if _, err := store.SaveSettings(context.Background(), settings, settings.Revision); !errors.Is(err, ErrClientRevision) {
		t.Fatalf("stale settings error = %v", err)
	}
	code, _ := newOpaqueToken()
	nonce, _ := newOpaqueToken()
	session, err := store.CreateReceived(context.Background(), LaunchRequest{Merchant: "https://merchant.example/", Handoff: code}, nonce)
	if err != nil {
		t.Fatal(err)
	}
	capsule, err := store.Capsule(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	capsule.Input = []byte(`{"secret":"request"}`)
	encoded, _ := json.Marshal(capsule)
	encrypted, err := protector.Seal(session.ID, clientCapsuleSchema, encoded)
	clear(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.database.Exec(`UPDATE sessions SET encrypted_capsule = ?, stage = 'complete', updated_at = 1 WHERE id = ?`, encrypted, session.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.PurgeExpiredRequestDetails(context.Background()); err != nil {
		t.Fatal(err)
	}
	capsule, err = store.Capsule(context.Background(), session.ID)
	if err != nil || len(capsule.Input) != 0 || capsule.HandoffCode != code {
		t.Fatalf("purged capsule = %+v, %v", capsule, err)
	}
}
