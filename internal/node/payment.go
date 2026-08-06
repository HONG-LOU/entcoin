package node

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/HONG-LOU/entcoin/internal/core"
	"github.com/HONG-LOU/entcoin/internal/ledger"
)

var (
	ErrPaymentWalletChanged   = errors.New("active wallet changed after payment review")
	ErrPaymentFeeExceeded     = errors.New("payment fee exceeds the reviewed ceiling")
	ErrWalletRecoveryRequired = errors.New("wallet recovery must be secured before Agent Pay")
)

type PaymentPreview struct {
	Wallet             string `json:"wallet"`
	Recipient          string `json:"recipient"`
	Amount             uint64 `json:"amount"`
	EstimatedFee       uint64 `json:"estimated_fee"`
	MaximumFee         uint64 `json:"maximum_fee"`
	TotalDebit         uint64 `json:"total_debit"`
	SpendableBalance   uint64 `json:"spendable_balance"`
	HasSufficientFunds bool   `json:"has_sufficient_funds"`
	WalletNeedsBackup  bool   `json:"wallet_needs_backup"`
}

func (s *Service) PreviewRecommendedPayment(expectedWallet, to string, amount uint64) (PaymentPreview, error) {
	if err := validatePaymentArguments(expectedWallet, to, amount); err != nil {
		return PaymentPreview{}, err
	}
	s.walletMutationMu.Lock()
	defer s.walletMutationMu.Unlock()
	chain, wallet, needsBackup, err := s.paymentWallet(expectedWallet)
	if err != nil {
		return PaymentPreview{}, err
	}
	defer s.wait.Done()
	utxo, err := chain.SpendableUTXO(context.Background(), wallet.Address)
	if err != nil {
		return PaymentPreview{}, err
	}
	_, spendable, err := chain.Balances(context.Background(), wallet.Address)
	if err != nil {
		return PaymentPreview{}, err
	}
	preview := PaymentPreview{
		Wallet: wallet.Address, Recipient: to, Amount: amount, SpendableBalance: spendable,
		EstimatedFee: ledger.MinimumRelayFee(1), WalletNeedsBackup: needsBackup,
	}
	transaction, fee, buildErr := buildRecommendedPayment(wallet, to, amount, utxo)
	if buildErr == nil {
		preview.EstimatedFee = fee
		preview.MaximumFee = conservativeFeeCeiling(transaction, fee)
	} else {
		preview.MaximumFee = preview.EstimatedFee
	}
	preview.TotalDebit, err = addPaymentAmounts(amount, preview.MaximumFee)
	if err != nil {
		return PaymentPreview{}, err
	}
	preview.HasSufficientFunds = buildErr == nil && spendable >= preview.TotalDebit
	if buildErr != nil && spendable >= preview.TotalDebit {
		return PaymentPreview{}, buildErr
	}
	return preview, nil
}

func (s *Service) SendRecommendedFrom(expectedWallet, to string, amount, maximumFee uint64) (core.Transaction, uint64, error) {
	transaction, fee, err := s.PrepareRecommendedPayment(expectedWallet, to, amount, maximumFee)
	if err != nil {
		return core.Transaction{}, fee, err
	}
	if err := s.CommitPreparedPayment(expectedWallet, to, amount, fee, maximumFee, transaction); err != nil {
		return core.Transaction{}, fee, err
	}
	return transaction, fee, nil
}

func (s *Service) PrepareRecommendedPayment(expectedWallet, to string, amount, maximumFee uint64) (core.Transaction, uint64, error) {
	if err := validatePaymentArguments(expectedWallet, to, amount); err != nil {
		return core.Transaction{}, 0, err
	}
	if maximumFee == 0 {
		return core.Transaction{}, 0, fmt.Errorf("payment fee ceiling must be greater than zero")
	}
	s.walletMutationMu.Lock()
	defer s.walletMutationMu.Unlock()
	chain, wallet, needsBackup, err := s.paymentWallet(expectedWallet)
	if err != nil {
		return core.Transaction{}, 0, err
	}
	defer s.wait.Done()
	if needsBackup {
		return core.Transaction{}, 0, ErrWalletRecoveryRequired
	}
	utxo, err := chain.SpendableUTXO(context.Background(), wallet.Address)
	if err != nil {
		return core.Transaction{}, 0, err
	}
	transaction, fee, err := buildRecommendedPayment(wallet, to, amount, utxo)
	if err != nil {
		return core.Transaction{}, 0, err
	}
	if fee > maximumFee {
		return core.Transaction{}, fee, fmt.Errorf("%w: need %s ENT, reviewed %s ENT", ErrPaymentFeeExceeded, core.FormatAmount(fee), core.FormatAmount(maximumFee))
	}
	return transaction, fee, nil
}

