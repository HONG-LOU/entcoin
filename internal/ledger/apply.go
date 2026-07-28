package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
)

const (
	addressDirectionInput  = -1
	addressDirectionOutput = 1
)

func (l *Ledger) ConnectBlock(ctx context.Context, block core.Block) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	tx, err := l.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin block connection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := connectBlock(ctx, tx, block); err != nil {
		return err
	}
	if err := rebuildMempool(ctx, tx, nil); err != nil {
		return fmt.Errorf("revalidate mempool after block %d: %w", block.Height, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit block %d: %w", block.Height, err)
	}
	return nil
}

func connectBlock(ctx context.Context, tx *sql.Tx, block core.Block) error {
	return connectBlockAtTime(ctx, tx, block, time.Now().Unix())
}

func connectBlockAtTime(ctx context.Context, tx *sql.Tx, block core.Block, validationTime int64) error {
	tip, err := tipFromQuery(ctx, tx)
	if err != nil {
		return err
	}
	if block.Height > math.MaxInt64 {
		return fmt.Errorf("block height exceeds storage range")
	}
	priorHeaders, err := headerWindowFromQuery(ctx, tx, tip.Height)
	if err != nil {
		return err
	}
	if len(priorHeaders) == 0 {
		return fmt.Errorf("ledger has no previous block header")
	}
	if err := core.ValidateBlockHeaderAtTime(block, priorHeaders[len(priorHeaders)-1], priorHeaders, validationTime); err != nil {
		return fmt.Errorf("validate block %d header: %w", block.Height, err)
	}
	if len(block.Transactions) == 0 || !block.Transactions[0].Coinbase {
		return fmt.Errorf("block %d: first transaction must be coinbase", block.Height)
	}

	blockData, err := encodeJSON(block)
	if err != nil {
		return fmt.Errorf("encode block %d: %w", block.Height, err)
	}
	cumulativeWork := new(big.Int).Add(new(big.Int).Set(tip.Work), blockWork(block.Difficulty))
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO blocks(
			height, hash, previous_hash, version, timestamp, merkle_root,
			difficulty, nonce, cumulative_work, data, encoded_size
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, int64(block.Height), block.Hash, block.PreviousHash, int64(block.Version), block.Timestamp,
		block.MerkleRoot, int64(block.Difficulty), encodeUint64(block.Nonce),
		encodeWork(cumulativeWork), blockData, len(blockData)); err != nil {
		return fmt.Errorf("insert block %d: %w", block.Height, err)
	}

	undo := UndoRecord{}
	seen := make(map[string]struct{}, len(block.Transactions))
	fees := uint64(0)
	for index, transaction := range block.Transactions[1:] {
		position := index + 1
		if transaction.Coinbase {
			return fmt.Errorf("block %d transaction %d: multiple coinbase transactions", block.Height, position)
		}
		if err := ensureNewTransactionID(ctx, tx, transaction.ID, seen); err != nil {
			return fmt.Errorf("block %d transaction %d: %w", block.Height, position, err)
		}
		inputs, inputUTXO, err := loadTransactionInputs(ctx, tx, transaction)
		if err != nil {
			return fmt.Errorf("block %d transaction %d: %w", block.Height, position, err)
		}
		fee, err := core.ValidateRegularTransaction(transaction, inputUTXO)
		if err != nil {
			return fmt.Errorf("block %d transaction %d: %w", block.Height, position, err)
		}
		if err := validateInputMaturity(inputs, block.Height); err != nil {
			return fmt.Errorf("block %d transaction %d: %w", block.Height, position, err)
		}
		fees, err = checkedAdd(fees, fee)
		if err != nil {
			return fmt.Errorf("block %d transaction fees: %w", block.Height, err)
		}
		created, err := applyRegularTransaction(ctx, tx, transaction, block.Height, inputs)
		if err != nil {
			return fmt.Errorf("block %d transaction %d: %w", block.Height, position, err)
		}
		if err := insertTransaction(ctx, tx, transaction, block.Height, position, inputs); err != nil {
			return fmt.Errorf("block %d transaction %d: %w", block.Height, position, err)
		}
		undo.Spent = append(undo.Spent, inputs...)
		undo.Created = append(undo.Created, created...)
		seen[transaction.ID] = struct{}{}
	}

	coinbase := block.Transactions[0]
	if err := ensureNewTransactionID(ctx, tx, coinbase.ID, seen); err != nil {
		return fmt.Errorf("block %d coinbase: %w", block.Height, err)
	}
	maximumReward, err := checkedAdd(core.Subsidy(block.Height), fees)
	if err != nil {
		return fmt.Errorf("block %d coinbase reward: %w", block.Height, err)
	}
	if err := core.ValidateCoinbase(coinbase, maximumReward); err != nil {
		return fmt.Errorf("block %d coinbase: %w", block.Height, err)
	}
	created, err := insertOutputs(ctx, tx, coinbase, block.Height, true)
	if err != nil {
		return fmt.Errorf("block %d coinbase: %w", block.Height, err)
	}
	if err := insertTransaction(ctx, tx, coinbase, block.Height, 0, nil); err != nil {
		return fmt.Errorf("block %d coinbase: %w", block.Height, err)
	}
	undo.Created = append(undo.Created, created...)

	undoData, err := encodeJSON(undo)
	if err != nil {
		return fmt.Errorf("encode block %d undo data: %w", block.Height, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO block_undo(block_height, block_hash, data) VALUES(?, ?, ?)
	`, int64(block.Height), block.Hash, undoData); err != nil {
		return fmt.Errorf("insert block %d undo data: %w", block.Height, err)
	}
	return nil
}

func headerWindowFromQuery(ctx context.Context, query sqlQueryer, throughHeight uint64) ([]core.Block, error) {
	if throughHeight > math.MaxInt64 {
		return nil, fmt.Errorf("block height exceeds storage range")
	}
	start := uint64(0)
	if throughHeight+1 > core.FirstAdjustment {
		start = throughHeight + 1 - core.FirstAdjustment
	}
	rows, err := query.QueryContext(ctx, `
		SELECT height, hash, previous_hash, version, timestamp, merkle_root, difficulty, nonce
		FROM blocks WHERE height BETWEEN ? AND ? ORDER BY height
	`, int64(start), int64(throughHeight))
	if err != nil {
		return nil, fmt.Errorf("query header validation window: %w", err)
	}
	defer rows.Close()
	headers := make([]core.Block, 0, int(throughHeight-start)+1)
	for rows.Next() {
		header, err := scanHeader(rows)
		if err != nil {
			return nil, err
		}
		headers = append(headers, header)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate header validation window: %w", err)
	}
	return headers, nil
}

func ensureNewTransactionID(ctx context.Context, tx *sql.Tx, id string, seen map[string]struct{}) error {
	if _, exists := seen[id]; exists {
		return fmt.Errorf("duplicate transaction %s", id)
	}
	var exists int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM transactions WHERE id = ?", id).Scan(&exists)
	if err == nil {
		return fmt.Errorf("transaction %s already exists", id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check transaction %s: %w", id, err)
	}
	return nil
}

func loadTransactionInputs(ctx context.Context, tx *sql.Tx, transaction core.Transaction) ([]UTXORecord, core.UTXO, error) {
	records := make([]UTXORecord, 0, len(transaction.Inputs))
	utxo := make(core.UTXO, len(transaction.Inputs))
	for _, input := range transaction.Inputs {
		record, err := loadUTXO(ctx, tx, core.Outpoint{TxID: input.TxID, Index: input.OutputIndex})
		if err != nil {
			return nil, nil, err
		}
		records = append(records, record)
		utxo[core.Outpoint{TxID: record.TxID, Index: record.OutputIndex}] = record.Output()
	}
	return records, utxo, nil
}

func loadUTXO(ctx context.Context, query sqlQueryer, outpoint core.Outpoint) (UTXORecord, error) {
	var amount, createdHeight int64
	var address string
	var coinbase int
	err := query.QueryRowContext(ctx, `
		SELECT amount, address, created_height, coinbase
		FROM utxos WHERE tx_id = ? AND output_index = ?
	`, outpoint.TxID, int64(outpoint.Index)).Scan(&amount, &address, &createdHeight, &coinbase)
	if errors.Is(err, sql.ErrNoRows) {
		return UTXORecord{}, fmt.Errorf("input %s:%d is missing or already spent", outpoint.TxID, outpoint.Index)
	}
	if err != nil {
		return UTXORecord{}, fmt.Errorf("read input %s:%d: %w", outpoint.TxID, outpoint.Index, err)
	}
	if amount <= 0 || createdHeight < 0 || (coinbase != 0 && coinbase != 1) {
		return UTXORecord{}, fmt.Errorf("stored input %s:%d contains invalid values", outpoint.TxID, outpoint.Index)
	}
	return UTXORecord{
		TxID:          outpoint.TxID,
		OutputIndex:   outpoint.Index,
		Amount:        uint64(amount),
		Address:       address,
		CreatedHeight: uint64(createdHeight),
		Coinbase:      coinbase == 1,
	}, nil
}

func validateInputMaturity(inputs []UTXORecord, spendingHeight uint64) error {
	for _, input := range inputs {
		if input.Coinbase && !core.IsCoinbaseMature(input.CreatedHeight, spendingHeight) {
			return fmt.Errorf("coinbase input %s:%d is immature", input.TxID, input.OutputIndex)
		}
	}
	return nil
}

func applyRegularTransaction(
	ctx context.Context,
	tx *sql.Tx,
	transaction core.Transaction,
	height uint64,
	inputs []UTXORecord,
) ([]core.Outpoint, error) {
	for _, input := range inputs {
		result, err := tx.ExecContext(ctx, `
			DELETE FROM utxos WHERE tx_id = ? AND output_index = ?
		`, input.TxID, int64(input.OutputIndex))
		if err != nil {
			return nil, fmt.Errorf("spend input %s:%d: %w", input.TxID, input.OutputIndex, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("check spent input %s:%d: %w", input.TxID, input.OutputIndex, err)
		}
		if rows != 1 {
			return nil, fmt.Errorf("input %s:%d changed while connecting block", input.TxID, input.OutputIndex)
		}
	}
	return insertOutputs(ctx, tx, transaction, height, false)
}

func insertOutputs(ctx context.Context, tx *sql.Tx, transaction core.Transaction, height uint64, coinbase bool) ([]core.Outpoint, error) {
	created := make([]core.Outpoint, 0, len(transaction.Outputs))
	for index, output := range transaction.Outputs {
		if output.Amount > math.MaxInt64 {
			return nil, fmt.Errorf("output %d amount exceeds storage range", index)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO utxos(tx_id, output_index, amount, address, created_height, coinbase)
			VALUES(?, ?, ?, ?, ?, ?)
		`, transaction.ID, int64(index), int64(output.Amount), output.Address, int64(height), coinbase); err != nil {
			return nil, fmt.Errorf("create output %s:%d: %w", transaction.ID, index, err)
		}
		created = append(created, core.Outpoint{TxID: transaction.ID, Index: uint32(index)})
	}
	return created, nil
}

