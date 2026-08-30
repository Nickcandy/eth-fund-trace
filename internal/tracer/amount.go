package tracer

import (
	"math/big"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

var (
	minimumETHTraceAmount  = new(big.Int).Exp(big.NewInt(10), big.NewInt(16), nil) // 0.01 ETH in wei
	minimumUSDTTraceAmount = big.NewInt(50_000_000)                                // 50 USDT (6 decimals)
)

// supportedSummaryAsset accepts native ETH and ERC-20 assets from the
// configured whitelist. Symbol text alone never establishes asset identity.
func supportedSummaryAsset(summary store.CounterpartySummary) bool {
	if summary.AssetType == "eth" || strings.EqualFold(summary.Asset, "ETH") {
		return true
	}
	if summary.AssetType != "erc20" {
		return false
	}
	contract, err := ethaddr.Normalize(summary.Asset)
	if err != nil {
		return false
	}
	_, ok := knownTokenFor(summary.Chain, contract)
	return ok
}

func canonicalizeSummaryAsset(chain string, summary *store.CounterpartySummary) {
	if token, ok := knownTokenFor(chain, summary.Asset); ok {
		summary.AssetType = "erc20"
		summary.Asset = token.Contract
		summary.Symbol = token.Symbol
		summary.Decimals = token.Decimals
		summary.TokenMetadataComplete = true
		return
	}
	if summary.AssetType == "erc20" {
		summary.Symbol = ""
	}
}

// aboveTraceThreshold applies the user-facing trace filters to raw on-chain
// integer amounts. The threshold itself is inclusive; other assets are not
// filtered by this rule.
func aboveTraceThreshold(summary store.CounterpartySummary) bool {
	// Thresholds are defined for Ethereum mainnet amounts. Synthetic and
	// cross-chain records without a chain ID retain the existing behavior.
	if summary.Chain != "ethereum" || summary.ChainID != 1 {
		return true
	}
	amount, ok := new(big.Int).SetString(summary.TotalAmount, 10)
	if !ok || amount.Sign() <= 0 {
		return false
	}
	var minimum *big.Int
	if summary.AssetType == "eth" || strings.EqualFold(summary.Asset, "ETH") {
		minimum = minimumETHTraceAmount
	} else if strings.EqualFold(summary.Symbol, "USDT") {
		minimum = minimumUSDTTraceAmount
	}
	return minimum == nil || amount.Cmp(minimum) >= 0
}