func (s *Service) CommitPreparedPayment(expectedWallet, to string, amount, fee, maximumFee uint64, transaction core.Transaction) error {
	if err := validatePaymentArguments(expectedWallet, to, amount); err != nil {
		return err
	}
	if fee == 0 || fee > maximumFee {
		return fmt.Errorf("%w: need %s ENT, reviewed %s ENT", ErrPaymentFeeExceeded, core.FormatAmount(fee), core.FormatAmount(maximumFee))
	}
	if err := validatePreparedPayment(expectedWallet, to, amount, transaction); err != nil {
		return err
	}
	s.walletMutationMu.Lock()
	defer s.walletMutationMu.Unlock()
	chain, _, needsBackup, err := s.paymentWallet(expectedWallet)
	if err != nil {
		return err
	}
	defer s.wait.Done()
	if needsBackup {
		return ErrWalletRecoveryRequired
	}
	utxo, err := chain.SpendableUTXO(context.Background(), expectedWallet)
	if err != nil {
		return err
	}
	actualFee, validationErr := core.ValidateRegularTransaction(transaction, utxo)
	if validationErr == nil && actualFee != fee {
		return fmt.Errorf("prepared payment fee does not match its journal")
	}
	if validationErr == nil && actualFee > maximumFee {
		return fmt.Errorf("%w: need %s ENT, reviewed %s ENT", ErrPaymentFeeExceeded, core.FormatAmount(actualFee), core.FormatAmount(maximumFee))
	}
	if err := chain.AddTransaction(context.Background(), transaction); err != nil && !errors.Is(err, ledger.ErrTransactionAlreadyKnown) {
		return err
	}
	s.broadcastTransaction(transaction, nil)
	return nil
}

func (s *Service) paymentWallet(expectedWallet string) (*ledger.Ledger, core.Wallet, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.ledger == nil || s.material == nil {
		return nil, core.Wallet{}, false, fmt.Errorf("node is closed")
	}
	if s.seedMode {
		return nil, core.Wallet{}, false, ErrSeedModeWalletUnavailable
	}
	if !core.AddressesEqual(expectedWallet, s.wallet.Address) {
		return nil, core.Wallet{}, false, ErrPaymentWalletChanged
	}
	s.wait.Add(1)
	return s.ledger, s.wallet, s.walletNeedsBackup, nil
}

func validatePaymentArguments(expectedWallet, to string, amount uint64) error {
	if err := core.ValidateAddress(expectedWallet); err != nil {
		return fmt.Errorf("expected payment wallet: %w", err)
	}
	if err := core.ValidateAddress(to); err != nil {
		return fmt.Errorf("payment recipient: %w", err)
	}
	if amount == 0 {
		return fmt.Errorf("payment amount must be greater than zero")
	}
	return nil
}

func validatePreparedPayment(expectedWallet, to string, amount uint64, transaction core.Transaction) error {
	if transaction.Coinbase || transaction.ID == "" || transaction.ID != transaction.ComputeID() || len(transaction.Inputs) == 0 {
		return fmt.Errorf("prepared payment transaction is invalid")
	}
	merchantOutputs := 0
	for _, output := range transaction.Outputs {
		switch {
		case core.AddressesEqual(output.Address, to) && output.Amount == amount:
			merchantOutputs++
		case core.AddressesEqual(output.Address, expectedWallet):
		default:
			return fmt.Errorf("prepared payment contains an unexpected output")
		}
	}
	if merchantOutputs != 1 {
		return fmt.Errorf("prepared payment must contain exactly one invoice output")
	}
	for _, input := range transaction.Inputs {
		if !core.AddressesEqual(core.AddressFromPublicKey(input.PublicKey), expectedWallet) {
			return fmt.Errorf("prepared payment input belongs to a different wallet")
		}
	}
	return nil
}

func buildRecommendedPayment(wallet core.Wallet, to string, amount uint64, utxo core.UTXO) (core.Transaction, uint64, error) {
	fee := ledger.MinimumRelayFee(1)
	for attempt := 0; ; attempt++ {
		transaction, err := core.BuildTransaction(&wallet, to, amount, fee, utxo)
		if err != nil {
			return core.Transaction{}, 0, err
		}
		required := ledger.MinimumRelayFee(core.EncodedTransactionSize(transaction))
		if fee >= required {
			return transaction, fee, nil
		}
		if attempt >= core.MaxTransactionInputs {
			return core.Transaction{}, 0, fmt.Errorf("automatic fee calculation did not converge")
		}
		fee = required
	}
}

func conservativeFeeCeiling(transaction core.Transaction, estimated uint64) uint64 {
	// ECDSA DER signatures vary slightly in length between preview and send.
	maximumSize := core.EncodedTransactionSize(transaction) + len(transaction.Inputs)*8 + 32
	return max(estimated, ledger.MinimumRelayFee(maximumSize))
}

func addPaymentAmounts(left, right uint64) (uint64, error) {
	if left > math.MaxUint64-right {
		return 0, fmt.Errorf("payment total exceeds the amount range")
	}
	return left + right, nil
}
