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

func TestTraceFiltersAmountBelowThresholdAfterBudgetClipping(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	anchor := "0x0000000000000000000000000000000000000002"
	first := "0x0000000000000000000000000000000000000003"
	dust := "0x0000000000000000000000000000000000000004"
	repository := &fakeRepository{
		addresses: map[string]store.Address{
			seed: completeAddress(0, 100), anchor: completeAddress(0, 100),
			first: completeAddress(0, 100), dust: completeAddress(0, 100),
		},
		labels: map[string][]store.Label{},
		transfers: []store.Transfer{
			{Chain: "ethereum", ChainID: 1, TxHash: "0xin", BlockNumber: 10, From: seed, To: anchor, AssetType: "eth", Asset: "ETH", Amount: "20000000000000000"},
			{Chain: "ethereum", ChainID: 1, TxHash: "0xfirst", BlockNumber: 11, From: anchor, To: first, AssetType: "eth", Asset: "ETH", Amount: "15000000000000000"},
			{Chain: "ethereum", ChainID: 1, TxHash: "0xdust", BlockNumber: 12, From: anchor, To: dust, AssetType: "eth", Asset: "ETH", Amount: "20000000000000000"},
		},
	}

	result, err := New(repository).Trace(context.Background(), Query{Chain: "ethereum", Address: seed, Direction: "out"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 2 || result.Edges[1].To != first {
		t.Fatalf("edges=%+v, want clipped 0.005 ETH edge excluded", result.Edges)
	}
}

func TestExtendBranchFiltersAmountBelowThresholdAfterBudgetClipping(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	anchor := "0x0000000000000000000000000000000000000002"
	next := "0x0000000000000000000000000000000000000003"
	repository := &fakeRepository{
		addresses: map[string]store.Address{anchor: completeAddress(0, 100), next: completeAddress(0, 100)},
		labels:    map[string][]store.Label{},
		transfers: []store.Transfer{
			{Chain: "ethereum", ChainID: 1, TxHash: "0xnext", BlockNumber: 11, From: anchor, To: next, AssetType: "eth", Asset: "ETH", Amount: "20000000000000000"},
		},
	}
	root := Result{Nodes: []Node{{Chain: "ethereum", Address: seed}, {Chain: "ethereum", Address: anchor, Depth: 1}}, DataThroughBlock: 100, Edges: []Edge{
		{Chain: "ethereum", From: seed, To: anchor, AssetType: "eth", Asset: "ETH", TotalAmount: "5000000000000000", FirstBlock: 10, LatestBlock: 10, Path: []string{seed, anchor}},
	}}

	result, err := New(repository).ExtendBranch(context.Background(), root, ExtensionRequest{Chain: "ethereum", Address: anchor, Direction: "out", Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Edges) != 0 {
		t.Fatalf("edges=%+v, want clipped 0.005 ETH edge excluded", result.Edges)
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