func insertTransaction(
	ctx context.Context,
	tx *sql.Tx,
	transaction core.Transaction,
	height uint64,
	position int,
	inputs []UTXORecord,
) error {
	data, err := encodeJSON(transaction)
	if err != nil {
		return fmt.Errorf("encode transaction %s: %w", transaction.ID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO transactions(id, block_height, tx_index, coinbase, data)
		VALUES(?, ?, ?, ?, ?)
	`, transaction.ID, int64(height), position, transaction.Coinbase, data); err != nil {
		return fmt.Errorf("insert transaction %s: %w", transaction.ID, err)
	}
	for index, input := range inputs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO transaction_addresses(tx_id, ordinal, address, direction, amount)
			VALUES(?, ?, ?, ?, ?)
		`, transaction.ID, index, input.Address, addressDirectionInput, int64(input.Amount)); err != nil {
			return fmt.Errorf("index transaction %s input %d: %w", transaction.ID, index, err)
		}
	}
	for index, output := range transaction.Outputs {
		if output.Amount > math.MaxInt64 {
			return fmt.Errorf("transaction %s output %d exceeds storage range", transaction.ID, index)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO transaction_addresses(tx_id, ordinal, address, direction, amount)
			VALUES(?, ?, ?, ?, ?)
		`, transaction.ID, index, output.Address, addressDirectionOutput, int64(output.Amount)); err != nil {
			return fmt.Errorf("index transaction %s output %d: %w", transaction.ID, index, err)
		}
	}
	return nil
}

func checkedAdd(left, right uint64) (uint64, error) {
	if math.MaxUint64-left < right {
		return 0, fmt.Errorf("amount overflows")
	}
	return left + right, nil
}
