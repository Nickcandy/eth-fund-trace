package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("MONGO_URI", "")
	t.Setenv("MONGO_DATABASE", "")
	t.Setenv("ETHERSCAN_API_KEY", "")
	t.Setenv("ETHERSCAN_BASE_URL", "")
	t.Setenv("ETHERSCAN_PAGE_SIZE", "")
	t.Setenv("ETHERSCAN_MAX_PAGES", "")
	t.Setenv("ETHERSCAN_REQUESTS_PER_SECOND", "")
	t.Setenv("ETHERSCAN_BURST", "")
	t.Setenv("ETHERSCAN_MAX_RETRIES", "")
	t.Setenv("ETHERSCAN_RETRY_BASE_MS", "")
	t.Setenv("SYNC_CACHE_TTL_MINUTES", "")
	t.Setenv("SYNC_CONFIRMATIONS", "")
	t.Setenv("SYNC_QUEUE_SIZE", "")
	t.Setenv("HTTP_API_KEY", "")
	t.Setenv("HTTP_TIMEOUT_SECONDS", "")
	t.Setenv("HTTP_BODY_LIMIT", "")
	t.Setenv("HTTP_REQUESTS_PER_SECOND", "")
	t.Setenv("HTTP_BURST", "")

	got := Load()
	if got.HTTPAddr != ":8080" || got.HTTPTimeoutSeconds != 30 || got.HTTPBodyLimit != "1M" || got.HTTPRequestsPerSecond != 20 || got.HTTPBurst != 10 || got.MongoURI != "mongodb://localhost:27017" || got.MongoDatabase != "eth_fund_trace" || got.EtherscanBaseURL != "https://api.etherscan.io/v2/api" || got.EtherscanPageSize != 100 || got.EtherscanMaxPages != 100 || got.EtherscanRequestsPerSecond != 5 || got.EtherscanBurst != 1 || got.EtherscanMaxRetries != 3 || got.SyncCacheTTLMinutes != 15 || got.SyncConfirmations != 12 || got.SyncQueueSize != 100 {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestLoadEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("MONGO_URI", "mongodb://mongo:27017")
	t.Setenv("MONGO_DATABASE", "test_db")
	t.Setenv("ETHERSCAN_API_KEY", "secret")
	t.Setenv("ETHERSCAN_BASE_URL", "http://localhost:9999/api")
	t.Setenv("ETHERSCAN_PAGE_SIZE", "25")
	t.Setenv("ETHERSCAN_MAX_PAGES", "4")
	t.Setenv("ETHERSCAN_REQUESTS_PER_SECOND", "7")
	t.Setenv("ETHERSCAN_BURST", "2")
	t.Setenv("ETHERSCAN_MAX_RETRIES", "4")
	t.Setenv("ETHERSCAN_RETRY_BASE_MS", "25")
	t.Setenv("SYNC_CACHE_TTL_MINUTES", "30")
	t.Setenv("SYNC_CONFIRMATIONS", "20")
	t.Setenv("SYNC_QUEUE_SIZE", "50")
	t.Setenv("HTTP_API_KEY", "api-secret")
	t.Setenv("HTTP_TIMEOUT_SECONDS", "12")
	t.Setenv("HTTP_BODY_LIMIT", "2M")
	t.Setenv("HTTP_REQUESTS_PER_SECOND", "8")
	t.Setenv("HTTP_BURST", "3")

	got := Load()
	if got.HTTPAddr != ":9090" || got.HTTPAPIKey != "api-secret" || got.HTTPTimeoutSeconds != 12 || got.HTTPBodyLimit != "2M" || got.HTTPRequestsPerSecond != 8 || got.HTTPBurst != 3 || got.MongoURI != "mongodb://mongo:27017" || got.MongoDatabase != "test_db" || got.EtherscanAPIKey != "secret" || got.EtherscanBaseURL != "http://localhost:9999/api" || got.EtherscanPageSize != 25 || got.EtherscanMaxPages != 4 || got.EtherscanRequestsPerSecond != 7 || got.EtherscanBurst != 2 || got.EtherscanMaxRetries != 4 || got.EtherscanRetryBaseMS != 25 || got.SyncCacheTTLMinutes != 30 || got.SyncConfirmations != 20 || got.SyncQueueSize != 50 {
		t.Fatalf("unexpected environment config: %+v", got)
	}
}
