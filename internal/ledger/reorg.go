package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
)

var (
	ErrGenesisDisconnect = errors.New("cannot disconnect genesis block")
	ErrInsufficientWork  = errors.New("replacement chain does not have more cumulative work")
	ErrStaleTip          = errors.New("chain tip changed while work was in progress")
)

const maxOrphanedMempoolBytes = 64 << 20

type orphanedMempoolCollector struct {
	blocks   [][]core.Transaction
	start    int
	count    int
	bytes    int
	maxCount int
	maxBytes int
}

func newOrphanedMempoolCollector(maxCount, maxBytes int) *orphanedMempoolCollector {
	return &orphanedMempoolCollector{maxCount: maxCount, maxBytes: maxBytes}
}

// add receives disconnected blocks newest-first. When the budget is exceeded,
// the newest transactions are discarded so older dependencies retain priority.
func (c *orphanedMempoolCollector) add(transactions []core.Transaction) {
	if len(transactions) == 0 || c.maxCount <= 0 || c.maxBytes <= 0 {
		return
	}
	c.blocks = append(c.blocks, transactions)
	for _, transaction := range transactions {
		c.count++
		c.bytes += core.EncodedTransactionSize(transaction)
	}
	for c.start < len(c.blocks) && (c.count > c.maxCount || c.bytes > c.maxBytes) {
		block := c.blocks[c.start]
		last := len(block) - 1
		transaction := block[last]
		c.count--
		c.bytes -= core.EncodedTransactionSize(transaction)
		block[last] = core.Transaction{}
		block = block[:last]
		if len(block) == 0 {
			c.blocks[c.start] = nil
			c.start++
		} else {
			c.blocks[c.start] = block
		}
	}
}

func (c *orphanedMempoolCollector) transactions() []core.Transaction {
	result := make([]core.Transaction, 0, c.count)
	for index := len(c.blocks) - 1; index >= c.start; index-- {
		result = append(result, c.blocks[index]...)
	}
	return result
}

