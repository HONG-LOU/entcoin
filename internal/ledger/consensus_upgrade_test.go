package ledger

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/HONG-LOU/entcoin/internal/core"
)

func TestConsensusUpgradeConnectAndReorgPreserveLedgerState(t *testing.T) {
	ctx := context.Background()
	chain, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()

	activationTime := upgradeTestActivationTime()
	insertUpgradeTestPrefix(t, chain, activationTime-core.TargetBlockSeconds)
	prefix, err := chain.HeaderWindow(ctx, core.ConsensusUpgradeHeight-1)
	if err != nil {
		t.Fatal(err)
	}
	alice := newTestWallet(t)
	bob := newTestWallet(t)

	active := buildUpgradeTestFork(t, prefix, 1, activationTime, alice.Address)
	connectUpgradeTestBlock(t, chain, active[0], activationTime+core.MaxFutureSeconds)
	oldTip, err := chain.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if oldTip.Height != core.ConsensusUpgradeHeight || active[0].Version != core.UpgradedBlockVersion {
		t.Fatalf("active upgrade tip = %d/v%d", oldTip.Height, active[0].Version)
	}
	aliceBefore, _, err := chain.Balances(ctx, alice.Address)
	if err != nil {
		t.Fatal(err)
	}
	if aliceBefore != core.Subsidy(core.ConsensusUpgradeHeight) {
		t.Fatalf("pre-reorg Alice balance = %d", aliceBefore)
	}

	equalWork := buildUpgradeTestFork(t, prefix, 1, activationTime, bob.Address)
	if err := chain.replaceFromSourceAtTime(
		ctx,
		core.ConsensusUpgradeHeight-1,
		len(equalWork),
		func(index int) (core.Block, error) { return equalWork[index], nil },
		activationTime+core.MaxFutureSeconds,
	); !errors.Is(err, ErrInsufficientWork) {
		t.Fatalf("equal-work activation fork error = %v, want %v", err, ErrInsufficientWork)
	}
	afterEqualWork, err := chain.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterEqualWork.Hash != oldTip.Hash || afterEqualWork.Work.Cmp(oldTip.Work) != 0 {
		t.Fatalf("equal-work rejection changed tip from %+v to %+v", oldTip, afterEqualWork)
	}

	candidate := buildUpgradeTestFork(t, prefix, 2, activationTime, bob.Address)
	if err := chain.replaceFromSourceAtTime(
		ctx,
		core.ConsensusUpgradeHeight-1,
		len(candidate),
		func(index int) (core.Block, error) { return candidate[index], nil },
		activationTime+core.MaxFutureSeconds,
	); err != nil {
		t.Fatalf("replace across consensus activation: %v", err)
	}

	newTip, err := chain.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if newTip.Height != core.ConsensusUpgradeHeight+1 || newTip.Hash != candidate[1].Hash || newTip.Work.Cmp(oldTip.Work) <= 0 {
		t.Fatalf("post-upgrade reorg tip = %+v", newTip)
	}
	aliceAfter, _, err := chain.Balances(ctx, alice.Address)
	if err != nil {
		t.Fatal(err)
	}
	bobAfter, _, err := chain.Balances(ctx, bob.Address)
	if err != nil {
		t.Fatal(err)
	}
	wantBob := core.Subsidy(core.ConsensusUpgradeHeight) + core.Subsidy(core.ConsensusUpgradeHeight+1)
	if aliceAfter != 0 || bobAfter != wantBob {
		t.Fatalf("post-reorg balances Alice/Bob = %d/%d, want 0/%d", aliceAfter, bobAfter, wantBob)
	}
	if err := chain.quickCheck(ctx); err != nil {
		t.Fatalf("database integrity after activation reorg: %v", err)
	}
	for range len(candidate) {
		if _, err := chain.DisconnectTip(ctx); err != nil {
			t.Fatalf("disconnect upgraded tip across activation: %v", err)
		}
	}
	preActivationTip, err := chain.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if preActivationTip.Height != core.ConsensusUpgradeHeight-1 {
		t.Fatalf("tip after crossing activation backwards = %d", preActivationTip.Height)
	}
}

