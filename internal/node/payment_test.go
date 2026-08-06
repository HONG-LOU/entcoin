package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"
)

func TestPaymentPreviewAndExpectedWalletSend(t *testing.T) {
	service := newTestNode(t)
	recipient, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	block, err := service.MineOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	makePaymentFundingOrdinary(t, service, block.Transactions[0].ID)
	if err := service.ConfirmWalletRecovery(); err != nil {
		t.Fatal(err)
	}
	amount := uint64(1_000_000)
	preview, err := service.PreviewRecommendedPayment(service.Address(), recipient.Address, amount)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Wallet != service.Address() || preview.Amount != amount || preview.EstimatedFee == 0 || preview.MaximumFee < preview.EstimatedFee || preview.TotalDebit != amount+preview.MaximumFee || !preview.HasSufficientFunds || preview.WalletNeedsBackup {
		t.Fatalf("unexpected payment preview: %+v", preview)
	}
	transaction, fee, err := service.SendRecommendedFrom(preview.Wallet, recipient.Address, amount, preview.MaximumFee)
	if err != nil {
		t.Fatal(err)
	}
	if fee > preview.MaximumFee || transaction.ID == "" || transaction.ID != transaction.ComputeID() {
		t.Fatalf("unexpected payment result: fee=%d transaction=%+v", fee, transaction)
	}
	encoded, err := json.Marshal(transaction)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("payment transaction is not serializable: %v", err)
	}
}

func TestAgentPaymentGuardsWalletBackupAndFee(t *testing.T) {
	service := newTestNode(t)
	recipient, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	block, err := service.MineOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	makePaymentFundingOrdinary(t, service, block.Transactions[0].ID)
	preview, err := service.PreviewRecommendedPayment(service.Address(), recipient.Address, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.WalletNeedsBackup {
		t.Fatal("new wallet preview did not require recovery confirmation")
	}
	if _, _, err := service.SendRecommendedFrom(service.Address(), recipient.Address, 1_000_000, preview.MaximumFee); !errors.Is(err, ErrWalletRecoveryRequired) {
		t.Fatalf("backup guard error = %v", err)
	}
	if err := service.ConfirmWalletRecovery(); err != nil {
		t.Fatal(err)
	}
	other, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.SendRecommendedFrom(other.Address, recipient.Address, 1_000_000, preview.MaximumFee); !errors.Is(err, ErrPaymentWalletChanged) {
		t.Fatalf("wallet guard error = %v", err)
	}
	if _, fee, err := service.SendRecommendedFrom(service.Address(), recipient.Address, 1_000_000, 1); !errors.Is(err, ErrPaymentFeeExceeded) || fee <= 1 {
		t.Fatalf("fee guard result = fee %d, err %v", fee, err)
	}
	dashboard, err := service.Dashboard()
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.PendingCount != 0 {
		t.Fatalf("guarded payment entered mempool: %d", dashboard.PendingCount)
	}
}

func TestPaymentPreviewReportsInsufficientFundsWithoutBroadcast(t *testing.T) {
	service := newTestNode(t)
	recipient, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewRecommendedPayment(service.Address(), recipient.Address, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if preview.HasSufficientFunds || preview.SpendableBalance != 0 || preview.EstimatedFee != ledger.MinimumRelayFee(1) || preview.TotalDebit != preview.Amount+preview.MaximumFee {
		t.Fatalf("unexpected insufficient preview: %+v", preview)
	}
}

func TestPreparedPaymentCommitIsIdempotentAndRejectsTampering(t *testing.T) {
	service := newTestNode(t)
	recipient, err := core.NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	block, err := service.MineOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	makePaymentFundingOrdinary(t, service, block.Transactions[0].ID)
	if err := service.ConfirmWalletRecovery(); err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewRecommendedPayment(service.Address(), recipient.Address, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	transaction, fee, err := service.PrepareRecommendedPayment(preview.Wallet, recipient.Address, preview.Amount, preview.MaximumFee)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CommitPreparedPayment(preview.Wallet, recipient.Address, preview.Amount, fee, preview.MaximumFee, transaction); err != nil {
		t.Fatal(err)
	}
	if err := service.CommitPreparedPayment(preview.Wallet, recipient.Address, preview.Amount, fee, preview.MaximumFee, transaction); err != nil {
		t.Fatalf("idempotent commit: %v", err)
	}
	dashboard, err := service.Dashboard()
	if err != nil || dashboard.PendingCount != 1 {
		t.Fatalf("pending count = %d, %v", dashboard.PendingCount, err)
	}
	tampered := transaction
	tampered.Outputs = append([]core.TxOutput(nil), transaction.Outputs...)
	tampered.Outputs[0].Address = preview.Wallet
	tampered.ID = tampered.ComputeID()
	if err := service.CommitPreparedPayment(preview.Wallet, recipient.Address, preview.Amount, fee, preview.MaximumFee, tampered); err == nil {
		t.Fatal("tampered prepared payment was accepted")
	}
}

func makePaymentFundingOrdinary(t *testing.T, service *Service, transactionID string) {
	t.Helper()
	database, err := sql.Open("sqlite", service.ledger.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	result, err := database.Exec(`UPDATE utxos SET coinbase = 0 WHERE tx_id = ?`, transactionID)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		t.Fatalf("funding update changed %d rows: %v", changed, err)
	}
}
