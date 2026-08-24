package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr                        string
	HTTPAPIKey                      string
	HTTPAuthDisabled                bool
	HTTPTimeoutSeconds              int
	HTTPBodyLimit                   string
	HTTPRequestsPerSecond           int
	HTTPBurst                       int
	WebDistDir                      string
	MongoURI                        string
	MongoDatabase                   string
	EtherscanAPIKey                 string
	EtherscanBaseURL                string
	EtherscanPageSize               int
	EtherscanMaxPages               int
	EtherscanRequestsPerSecond      int
	EtherscanBurst                  int
	EtherscanMaxRetries             int
	EtherscanRetryBaseMS            int
	EtherscanHTTPTimeoutSeconds     int
	EtherscanInternalLookbackBlocks int64
	SyncCacheTTLMinutes             int
	SyncConfirmations               int
	SyncQueueSize                   int
	EthereumSyncStartBlock          int64
	BaseSyncStartBlock              int64
	EthereumRPCURL                  string
	BaseRPCURL                      string
	EthereumBridgeConfirmations     int
	BaseBridgeConfirmations         int
	BridgeSyncIntervalSeconds       int
	BridgeSyncBatchSize             int
	BridgeSyncMaxRetries            int
	BridgeSyncMaxConcurrency        int
}

func Load() Config {
	return Config{
		HTTPAddr:                        value("HTTP_ADDR", ":8080"),
		HTTPAPIKey:                      os.Getenv("HTTP_API_KEY"),
		HTTPAuthDisabled:                boolValue("HTTP_AUTH_DISABLED", false),
		HTTPTimeoutSeconds:              intValue("HTTP_TIMEOUT_SECONDS", 30),
		HTTPBodyLimit:                   value("HTTP_BODY_LIMIT", "1M"),
		HTTPRequestsPerSecond:           intValue("HTTP_REQUESTS_PER_SECOND", 20),
		HTTPBurst:                       intValue("HTTP_BURST", 10),
		WebDistDir:                      value("WEB_DIST_DIR", "web/dist"),
		MongoURI:                        value("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase:                   value("MONGO_DATABASE", "eth_fund_trace"),
		EtherscanAPIKey:                 value("ETHERSCAN_API_KEY", ""),
		EtherscanBaseURL:                value("ETHERSCAN_BASE_URL", "https://api.etherscan.io/v2/api"),
		EtherscanPageSize:               intValue("ETHERSCAN_PAGE_SIZE", 1000),
		EtherscanMaxPages:               intValue("ETHERSCAN_MAX_PAGES", 10),
		EtherscanRequestsPerSecond:      intValue("ETHERSCAN_REQUESTS_PER_SECOND", 3),
		EtherscanBurst:                  intValue("ETHERSCAN_BURST", 1),
		EtherscanMaxRetries:             nonNegativeIntValue("ETHERSCAN_MAX_RETRIES", 5),
		EtherscanRetryBaseMS:            intValue("ETHERSCAN_RETRY_BASE_MS", 1000),
		EtherscanHTTPTimeoutSeconds:     intValue("ETHERSCAN_HTTP_TIMEOUT_SECONDS", 120),
		EtherscanInternalLookbackBlocks: nonNegativeInt64Value("ETHERSCAN_INTERNAL_LOOKBACK_BLOCKS", 100_000),
		SyncCacheTTLMinutes:             intValue("SYNC_CACHE_TTL_MINUTES", 15),
		SyncConfirmations:               nonNegativeIntValue("SYNC_CONFIRMATIONS", 12),
		SyncQueueSize:                   intValue("SYNC_QUEUE_SIZE", 100),
		EthereumSyncStartBlock:          int64Value("ETHEREUM_SYNC_START_BLOCK", 21525891),
		BaseSyncStartBlock:              int64Value("BASE_SYNC_START_BLOCK", 24450127),
		EthereumRPCURL:                  os.Getenv("ETHEREUM_RPC_URL"),
		BaseRPCURL:                      os.Getenv("BASE_RPC_URL"),
		EthereumBridgeConfirmations:     nonNegativeIntValue("ETHEREUM_BRIDGE_CONFIRMATIONS", 12),
		BaseBridgeConfirmations:         nonNegativeIntValue("BASE_BRIDGE_CONFIRMATIONS", 20),
		BridgeSyncIntervalSeconds:       intValue("BRIDGE_SYNC_INTERVAL_SECONDS", 60),
		BridgeSyncBatchSize:             intValue("BRIDGE_SYNC_BATCH_SIZE", 50),
		BridgeSyncMaxRetries:            intValue("BRIDGE_SYNC_MAX_RETRIES", 8),
		BridgeSyncMaxConcurrency:        intValue("BRIDGE_SYNC_MAX_CONCURRENCY", 2),
	}
}

func boolValue(key string, fallback bool) bool {
	parsed, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return parsed
}

func nonNegativeIntValue(key string, fallback int) int {
	parsed, err := strconv.Atoi(os.Getenv(key))
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func value(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func intValue(key string, fallback int) int {
	parsed, err := strconv.Atoi(os.Getenv(key))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func int64Value(key string, fallback int64) int64 {
	parsed, err := strconv.ParseInt(os.Getenv(key), 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func nonNegativeInt64Value(key string, fallback int64) int64 {
	parsed, err := strconv.ParseInt(os.Getenv(key), 10, 64)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
