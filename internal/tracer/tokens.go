package tracer

import "strings"

const (
	ethereumDAI  = "0x6b175474e89094c44da98b954eedeac495271d0f"
	ethereumUSDC = "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
	ethereumUSDT = "0xdac17f958d2ee523a2206206994597c13d831ec7"
	ethereumWETH = "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"
)

type traceAsset struct {
	AssetMode string
	Asset     string
}

type knownToken struct {
	Contract string
	Symbol   string
	Decimals int32
}

var knownTokens = map[string][]knownToken{
	"ethereum": {
		{Contract: ethereumDAI, Symbol: "DAI", Decimals: 18},
		{Contract: ethereumUSDC, Symbol: "USDC", Decimals: 6},
		{Contract: ethereumUSDT, Symbol: "USDT", Decimals: 6},
		{Contract: ethereumWETH, Symbol: "WETH", Decimals: 18},
	},
}

func rootAssets(chain string) []traceAsset {
	tokens := knownTokens[chain]
	assets := make([]traceAsset, 1, len(tokens)+1)
	assets[0] = traceAsset{AssetMode: "eth", Asset: "ETH"}
	for _, token := range tokens {
		assets = append(assets, traceAsset{AssetMode: "contract", Asset: token.Contract})
	}
	return assets
}

func knownTokenFor(chain, contract string) (knownToken, bool) {
	for _, token := range knownTokens[chain] {
		if strings.EqualFold(token.Contract, contract) {
			return token, true
		}
	}
	return knownToken{}, false
}
