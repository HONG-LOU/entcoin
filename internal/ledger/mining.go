package ledger

import (
	"container/heap"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
)

func (l *Ledger) BuildMiningCandidate(ctx context.Context, address string) (core.Block, Tip, error) {
	return l.buildMiningCandidateAtTime(ctx, address, time.Now().Unix())
}

func (l *Ledger) buildMiningCandidateAtTime(ctx context.Context, address string, now int64) (core.Block, Tip, error) {
	if err := core.ValidateAddress(address); err != nil {
		return core.Block{}, Tip{}, err
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	tx, err := l.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return core.Block{}, Tip{}, fmt.Errorf("begin mining snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	tip, err := tipFromQuery(ctx, tx)
	if err != nil {
		return core.Block{}, Tip{}, err
	}
	headers, err := headerWindowFromQuery(ctx, tx, tip.Height)
	if err != nil {
		return core.Block{}, Tip{}, err
	}
	pending, err := mempoolTransactionsFromQuery(ctx, tx)
	if err != nil {
		return core.Block{}, Tip{}, err
	}

	if tip.Height >= math.MaxInt64 {
		return core.Block{}, Tip{}, fmt.Errorf("chain height is exhausted")
	}
	height := tip.Height + 1
	// Reserve a one-output coinbase. A zero-reward coinbase is smaller, never larger.
	coinbaseReserve, err := core.NewCoinbase(address, height, 1)
	if err != nil {
		return core.Block{}, Tip{}, err
	}
	encodedSize := 256 + core.EncodedTransactionSize(coinbaseReserve)
	view := newMiningUTXOView(tx, height)
	selected := make([]core.Transaction, 0, min(len(pending), core.MaxBlockTransactions-1))
	fees := uint64(0)
	for _, transaction := range pending {
		if len(selected) >= core.MaxBlockTransactions-1 {
			break
		}
		transactionSize := core.EncodedTransactionSize(transaction)
		if encodedSize+transactionSize > core.MaxBlockBytes {
			continue
		}
		inputUTXO, err := view.inputs(ctx, transaction)
		if err != nil {
			continue
		}
		fee, err := core.ValidateRegularTransaction(transaction, inputUTXO)
		if err != nil {
			continue
		}
		if err := view.apply(transaction); err != nil {
			continue
		}
		fees, err = checkedAdd(fees, fee)
		if err != nil {
			return core.Block{}, Tip{}, err
		}
		selected = append(selected, transaction)
		encodedSize += transactionSize
	}
	reward, err := checkedAdd(core.Subsidy(height), fees)
	if err != nil {
		return core.Block{}, Tip{}, err
	}
	coinbase, err := core.NewCoinbase(address, height, reward)
	if err != nil {
		return core.Block{}, Tip{}, err
	}
	transactions := make([]core.Transaction, 0, len(selected)+1)
	transactions = append(transactions, coinbase)
	transactions = append(transactions, selected...)
	timestamp := core.NextTimestampAt(headers, now)
	block := core.Block{
		Version:      core.BlockVersion(height),
		Height:       height,
		Timestamp:    timestamp,
		PreviousHash: tip.Hash,
		MerkleRoot:   core.MerkleRoot(transactions),
		Difficulty:   core.ExpectedDifficultyAt(headers, height, timestamp),
		Transactions: transactions,
	}
	block.Hash = block.ComputeHash()
	return block, tip, nil
}

func (l *Ledger) CommitMinedBlock(ctx context.Context, block core.Block, expectedTip Tip) error {
	return l.commitMinedBlockAtTime(ctx, block, expectedTip, time.Now().Unix())
}

func (l *Ledger) commitMinedBlockAtTime(ctx context.Context, block core.Block, expectedTip Tip, validationTime int64) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	tx, err := l.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin mined block commit: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	currentTip, err := tipFromQuery(ctx, tx)
	if err != nil {
		return err
	}
	if currentTip.Height != expectedTip.Height || currentTip.Hash != expectedTip.Hash {
		return fmt.Errorf("%w: expected %d/%s, current %d/%s", ErrStaleTip,
			expectedTip.Height, expectedTip.Hash, currentTip.Height, currentTip.Hash)
	}
	if err := connectBlockAtTime(ctx, tx, block, validationTime); err != nil {
		return err
	}
	if err := rebuildMempool(ctx, tx, nil); err != nil {
		return fmt.Errorf("revalidate mempool after mined block %d: %w", block.Height, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mined block %d: %w", block.Height, err)
	}
	return nil
}

func mempoolTransactionsFromQuery(ctx context.Context, query sqlQueryer) ([]core.Transaction, error) {
	rows, err := query.QueryContext(ctx, `
		SELECT tx_id, data, fee, encoded_size, sequence
		FROM mempool ORDER BY sequence
	`)
	if err != nil {
		return nil, fmt.Errorf("query mining mempool: %w", err)
	}
	defer rows.Close()
	items := make([]miningMempoolItem, 0)
	for rows.Next() {
		var txID string
		var data []byte
		var fee, encodedSize, sequence int64
		if err := rows.Scan(&txID, &data, &fee, &encodedSize, &sequence); err != nil {
			return nil, fmt.Errorf("scan mining transaction: %w", err)
		}
		if fee < 0 || encodedSize <= 0 || encodedSize > core.MaxTransactionBytes || sequence <= 0 {
			return nil, fmt.Errorf("stored mining transaction %s contains invalid policy metadata", txID)
		}
		var transaction core.Transaction
		if err := decodeJSON(data, &transaction); err != nil {
			return nil, fmt.Errorf("decode mining transaction: %w", err)
		}
		if transaction.ID != txID || core.EncodedTransactionSize(transaction) != int(encodedSize) {
			return nil, fmt.Errorf("stored mining transaction %s metadata does not match its body", txID)
		}
		items = append(items, miningMempoolItem{
			transaction: transaction,
			fee:         uint64(fee),
			encodedSize: uint64(encodedSize),
			sequence:    sequence,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mining mempool: %w", err)
	}
	return prioritizeMempoolForMining(items)
}

type miningMempoolItem struct {
	transaction core.Transaction
	fee         uint64
	encodedSize uint64
	sequence    int64
}

type miningPriorityQueue struct {
	items   []miningMempoolItem
	indexes []int
}

func (q miningPriorityQueue) Len() int { return len(q.indexes) }

func (q miningPriorityQueue) Less(left, right int) bool {
	return higherMiningPriority(q.items[q.indexes[left]], q.items[q.indexes[right]])
}

func (q miningPriorityQueue) Swap(left, right int) {
	q.indexes[left], q.indexes[right] = q.indexes[right], q.indexes[left]
}

func (q *miningPriorityQueue) Push(value any) {
	q.indexes = append(q.indexes, value.(int))
}

func (q *miningPriorityQueue) Pop() any {
	last := len(q.indexes) - 1
	value := q.indexes[last]
	q.indexes = q.indexes[:last]
	return value
}

func higherMiningPriority(left, right miningMempoolItem) bool {
	leftHigh, leftLow := bits.Mul64(left.fee, right.encodedSize)
	rightHigh, rightLow := bits.Mul64(right.fee, left.encodedSize)
	if leftHigh != rightHigh {
		return leftHigh > rightHigh
	}
	if leftLow != rightLow {
		return leftLow > rightLow
	}
	if left.fee != right.fee {
		return left.fee > right.fee
	}
	return left.sequence < right.sequence
}

func prioritizeMempoolForMining(items []miningMempoolItem) ([]core.Transaction, error) {
	byID := make(map[string]int, len(items))
	for index, item := range items {
		if item.transaction.ID == "" || item.encodedSize == 0 {
			return nil, fmt.Errorf("mining mempool contains invalid transaction metadata")
		}
		if _, exists := byID[item.transaction.ID]; exists {
			return nil, fmt.Errorf("mining mempool contains duplicate transaction %s", item.transaction.ID)
		}
		byID[item.transaction.ID] = index
	}

	indegree := make([]int, len(items))
	children := make([][]int, len(items))
	for childIndex, item := range items {
		for _, input := range item.transaction.Inputs {
			parentIndex, pendingParent := byID[input.TxID]
			if !pendingParent {
				continue
			}
			indegree[childIndex]++
			children[parentIndex] = append(children[parentIndex], childIndex)
		}
	}

	queue := &miningPriorityQueue{items: items, indexes: make([]int, 0, len(items))}
	heap.Init(queue)
	for index, dependencies := range indegree {
		if dependencies == 0 {
			heap.Push(queue, index)
		}
	}
	transactions := make([]core.Transaction, 0, len(items))
	for queue.Len() > 0 {
		index := heap.Pop(queue).(int)
		transactions = append(transactions, items[index].transaction)
		for _, childIndex := range children[index] {
			indegree[childIndex]--
			if indegree[childIndex] == 0 {
				heap.Push(queue, childIndex)
			}
		}
	}
	if len(transactions) != len(items) {
		return nil, fmt.Errorf("mining mempool contains a transaction dependency cycle")
	}
	return transactions, nil
}

type miningUTXOView struct {
	query          sqlQueryer
	spendingHeight uint64
	created        core.UTXO
	spent          map[core.Outpoint]struct{}
}

func newMiningUTXOView(query sqlQueryer, spendingHeight uint64) *miningUTXOView {
	return &miningUTXOView{
		query:          query,
		spendingHeight: spendingHeight,
		created:        make(core.UTXO),
		spent:          make(map[core.Outpoint]struct{}),
	}
}

func (v *miningUTXOView) inputs(ctx context.Context, transaction core.Transaction) (core.UTXO, error) {
	utxo := make(core.UTXO, len(transaction.Inputs))
	for _, input := range transaction.Inputs {
		outpoint := core.Outpoint{TxID: input.TxID, Index: input.OutputIndex}
		if _, spent := v.spent[outpoint]; spent {
			return nil, fmt.Errorf("input %s:%d is already selected for spending", input.TxID, input.OutputIndex)
		}
		if output, exists := v.created[outpoint]; exists {
			utxo[outpoint] = output
			continue
		}
		record, err := loadUTXO(ctx, v.query, outpoint)
		if err != nil {
			return nil, err
		}
		if record.Coinbase && !core.IsCoinbaseMature(record.CreatedHeight, v.spendingHeight) {
			return nil, fmt.Errorf("coinbase input %s:%d is immature", input.TxID, input.OutputIndex)
		}
		utxo[outpoint] = record.Output()
	}
	return utxo, nil
}

func (v *miningUTXOView) apply(transaction core.Transaction) error {
	for _, input := range transaction.Inputs {
		outpoint := core.Outpoint{TxID: input.TxID, Index: input.OutputIndex}
		if _, spent := v.spent[outpoint]; spent {
			return errors.New("transaction input was selected twice")
		}
		delete(v.created, outpoint)
		v.spent[outpoint] = struct{}{}
	}
	for index, output := range transaction.Outputs {
		outpoint := core.Outpoint{TxID: transaction.ID, Index: uint32(index)}
		if _, exists := v.created[outpoint]; exists {
			return fmt.Errorf("duplicate selected output %s:%d", transaction.ID, index)
		}
		v.created[outpoint] = output
	}
	return nil
}
