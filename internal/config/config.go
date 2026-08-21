package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr                   string
	MongoURI                   string
	MongoDatabase              string
	EtherscanAPIKey            string
	EtherscanBaseURL           string
	EtherscanPageSize          int
	EtherscanMaxPages          int
	EtherscanRequestIntervalMS int
}

func Load() Config {
	return Config{
		HTTPAddr:                   value("HTTP_ADDR", ":8080"),
		MongoURI:                   value("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase:              value("MONGO_DATABASE", "eth_fund_trace"),
		EtherscanAPIKey:            value("ETHERSCAN_API_KEY", ""),
		EtherscanBaseURL:           value("ETHERSCAN_BASE_URL", "https://api.etherscan.io/v2/api"),
		EtherscanPageSize:          intValue("ETHERSCAN_PAGE_SIZE", 100),
		EtherscanMaxPages:          intValue("ETHERSCAN_MAX_PAGES", 100),
		EtherscanRequestIntervalMS: intValue("ETHERSCAN_REQUEST_INTERVAL_MS", 250),
	}
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
