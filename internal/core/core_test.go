package core

import (
	"context"
	"crypto/elliptic"
	"encoding/asn1"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestEmissionIsExactlyTwoMillionENT(t *testing.T) {
	tests := []struct {
		height uint64
		want   uint64
	}{
		{0, 0},
		{1, BaseSubsidy + 1},
		{BonusSubsidyBlocks, BaseSubsidy + 1},
		{BonusSubsidyBlocks + 1, BaseSubsidy},
		{EmissionBlocks, BaseSubsidy},
		{EmissionBlocks + 1, 0},
	}
	for _, test := range tests {
		if got := Subsidy(test.height); got != test.want {
			t.Fatalf("Subsidy(%d) = %d, want %d", test.height, got, test.want)
		}
	}
	if got := MintedThrough(EmissionBlocks); got != MaxSupply {
		t.Fatalf("total emission = %d, want %d", got, MaxSupply)
	}
	if got := MintedThrough(EmissionBlocks + 10_000); got != MaxSupply {
		t.Fatalf("emission exceeded cap: %d", got)
	}
}

func TestMainnetIdentityAndGenesis(t *testing.T) {
	if NetworkID != "entropy-mainnet-v1" {
		t.Fatalf("network ID = %q", NetworkID)
	}
	genesis := GenesisBlock()
	if genesis.Timestamp != 1783983600 || genesis.Height != 0 || genesis.Difficulty != 0 || len(genesis.Transactions) != 0 {
		t.Fatalf("unexpected mainnet genesis: %#v", genesis)
	}
	const expectedHash = "f58101a2332dbffff670b4b2f8d08deea08883e0719df9b008b7eb1c8d5b2f0e"
	if genesis.Hash != expectedHash {
		t.Fatalf("mainnet genesis hash = %s, want %s", genesis.Hash, expectedHash)
	}
	previousTestnet := genesis
	previousTestnet.Timestamp = 1783900800
	previousTestnet.Hash = previousTestnet.ComputeHash()
	if previousTestnet.Hash == genesis.Hash {
		t.Fatal("mainnet reused the published testnet genesis")
	}
	var legacyBlock encoder
	legacyBlock.uint32(genesis.Version)
	legacyBlock.uint64(genesis.Height)
	legacyBlock.int64(genesis.Timestamp)
	legacyBlock.string(genesis.PreviousHash)
	legacyBlock.string(genesis.MerkleRoot)
	legacyBlock.uint8(genesis.Difficulty)
	legacyBlock.uint64(genesis.Nonce)
	if genesis.Hash == hashHex(legacyBlock.Bytes()) {
		t.Fatal("block hash is not separated by the mainnet network ID")
	}
	domainTransaction := Transaction{Coinbase: true, Nonce: 42}
	var legacyTransaction encoder
	legacyTransaction.bool(domainTransaction.Coinbase)
	legacyTransaction.uint64(domainTransaction.Nonce)
	legacyTransaction.uint64(0)
	legacyTransaction.uint64(0)
	if domainTransaction.ComputeID() == hashHex(legacyTransaction.Bytes()) {
		t.Fatal("transaction ID is not separated by the mainnet network ID")
	}
}

func TestCoinbaseMaturityFromFirstRewardBlock(t *testing.T) {
	tests := []struct {
		name           string
		createdHeight  uint64
		spendingHeight uint64
		wantMature     bool
	}{
		{name: "same block", createdHeight: 1, spendingHeight: 1, wantMature: false},
		{name: "ninety nine blocks", createdHeight: 1, spendingHeight: 100, wantMature: false},
		{name: "exactly one hundred blocks", createdHeight: 1, spendingHeight: 101, wantMature: true},
		{name: "height regression", createdHeight: 101, spendingHeight: 99, wantMature: false},
		{name: "overflow safe", createdHeight: math.MaxUint64 - 50, spendingHeight: math.MaxUint64, wantMature: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsCoinbaseMature(test.createdHeight, test.spendingHeight); got != test.wantMature {
				t.Fatalf("IsCoinbaseMature(%d, %d) = %v, want %v", test.createdHeight, test.spendingHeight, got, test.wantMature)
			}
		})
	}

	wallet, err := NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	outpoint := Outpoint{TxID: strings.Repeat("a", 64), Index: 0}
	utxo := UTXO{outpoint: {Amount: 10_000, Address: wallet.Address}}
	transaction, err := BuildTransaction(wallet, recipient.Address, 5_000, 100, utxo)
	if err != nil {
		t.Fatal(err)
	}
	origins := outputOrigins{outpoint: {CreatedHeight: 1, Coinbase: true}}
	if err := validateCoinbaseMaturity(transaction, origins, 2); err == nil {
		t.Fatal("mainnet accepted an immature coinbase near genesis")
	}
	if err := validateCoinbaseMaturity(transaction, origins, 101); err != nil {
		t.Fatalf("mature coinbase transaction was rejected: %v", err)
	}
	if _, exists := matureUTXO(utxo, origins, 100)[outpoint]; exists {
		t.Fatal("immature coinbase appeared in spendable UTXO")
	}
	if _, exists := matureUTXO(utxo, origins, 101)[outpoint]; !exists {
		t.Fatal("mature coinbase was absent from spendable UTXO")
	}
}

