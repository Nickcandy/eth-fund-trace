package store

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Address struct {
	Chain                string    `bson:"chain" json:"chain"`
	ChainID              int64     `bson:"chainId" json:"chainId"`
	Address              string    `bson:"address" json:"address"`
	IsContract           bool      `bson:"isContract" json:"isContract"`
	IsTerminal           bool      `bson:"isTerminal" json:"isTerminal"`
	EarliestSyncedBlock  int64     `bson:"earliestSyncedBlock" json:"earliestSyncedBlock"`
	HistorySyncedToBlock int64     `bson:"historySyncedToBlock" json:"historySyncedToBlock"`
	LatestSyncedBlock    int64     `bson:"latestSyncedBlock" json:"latestSyncedBlock"`
	LastSyncedAt         time.Time `bson:"lastSyncedAt,omitempty" json:"lastSyncedAt,omitempty"`
	SyncStatus           string    `bson:"syncStatus" json:"syncStatus"`
	SyncError            string    `bson:"syncError,omitempty" json:"syncError,omitempty"`
}

type Transfer struct {
	Chain                 string    `bson:"chain" json:"chain"`
	ChainID               int64     `bson:"chainId" json:"chainId"`
	TxHash                string    `bson:"txHash" json:"txHash"`
	BlockNumber           int64     `bson:"blockNumber" json:"blockNumber"`
	BlockTime             time.Time `bson:"blockTime,omitempty" json:"blockTime,omitempty"`
	From                  string    `bson:"from" json:"from"`
	To                    string    `bson:"to" json:"to"`
	AssetType             string    `bson:"assetType" json:"assetType"`
	Asset                 string    `bson:"asset" json:"asset"`
	Symbol                string    `bson:"symbol,omitempty" json:"symbol,omitempty"`
	Decimals              int32     `bson:"decimals,omitempty" json:"decimals,omitempty"`
	Amount                string    `bson:"amount,omitempty" json:"amount,omitempty"`
	TokenValue            string    `bson:"tokenValue,omitempty" json:"tokenValue,omitempty"`
	TokenName             string    `bson:"tokenName,omitempty" json:"tokenName,omitempty"`
	TransferKind          string    `bson:"transferKind" json:"transferKind"`
	TransactionGroup      string    `bson:"transactionGroup" json:"transactionGroup"`
	LogIndexSynthetic     bool      `bson:"logIndexSynthetic,omitempty" json:"logIndexSynthetic,omitempty"`
	TokenMetadataComplete bool      `bson:"tokenMetadataComplete,omitempty" json:"tokenMetadataComplete"`
	Source                string    `bson:"source" json:"source"`
	TraceID               string    `bson:"traceId" json:"traceId"`
	LogIndex              int64     `bson:"logIndex" json:"logIndex"`
	ObservedAt            time.Time `bson:"observedAt" json:"observedAt"`
}

type Label struct {
	Chain      string    `bson:"chain" json:"chain"`
	ChainID    int64     `bson:"chainId" json:"chainId"`
	Address    string    `bson:"address" json:"address"`
	Type       string    `bson:"type" json:"type"`
	Source     string    `bson:"source" json:"source"`
	Note       string    `bson:"note,omitempty" json:"note,omitempty"`
	RiskLevel  string    `bson:"riskLevel,omitempty" json:"riskLevel,omitempty"`
	Confidence float64   `bson:"confidence" json:"confidence"`
	Evidence   []string  `bson:"evidence,omitempty" json:"evidence,omitempty"`
	ObservedAt time.Time `bson:"observedAt" json:"observedAt"`
}

type TraceJob struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Chain            string             `bson:"chain" json:"chain"`
	SeedAddress      string             `bson:"seedAddress" json:"seedAddress"`
	Direction        string             `bson:"direction" json:"direction"`
	Depth            int                `bson:"depth" json:"depth"`
	TopN             int                `bson:"topN" json:"topN"`
	Asset            string             `bson:"asset" json:"asset"`
	Status           string             `bson:"status" json:"status"`
	CreatedAt        time.Time          `bson:"createdAt" json:"createdAt"`
	StartedAt        time.Time          `bson:"startedAt,omitempty" json:"startedAt,omitempty"`
	FinishedAt       time.Time          `bson:"finishedAt,omitempty" json:"finishedAt,omitempty"`
	CurrentDepth     int                `bson:"currentDepth" json:"currentDepth"`
	VisitedNodes     int                `bson:"visitedNodes" json:"visitedNodes"`
	EdgeCount        int                `bson:"edgeCount" json:"edgeCount"`
	SyncJobIDs       []string           `bson:"syncJobIds,omitempty" json:"syncJobIds,omitempty"`
	Result           any                `bson:"result,omitempty" json:"result,omitempty"`
	DataThroughBlock int64              `bson:"dataThroughBlock" json:"dataThroughBlock"`
	RuleVersion      string             `bson:"ruleVersion" json:"ruleVersion"`
	ErrorCode        string             `bson:"errorCode,omitempty" json:"errorCode,omitempty"`
	Error            string             `bson:"error,omitempty" json:"error,omitempty"`
	Retryable        bool               `bson:"retryable" json:"retryable"`
}

