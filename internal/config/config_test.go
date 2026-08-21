package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("MONGO_URI", "")
	t.Setenv("MONGO_DATABASE", "")

	got := Load()
	if got.HTTPAddr != ":8080" || got.MongoURI != "mongodb://localhost:27017" || got.MongoDatabase != "eth_fund_trace" {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestLoadEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("MONGO_URI", "mongodb://mongo:27017")
	t.Setenv("MONGO_DATABASE", "test_db")

	got := Load()
	if got.HTTPAddr != ":9090" || got.MongoURI != "mongodb://mongo:27017" || got.MongoDatabase != "test_db" {
		t.Fatalf("unexpected environment config: %+v", got)
	}
}
