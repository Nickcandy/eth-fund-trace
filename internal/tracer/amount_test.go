package tracer

import (
	"context"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestTraceFiltersLowValueEthereumTransfers(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	low := "0x0000000000000000000000000000000000000002"
	boundary := "0x0000000000000000000000000000000000000003"
	repository := &fakeRepository{
		addresses: map[string]store.Address{seed: completeAddress(0, 100), low: completeAddress(0, 100), boundary: completeAddress(0, 100)},
		labels:    map[string][]store.Label{},
		transfers: []store.Transfer{
			{Chain: "ethereum", ChainID: 1, TxHash: "0xlow", From: seed, To: low, AssetType: "eth", Asset: "ETH", Amount: "9999999999999999"},
			{Chain: "ethereum", ChainID: 1, TxHash: "0xboundary", From: seed, To: boundary, AssetType: "eth", Asset: "ETH", Amount: "10000000000000000"},
		},
	}
	result, err := New(repository).Trace(context.Background(), Query{Address: seed, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 1 || result.Edges[0].To != boundary || len(result.Nodes) != 2 {
		t.Fatalf("result=%+v, want only boundary transfer", result)
	}
}

func TestAboveTraceThreshold(t *testing.T) {
	tests := []struct {
		name    string
		summary store.CounterpartySummary
		want    bool
	}{
		{name: "eth below", summary: store.CounterpartySummary{Chain: "ethereum", ChainID: 1, AssetType: "eth", Asset: "ETH", TotalAmount: "9999999999999999"}, want: false},
		{name: "eth boundary", summary: store.CounterpartySummary{Chain: "ethereum", ChainID: 1, AssetType: "eth", Asset: "ETH", TotalAmount: "10000000000000000"}, want: true},
		{name: "usdt below", summary: store.CounterpartySummary{Chain: "ethereum", ChainID: 1, AssetType: "erc20", Symbol: "USDT", TotalAmount: "49999999"}, want: false},
		{name: "usdt boundary", summary: store.CounterpartySummary{Chain: "ethereum", ChainID: 1, AssetType: "erc20", Symbol: "USDT", TotalAmount: "50000000"}, want: true},
		{name: "other asset", summary: store.CounterpartySummary{Chain: "ethereum", ChainID: 1, AssetType: "erc20", Symbol: "DAI", TotalAmount: "1"}, want: true},
		{name: "unknown chain id", summary: store.CounterpartySummary{Chain: "ethereum", AssetType: "eth", Asset: "ETH", TotalAmount: "1"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aboveTraceThreshold(tt.summary); got != tt.want {
				t.Fatalf("aboveTraceThreshold(%+v) = %v, want %v", tt.summary, got, tt.want)
			}
		})
	}
}
