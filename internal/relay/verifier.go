package relay

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

const (
	EthereumChainID  = 1
	ArbitrumChainID  = 42161
	Solver           = "0xf70da97812cb96acdf810712aa562db8dfa3dbef"
	ArbitrumRouter   = "0x1231deb6f5749ef6ce6943a275a1d3e7486f4eae"
	startBridgeRelay = "ae328590"
)

type Config struct {
	StatusBaseURL string
	ArbitrumRPC   string
	HTTPClient    *http.Client
}

type Verifier struct {
	config Config
	client *http.Client
}

func New(config Config) *Verifier {
	if config.StatusBaseURL == "" {
		config.StatusBaseURL = "https://api.relay.link"
	}
	if config.ArbitrumRPC == "" {
		config.ArbitrumRPC = "https://arb1.arbitrum.io/rpc"
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Verifier{config: config, client: client}
}

type statusResponse struct {
	Status             string   `json:"status"`
	InTxHashes         []string `json:"inTxHashes"`
	TxHashes           []string `json:"txHashes"`
	OriginChainID      int64    `json:"originChainId"`
	DestinationChainID int64    `json:"destinationChainId"`
}

type rpcTransaction struct {
	Hash  string `json:"hash"`
	From  string `json:"from"`
	To    string `json:"to"`
	Value string `json:"value"`
	Input string `json:"input"`
}

type rpcReceipt struct {
	TransactionHash string `json:"transactionHash"`
	Status          string `json:"status"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Verify links a Relay solver payment on Ethereum to its Arbitrum source transaction.
func (v *Verifier) Verify(ctx context.Context, analysis store.TransactionAnalysis) (store.CrossChainAnalysis, bool, error) {
	requestID, candidate := destinationRequestID(analysis)
	if !candidate {
		return store.CrossChainAnalysis{}, false, nil
	}
	status, err := v.status(ctx, requestID)
	if err != nil {
		return store.CrossChainAnalysis{}, false, fmt.Errorf("fetch Relay status: %w", err)
	}
	if status.Status != "success" || status.OriginChainID != ArbitrumChainID || status.DestinationChainID != EthereumChainID || len(status.InTxHashes) != 1 || !containsHash(status.TxHashes, analysis.TxHash) {
		return store.CrossChainAnalysis{}, false, nil
	}
	origin, err := v.transaction(ctx, status.InTxHashes[0])
	if err != nil {
		return store.CrossChainAnalysis{}, false, fmt.Errorf("fetch Relay origin transaction: %w", err)
	}
	receipt, err := v.receipt(ctx, status.InTxHashes[0])
	if err != nil {
		return store.CrossChainAnalysis{}, false, fmt.Errorf("fetch Relay origin receipt: %w", err)
	}
	bridge, ok := decodeOrigin(*origin)
	if !ok || receipt.Status != "0x1" || !strings.EqualFold(receipt.TransactionHash, origin.Hash) || !strings.EqualFold(origin.Hash, status.InTxHashes[0]) || !strings.EqualFold(bridge.requestID, requestID) || !strings.EqualFold(bridge.receiver, analysis.To) || bridge.destinationChainID != EthereumChainID || bridge.amount != origin.Value {
		return store.CrossChainAnalysis{}, false, nil
	}
	sourceAmount, sourceOK := hexQuantity(origin.Value)
	targetAmount, targetOK := new(big.Int).SetString(analysis.Value, 10)
	if !sourceOK || !targetOK || targetAmount.Sign() <= 0 || sourceAmount.Cmp(targetAmount) < 0 {
		return store.CrossChainAnalysis{}, false, nil
	}
	fee := new(big.Int).Sub(sourceAmount, targetAmount)
	return store.CrossChainAnalysis{
		Protocol: "relay", Status: "complete", RequestID: requestID,
		SourceChain: "arbitrum", SourceChainID: ArbitrumChainID, TargetChain: "ethereum", TargetChainID: EthereumChainID,
		SourceTxHash: strings.ToLower(origin.Hash), TargetTxHash: strings.ToLower(analysis.TxHash),
		From: strings.ToLower(origin.From), To: strings.ToLower(analysis.To), SourceAsset: "ETH", SourceAmount: sourceAmount.String(), TargetAsset: "ETH", TargetAmount: targetAmount.String(), FeeAmount: fee.String(),
	}, true, nil
}

// Supports reports whether a transaction has the bounded Relay destination shape.
func (v *Verifier) Supports(analysis store.TransactionAnalysis) bool {
	_, ok := destinationRequestID(analysis)
	return ok
}

func destinationRequestID(analysis store.TransactionAnalysis) (string, bool) {
	input := strings.ToLower(strings.TrimSpace(analysis.Input))
	if analysis.Chain != "ethereum" || !analysis.Succeeded || !strings.EqualFold(analysis.From, Solver) || analysis.To == "" || analysis.Value == "0" || len(input) != 66 || !strings.HasPrefix(input, "0x") {
		return "", false
	}
	if _, err := hex.DecodeString(input[2:]); err != nil {
		return "", false
	}
	return input, true
}

type originBridge struct {
	requestID          string
	receiver           string
	amount             string
	destinationChainID int64
}

func decodeOrigin(transaction rpcTransaction) (originBridge, bool) {
	input := strings.TrimPrefix(strings.ToLower(transaction.Input), "0x")
	if !strings.EqualFold(transaction.To, ArbitrumRouter) || len(input) < 8+18*64 || input[:8] != startBridgeRelay || input[8:72] != fmt.Sprintf("%064x", 64) {
		return originBridge{}, false
	}
	words := input[8:]
	word := func(index int) string { return words[index*64 : (index+1)*64] }
	receiver := "0x" + word(7)[24:]
	amount := "0x" + strings.TrimLeft(word(8), "0")
	if amount == "0x" {
		amount = "0x0"
	}
	destination := new(big.Int)
	if _, ok := destination.SetString(word(9), 16); !ok || !destination.IsInt64() {
		return originBridge{}, false
	}
	requestID := "0x" + word(16)
	if word(17)[24:] != strings.TrimPrefix(receiver, "0x") {
		return originBridge{}, false
	}
	return originBridge{requestID: requestID, receiver: receiver, amount: amount, destinationChainID: destination.Int64()}, true
}

func (v *Verifier) status(ctx context.Context, requestID string) (statusResponse, error) {
	endpoint := strings.TrimRight(v.config.StatusBaseURL, "/") + "/intents/status/v3?requestId=" + url.QueryEscape(requestID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return statusResponse{}, err
	}
	request.Header.Set("Accept", "application/json")
	var status statusResponse
	if err := v.doJSON(request, &status); err != nil {
		return statusResponse{}, err
	}
	return status, nil
}

func (v *Verifier) transaction(ctx context.Context, hash string) (*rpcTransaction, error) {
	var transaction rpcTransaction
	if err := v.rpc(ctx, "eth_getTransactionByHash", hash, &transaction); err != nil {
		return nil, err
	}
	return &transaction, nil
}

func (v *Verifier) receipt(ctx context.Context, hash string) (*rpcReceipt, error) {
	var receipt rpcReceipt
	if err := v.rpc(ctx, "eth_getTransactionReceipt", hash, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (v *Verifier) rpc(ctx context.Context, method, hash string, target any) error {
	body := strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"params":[%q],"id":1}`, method, hash))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, v.config.ArbitrumRPC, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	var response rpcResponse
	if err := v.doJSON(request, &response); err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("RPC error %d: %s", response.Error.Code, response.Error.Message)
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return fmt.Errorf("RPC result not found")
	}
	return json.Unmarshal(response.Result, target)
}

func (v *Verifier) doJSON(request *http.Request, target any) error {
	response, err := v.client.Do(request)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if err := response.Body.Close(); err != nil {
			return fmt.Errorf("close error response: %w", err)
		}
		return fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target)
	closeErr := response.Body.Close()
	if decodeErr != nil {
		return decodeErr
	}
	if closeErr != nil {
		return fmt.Errorf("close response: %w", closeErr)
	}
	return nil
}

func containsHash(hashes []string, expected string) bool {
	for _, hash := range hashes {
		if strings.EqualFold(hash, expected) {
			return true
		}
	}
	return false
}

func hexQuantity(value string) (*big.Int, bool) {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	if value == "" {
		return nil, false
	}
	parsed := new(big.Int)
	_, ok := parsed.SetString(value, 16)
	return parsed, ok && parsed.Sign() > 0
}
