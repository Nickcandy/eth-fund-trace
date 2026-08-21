package config

import "os"

type Config struct {
	HTTPAddr      string
	MongoURI      string
	MongoDatabase string
}

func Load() Config {
	return Config{
		HTTPAddr:      value("HTTP_ADDR", ":8080"),
		MongoURI:      value("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase: value("MONGO_DATABASE", "eth_fund_trace"),
	}
}

func value(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
