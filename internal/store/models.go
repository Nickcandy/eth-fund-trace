package store

import "time"

type Address struct {
	Chain                string    `bson:"chain"`
	ChainID              int64     `bson:"chainId"`
	Address              string    `bson:"address"`
	IsContract           bool      `bson:"isContract"`
	IsTerminal           bool      `bson:"isTerminal"`
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
	Chain      string    `bson:"chain"`
	ChainID    int64     `bson:"chainId"`
	Address    string    `bson:"address"`
	Status     string    `bson:"status"`
	StartedAt  time.Time `bson:"startedAt"`
	FinishedAt time.Time `bson:"finishedAt,omitempty"`
	DurationMS int64     `bson:"durationMs,omitempty"`
	Fetched    int64     `bson:"fetched"`
	Error      string    `bson:"error,omitempty"`
}