func TestValidateConfirmedIgnoresOnlyPendingPolicy(t *testing.T) {
	state := NewState()
	state.Pending = []Transaction{{ID: strings.Repeat("f", 64)}}
	if err := state.ValidateConfirmed(); err != nil {
		t.Fatalf("confirmed chain rejected because of pending policy: %v", err)
	}
	if err := state.Validate(); err == nil {
		t.Fatal("full state validation accepted an invalid pending transaction")
	}
}

func TestMineBlockWithWorkers(t *testing.T) {
	block := GenesisBlock()
	mined, err := MineBlockWithWorkers(context.Background(), block, 4)
	if err != nil {
		t.Fatalf("parallel mine: %v", err)
	}
	if mined.Hash != mined.ComputeHash() || !mined.HasValidWork() {
		t.Fatalf("parallel miner returned invalid block: %#v", mined)
	}
	if _, err := MineBlockWithWorkers(context.Background(), block, 0); err == nil {
		t.Fatal("zero mining workers were accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	time.AfterFunc(10*time.Millisecond, cancel)
	block.Difficulty = MaximumDifficulty
	if _, err := MineBlockWithWorkers(canceled, block, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled mining error = %v, want context.Canceled", err)
	}
}

func TestAmountRoundTrip(t *testing.T) {
	values := []string{"0", "0.00000001", "12.5", "2000000"}
	for _, value := range values {
		amount, err := ParseAmount(value)
		if err != nil {
			t.Fatalf("ParseAmount(%q): %v", value, err)
		}
		roundTrip, err := ParseAmount(FormatAmount(amount))
		if err != nil || roundTrip != amount {
			t.Fatalf("amount round trip for %q: got %d, err %v", value, roundTrip, err)
		}
	}
	for _, value := range []string{"", "-1", ".1", "1.000000001", "1e2"} {
		if _, err := ParseAmount(value); err == nil {
			t.Fatalf("ParseAmount(%q) unexpectedly succeeded", value)
		}
	}
}

func TestMineTransferAndRejectTampering(t *testing.T) {
	alice, err := NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	bob, err := NewWallet()
	if err != nil {
		t.Fatal(err)
	}

	amount := uint64(2 * UnitsPerENT / 100)
	fee := uint64(UnitsPerENT / 1000)
	outpoint := Outpoint{TxID: strings.Repeat("c", 64), Index: 0}
	utxo := UTXO{outpoint: {Amount: Subsidy(1), Address: alice.Address}}
	tx, err := BuildTransaction(alice, bob.Address, amount, fee, utxo)
	if err != nil {
		t.Fatalf("build transaction: %v", err)
	}
	highS := tx
	highS.Inputs = append([]TxInput(nil), tx.Inputs...)
	var signature ecdsaSignature
	if _, err := asn1.Unmarshal(highS.Inputs[0].Signature, &signature); err != nil {
		t.Fatal(err)
	}
	signature.S.Sub(elliptic.P256().Params().N, signature.S)
	highS.Inputs[0].Signature, err = asn1.Marshal(signature)
	if err != nil {
		t.Fatal(err)
	}
	highS.ID = highS.ComputeID()
	if _, err := validateRegularTransaction(highS, utxo); err == nil {
		t.Fatal("high-S transaction signature was accepted")
	}
	if gotFee, err := validateRegularTransaction(tx, utxo); err != nil || gotFee != fee {
		t.Fatalf("valid signed transaction = fee %d, err %v", gotFee, err)
	}

	tampered := tx
	tampered.Outputs = append([]TxOutput(nil), tx.Outputs...)
	tampered.Outputs[0].Amount++
	if _, err := validateRegularTransaction(tampered, utxo); err == nil {
		t.Fatal("tampered transaction was accepted")
	}

	state := NewState()
	if _, err := state.Mine(context.Background(), alice.Address); err != nil {
		t.Fatalf("mine first mainnet block: %v", err)
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("validate mined mainnet block: %v", err)
	}
}

func TestAddressChecksum(t *testing.T) {
	wallet, err := NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	if err := wallet.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := wallet.Address[:len(wallet.Address)-1] + "0"
	if bad == wallet.Address {
		bad = wallet.Address[:len(wallet.Address)-1] + "1"
	}
	if err := ValidateAddress(bad); err == nil {
		t.Fatal("address with bad checksum was accepted")
	}
}

func TestMixedCaseAddressOutputRemainsSpendable(t *testing.T) {
	owner, err := NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	mixedCase := recipient.Address[:4] + strings.ToUpper(recipient.Address[4:])
	if err := ValidateAddress(mixedCase); err != nil {
		t.Fatalf("mixed-case hex address was not decoded: %v", err)
	}
	if !AddressesEqual(mixedCase, recipient.Address) {
		t.Fatal("mixed-case address was not equivalent to its canonical form")
	}
	outpoint := Outpoint{TxID: strings.Repeat("d", 64), Index: 0}
	utxo := UTXO{outpoint: {Amount: Subsidy(1), Address: owner.Address}}
	amount := Subsidy(1) / 2
	receive, err := BuildTransaction(owner, mixedCase, amount, 0, utxo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateRegularTransaction(receive, utxo); err != nil {
		t.Fatal(err)
	}
	origins := outputOrigins{outpoint: {CreatedHeight: 1}}
	if err := applyRegularTransaction(receive, utxo, origins, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildTransaction(recipient, owner.Address, amount, 0, utxo); err != nil {
		t.Fatalf("mixed-case address output could not be spent: %v", err)
	}
}

func TestDifficultyAdjustmentUsesMedianTime(t *testing.T) {
	steady := blocksWithSpacing(10)
	if got := expectedDifficulty(steady, FirstAdjustment); got != InitialDifficulty {
		t.Fatalf("steady difficulty = %d, want %d", got, InitialDifficulty)
	}
	fast := blocksWithSpacing(2)
	if got := expectedDifficulty(fast, FirstAdjustment); got != InitialDifficulty+2 {
		t.Fatalf("fast difficulty = %d, want %d", got, InitialDifficulty+2)
	}
	slow := blocksWithSpacing(50)
	if got := expectedDifficulty(slow, FirstAdjustment); got != InitialDifficulty-2 {
		t.Fatalf("slow difficulty = %d, want %d", got, InitialDifficulty-2)
	}
	steady[len(steady)-1].Timestamp += 10_000
	if got := expectedDifficulty(steady, FirstAdjustment); got != InitialDifficulty {
		t.Fatalf("one timestamp outlier changed difficulty to %d", got)
	}
}

func TestDifficultyAdjustmentThresholdBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		span       int64
		difficulty uint8
	}{
		{name: "quarter target", span: 150, difficulty: InitialDifficulty + 2},
		{name: "above quarter target", span: 151, difficulty: InitialDifficulty + 1},
		{name: "half target", span: 300, difficulty: InitialDifficulty},
		{name: "twice target", span: 1_200, difficulty: InitialDifficulty},
		{name: "above twice target", span: 1_201, difficulty: InitialDifficulty - 1},
		{name: "four times target", span: 2_400, difficulty: InitialDifficulty - 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			blocks := blocksWithMedianSpan(test.span)
			if got := expectedDifficulty(blocks, FirstAdjustment); got != test.difficulty {
				t.Fatalf("difficulty for %d-second median span = %d, want %d", test.span, got, test.difficulty)
			}
		})
	}
}

func TestConsensusUpgradeAnchorVector(t *testing.T) {
	anchor := Block{
		Version:      LegacyBlockVersion,
		Height:       ASERTAnchorHeight,
		Timestamp:    ASERTAnchorTimestamp,
		PreviousHash: "0000000019ca169403b1163c95fc4da0fc6cf7bc7747173d8b18e83356d4746a",
		MerkleRoot:   "b1a301231205d499d72b91a27959a28cb3fdd3d4a2aa7a17229a9889f07807c5",
		Difficulty:   ASERTAnchorDifficulty,
		Nonce:        24_601_635_115_426_568,
		Hash:         ASERTAnchorHash,
	}
	if got := anchor.ComputeHash(); got != ASERTAnchorHash {
		t.Fatalf("anchor hash = %s, want %s", got, ASERTAnchorHash)
	}
	if !anchor.HasValidWork() {
		t.Fatal("anchor proof of work is invalid")
	}
}

func TestConsensusUpgradeVersionBoundary(t *testing.T) {
	if got := BlockVersion(ConsensusUpgradeHeight - 1); got != LegacyBlockVersion {
		t.Fatalf("pre-activation block version = %d, want %d", got, LegacyBlockVersion)
	}
	if got := BlockVersion(ConsensusUpgradeHeight); got != UpgradedBlockVersion {
		t.Fatalf("activation block version = %d, want %d", got, UpgradedBlockVersion)
	}
}

func TestASERTDifficultyVectors(t *testing.T) {
	ideal := ASERTAnchorTimestamp + int64(ConsensusUpgradeHeight-ASERTAnchorHeight)*TargetBlockSeconds
	tests := []struct {
		name       string
		timestamp  int64
		difficulty uint8
	}{
		{name: "on schedule", timestamp: ideal, difficulty: ASERTAnchorDifficulty},
		{name: "just below early half life", timestamp: ideal - ASERTHalfLifeSeconds/2 + 1, difficulty: ASERTAnchorDifficulty},
		{name: "early half life tie", timestamp: ideal - ASERTHalfLifeSeconds/2, difficulty: ASERTAnchorDifficulty + 1},
		{name: "one half life early", timestamp: ideal - ASERTHalfLifeSeconds, difficulty: ASERTAnchorDifficulty + 1},
		{name: "just below late half life", timestamp: ideal + ASERTHalfLifeSeconds/2 - 1, difficulty: ASERTAnchorDifficulty},
		{name: "late half life tie", timestamp: ideal + ASERTHalfLifeSeconds/2, difficulty: ASERTAnchorDifficulty - 1},
		{name: "one half life late", timestamp: ideal + ASERTHalfLifeSeconds, difficulty: ASERTAnchorDifficulty - 1},
		{name: "minimum clamp", timestamp: math.MaxInt64, difficulty: MinimumDifficulty},
		{name: "maximum clamp", timestamp: math.MinInt64, difficulty: MaximumDifficulty},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := expectedDifficultyAt(nil, ConsensusUpgradeHeight, test.timestamp); got != test.difficulty {
				t.Fatalf("difficulty at timestamp %d = %d, want %d", test.timestamp, got, test.difficulty)
			}
		})
	}
}

