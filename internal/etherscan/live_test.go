//go:build integration

package etherscan

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveTransactions(t *testing.T) {
	if os.Getenv("ETHERSCAN_LIVE_TEST") != "1" || os.Getenv("ETHERSCAN_API_KEY") == "" {
		t.Skip("set ETHERSCAN_LIVE_TEST=1 and ETHERSCAN_API_KEY to run")
	}
	client := NewClient(Config{
		APIKey:          os.Getenv("ETHERSCAN_API_KEY"),
		RequestInterval: 250 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	transfers, err := client.ListTransactions(ctx, "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", 0, 99999999)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("received %d transactions", len(transfers))
}