type SyncJob struct {
	ID                  primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Chain               string             `bson:"chain" json:"chain"`
	ChainID             int64              `bson:"chainId" json:"chainId"`
	Address             string             `bson:"address" json:"address"`
	StartBlock          int64              `bson:"startBlock" json:"startBlock"`
	NeighborLimit       int                `bson:"neighborLimit" json:"neighborLimit"`
	Status              string             `bson:"status" json:"status"`
	CreatedAt           time.Time          `bson:"createdAt" json:"createdAt"`
	StartedAt           time.Time          `bson:"startedAt,omitempty" json:"startedAt,omitempty"`
	FinishedAt          time.Time          `bson:"finishedAt,omitempty" json:"finishedAt,omitempty"`
	DurationMS          int64              `bson:"durationMs,omitempty" json:"durationMs,omitempty"`
	SafeHead            int64              `bson:"safeHead,omitempty" json:"safeHead,omitempty"`
	TotalAddresses      int                `bson:"totalAddresses" json:"totalAddresses"`
	CompletedAddresses  int                `bson:"completedAddresses" json:"completedAddresses"`
	CachedAddresses     int                `bson:"cachedAddresses" json:"cachedAddresses"`
	Fetched             int64              `bson:"fetched" json:"fetched"`
	ActionCounts        map[string]int64   `bson:"actionCounts,omitempty" json:"actionCounts,omitempty"`
	SuccessfulNeighbors []string           `bson:"successfulNeighbors,omitempty" json:"successfulNeighbors,omitempty"`
	FailedNeighbors     []SyncFailure      `bson:"failedNeighbors,omitempty" json:"failedNeighbors,omitempty"`
	ErrorCode           string             `bson:"errorCode,omitempty" json:"errorCode,omitempty"`
	Error               string             `bson:"error,omitempty" json:"error,omitempty"`
	Retryable           bool               `bson:"retryable" json:"retryable"`
}

type SyncFailure struct {
	Address   string `bson:"address" json:"address"`
	Code      string `bson:"code" json:"code"`
	Message   string `bson:"message" json:"message"`
	Retryable bool   `bson:"retryable" json:"retryable"`
}

type AddressProfile struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	Chain              string             `bson:"chain" json:"chain"`
	ChainID            int64              `bson:"chainId" json:"chainId"`
	Address            string             `bson:"address" json:"address"`
	RuleVersion        string             `bson:"ruleVersion" json:"ruleVersion"`
	DataThroughBlock   int64              `bson:"dataThroughBlock" json:"dataThroughBlock"`
	WindowStart        time.Time          `bson:"windowStart,omitempty" json:"windowStart,omitempty"`
	WindowEnd          time.Time          `bson:"windowEnd,omitempty" json:"windowEnd,omitempty"`
	Features           ProfileFeatures    `bson:"features" json:"features"`
	Score              int                `bson:"score" json:"score"`
	Classification     string             `bson:"classification" json:"classification"`
	SuspectedHotWallet bool               `bson:"suspectedHotWallet" json:"suspectedHotWallet"`
	Signals            []string           `bson:"signals,omitempty" json:"signals,omitempty"`
	ComputedAt         time.Time          `bson:"computedAt" json:"computedAt"`
}

type ProfileFeatures struct {
	LifetimeTransfers  int64 `bson:"lifetimeTransfers" json:"lifetimeTransfers"`
	LifetimeIncoming   int64 `bson:"lifetimeIncoming" json:"lifetimeIncoming"`
	LifetimeOutgoing   int64 `bson:"lifetimeOutgoing" json:"lifetimeOutgoing"`
	WindowTransfers    int64 `bson:"windowTransfers" json:"windowTransfers"`
	Incoming           int64 `bson:"incoming" json:"incoming"`
	Outgoing           int64 `bson:"outgoing" json:"outgoing"`
	UniqueCounterparts int   `bson:"uniqueCounterparties" json:"uniqueCounterparties"`
	UniqueSenders      int   `bson:"uniqueSenders" json:"uniqueSenders"`
	UniqueRecipients   int   `bson:"uniqueRecipients" json:"uniqueRecipients"`
	ActiveDays         int   `bson:"activeDays" json:"activeDays"`
	ETHTransfers       int64 `bson:"ethTransfers" json:"ethTransfers"`
	ERC20Transfers     int64 `bson:"erc20Transfers" json:"erc20Transfers"`
}

type AddressActivity struct {
	LatestTransferAt time.Time
	Features         ProfileFeatures
}

type TransferCursor struct {
	BlockNumber int64
	TxHash      string
	Source      string
	TraceID     string
	LogIndex    int64
	Asset       string
}

type TransferQuery struct {
	Chain     string
	Addresses []string
	Direction string
	AssetMode string
	Asset     string
	FromBlock int64
	ToBlock   int64
	Limit     int64
	After     *TransferCursor
}

type CrossChainLink struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	SourceChain    string             `bson:"sourceChain" json:"sourceChain"`
	SourceChainID  int64              `bson:"sourceChainId" json:"sourceChainId"`
	SourceTxHash   string             `bson:"sourceTxHash" json:"sourceTxHash"`
	SourceLogIndex int64              `bson:"sourceLogIndex" json:"sourceLogIndex"`
	SourceAddress  string             `bson:"sourceAddress" json:"sourceAddress"`
	TargetChain    string             `bson:"targetChain" json:"targetChain"`
	TargetChainID  int64              `bson:"targetChainId" json:"targetChainId"`
	TargetTxHash   string             `bson:"targetTxHash" json:"targetTxHash"`
	TargetLogIndex int64              `bson:"targetLogIndex" json:"targetLogIndex"`
	TargetAddress  string             `bson:"targetAddress" json:"targetAddress"`
	BridgeAddress  string             `bson:"bridgeAddress" json:"bridgeAddress"`
	Asset          string             `bson:"asset" json:"asset"`
	Amount         string             `bson:"amount" json:"amount"`
	Status         string             `bson:"status" json:"status"`
	Evidence       []string           `bson:"evidence" json:"evidence"`
	ObservedAt     time.Time          `bson:"observedAt" json:"observedAt"`
}
