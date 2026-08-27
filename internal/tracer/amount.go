package tracer

import (
	"math/big"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

// TraceableAmount reports whether a known asset meets the minimum tracing threshold.
func TraceableAmount(asset, assetType string, decimals int32, raw string) bool {
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok || value.Sign() <= 0 {
		return false
	}
	asset = strings.ToUpper(asset)
	if assetType == "eth" || asset == "ETH" || asset == "WETH" {
		return value.Cmp(new(big.Int).Exp(big.NewInt(10), big.NewInt(16), nil)) >= 0
	}
	if asset == "USDT" || asset == "USDC" || asset == "DAI" {
		if decimals < 0 {
			return false
		}
		threshold := new(big.Int).Mul(big.NewInt(10), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
		return value.Cmp(threshold) >= 0
	}
	return false
}

func traceableSummaryAmount(chain string, summary store.CounterpartySummary) bool {
	asset := summary.Asset
	if token, ok := knownTokenFor(chain, asset); ok {
		asset = token.Symbol
	}
	return TraceableAmount(asset, summary.AssetType, summary.Decimals, summary.TotalAmount)
}
