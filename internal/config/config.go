package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr                     string
	HTTPAPIKey                   string
	HTTPAuthDisabled             bool
	HTTPTimeoutSeconds           int
	HTTPBodyLimit                string
	HTTPRequestsPerSecond        int
	HTTPBurst                    int
	WebDistDir                   string
	MongoURI                     string
	MongoDatabase                string
	EtherscanAPIKey              string
	EtherscanBaseURL             string
	EtherscanPageSize            int
	EtherscanMaxPages            int
	EtherscanRequestsPerSecond   int
	EtherscanBurst               int
	EtherscanMaxRetries          int
	EtherscanRetryBaseMS         int
	EtherscanHTTPTimeoutSeconds  int
	SyncCacheTTLMinutes          int
	SyncConfirmations            int
	SyncQueueSize                int
	SyncMaxRecordsPerAction      int64
	TraceExistingDataOnly        bool
	EthereumSyncStartBlock       int64
	EthereumSyncEndBlock         int64
	EthereumRPCURL               string
	THORChainStatusURL           string
	THORChainClientID            string
	BitcoinAPIURL                string
	CrossChainHTTPTimeoutSeconds int
}

func Load() Config {
	return Config{
		HTTPAddr:                     value("HTTP_ADDR", ":8080"),
		HTTPAPIKey:                   os.Getenv("HTTP_API_KEY"),
		HTTPAuthDisabled:             boolValue("HTTP_AUTH_DISABLED", false),
		HTTPTimeoutSeconds:           intValue("HTTP_TIMEOUT_SECONDS", 30),
		HTTPBodyLimit:                value("HTTP_BODY_LIMIT", "1M"),
		HTTPRequestsPerSecond:        intValue("HTTP_REQUESTS_PER_SECOND", 20),
		HTTPBurst:                    intValue("HTTP_BURST", 10),
		WebDistDir:                   value("WEB_DIST_DIR", "web/dist"),
		MongoURI:                     value("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase:                value("MONGO_DATABASE", "eth_fund_trace"),
		EtherscanAPIKey:              value("ETHERSCAN_API_KEY", ""),
		EtherscanBaseURL:             value("ETHERSCAN_BASE_URL", "https://api.etherscan.io/v2/api"),
		EtherscanPageSize:            intValue("ETHERSCAN_PAGE_SIZE", 1000),
		EtherscanMaxPages:            intValue("ETHERSCAN_MAX_PAGES", 10),
		EtherscanRequestsPerSecond:   intValue("ETHERSCAN_REQUESTS_PER_SECOND", 3),
		EtherscanBurst:               intValue("ETHERSCAN_BURST", 1),
		EtherscanMaxRetries:          nonNegativeIntValue("ETHERSCAN_MAX_RETRIES", 5),
		EtherscanRetryBaseMS:         intValue("ETHERSCAN_RETRY_BASE_MS", 1000),
		EtherscanHTTPTimeoutSeconds:  intValue("ETHERSCAN_HTTP_TIMEOUT_SECONDS", 120),
		SyncCacheTTLMinutes:          nonNegativeIntValue("SYNC_CACHE_TTL_MINUTES", 15),
		SyncConfirmations:            nonNegativeIntValue("SYNC_CONFIRMATIONS", 12),
		SyncQueueSize:                intValue("SYNC_QUEUE_SIZE", 100),
		SyncMaxRecordsPerAction:      nonNegativeInt64Value("SYNC_MAX_RECORDS_PER_ACTION", 50_000),
		TraceExistingDataOnly:        boolValue("TRACE_EXISTING_DATA_ONLY", false),
		EthereumSyncStartBlock:       int64Value("ETHEREUM_SYNC_START_BLOCK", 21525891),
		EthereumSyncEndBlock:         nonNegativeInt64Value("ETHEREUM_SYNC_END_BLOCK", 0),
		EthereumRPCURL:               os.Getenv("ETHEREUM_RPC_URL"),
		THORChainStatusURL:           value("THORCHAIN_STATUS_URL", "https://gateway.liquify.com/chain/thorchain_api"),
		THORChainClientID:            value("THORCHAIN_CLIENT_ID", "eth-fund-trace"),
		BitcoinAPIURL:                value("BITCOIN_API_URL", "https://mempool.space"),
		CrossChainHTTPTimeoutSeconds: intValue("CROSS_CHAIN_HTTP_TIMEOUT_SECONDS", 15),
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
