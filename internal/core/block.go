package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// genesisTimestamp is 2026-07-13 23:00:00 UTC. Mainnet uses a new,
// reward-free genesis so no previously published testnet history can replay.
const genesisTimestamp int64 = 1783983600

const MaxMiningWorkers = 1_024

var zeroHash = strings.Repeat("0", 64)

func NewState() *State {
	return &State{
		Version: StateVersion,
		Name:    ChainName,
		Symbol:  ChainSymbol,
		Blocks:  []Block{GenesisBlock()},
		Pending: []Transaction{},
	}
}

func GenesisBlock() Block {
	block := Block{
		Version:      LegacyBlockVersion,
		Height:       0,
		Timestamp:    genesisTimestamp,
		PreviousHash: zeroHash,
		MerkleRoot:   merkleRoot(nil),
		Difficulty:   0,
		Transactions: []Transaction{},
	}
	block.Hash = block.ComputeHash()
	return block
}

func (b Block) ComputeHash() string {
	var e encoder
	e.string(NetworkID)
	e.uint32(b.Version)
	e.uint64(b.Height)
	e.int64(b.Timestamp)
	e.string(b.PreviousHash)
	e.string(b.MerkleRoot)
	e.uint8(b.Difficulty)
	e.uint64(b.Nonce)
	return hashHex(e.Bytes())
}

func (b Block) HasValidWork() bool {
	decoded, err := decodeHash(b.Hash)
	if err != nil {
		return false
	}
	return leadingZeroBits(decoded) >= int(b.Difficulty)
}

func mineBlock(ctx context.Context, block Block) (Block, error) {
	return MineBlockWithWorkers(ctx, block, runtime.GOMAXPROCS(0))
}

func MineBlockWithWorkers(ctx context.Context, block Block, workers int) (Block, error) {
	if workers <= 0 {
		return Block{}, fmt.Errorf("mining worker count must be positive")
	}
	if workers > MaxMiningWorkers {
		return Block{}, fmt.Errorf("mining worker count exceeds %d", MaxMiningWorkers)
	}
	if err := ctx.Err(); err != nil {
		return Block{}, err
	}
	workContext, cancel := context.WithCancel(ctx)
	defer cancel()
	found := make(chan Block, 1)
	done := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(workers)
	step := uint64(workers)
	for worker := 0; worker < workers; worker++ {
		start := uint64(worker)
		go func() {
			defer wait.Done()
			candidate := block
			nonce := start
			iterations := uint64(0)
			for {
				if iterations&4_095 == 0 {
					select {
					case <-workContext.Done():
						return
					default:
					}
				}
				candidate.Nonce = nonce
				candidate.Hash = candidate.ComputeHash()
				if candidate.HasValidWork() {
					select {
					case found <- candidate:
						cancel()
					case <-workContext.Done():
					}
					return
				}
				if nonce > math.MaxUint64-step {
					return
				}
				nonce += step
				iterations++
			}
		}()
	}
	go func() {
		wait.Wait()
		close(done)
	}()

	select {
	case mined := <-found:
		cancel()
		<-done
		return mined, nil
	case <-ctx.Done():
		cancel()
		<-done
		return Block{}, ctx.Err()
	case <-done:
		select {
		case mined := <-found:
			return mined, nil
		default:
		}
		if err := ctx.Err(); err != nil {
			return Block{}, err
		}
		return Block{}, fmt.Errorf("nonce space exhausted")
	}
}

func expectedDifficulty(blocks []Block, nextHeight uint64) uint8 {
	if nextHeight == 1 {
		return InitialDifficulty
	}
	previous := blocks[len(blocks)-1].Difficulty
	if nextHeight < FirstAdjustment || nextHeight%AdjustmentBlocks != 0 {
		return previous
	}

	lastTime := medianTimeAt(blocks, len(blocks)-1)
	firstTime := medianTimeAt(blocks, len(blocks)-1-AdjustmentBlocks)
	actualSeconds := lastTime - firstTime
	targetSeconds := int64(AdjustmentBlocks) * TargetBlockSeconds
	if actualSeconds <= targetSeconds/4 {
		return clampDifficulty(int(previous) + 2)
	}
	if actualSeconds < targetSeconds/2 {
		return clampDifficulty(int(previous) + 1)
	}
	if actualSeconds >= targetSeconds*4 {
		return clampDifficulty(int(previous) - 2)
	}
	if actualSeconds > targetSeconds*2 {
		return clampDifficulty(int(previous) - 1)
	}
	return previous
}

func expectedDifficultyAt(blocks []Block, nextHeight uint64, nextTimestamp int64) uint8 {
	if RulesAtHeight(nextHeight).Difficulty == LegacyEpochDifficulty {
		return expectedDifficulty(blocks, nextHeight)
	}
	return asertDifficulty(nextHeight, nextTimestamp)
}

