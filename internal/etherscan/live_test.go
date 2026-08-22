//go:build integration

package etherscan

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestLiveActions(t *testing.T) {
	if os.Getenv("ETHERSCAN_LIVE_TEST") != "1" || os.Getenv("ETHERSCAN_API_KEY") == "" {
		t.Skip("set ETHERSCAN_LIVE_TEST=1 and ETHERSCAN_API_KEY to run")
	}
	client := NewClient(Config{
		APIKey:            os.Getenv("ETHERSCAN_API_KEY"),
		RequestsPerSecond: 5,
		MaxRetries:        3,
		RetryBase:         500 * time.Millisecond,
	})

	const address = "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	tests := []struct {
		name       string
		block      int64
		call       func(context.Context, string, int64, int64) ([]store.Transfer, error)
		assertions func(*testing.T, []store.Transfer)
	}{
		{
			name:  "normal contract creation",
			block: 341505,
			call:  client.ListTransactions,
			assertions: func(t *testing.T, transfers []store.Transfer) {
				t.Helper()
				const contract = "0x3ec7829f9c875894d9e693b9b422eab0e917e38c"
				for _, transfer := range transfers {
					if strings.EqualFold(transfer.To, contract) {
						return
					}
				}
				t.Fatalf("contract creation target %s not found", contract)
			},
		},
		{name: "internal", block: 318074, call: client.ListInternalTransactions},
		{
			name:  "token",
			block: 1545550,
			call:  client.ListTokenTransfers,
			assertions: func(t *testing.T, transfers []store.Transfer) {
				t.Helper()
				if transfers[0].LogIndex >= 0 {
					t.Fatalf("log index = %d, want synthetic negative index", transfers[0].LogIndex)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			transfers, err := tt.call(ctx, address, tt.block, tt.block)
			if err != nil {
				t.Fatal(err)
			}
			if len(transfers) == 0 {
				t.Fatal("received no transfers")
			}
			if tt.assertions != nil {
				tt.assertions(t, transfers)
			}
			t.Logf("received %d transfers", len(transfers))
		})
	}
}

func TestLiveLatestBlock(t *testing.T) {
	if os.Getenv("ETHERSCAN_LIVE_TEST") != "1" || os.Getenv("ETHERSCAN_API_KEY") == "" {
		t.Skip("set ETHERSCAN_LIVE_TEST=1 and ETHERSCAN_API_KEY to run")
	}
	client := NewClient(Config{APIKey: os.Getenv("ETHERSCAN_API_KEY"), RequestsPerSecond: 5, MaxRetries: 3, RetryBase: 500 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	block, err := client.LatestBlock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if block <= 0 {
		t.Fatalf("latest block = %d", block)
	}
	t.Logf("latest block %d", block)
}

func TestLiveBaseLatestBlock(t *testing.T) {
	if os.Getenv("ETHERSCAN_LIVE_TEST") != "1" || os.Getenv("ETHERSCAN_API_KEY") == "" {
		t.Skip("set ETHERSCAN_LIVE_TEST=1 and ETHERSCAN_API_KEY to run")
	}
	client := NewClient(Config{Chain: "base", ChainID: 8453, APIKey: os.Getenv("ETHERSCAN_API_KEY"), RequestsPerSecond: 5, MaxRetries: 3, RetryBase: 500 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	block, err := client.LatestBlock(ctx)
	if err != nil || block <= 0 {
		t.Fatalf("latest Base block=%d err=%v", block, err)
	}
	t.Logf("latest Base block %d", block)
}
