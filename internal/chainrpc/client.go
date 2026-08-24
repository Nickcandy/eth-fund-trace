// Package chainrpc provides a small Ethereum JSON-RPC client.
package chainrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
)

var (
	ErrRateLimited = errors.New("chain RPC rate limited")
	ErrUnavailable = errors.New("chain RPC unavailable")
	ErrMalformed   = errors.New("malformed chain RPC response")
	ErrNotFound    = errors.New("chain RPC object not found")
	ErrPending     = errors.New("chain RPC receipt pending")
	ErrWrongChain  = errors.New("chain RPC returned unexpected chain ID")
)

// Config defines one chain RPC endpoint.
type Config struct {
	URL        string
	ChainID    int64
	HTTPClient *http.Client
}

// LogFilter selects logs by block range, address and topics.
type LogFilter struct {
	FromBlock string     `json:"fromBlock,omitempty"`
	ToBlock   string     `json:"toBlock,omitempty"`
	Address   string     `json:"address,omitempty"`
	Topics    [][]string `json:"topics,omitempty"`
}

// Log is a receipt or eth_getLogs entry.
type Log struct {
	Address         string   `json:"address"`
	Topics          []string `json:"topics"`
	Data            string   `json:"data"`
	LogIndex        string   `json:"logIndex"`
	TransactionHash string   `json:"transactionHash"`
	BlockNumber     string   `json:"blockNumber"`
}

// Client calls one Ethereum-compatible JSON-RPC endpoint.
type Client struct {
	endpoint string
	chainID  int64
	http     *http.Client
	redacted string
}

// New creates a chain RPC client.
func New(config Config) *Client {
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{endpoint: config.URL, chainID: config.ChainID, http: client, redacted: redactURL(config.URL)}
}

// ValidateChain verifies that the endpoint serves the configured chain.
func (c *Client) ValidateChain(ctx context.Context) error {
	var raw string
	if err := c.call(ctx, "eth_chainId", []any{}, &raw); err != nil {
		return err
	}
	value, err := strconv.ParseInt(strings.TrimPrefix(raw, "0x"), 16, 64)
	if err != nil {
		return fmt.Errorf("%w: invalid chain ID", ErrMalformed)
	}
	if value != c.chainID {
		return fmt.Errorf("%w: got %d want %d", ErrWrongChain, value, c.chainID)
	}
	return nil
}

// TransactionByHash fetches one transaction.
func (c *Client) TransactionByHash(ctx context.Context, hash string) (etherscan.RPCTransaction, error) {
	var result *etherscan.RPCTransaction
	if err := c.call(ctx, "eth_getTransactionByHash", []any{hash}, &result); err != nil {
		return etherscan.RPCTransaction{}, err
	}
	if result == nil {
		return etherscan.RPCTransaction{}, ErrNotFound
	}
	return *result, nil
}

// TransactionReceipt fetches one confirmed receipt.
func (c *Client) TransactionReceipt(ctx context.Context, hash string) (etherscan.RPCReceipt, error) {
	var result *etherscan.RPCReceipt
	if err := c.call(ctx, "eth_getTransactionReceipt", []any{hash}, &result); err != nil {
		return etherscan.RPCReceipt{}, err
	}
	if result == nil {
		return etherscan.RPCReceipt{}, ErrPending
	}
	return *result, nil
}

// BlockNumber returns the latest block number.
func (c *Client) BlockNumber(ctx context.Context) (int64, error) {
	var raw string
	if err := c.call(ctx, "eth_blockNumber", []any{}, &raw); err != nil {
		return 0, err
	}
	value, err := strconv.ParseInt(strings.TrimPrefix(raw, "0x"), 16, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid block number", ErrMalformed)
	}
	return value, nil
}

// Logs queries chain logs.
func (c *Client) Logs(ctx context.Context, filter LogFilter) ([]Log, error) {
	var result []Log
	if err := c.call(ctx, "eth_getLogs", []any{filter}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Call executes eth_call at the latest block.
func (c *Client) Call(ctx context.Context, to, data string) (string, error) {
	var result string
	if err := c.call(ctx, "eth_call", []any{map[string]string{"to": to, "data": data}, "latest"}, &result); err != nil {
		return "", err
	}
	return result, nil
}

func (c *Client) call(ctx context.Context, method string, params []any, target any) error {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return fmt.Errorf("marshal RPC request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create RPC request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %s", ErrUnavailable, c.redacted)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("%w: %s", ErrRateLimited, c.redacted)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: HTTP %d from %s", ErrUnavailable, resp.StatusCode, c.redacted)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: read response", ErrUnavailable)
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("%w: invalid JSON", ErrMalformed)
	}
	if envelope.Error != nil {
		if envelope.Error.Code == -32005 || strings.Contains(strings.ToLower(envelope.Error.Message), "rate") {
			return fmt.Errorf("%w: RPC error %d", ErrRateLimited, envelope.Error.Code)
		}
		return fmt.Errorf("%w: RPC error %d", ErrUnavailable, envelope.Error.Code)
	}
	if len(envelope.Result) == 0 {
		return fmt.Errorf("%w: missing result", ErrMalformed)
	}
	if err := json.Unmarshal(envelope.Result, target); err != nil {
		return fmt.Errorf("%w: invalid result", ErrMalformed)
	}
	return nil
}

func redactURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return "[redacted-rpc-url]"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Path = ""
	return parsed.String()
}
