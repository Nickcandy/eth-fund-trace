package tracer

import (
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestTraceableAmount(t *testing.T) {
	for _, test := range []struct {
		asset, kind, raw string
		decimals         int32
		want             bool
	}{
		{"ETH", "eth", "9999999999999999", 18, false},
		{"ETH", "eth", "10000000000000000", 18, true},
		{"USDT", "erc20", "9999999", 6, false},
		{"USDT", "erc20", "10000000", 6, true},
		{"0xunknown", "erc20", "1000000000000000000", 18, false},
	} {
		if got := TraceableAmount(test.asset, test.kind, test.decimals, test.raw); got != test.want {
			t.Fatalf("traceableAmount(%q,%q,%d,%q)=%v, want %v", test.asset, test.kind, test.decimals, test.raw, got, test.want)
		}
	}
}

func TestTraceableSummaryAmountRecognizesOfficialContract(t *testing.T) {
	summary := store.CounterpartySummary{AssetType: "erc20", Asset: ethereumUSDT, Decimals: 6, TotalAmount: "10000000"}
	if !traceableSummaryAmount("ethereum", summary) {
		t.Fatal("official USDT contract at threshold should be traceable")
	}
}