func TestASERTPublishedVectors(t *testing.T) {
	tests := []struct {
		height     uint64
		timestamp  int64
		difficulty uint8
	}{
		{height: 160_000, timestamp: 1_785_569_203, difficulty: 35},
		{height: 160_000, timestamp: 1_785_569_083, difficulty: 35},
		{height: 160_000, timestamp: 1_785_569_323, difficulty: 35},
		{height: 160_000, timestamp: 1_785_568_903, difficulty: 36},
		{height: 160_000, timestamp: 1_785_569_503, difficulty: 34},
		{height: 160_000, timestamp: 1_785_574_003, difficulty: 27},
		{height: 160_060, timestamp: 1_785_569_203, difficulty: 36},
	}
	for _, test := range tests {
		if got := expectedDifficultyAt(nil, test.height, test.timestamp); got != test.difficulty {
			t.Fatalf("published vector (%d, %d) = %d, want %d", test.height, test.timestamp, got, test.difficulty)
		}
	}
}

func TestASERTRecoversWithoutCompletingAnEpoch(t *testing.T) {
	height := ConsensusUpgradeHeight
	ideal := ASERTAnchorTimestamp + int64(height-ASERTAnchorHeight)*TargetBlockSeconds
	if got := expectedDifficultyAt(nil, height, ideal); got != ASERTAnchorDifficulty {
		t.Fatalf("on-schedule difficulty = %d, want %d", got, ASERTAnchorDifficulty)
	}
	recoveryDelay := int64(ASERTAnchorDifficulty-27) * ASERTHalfLifeSeconds
	if got := expectedDifficultyAt(nil, height, ideal+recoveryDelay); got != 27 {
		t.Fatalf("difficulty after %d-second stall = %d, want 27", recoveryDelay, got)
	}
}