func (l *Ledger) DisconnectTip(ctx context.Context) (core.Block, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	tx, err := l.database.BeginTx(ctx, nil)
	if err != nil {
		return core.Block{}, fmt.Errorf("begin tip disconnection: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	block, orphaned, err := disconnectTip(ctx, tx)
	if err != nil {
		return core.Block{}, err
	}
	if err := rebuildMempool(ctx, tx, orphaned); err != nil {
		return core.Block{}, fmt.Errorf("revalidate mempool after disconnecting block %d: %w", block.Height, err)
	}
	if err := tx.Commit(); err != nil {
		return core.Block{}, fmt.Errorf("commit tip disconnection: %w", err)
	}
	return block, nil
}

func (l *Ledger) ReplaceFrom(ctx context.Context, ancestorHeight uint64, blocks []core.Block) error {
	if len(blocks) == 0 {
		return ErrInsufficientWork
	}
	return l.ReplaceFromSource(ctx, ancestorHeight, len(blocks), func(index int) (core.Block, error) {
		return blocks[index], nil
	})
}

// ReplaceFromSource streams candidate blocks into one atomic reorganization.
// The callback must return each block exactly once in ascending height order
// and must not call back into the same Ledger.
func (l *Ledger) ReplaceFromSource(
	ctx context.Context,
	ancestorHeight uint64,
	blockCount int,
	blockAt func(index int) (core.Block, error),
) error {
	return l.replaceFromSourceAtTime(ctx, ancestorHeight, blockCount, blockAt, time.Now().Unix())
}

// ReplaceFromSourceAtTime applies one validation-time snapshot to every header
// in a replacement. It keeps future-time decisions deterministic across a
// potentially long atomic reorganization.
func (l *Ledger) ReplaceFromSourceAtTime(
	ctx context.Context,
	ancestorHeight uint64,
	blockCount int,
	blockAt func(index int) (core.Block, error),
	validationTime int64,
) error {
	return l.replaceFromSourceAtTime(ctx, ancestorHeight, blockCount, blockAt, validationTime)
}

func (l *Ledger) replaceFromSourceAtTime(
	ctx context.Context,
	ancestorHeight uint64,
	blockCount int,
	blockAt func(index int) (core.Block, error),
	validationTime int64,
) error {
	if blockCount <= 0 || blockAt == nil {
		return ErrInsufficientWork
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	tx, err := l.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin chain replacement: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	oldTip, err := tipFromQuery(ctx, tx)
	if err != nil {
		return err
	}
	if ancestorHeight > oldTip.Height || ancestorHeight > math.MaxInt64 {
		return fmt.Errorf("replacement ancestor %d is not on the active chain", ancestorHeight)
	}
	prunedHeight, err := prunedThrough(ctx, tx)
	if err != nil {
		return fmt.Errorf("read prune horizon before chain replacement: %w", err)
	}
	if ancestorHeight < prunedHeight {
		return fmt.Errorf("%w: ancestor %d is below retained height %d", ErrReorgBeyondPrune, ancestorHeight, prunedHeight)
	}

	orphaned := newOrphanedMempoolCollector(core.MaxPendingTransactions, maxOrphanedMempoolBytes)
	for height := oldTip.Height; height > ancestorHeight; height-- {
		_, transactions, err := disconnectTip(ctx, tx)
		if err != nil {
			return fmt.Errorf("disconnect old chain at height %d: %w", height, err)
		}
		orphaned.add(transactions)
	}
	for index := 0; index < blockCount; index++ {
		block, err := blockAt(index)
		if err != nil {
			return fmt.Errorf("load replacement block %d: %w", index, err)
		}
		if err := connectBlockAtTime(ctx, tx, block, validationTime); err != nil {
			return fmt.Errorf("connect replacement block %d: %w", index, err)
		}
	}
	newTip, err := tipFromQuery(ctx, tx)
	if err != nil {
		return err
	}
	if newTip.Work.Cmp(oldTip.Work) <= 0 {
		return fmt.Errorf("%w: old=%s new=%s", ErrInsufficientWork, oldTip.Work, newTip.Work)
	}
	if err := rebuildMempool(ctx, tx, orphaned.transactions()); err != nil {
		return fmt.Errorf("revalidate mempool after chain replacement: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit chain replacement: %w", err)
	}
	return nil
}

func disconnectTip(ctx context.Context, tx *sql.Tx) (core.Block, []core.Transaction, error) {
	tip, err := tipFromQuery(ctx, tx)
	if err != nil {
		return core.Block{}, nil, err
	}
	if tip.Height == 0 {
		return core.Block{}, nil, ErrGenesisDisconnect
	}
	prunedHeight, err := prunedThrough(ctx, tx)
	if err != nil {
		return core.Block{}, nil, fmt.Errorf("read prune horizon before disconnecting tip: %w", err)
	}
	if tip.Height <= prunedHeight {
		return core.Block{}, nil, fmt.Errorf("%w: tip %d is not retained", ErrReorgBeyondPrune, tip.Height)
	}
	block, err := blockFromQuery(ctx, tx, tip.Height)
	if err != nil {
		return core.Block{}, nil, err
	}

	var undoHash string
	var undoData []byte
	if err := tx.QueryRowContext(ctx, `
		SELECT block_hash, data FROM block_undo WHERE block_height = ?
	`, int64(tip.Height)).Scan(&undoHash, &undoData); err != nil {
		return core.Block{}, nil, fmt.Errorf("read block %d undo data: %w", tip.Height, err)
	}
	if undoHash != tip.Hash {
		return core.Block{}, nil, fmt.Errorf("block %d undo hash does not match active tip", tip.Height)
	}
	var undo UndoRecord
	if err := decodeJSON(undoData, &undo); err != nil {
		return core.Block{}, nil, fmt.Errorf("decode block %d undo data: %w", tip.Height, err)
	}
	orphaned, err := regularTransactionsAt(ctx, tx, tip.Height)
	if err != nil {
		return core.Block{}, nil, err
	}

	created := make(map[core.Outpoint]struct{}, len(undo.Created))
	for _, outpoint := range undo.Created {
		if _, exists := created[outpoint]; exists {
			return core.Block{}, nil, fmt.Errorf("block %d undo data contains duplicate created output %s:%d", tip.Height, outpoint.TxID, outpoint.Index)
		}
		created[outpoint] = struct{}{}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM utxos WHERE tx_id = ? AND output_index = ?
		`, outpoint.TxID, int64(outpoint.Index)); err != nil {
			return core.Block{}, nil, fmt.Errorf("remove block %d output %s:%d: %w", tip.Height, outpoint.TxID, outpoint.Index, err)
		}
	}
	for _, record := range undo.Spent {
		outpoint := core.Outpoint{TxID: record.TxID, Index: record.OutputIndex}
		if _, createdInBlock := created[outpoint]; createdInBlock {
			continue
		}
		if record.Amount == 0 || record.Amount > math.MaxInt64 || record.CreatedHeight > math.MaxInt64 {
			return core.Block{}, nil, fmt.Errorf("block %d undo input %s:%d contains invalid numeric values", tip.Height, record.TxID, record.OutputIndex)
		}
		if err := core.ValidateAddress(record.Address); err != nil {
			return core.Block{}, nil, fmt.Errorf("block %d undo input %s:%d: %w", tip.Height, record.TxID, record.OutputIndex, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO utxos(tx_id, output_index, amount, address, created_height, coinbase)
			VALUES(?, ?, ?, ?, ?, ?)
		`, record.TxID, int64(record.OutputIndex), int64(record.Amount), record.Address,
			int64(record.CreatedHeight), record.Coinbase); err != nil {
			return core.Block{}, nil, fmt.Errorf("restore block %d input %s:%d: %w", tip.Height, record.TxID, record.OutputIndex, err)
		}
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM blocks WHERE height = ? AND hash = ?", int64(tip.Height), tip.Hash)
	if err != nil {
		return core.Block{}, nil, fmt.Errorf("delete block %d: %w", tip.Height, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return core.Block{}, nil, fmt.Errorf("check deleted block %d: %w", tip.Height, err)
	}
	if rows != 1 {
		return core.Block{}, nil, fmt.Errorf("active tip changed while disconnecting block %d", tip.Height)
	}
	return block, orphaned, nil
}

func blockFromQuery(ctx context.Context, query sqlQueryer, height uint64) (core.Block, error) {
	if height > math.MaxInt64 {
		return core.Block{}, ErrBlockNotFound
	}
	var data []byte
	err := query.QueryRowContext(ctx, "SELECT data FROM blocks WHERE height = ?", int64(height)).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Block{}, ErrBlockNotFound
	}
	if err != nil {
		return core.Block{}, fmt.Errorf("read block %d: %w", height, err)
	}
	if len(data) != 0 {
		var block core.Block
		if err := decodeJSON(data, &block); err != nil {
			return core.Block{}, fmt.Errorf("decode block %d: %w", height, err)
		}
		return block, nil
	}
	header, err := scanHeader(query.QueryRowContext(ctx, `
		SELECT height, hash, previous_hash, version, timestamp, merkle_root, difficulty, nonce
		FROM blocks WHERE height = ?
	`, int64(height)))
	if err != nil {
		return core.Block{}, err
	}
	transactions, err := transactionsAt(ctx, query, height)
	if err != nil {
		return core.Block{}, err
	}
	header.Transactions = transactions
	return header, nil
}

func regularTransactionsAt(ctx context.Context, tx *sql.Tx, height uint64) ([]core.Transaction, error) {
	transactions, err := transactionsAt(ctx, tx, height)
	if err != nil {
		return nil, err
	}
	regular := make([]core.Transaction, 0, len(transactions))
	for _, transaction := range transactions {
		if !transaction.Coinbase {
			regular = append(regular, transaction)
		}
	}
	return regular, nil
}

func transactionsAt(ctx context.Context, query sqlQueryer, height uint64) ([]core.Transaction, error) {
	rows, err := query.QueryContext(ctx, `
		SELECT tx_index, data FROM transactions
		WHERE block_height = ? ORDER BY tx_index
	`, int64(height))
	if err != nil {
		return nil, fmt.Errorf("query block %d transactions: %w", height, err)
	}
	defer rows.Close()
	transactions := make([]core.Transaction, 0)
	for rows.Next() {
		var position int
		var data []byte
		if err := rows.Scan(&position, &data); err != nil {
			return nil, fmt.Errorf("scan block %d transaction: %w", height, err)
		}
		if position != len(transactions) {
			return nil, fmt.Errorf("block %d transaction positions are not contiguous", height)
		}
		var transaction core.Transaction
		if len(data) == 0 {
			return nil, fmt.Errorf("%w: transaction bodies at height %d are not retained", ErrReorgBeyondPrune, height)
		}
		if err := decodeJSON(data, &transaction); err != nil {
			return nil, fmt.Errorf("decode block %d transaction: %w", height, err)
		}
		transactions = append(transactions, transaction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate block %d transactions: %w", height, err)
	}
	return transactions, nil
}
