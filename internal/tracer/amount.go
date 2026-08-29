package tracer

import (
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/ethaddr"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

// supportedSummaryAsset accepts native ETH and ERC-20 assets identified by a
// contract address. Symbol text alone never establishes asset identity.
func supportedSummaryAsset(summary store.CounterpartySummary) bool {
	if summary.AssetType == "eth" || strings.EqualFold(summary.Asset, "ETH") {
		return true
	}
	if summary.AssetType != "erc20" {
		return false
	}
	_, err := ethaddr.Normalize(summary.Asset)
	return err == nil
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