func TestASERTHeightTimeAndTimestampMonotonicity(t *testing.T) {
	for height := ConsensusUpgradeHeight; height < ConsensusUpgradeHeight+10_000; height += 137 {
		ideal := ASERTAnchorTimestamp + int64(height-ASERTAnchorHeight)*TargetBlockSeconds
		if got := expectedDifficultyAt(nil, height, ideal); got != ASERTAnchorDifficulty {
			t.Fatalf("on-schedule difficulty at height %d = %d, want %d", height, got, ASERTAnchorDifficulty)
		}
		if got := expectedDifficultyAt(nil, height+60, ideal); got != ASERTAnchorDifficulty+1 {
			t.Fatalf("60-block early difficulty at height %d = %d, want %d", height+60, got, ASERTAnchorDifficulty+1)
		}
		previous := uint8(MaximumDifficulty)
		for offset := int64(-10_000); offset <= 10_000; offset += 97 {
			got := expectedDifficultyAt(nil, height, ideal+offset)
			if got > previous {
				t.Fatalf("difficulty increased as timestamp advanced at height %d: %d after %d", height, got, previous)
			}
			previous = got
		}
	}
}

func TestASERTRapidHashRateArrivalIsBounded(t *testing.T) {
	ideal := ASERTAnchorTimestamp + int64(ConsensusUpgradeHeight-ASERTAnchorHeight)*TargetBlockSeconds
	height := ConsensusUpgradeHeight
	timestamp := ideal
	for range 600 {
		timestamp++
		height++
	}
	if got := expectedDifficultyAt(nil, height, timestamp); got < ASERTAnchorDifficulty+8 {
		t.Fatalf("difficulty after 600 minimum-time blocks = %d, want at least %d", got, ASERTAnchorDifficulty+8)
	}
	if elapsed := timestamp - ideal; elapsed > ASERTHalfLifeSeconds*2 {
		t.Fatalf("rapid-hash simulation consumed %d seconds", elapsed)
	}
}

