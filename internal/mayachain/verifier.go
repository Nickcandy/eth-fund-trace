package mayachain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

const ethereumRouter = "0xe3985e6b61b814f7cdb188766562ba71b446b46d"

type Config struct {
	StatusBaseURL  string
	BitcoinBaseURL string
	HTTPClient     *http.Client
}

type Verifier struct {
	config Config
	client *http.Client
}

func New(config Config) *Verifier {
	if config.StatusBaseURL == "" {
		config.StatusBaseURL = "https://mayanode.mayachain.info"
	}
	if config.BitcoinBaseURL == "" {
		config.BitcoinBaseURL = "https://mempool.space"
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Verifier{config: config, client: client}
}

type coin struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
}

type transaction struct {
	ID          string `json:"id"`
	Chain       string `json:"chain"`
	FromAddress string `json:"from_address"`
	ToAddress   string `json:"to_address"`
	Coins       []coin `json:"coins"`
	Memo        string `json:"memo"`
}

type transactionDetails struct {
	Tx struct {
		Tx     transaction `json:"tx"`
		Status string      `json:"status"`
	} `json:"tx"`
	OutTxs []transaction `json:"out_txs"`
}

type bitcoinTransaction struct {
	TxID string `json:"txid"`
	Vout []struct {
		Address string `json:"scriptpubkey_address"`
		Value   int64  `json:"value"`
	} `json:"vout"`
	Status struct {
		Confirmed   bool  `json:"confirmed"`
		BlockHeight int64 `json:"block_height"`
	} `json:"status"`
}

func (v *Verifier) Verify(ctx context.Context, analysis store.TransactionAnalysis) (store.VerifiedCrossChainTransfer, bool, error) {
	if analysis.Chain != "ethereum" || !analysis.Succeeded || !strings.EqualFold(analysis.To, ethereumRouter) || analysis.ProtocolAction != "router_inbound" || analysis.ProtocolAsset != "BTC.BTC" || analysis.ProtocolMemo == "" || analysis.ProtocolDestination == "" || analysis.ProtocolVault == "" {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	hash := strings.ToUpper(strings.TrimPrefix(analysis.TxHash, "0x"))
	if len(hash) != 64 {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	var details transactionDetails
	if err := v.getJSON(ctx, strings.TrimRight(v.config.StatusBaseURL, "/")+"/mayachain/tx/details/"+hash, &details); err != nil {
		return store.VerifiedCrossChainTransfer{}, false, fmt.Errorf("fetch MayaChain transaction details: %w", err)
	}
	if details.Tx.Status != "done" || !matchesInbound(analysis, hash, details.Tx.Tx) {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	outbound, amount, ok := matchingOutbound(details.OutTxs, analysis.ProtocolDestination, hash)
	if !ok {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	outHash := strings.ToLower(outbound.ID)
	var bitcoin bitcoinTransaction
	if err := v.getJSON(ctx, strings.TrimRight(v.config.BitcoinBaseURL, "/")+"/api/tx/"+outHash, &bitcoin); err != nil {
		return store.VerifiedCrossChainTransfer{}, false, fmt.Errorf("fetch Bitcoin transaction: %w", err)
	}
	if !bitcoin.Status.Confirmed || !strings.EqualFold(bitcoin.TxID, outHash) || !hasBitcoinOutput(bitcoin, analysis.ProtocolDestination, amount) {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	return store.VerifiedCrossChainTransfer{Protocol: "mayachain", SourceChain: "ethereum", TargetChain: "bitcoin", From: strings.ToLower(analysis.To), To: strings.ToLower(analysis.ProtocolDestination), Asset: "BTC", Amount: amount, TxHash: outHash, BlockNumber: bitcoin.Status.BlockHeight}, true, nil
}

func matchesInbound(analysis store.TransactionAnalysis, hash string, inbound transaction) bool {
	if !strings.EqualFold(inbound.ID, hash) || inbound.Chain != "ETH" || !strings.EqualFold(inbound.FromAddress, analysis.From) || !strings.EqualFold(inbound.ToAddress, analysis.ProtocolVault) || inbound.Memo != analysis.ProtocolMemo {
		return false
	}
	wei, ok := new(big.Int).SetString(analysis.Value, 10)
	if !ok || wei.Sign() <= 0 {
		return false
	}
	expected := new(big.Int).Quo(wei, big.NewInt(10_000_000_000)).String()
	for _, value := range inbound.Coins {
		if value.Asset == "ETH.ETH" && value.Amount == expected {
			return true
		}
	}
	return false
}

func matchingOutbound(values []transaction, destination, inboundHash string) (transaction, string, bool) {
	zeroHash := strings.Repeat("0", 64)
	for _, value := range values {
		if value.Chain != "BTC" || value.ID == "" || strings.EqualFold(value.ID, zeroHash) || !strings.EqualFold(value.ToAddress, destination) || !strings.EqualFold(value.Memo, "OUT:"+inboundHash) {
			continue
		}
		for _, output := range value.Coins {
			amount, ok := new(big.Int).SetString(output.Amount, 10)
			if output.Asset == "BTC.BTC" && ok && amount.Sign() > 0 {
				return value, output.Amount, true
			}
		}
	}
	return transaction{}, "", false
}

func hasBitcoinOutput(transaction bitcoinTransaction, destination, amount string) bool {
	expected, ok := new(big.Int).SetString(amount, 10)
	if !ok || !expected.IsInt64() {
		return false
	}
	for _, output := range transaction.Vout {
		if strings.EqualFold(output.Address, destination) && output.Value == expected.Int64() {
			return true
		}
	}
	return false
}

func (v *Verifier) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := v.client.Do(request)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		return fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target)
	_ = response.Body.Close()
	return err
}
