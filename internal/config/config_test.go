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
	t.Setenv("ETHERSCAN_REQUEST_INTERVAL_MS", "")

	got := Load()
	if got.HTTPAddr != ":8080" || got.MongoURI != "mongodb://localhost:27017" || got.MongoDatabase != "eth_fund_trace" || got.EtherscanBaseURL != "https://api.etherscan.io/v2/api" || got.EtherscanPageSize != 100 || got.EtherscanMaxPages != 100 || got.EtherscanRequestIntervalMS != 250 {
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
	t.Setenv("ETHERSCAN_REQUEST_INTERVAL_MS", "10")

	got := Load()
	if got.HTTPAddr != ":9090" || got.MongoURI != "mongodb://mongo:27017" || got.MongoDatabase != "test_db" || got.EtherscanAPIKey != "secret" || got.EtherscanBaseURL != "http://localhost:9999/api" || got.EtherscanPageSize != 25 || got.EtherscanMaxPages != 4 || got.EtherscanRequestIntervalMS != 10 {
		t.Fatalf("unexpected environment config: %+v", got)
	}
}
