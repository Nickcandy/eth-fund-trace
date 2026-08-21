package store

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Address struct {
	Chain                string    `bson:"chain"`
	ChainID              int64     `bson:"chainId"`
	Address              string    `bson:"address"`
	IsContract           bool      `bson:"isContract"`
	IsTerminal           bool      `bson:"isTerminal"`
	EarliestSyncedBlock  int64     `bson:"earliestSyncedBlock"`
	HistorySyncedToBlock int64     `bson:"historySyncedToBlock"`
	LatestSyncedBlock    int64     `bson:"latestSyncedBlock"`
	LastSyncedAt         time.Time `bson:"lastSyncedAt,omitempty"`
	SyncStatus           string    `bson:"syncStatus"`
	SyncError            string    `bson:"syncError,omitempty"`
}

type Transfer struct {
	Chain       string    `bson:"chain"`
	ChainID     int64     `bson:"chainId"`
	TxHash      string    `bson:"txHash"`
	BlockNumber int64     `bson:"blockNumber"`
	BlockTime   time.Time `bson:"blockTime,omitempty"`
	From        string    `bson:"from"`
	To          string    `bson:"to"`
	AssetType   string    `bson:"assetType"`
	Asset       string    `bson:"asset"`
	Symbol      string    `bson:"symbol,omitempty"`
	Decimals    int32     `bson:"decimals,omitempty"`
	Amount      string    `bson:"amount,omitempty"`
	TokenValue  string    `bson:"tokenValue,omitempty"`
	Source      string    `bson:"source"`
	TraceID     string    `bson:"traceId,omitempty"`
	LogIndex    int64     `bson:"logIndex,omitempty"`
	ObservedAt  time.Time `bson:"observedAt"`
}

type Label struct {
	Chain      string    `bson:"chain"`
	ChainID    int64     `bson:"chainId"`
	Address    string    `bson:"address"`
	Type       string    `bson:"type"`
	Source     string    `bson:"source"`
	Note       string    `bson:"note,omitempty"`
	RiskLevel  string    `bson:"riskLevel,omitempty"`
	ObservedAt time.Time `bson:"observedAt"`
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
