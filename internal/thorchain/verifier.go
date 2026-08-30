package thorchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

type Config struct {
	StatusBaseURL  string
	BitcoinBaseURL string
	ClientID       string
	HTTPClient     *http.Client
}

type Verifier struct {
	config Config
	client *http.Client
}

func New(config Config) *Verifier {
	if config.StatusBaseURL == "" {
		config.StatusBaseURL = "https://gateway.liquify.com/chain/thorchain_api"
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

type statusTransaction struct {
	ID          string `json:"id"`
	Chain       string `json:"chain"`
	FromAddress string `json:"from_address"`
	ToAddress   string `json:"to_address"`
	Coins       []coin `json:"coins"`
	Memo        string `json:"memo"`
}

type plannedOutbound struct {
	Chain     string `json:"chain"`
	ToAddress string `json:"to_address"`
	Coin      coin   `json:"coin"`
	Refund    bool   `json:"refund"`
}

type transactionStatus struct {
	Tx            statusTransaction   `json:"tx"`
	PlannedOutTxs []plannedOutbound   `json:"planned_out_txs"`
	OutTxs        []statusTransaction `json:"out_txs"`
	Stages        struct {
		InboundObserved struct {
			Completed bool `json:"completed"`
		} `json:"inbound_observed"`
		InboundFinalised struct {
			Completed bool `json:"completed"`
		} `json:"inbound_finalised"`
		SwapStatus struct {
			Pending bool `json:"pending"`
		} `json:"swap_status"`
		SwapFinalised struct {
			Completed bool `json:"completed"`
		} `json:"swap_finalised"`
		OutboundSigned struct {
			Completed bool `json:"completed"`
		} `json:"outbound_signed"`
	} `json:"stages"`
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
	if analysis.Chain != "ethereum" || !analysis.Succeeded || analysis.ProtocolAction != "router_inbound" || analysis.ProtocolAsset != "BTC.BTC" || analysis.ProtocolMemo == "" || analysis.ProtocolDestination == "" {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	hash := strings.ToUpper(strings.TrimPrefix(analysis.TxHash, "0x"))
	if len(hash) != 64 {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	var status transactionStatus
	if err := v.getJSON(ctx, strings.TrimRight(v.config.StatusBaseURL, "/")+"/thorchain/tx/status/"+hash, &status); err != nil {
		return store.VerifiedCrossChainTransfer{}, false, fmt.Errorf("fetch THORChain status: %w", err)
	}
	if !v.matchesInbound(analysis, hash, status) {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	plannedAmount, planned := matchingPlan(status.PlannedOutTxs, analysis.ProtocolDestination)
	if !planned {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	outbound, found := matchingOutbound(status.OutTxs, analysis.ProtocolDestination, plannedAmount, hash)
	if !found {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	var bitcoin bitcoinTransaction
	outHash := strings.ToLower(outbound.ID)
	if err := v.getJSON(ctx, strings.TrimRight(v.config.BitcoinBaseURL, "/")+"/api/tx/"+outHash, &bitcoin); err != nil {
		return store.VerifiedCrossChainTransfer{}, false, fmt.Errorf("fetch Bitcoin transaction: %w", err)
	}
	if !bitcoin.Status.Confirmed || !strings.EqualFold(bitcoin.TxID, outHash) || !hasBitcoinOutput(bitcoin, analysis.ProtocolDestination, plannedAmount) {
		return store.VerifiedCrossChainTransfer{}, false, nil
	}
	return store.VerifiedCrossChainTransfer{SourceChain: "ethereum", TargetChain: "bitcoin", From: strings.ToLower(analysis.To), To: strings.ToLower(analysis.ProtocolDestination), Asset: "BTC", Amount: plannedAmount, TxHash: outHash, BlockNumber: bitcoin.Status.BlockHeight}, true, nil
}

func (v *Verifier) matchesInbound(analysis store.TransactionAnalysis, hash string, status transactionStatus) bool {
	expectedTo := analysis.To
	if analysis.ProtocolVault != "" {
		expectedTo = analysis.ProtocolVault
	}
	if !strings.EqualFold(status.Tx.ID, hash) || status.Tx.Chain != "ETH" || !strings.EqualFold(status.Tx.FromAddress, analysis.From) || !strings.EqualFold(status.Tx.ToAddress, expectedTo) || status.Tx.Memo != analysis.ProtocolMemo {
		return false
	}
	if !status.Stages.InboundObserved.Completed || !status.Stages.InboundFinalised.Completed || status.Stages.SwapStatus.Pending || !status.Stages.SwapFinalised.Completed || !status.Stages.OutboundSigned.Completed {
		return false
	}
	wei, ok := new(big.Int).SetString(analysis.Value, 10)
	if !ok || wei.Sign() <= 0 {
		return false
	}
	expected := new(big.Int).Quo(wei, big.NewInt(10_000_000_000)).String()
	for _, value := range status.Tx.Coins {
		if value.Asset == "ETH.ETH" && value.Amount == expected {
			return true
		}
	}
	return false
}

func matchingPlan(values []plannedOutbound, destination string) (string, bool) {
	for _, value := range values {
		if value.Chain == "BTC" && !value.Refund && strings.EqualFold(value.ToAddress, destination) && value.Coin.Asset == "BTC.BTC" && positiveDecimal(value.Coin.Amount) {
			return value.Coin.Amount, true
		}
	}
	return "", false
}

func matchingOutbound(values []statusTransaction, destination, amount, inboundHash string) (statusTransaction, bool) {
	zeroHash := strings.Repeat("0", 64)
	for _, value := range values {
		if value.Chain != "BTC" || value.ID == "" || strings.EqualFold(value.ID, zeroHash) || !strings.EqualFold(value.ToAddress, destination) || !strings.EqualFold(value.Memo, "OUT:"+inboundHash) {
			continue
		}
		for _, output := range value.Coins {
			if output.Asset == "BTC.BTC" && output.Amount == amount {
				return value, true
			}
		}
	}
	return statusTransaction{}, false
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

func positiveDecimal(value string) bool {
	number, ok := new(big.Int).SetString(value, 10)
	return ok && number.Sign() > 0
}

func (v *Verifier) getJSON(ctx context.Context, endpoint string, target any) error {
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		request.Header.Set("Accept", "application/json")
		if v.config.ClientID != "" {
			request.Header.Set("x-client-id", v.config.ClientID)
		}
		response, err := v.client.Do(request)
		if err != nil {
			return err
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
			_ = response.Body.Close()
			if attempt == 2 {
				return fmt.Errorf("unexpected HTTP status %d after retries", response.StatusCode)
			}
			delay := time.Duration(100*(1<<attempt)) * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			return fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
		}
		decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
		err = decoder.Decode(target)
		if err == nil && decoder.Decode(&struct{}{}) != io.EOF {
			err = errors.New("unexpected trailing JSON data")
		}
		_ = response.Body.Close()
		return err
	}
	return errors.New("unreachable retry state")
}
