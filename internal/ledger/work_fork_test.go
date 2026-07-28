package ledger

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/HONG-LOU/entcoin/internal/core"
)

func TestShorterHigherWorkForkWins(t *testing.T) {
	ctx := context.Background()
	chain, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()

	// The synthetic shared prefix is a trusted test checkpoint. Both competing
	// suffixes below use normal difficulty, PoW, block, undo, and reorg paths.
	insertLowDifficultyCheckpoint(t, chain, core.FirstAdjustment-1)
	prefix, err := chain.HeaderWindow(ctx, core.FirstAdjustment-1)
	if err != nil {
		t.Fatal(err)
	}
	wallet := newTestWallet(t)

	oldBlocks := buildWorkFork(t, prefix, 64, 50, wallet.Address)
	for _, block := range oldBlocks {
		if err := chain.ConnectBlock(ctx, block); err != nil {
			t.Fatalf("connect longer low-work block %d: %v", block.Height, err)
		}
	}
	oldTip, err := chain.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}

	candidateBlocks := buildWorkFork(t, prefix, 62, 1, wallet.Address)
	if len(candidateBlocks) >= len(oldBlocks) {
		t.Fatalf("candidate length %d is not shorter than old length %d", len(candidateBlocks), len(oldBlocks))
	}
	if oldBlocks[len(oldBlocks)-1].Difficulty != core.MinimumDifficulty {
		t.Fatalf("old tip difficulty = %d, want %d", oldBlocks[len(oldBlocks)-1].Difficulty, core.MinimumDifficulty)
	}
	if candidateBlocks[len(candidateBlocks)-1].Difficulty != core.MinimumDifficulty+2 {
		t.Fatalf("candidate tip difficulty = %d, want %d", candidateBlocks[len(candidateBlocks)-1].Difficulty, core.MinimumDifficulty+2)
	}

	ancestorWork, err := chain.WorkAt(ctx, core.FirstAdjustment-1)
	if err != nil {
		t.Fatal(err)
	}
	candidateWork := new(big.Int).Set(ancestorWork)
	for _, block := range candidateBlocks {
		candidateWork.Add(candidateWork, blockWork(block.Difficulty))
	}
	if candidateWork.Cmp(oldTip.Work) <= 0 {
		t.Fatalf("shorter candidate work %s does not exceed old work %s", candidateWork, oldTip.Work)
	}

	if err := chain.ReplaceFrom(ctx, core.FirstAdjustment-1, candidateBlocks); err != nil {
		t.Fatalf("replace longer chain with shorter higher-work fork: %v", err)
	}
	newTip, err := chain.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantTip := candidateBlocks[len(candidateBlocks)-1]
	if newTip.Height != wantTip.Height || newTip.Hash != wantTip.Hash || newTip.Work.Cmp(candidateWork) != 0 {
		t.Fatalf("higher-work tip = %#v, want height %d hash %s work %s", newTip, wantTip.Height, wantTip.Hash, candidateWork)
	}
	if newTip.Height >= oldTip.Height {
		t.Fatalf("winning tip height %d is not shorter than old tip height %d", newTip.Height, oldTip.Height)
	}
	if err := chain.quickCheck(ctx); err != nil {
		t.Fatalf("database integrity after higher-work reorg: %v", err)
	}
}

func insertLowDifficultyCheckpoint(t *testing.T, chain *Ledger, through uint64) {
	t.Helper()
	ctx := context.Background()
	previous := core.GenesisBlock()
	unitWork := blockWork(core.MinimumDifficulty)
	baseTimestamp := time.Now().Unix() - 6_000
	for height := uint64(1); height <= through; height++ {
		hash := fmt.Sprintf("%064x", 1_000_000+height)
		timestamp := baseTimestamp + int64(height)*core.TargetBlockSeconds
		work := new(big.Int).Mul(new(big.Int).SetUint64(height), unitWork)
		if _, err := chain.database.ExecContext(ctx, `
			INSERT INTO blocks(
				height, hash, previous_hash, version, timestamp, merkle_root,
				difficulty, nonce, cumulative_work, data, encoded_size
			) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 0)
		`, int64(height), hash, previous.Hash, int64(core.StateVersion), timestamp,
			core.MerkleRoot(nil), int64(core.MinimumDifficulty), encodeUint64(0), encodeWork(work)); err != nil {
			t.Fatalf("insert shared checkpoint header %d: %v", height, err)
		}
		previous = core.Block{Height: height, Hash: hash, Timestamp: timestamp, Difficulty: core.MinimumDifficulty}
	}
}

func buildWorkFork(t *testing.T, prefix []core.Block, count int, spacing int64, address string) []core.Block {
	t.Helper()
	ctx := context.Background()
	window := append([]core.Block(nil), prefix...)
	previous := window[len(window)-1]
	blocks := make([]core.Block, 0, count)
	for range count {
		height := previous.Height + 1
		coinbase, err := core.NewCoinbase(address, height, core.Subsidy(height))
		if err != nil {
			t.Fatal(err)
		}
		transactions := []core.Transaction{coinbase}
		block := core.Block{
			Version:      core.StateVersion,
			Height:       height,
			Timestamp:    previous.Timestamp + spacing,
			PreviousHash: previous.Hash,
			MerkleRoot:   core.MerkleRoot(transactions),
			Difficulty:   core.ExpectedDifficultyAt(window, height, previous.Timestamp+spacing),
			Transactions: transactions,
		}
		mined, err := core.MineBlockWithWorkers(ctx, block, 1)
		if err != nil {
			t.Fatalf("mine fork block %d: %v", height, err)
		}
		if err := core.ValidateBlockHeader(mined, previous, window); err != nil {
			t.Fatalf("validate fork block %d: %v", height, err)
		}
		blocks = append(blocks, mined)
		window = append(window, mined)
		if len(window) > core.FirstAdjustment {
			window = window[len(window)-core.FirstAdjustment:]
		}
		previous = mined
	}
	return blocks
}