func TestConsensusUpgradeTimestampRules(t *testing.T) {
	ideal := ASERTAnchorTimestamp + int64(ConsensusUpgradeHeight-ASERTAnchorHeight)*TargetBlockSeconds
	previous := Block{Height: ConsensusUpgradeHeight - 1, Hash: strings.Repeat("1", 64), Timestamp: ideal - 1}
	prior := make([]Block, MedianTimeBlocks)
	for index := range prior {
		prior[index] = Block{Height: previous.Height - uint64(len(prior)-1-index), Timestamp: ideal - 20 + int64(index)}
	}
	prior[len(prior)-1] = previous
	block := Block{
		Version:      UpgradedBlockVersion,
		Height:       ConsensusUpgradeHeight,
		Timestamp:    ideal,
		PreviousHash: previous.Hash,
		MerkleRoot:   MerkleRoot(nil),
		Difficulty:   ASERTAnchorDifficulty,
	}
	if err := validateHeaderConsensusFields(block, previous, prior, ideal); err != nil {
		t.Fatalf("valid activation fields were rejected: %v", err)
	}

	block.Timestamp = previous.Timestamp
	block.Difficulty = expectedDifficultyAt(prior, block.Height, block.Timestamp)
	if err := validateHeaderConsensusFields(block, previous, prior, ideal); err == nil || !strings.Contains(err.Error(), "previous block time") {
		t.Fatalf("non-increasing activation timestamp error = %v", err)
	}

	block.Timestamp = ideal
	block.Difficulty = ASERTAnchorDifficulty
	block.Version = LegacyBlockVersion
	if err := validateHeaderConsensusFields(block, previous, prior, ideal); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("legacy activation version error = %v", err)
	}

	block.Height = ConsensusUpgradeHeight - 1
	block.Version = UpgradedBlockVersion
	if err := validateHeaderConsensusFields(block, previous, prior, ideal); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("upgraded pre-activation version error = %v", err)
	}

	block.Height = ConsensusUpgradeHeight
	block.Version = UpgradedBlockVersion + 1
	if err := validateHeaderConsensusFields(block, previous, prior, ideal); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("unknown activation version error = %v", err)
	}
}