func TestConsensusUpgradeMiningCandidateAndCommit(t *testing.T) {
	ctx := context.Background()
	chain, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	activationTime := upgradeTestActivationTime()
	insertUpgradeTestPrefix(t, chain, activationTime-core.TargetBlockSeconds)
	wallet := newTestWallet(t)
	candidate, expectedTip, err := chain.buildMiningCandidateAtTime(ctx, wallet.Address, activationTime)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Height != core.ConsensusUpgradeHeight || candidate.Version != core.UpgradedBlockVersion ||
		candidate.Timestamp != activationTime || candidate.Difficulty != core.MinimumDifficulty {
		t.Fatalf("activation mining candidate = %+v", candidate)
	}
	mined, err := core.MineBlockWithWorkers(ctx, candidate, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.commitMinedBlockAtTime(ctx, mined, expectedTip, activationTime+core.MaxFutureSeconds); err != nil {
		t.Fatalf("commit activation mining candidate: %v", err)
	}
	tip, err := chain.Tip(ctx)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, _, err := chain.Balances(ctx, wallet.Address)
	if err != nil {
		t.Fatal(err)
	}
	if tip.Hash != mined.Hash || tip.Height != core.ConsensusUpgradeHeight || confirmed != core.Subsidy(core.ConsensusUpgradeHeight) {
		t.Fatalf("committed activation candidate tip/balance = %+v/%d", tip, confirmed)
	}
}

func upgradeTestActivationTime() int64 {
	ideal := core.ASERTAnchorTimestamp + int64(core.ConsensusUpgradeHeight-core.ASERTAnchorHeight)*core.TargetBlockSeconds
	return ideal + int64(core.ASERTAnchorDifficulty-core.MinimumDifficulty)*core.ASERTHalfLifeSeconds
}

func insertUpgradeTestPrefix(t *testing.T, chain *Ledger, tipTimestamp int64) {
	t.Helper()
	start := core.ConsensusUpgradeHeight - core.FirstAdjustment
	previousHash := fmt.Sprintf("%064x", start)
	baseWork := new(big.Int).Lsh(big.NewInt(1), 40)
	for height := start; height < core.ConsensusUpgradeHeight; height++ {
		hash := fmt.Sprintf("%064x", height+1)
		timestamp := tipTimestamp - int64(core.ConsensusUpgradeHeight-1-height)*core.TargetBlockSeconds
		if _, err := chain.database.Exec(`
			INSERT INTO blocks(
				height, hash, previous_hash, version, timestamp, merkle_root,
				difficulty, nonce, cumulative_work, data, encoded_size
			) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 0)
		`, int64(height), hash, previousHash, int64(core.LegacyBlockVersion), timestamp,
			core.MerkleRoot(nil), int64(core.ASERTAnchorDifficulty), encodeUint64(0), encodeWork(baseWork)); err != nil {
			t.Fatalf("insert activation prefix header %d: %v", height, err)
		}
		previousHash = hash
	}
}

func buildUpgradeTestFork(t *testing.T, prefix []core.Block, count int, firstTimestamp int64, address string) []core.Block {
	t.Helper()
	window := append([]core.Block(nil), prefix...)
	previous := window[len(window)-1]
	blocks := make([]core.Block, 0, count)
	for index := 0; index < count; index++ {
		height := previous.Height + 1
		timestamp := firstTimestamp + int64(index)*core.TargetBlockSeconds
		coinbase, err := core.NewCoinbase(address, height, core.Subsidy(height))
		if err != nil {
			t.Fatal(err)
		}
		transactions := []core.Transaction{coinbase}
		block := core.Block{
			Version:      core.BlockVersion(height),
			Height:       height,
			Timestamp:    timestamp,
			PreviousHash: previous.Hash,
			MerkleRoot:   core.MerkleRoot(transactions),
			Difficulty:   core.ExpectedDifficultyAt(window, height, timestamp),
			Transactions: transactions,
		}
		if block.Difficulty != core.MinimumDifficulty {
			t.Fatalf("upgrade test difficulty = %d, want %d", block.Difficulty, core.MinimumDifficulty)
		}
		mined, err := core.MineBlockWithWorkers(context.Background(), block, 1)
		if err != nil {
			t.Fatal(err)
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

func connectUpgradeTestBlock(t *testing.T, chain *Ledger, block core.Block, validationTime int64) {
	t.Helper()
	tx, err := chain.database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := connectBlockAtTime(context.Background(), tx, block, validationTime); err != nil {
		t.Fatalf("connect upgrade block %d: %v", block.Height, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
