package core

const (
	ChainName                               = "Entropy"
	ProductName                             = "Entcoin"
	ChainSymbol                             = "ENT"
	NetworkID                               = "entropy-mainnet-v1"
	StateVersion                            = 1
	LegacyBlockVersion               uint32 = 1
	UpgradedBlockVersion             uint32 = 2
	ConsensusUpgradeHeight           uint64 = 125_555
	ASERTAnchorHeight                uint64 = 123_265
	ASERTAnchorTimestamp             int64  = 1_785_201_853
	ASERTAnchorDifficulty            uint8  = 35
	ASERTHalfLifeSeconds             int64  = 600
	ASERTAnchorHash                         = "000000000b6ffc20400cafe308ae13a73cead4c5a7cb232214d714ff2949dead"
	InitialDifficulty                       = 22
	AdjustmentBlocks                        = 60
	FirstAdjustment                         = 120
	MinimumDifficulty                       = 4
	MaximumDifficulty                       = 255
	MedianTimeBlocks                        = 11
	MaxFutureSeconds                        = 120
	MaxBlockBytes                           = 1 << 20
	MaxTransactionBytes                     = 64 << 10
	MaxBlockTransactions                    = 2_000
	MaxTransactionInputs                    = 256
	MaxTransactionOutputs                   = 256
	MaxPendingTransactions                  = 5_000
	CoinbaseMaturity                 uint64 = 100
	CoinbaseMaturityActivationHeight uint64 = 1
)

type TxInput struct {
	TxID        string `json:"tx_id"`
	OutputIndex uint32 `json:"output_index"`
	PublicKey   []byte `json:"public_key"`
	Signature   []byte `json:"signature"`
}

type TxOutput struct {
	Amount  uint64 `json:"amount"`
	Address string `json:"address"`
}

type Transaction struct {
	ID       string     `json:"id"`
	Coinbase bool       `json:"coinbase"`
	Nonce    uint64     `json:"nonce"`
	Inputs   []TxInput  `json:"inputs"`
	Outputs  []TxOutput `json:"outputs"`
}

type Block struct {
	Version      uint32        `json:"version"`
	Height       uint64        `json:"height"`
	Timestamp    int64         `json:"timestamp"`
	PreviousHash string        `json:"previous_hash"`
	MerkleRoot   string        `json:"merkle_root"`
	Difficulty   uint8         `json:"difficulty"`
	Nonce        uint64        `json:"nonce"`
	Hash         string        `json:"hash"`
	Transactions []Transaction `json:"transactions"`
}

type State struct {
	Version int           `json:"version"`
	Name    string        `json:"name"`
	Symbol  string        `json:"symbol"`
	Blocks  []Block       `json:"blocks"`
	Pending []Transaction `json:"pending"`
}

type Outpoint struct {
	TxID  string
	Index uint32
}

type UTXO map[Outpoint]TxOutput

type DifficultyAlgorithm uint8

const (
	LegacyEpochDifficulty DifficultyAlgorithm = iota + 1
	IntegerASERTDifficulty
)

type ConsensusRules struct {
	Version                   uint32
	Difficulty                DifficultyAlgorithm
	RequireMonotonicTimestamp bool
}

func RulesAtHeight(height uint64) ConsensusRules {
	if height >= ConsensusUpgradeHeight {
		return ConsensusRules{
			Version:                   UpgradedBlockVersion,
			Difficulty:                IntegerASERTDifficulty,
			RequireMonotonicTimestamp: true,
		}
	}
	return ConsensusRules{Version: LegacyBlockVersion, Difficulty: LegacyEpochDifficulty}
}