func TestConsensusFutureTimestampBoundDoesNotOverflow(t *testing.T) {
	previous := Block{
		Version:    UpgradedBlockVersion,
		Height:     ConsensusUpgradeHeight,
		Timestamp:  math.MaxInt64 - 1,
		Hash:       strings.Repeat("1", 64),
		Difficulty: MinimumDifficulty,
	}
	block := Block{
		Version:      UpgradedBlockVersion,
		Height:       previous.Height + 1,
		Timestamp:    math.MaxInt64,
		PreviousHash: previous.Hash,
		MerkleRoot:   MerkleRoot(nil),
		Difficulty:   MinimumDifficulty,
	}
	if err := validateHeaderConsensusFields(block, previous, []Block{previous}, math.MaxInt64); err != nil {
		t.Fatalf("maximum timestamp at maximum validation time was rejected: %v", err)
	}
	if err := validateHeaderConsensusFields(block, previous, []Block{previous}, math.MaxInt64-MaxFutureSeconds-1); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("timestamp beyond saturated future bound error = %v", err)
	}
}

func TestConsensusResourceAndTimestampLimits(t *testing.T) {
	wallet, err := NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	oversized := Transaction{
		Inputs:  make([]TxInput, MaxTransactionInputs+1),
		Outputs: []TxOutput{{Amount: 1, Address: wallet.Address}},
	}
	if _, err := validateRegularTransaction(oversized, UTXO{}); err == nil {
		t.Fatal("transaction input limit was not enforced")
	}

	coinbase, err := NewCoinbase(wallet.Address, 1, Subsidy(1))
	if err != nil {
		t.Fatal(err)
	}
	coinbase.Outputs = append(coinbase.Outputs, TxOutput{Amount: 1, Address: wallet.Address})
	coinbase.ID = coinbase.ComputeID()
	if err := validateCoinbase(coinbase, Subsidy(1)+1); err == nil {
		t.Fatal("coinbase output limit was not enforced")
	}

	genesis := GenesisBlock()
	badTime := Block{
		Version:      StateVersion,
		Height:       1,
		Timestamp:    math.MaxInt64,
		PreviousHash: genesis.Hash,
		Difficulty:   InitialDifficulty,
	}
	if err := validateBlockHeader(badTime, genesis, []Block{genesis}); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("future timestamp error = %v", err)
	}
	badTime.Timestamp = genesis.Timestamp
	if err := validateBlockHeader(badTime, genesis, []Block{genesis}); err == nil || !strings.Contains(err.Error(), "median") {
		t.Fatalf("MTP timestamp error = %v", err)
	}
	badTime.Timestamp = time.Now().Unix() + MaxFutureSeconds + 1
	if err := validateBlockHeader(badTime, genesis, []Block{genesis}); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("future drift error = %v", err)
	}

	if got := clampDifficulty(1_000); got != MaximumDifficulty {
		t.Fatalf("maximum difficulty clamp = %d", got)
	}
}

func TestHeaderOnlyValidationDoesNotRequireBlockBody(t *testing.T) {
	wallet, err := NewWallet()
	if err != nil {
		t.Fatal(err)
	}
	state := NewState()
	block, err := state.Mine(context.Background(), wallet.Address)
	if err != nil {
		t.Fatal(err)
	}
	header := block
	header.Transactions = nil
	genesis := GenesisBlock()
	if err := ValidateHeader(header, genesis, []Block{genesis}); err != nil {
		t.Fatalf("valid header was rejected without its body: %v", err)
	}
	if err := ValidateBlockHeader(header, genesis, []Block{genesis}); err == nil {
		t.Fatal("full block validation accepted a missing body")
	}
	if err := ValidateHeader(header, genesis, nil); err == nil {
		t.Fatal("header validation accepted an empty prior window")
	}
}

func blocksWithSpacing(seconds int64) []Block {
	blocks := make([]Block, FirstAdjustment)
	for index := range blocks {
		blocks[index] = Block{
			Height:     uint64(index),
			Timestamp:  genesisTimestamp + int64(index)*seconds,
			Difficulty: InitialDifficulty,
		}
	}
	blocks[0].Difficulty = 0
	return blocks
}

func blocksWithMedianSpan(span int64) []Block {
	blocks := make([]Block, FirstAdjustment)
	for index := range blocks {
		blocks[index] = Block{Height: uint64(index), Difficulty: InitialDifficulty}
	}
	blocks[0].Difficulty = 0
	for index := len(blocks) - MedianTimeBlocks; index < len(blocks); index++ {
		blocks[index].Timestamp = span
	}
	return blocks
}