func asertDifficulty(nextHeight uint64, nextTimestamp int64) uint8 {
	heightDelta := new(big.Int).SetUint64(nextHeight - ASERTAnchorHeight)
	idealElapsed := heightDelta.Mul(heightDelta, big.NewInt(TargetBlockSeconds))
	actualElapsed := new(big.Int).Sub(big.NewInt(nextTimestamp), big.NewInt(ASERTAnchorTimestamp))
	deviation := new(big.Int).Sub(idealElapsed, actualElapsed)
	correction := roundedQuotient(deviation, ASERTHalfLifeSeconds)

	maximum := int64(MaximumDifficulty - ASERTAnchorDifficulty)
	minimum := -int64(ASERTAnchorDifficulty - MinimumDifficulty)
	if correction.Cmp(big.NewInt(maximum)) > 0 {
		return MaximumDifficulty
	}
	if correction.Cmp(big.NewInt(minimum)) < 0 {
		return MinimumDifficulty
	}
	return uint8(int64(ASERTAnchorDifficulty) + correction.Int64())
}

func roundedQuotient(numerator *big.Int, denominator int64) *big.Int {
	adjusted := new(big.Int).Set(numerator)
	half := big.NewInt(denominator / 2)
	if adjusted.Sign() < 0 {
		adjusted.Sub(adjusted, half)
	} else {
		adjusted.Add(adjusted, half)
	}
	return adjusted.Quo(adjusted, big.NewInt(denominator))
}

func ExpectedDifficultyAt(blocks []Block, nextHeight uint64, nextTimestamp int64) uint8 {
	return expectedDifficultyAt(blocks, nextHeight, nextTimestamp)
}

func BlockVersion(height uint64) uint32 {
	return RulesAtHeight(height).Version
}

func clampDifficulty(difficulty int) uint8 {
	if difficulty < MinimumDifficulty {
		return MinimumDifficulty
	}
	if difficulty > MaximumDifficulty {
		return MaximumDifficulty
	}
	return uint8(difficulty)
}

func medianTimePast(blocks []Block) int64 {
	return medianTimeAt(blocks, len(blocks)-1)
}

func medianTimeAt(blocks []Block, end int) int64 {
	start := end - MedianTimeBlocks + 1
	if start < 0 {
		start = 0
	}
	timestamps := make([]int64, 0, end-start+1)
	for index := start; index <= end; index++ {
		timestamps = append(timestamps, blocks[index].Timestamp)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i] < timestamps[j] })
	return timestamps[len(timestamps)/2]
}

func merkleRoot(transactions []Transaction) string {
	if len(transactions) == 0 {
		empty := sha256.Sum256(nil)
		return hex.EncodeToString(empty[:])
	}
	level := make([][]byte, 0, len(transactions))
	for _, tx := range transactions {
		decoded, err := decodeHash(tx.ID)
		if err != nil {
			return ""
		}
		level = append(level, decoded)
	}
	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			right := level[i]
			if i+1 < len(level) {
				right = level[i+1]
			}
			combined := append(append([]byte(nil), level[i]...), right...)
			hash := sha256.Sum256(combined)
			next = append(next, hash[:])
		}
		level = next
	}
	return hex.EncodeToString(level[0])
}

func MerkleRoot(transactions []Transaction) string {
	return merkleRoot(transactions)
}

func leadingZeroBits(value []byte) int {
	count := 0
	for _, b := range value {
		if b == 0 {
			count += 8
			continue
		}
		for mask := byte(0x80); mask != 0 && b&mask == 0; mask >>= 1 {
			count++
		}
		break
	}
	return count
}

func nextTimestamp(blocks []Block) int64 {
	return nextTimestampAt(blocks, time.Now().Unix())
}

func nextTimestampAt(blocks []Block, now int64) int64 {
	minimum := medianTimePast(blocks) + 1
	nextHeight := blocks[len(blocks)-1].Height + 1
	if RulesAtHeight(nextHeight).RequireMonotonicTimestamp && blocks[len(blocks)-1].Timestamp >= minimum {
		minimum = blocks[len(blocks)-1].Timestamp + 1
	}
	if now < minimum {
		return minimum
	}
	return now
}

func MineBlock(ctx context.Context, block Block) (Block, error) {
	return MineBlockWithWorkers(ctx, block, runtime.GOMAXPROCS(0))
}

func NextTimestamp(blocks []Block) int64 {
	return nextTimestamp(blocks)
}

func NextTimestampAt(blocks []Block, now int64) int64 {
	return nextTimestampAt(blocks, now)
}
