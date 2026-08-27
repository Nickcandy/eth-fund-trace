package etherscan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"golang.org/x/time/rate"
)

var (
	ErrRateLimited       = errors.New("etherscan rate limited")
	ErrAPI               = errors.New("etherscan API error")
	ErrMalformedResponse = errors.New("malformed etherscan response")
	ErrPageLimit         = errors.New("etherscan page limit exceeded")
	ErrTransient         = errors.New("transient etherscan error")
	ErrNotFound          = errors.New("etherscan object not found")
	ErrPending           = errors.New("etherscan receipt pending")
)

const maxResultWindow = 10_000

// RPCTransaction is the subset of an Ethereum transaction used by analysis.
type RPCTransaction struct {
	Hash        string `json:"hash"`
	From        string `json:"from"`
	To          string `json:"to"`
	Value       string `json:"value"`
	Input       string `json:"input"`
	BlockNumber string `json:"blockNumber"`
}

// RPCLog is a raw receipt log returned by the Etherscan proxy.
type RPCLog struct {
	Address         string   `json:"address"`
	Topics          []string `json:"topics"`
	Data            string   `json:"data"`
	LogIndex        string   `json:"logIndex"`
	TransactionHash string   `json:"transactionHash"`
	BlockNumber     string   `json:"blockNumber"`
}

// RPCReceipt is the subset of a transaction receipt used by analysis.
type RPCReceipt struct {
	TransactionHash string   `json:"transactionHash"`
	BlockNumber     string   `json:"blockNumber"`
	Status          string   `json:"status"`
	Logs            []RPCLog `json:"logs"`
}

type PageLimitError struct {
	Action    string
	MaxPages  int
	LastBlock int64
}

func (e *PageLimitError) Error() string {
	return fmt.Sprintf("%s: action=%s max_pages=%d last_block=%d", ErrPageLimit, e.Action, e.MaxPages, e.LastBlock)
}

func (e *PageLimitError) Unwrap() error { return ErrPageLimit }

type Client interface {
	ListTransactions(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error)
	ListInternalTransactions(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error)
	ListTokenTransfers(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error)
	LatestBlock(ctx context.Context) (int64, error)
}

type PageProgress struct {
	Action     string
	Address    string
	StartBlock int64
	EndBlock   int64
	Page       int
	Items      int
}

type ProgressFunc func(PageProgress)

// InternalTransaction is one flattened call returned by Etherscan for a transaction hash.
type InternalTransaction struct {
	From    string
	To      string
	Value   string
	Type    string
	TraceID string
	IsError bool
}

// ProgressClient is optional so alternate chain sources can keep implementing Client.
type ProgressClient interface {
	ListTransactionsWithProgress(context.Context, string, int64, int64, ProgressFunc) ([]store.Transfer, error)
	ListInternalTransactionsWithProgress(context.Context, string, int64, int64, ProgressFunc) ([]store.Transfer, error)
	ListTokenTransfersWithProgress(context.Context, string, int64, int64, ProgressFunc) ([]store.Transfer, error)
}

type Config struct {
	Chain             string
	ChainID           int64
	APIKey            string
	BaseURL           string
	PageSize          int
	MaxPages          int
	RequestsPerSecond float64
	Burst             int
	MaxRetries        int
	RetryBase         time.Duration
	Descending        bool
	HTTPClient        *http.Client
	Limiter           *rate.Limiter
}

type APIClient struct {
	config  Config
	client  *http.Client
	limiter *rate.Limiter
}

type redactedError struct {
	err     error
	message string
}

func (e redactedError) Error() string { return e.message }
func (e redactedError) Unwrap() error { return e.err }

func NewClient(config Config) *APIClient {
	if config.Chain == "" {
		config.Chain = "ethereum"
	}
	if config.ChainID <= 0 {
		config.ChainID = 1
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.etherscan.io/v2/api"
	}
	if config.PageSize <= 0 {
		config.PageSize = 100
	}
	if config.MaxPages <= 0 {
		config.MaxPages = maxResultWindow / config.PageSize
		if config.MaxPages < 1 {
			config.MaxPages = 1
		}
	}
	maxPagesForWindow := maxResultWindow / config.PageSize
	if maxPagesForWindow < 1 {
		maxPagesForWindow = 1
	}
	if config.MaxPages > maxPagesForWindow {
		config.MaxPages = maxPagesForWindow
	}
	if config.RequestsPerSecond <= 0 {
		config.RequestsPerSecond = 5
	}
	if config.Burst <= 0 {
		config.Burst = 1
	}
	if config.MaxRetries < 0 {
		config.MaxRetries = 0
	}
	if config.RetryBase <= 0 {
		config.RetryBase = 500 * time.Millisecond
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	limiter := config.Limiter
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Limit(config.RequestsPerSecond), config.Burst)
	}
	return &APIClient{
		config:  config,
		client:  client,
		limiter: limiter,
	}
}

func (c *APIClient) ListTransactions(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error) {
	return c.list(ctx, address, startBlock, endBlock, "txlist", nil)
}

func (c *APIClient) ListInternalTransactions(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error) {
	return c.list(ctx, address, startBlock, endBlock, "txlistinternal", nil)
}

func (c *APIClient) ListTokenTransfers(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error) {
	return c.list(ctx, address, startBlock, endBlock, "tokentx", nil)
}

// InternalTransactionsByHash returns all flattened internal calls for one transaction.
func (c *APIClient) InternalTransactionsByHash(ctx context.Context, txHash string) ([]InternalTransaction, error) {
	if txHash == "" {
		return nil, fmt.Errorf("%w: empty transaction hash", ErrMalformedResponse)
	}
	endpoint, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base URL", ErrMalformedResponse)
	}
	query := endpoint.Query()
	query.Set("chainid", strconv.FormatInt(c.config.ChainID, 10))
	query.Set("module", "account")
	query.Set("action", "txlistinternal")
	query.Set("txhash", txHash)
	query.Set("apikey", c.config.APIKey)
	endpoint.RawQuery = query.Encode()
	body, err := c.get(ctx, endpoint.String())
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Result  []struct {
			From    string `json:"from"`
			To      string `json:"to"`
			Value   string `json:"value"`
			Type    string `json:"type"`
			TraceID string `json:"traceId"`
			IsError string `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: invalid internal transaction JSON", ErrMalformedResponse)
	}
	if envelope.Status != "1" {
		if strings.Contains(strings.ToLower(envelope.Message), "no transactions") {
			return []InternalTransaction{}, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrAPI, envelope.Message)
	}
	result := make([]InternalTransaction, 0, len(envelope.Result))
	for _, row := range envelope.Result {
		if row.From == "" || row.To == "" || row.Value == "" {
			return nil, fmt.Errorf("%w: incomplete internal transaction", ErrMalformedResponse)
		}
		if value, ok := new(big.Int).SetString(row.Value, 10); !ok || value.Sign() < 0 {
			return nil, fmt.Errorf("%w: invalid internal transaction value", ErrMalformedResponse)
		}
		result = append(result, InternalTransaction{From: strings.ToLower(row.From), To: strings.ToLower(row.To), Value: row.Value, Type: row.Type, TraceID: row.TraceID, IsError: row.IsError == "1"})
	}
	return result, nil
}

func (c *APIClient) ListTransactionsWithProgress(ctx context.Context, address string, startBlock, endBlock int64, progress ProgressFunc) ([]store.Transfer, error) {
	return c.list(ctx, address, startBlock, endBlock, "txlist", progress)
}

func (c *APIClient) ListInternalTransactionsWithProgress(ctx context.Context, address string, startBlock, endBlock int64, progress ProgressFunc) ([]store.Transfer, error) {
	return c.list(ctx, address, startBlock, endBlock, "txlistinternal", progress)
}

func (c *APIClient) ListTokenTransfersWithProgress(ctx context.Context, address string, startBlock, endBlock int64, progress ProgressFunc) ([]store.Transfer, error) {
	return c.list(ctx, address, startBlock, endBlock, "tokentx", progress)
}

func (c *APIClient) list(ctx context.Context, address string, startBlock, endBlock int64, action string, progress ProgressFunc) ([]store.Transfer, error) {
	if address == "" {
		return nil, fmt.Errorf("%w: empty address", ErrMalformedResponse)
	}
	if startBlock < 0 || endBlock < 0 || startBlock > endBlock {
		return nil, fmt.Errorf("%w: invalid block range", ErrMalformedResponse)
	}
	var transfers []store.Transfer
	tokenOccurrences := make(map[string]int64)
	var lastRawBlock int64
	for page := 1; page <= c.config.MaxPages; page++ {
		items, err := c.fetchPage(ctx, address, startBlock, endBlock, page, action)
		if err != nil {
			return nil, err
		}
		pageTransfers, err := normalizeWithState(items, action, tokenOccurrences)
		if err != nil {
			return nil, err
		}
		for i := range pageTransfers {
			pageTransfers[i].Chain, pageTransfers[i].ChainID = c.config.Chain, c.config.ChainID
			pageTransfers[i].TransactionGroup = fmt.Sprintf("%d:%s", c.config.ChainID, strings.ToLower(pageTransfers[i].TxHash))
		}
		transfers = append(transfers, pageTransfers...)
		if len(items) > 0 {
			var boundary struct {
				BlockNumber string `json:"blockNumber"`
			}
			if unmarshalErr := json.Unmarshal(items[len(items)-1], &boundary); unmarshalErr != nil {
				return nil, fmt.Errorf("%w: invalid page boundary", ErrMalformedResponse)
			}
			lastRawBlock, err = parseInt(boundary.BlockNumber, "blockNumber")
			if err != nil {
				return nil, err
			}
		}
		if progress != nil {
			progress(PageProgress{Action: action, Address: address, StartBlock: startBlock, EndBlock: endBlock, Page: page, Items: len(items)})
		}
		if len(items) < c.config.PageSize {
			return transfers, nil
		}
	}
	return transfers, &PageLimitError{Action: action, MaxPages: c.config.MaxPages, LastBlock: lastRawBlock}
}

func (c *APIClient) LatestBlock(ctx context.Context) (int64, error) {
	endpoint, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid base URL", ErrMalformedResponse)
	}
	query := endpoint.Query()
	query.Set("chainid", strconv.FormatInt(c.config.ChainID, 10))
	query.Set("module", "proxy")
	query.Set("action", "eth_blockNumber")
	query.Set("apikey", c.config.APIKey)
	endpoint.RawQuery = query.Encode()

	body, err := c.get(ctx, endpoint.String())
	if err != nil {
		return 0, err
	}
	var envelope struct {
		Result string `json:"result"`
		Error  any    `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Result == "" || envelope.Error != nil {
		return 0, fmt.Errorf("%w: invalid latest block response", ErrMalformedResponse)
	}
	block, err := strconv.ParseInt(strings.TrimPrefix(envelope.Result, "0x"), 16, 64)
	if err != nil || block < 0 {
		return 0, fmt.Errorf("%w: invalid latest block", ErrMalformedResponse)
	}
	return block, nil
}

// TransactionByHash fetches a transaction through Etherscan's proxy API.
func (c *APIClient) TransactionByHash(ctx context.Context, txHash string) (RPCTransaction, error) {
	var transaction RPCTransaction
	found, err := c.proxy(ctx, "eth_getTransactionByHash", map[string]string{"txhash": txHash}, &transaction)
	if err != nil {
		return RPCTransaction{}, err
	}
	if !found {
		return RPCTransaction{}, ErrNotFound
	}
	return transaction, nil
}

// TransactionReceipt fetches a confirmed receipt through Etherscan's proxy API.
func (c *APIClient) TransactionReceipt(ctx context.Context, txHash string) (RPCReceipt, error) {
	var receipt RPCReceipt
	found, err := c.proxy(ctx, "eth_getTransactionReceipt", map[string]string{"txhash": txHash}, &receipt)
	if err != nil {
		return RPCReceipt{}, err
	}
	if !found {
		return RPCReceipt{}, ErrPending
	}
	return receipt, nil
}

// Call executes a read-only eth_call at the latest block.
func (c *APIClient) Call(ctx context.Context, to, data string) (string, error) {
	var result string
	found, err := c.proxy(ctx, "eth_call", map[string]string{"to": to, "data": data, "tag": "latest"}, &result)
	if err != nil {
		return "", err
	}
	if !found || result == "" {
		return "", fmt.Errorf("%w: empty eth_call result", ErrMalformedResponse)
	}
	return result, nil
}

// CodeAt returns the runtime bytecode for an address at the latest block.
func (c *APIClient) CodeAt(ctx context.Context, address string) (string, error) {
	var result string
	found, err := c.proxy(ctx, "eth_getCode", map[string]string{"address": address, "tag": "latest"}, &result)
	if err != nil {
		return "", err
	}
	if !found || result == "" {
		return "", fmt.Errorf("%w: empty eth_getCode result", ErrMalformedResponse)
	}
	return result, nil
}

func (c *APIClient) proxy(ctx context.Context, action string, values map[string]string, target any) (bool, error) {
	endpoint, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return false, fmt.Errorf("%w: invalid base URL", ErrMalformedResponse)
	}
	query := endpoint.Query()
	query.Set("chainid", strconv.FormatInt(c.config.ChainID, 10))
	query.Set("module", "proxy")
	query.Set("action", action)
	query.Set("apikey", c.config.APIKey)
	for key, value := range values {
		query.Set(key, value)
	}
	endpoint.RawQuery = query.Encode()
	body, err := c.get(ctx, endpoint.String())
	if err != nil {
		return false, c.redact(err)
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false, fmt.Errorf("%w: invalid proxy JSON", ErrMalformedResponse)
	}
	if envelope.Error != nil {
		if isRateLimited(envelope.Error.Message, nil) {
			return false, fmt.Errorf("%w: proxy response", ErrRateLimited)
		}
		return false, c.redact(fmt.Errorf("%w: proxy error %d: %s", ErrAPI, envelope.Error.Code, envelope.Error.Message))
	}
	if bytes.Equal(bytes.TrimSpace(envelope.Result), []byte("null")) || len(envelope.Result) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(envelope.Result, target); err != nil {
		return false, fmt.Errorf("%w: invalid proxy result", ErrMalformedResponse)
	}
	return true, nil
}

func (c *APIClient) redact(err error) error {
	if err == nil || c.config.APIKey == "" || !strings.Contains(err.Error(), c.config.APIKey) {
		return err
	}
	return redactedError{err: err, message: strings.ReplaceAll(err.Error(), c.config.APIKey, "[redacted]")}
}

func (c *APIClient) fetchPage(ctx context.Context, address string, startBlock, endBlock int64, page int, action string) ([]json.RawMessage, error) {
	endpoint, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base URL", ErrMalformedResponse)
	}
	query := endpoint.Query()
	query.Set("chainid", strconv.FormatInt(c.config.ChainID, 10))
	query.Set("module", "account")
	query.Set("action", action)
	query.Set("address", address)
	query.Set("startblock", strconv.FormatInt(startBlock, 10))
	query.Set("endblock", strconv.FormatInt(endBlock, 10))
	query.Set("page", strconv.Itoa(page))
	query.Set("offset", strconv.Itoa(c.config.PageSize))
	sortOrder := "asc"
	if c.config.Descending {
		sortOrder = "desc"
	}
	query.Set("sort", sortOrder)
	query.Set("apikey", c.config.APIKey)
	endpoint.RawQuery = query.Encode()

	body, err := c.get(ctx, endpoint.String())
	if err != nil {
		return nil, err
	}

	var envelope struct {
		Status  string          `json:"status"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedResponse, err)
	}
	if isRateLimited(envelope.Message, envelope.Result) {
		return nil, fmt.Errorf("%w: %s", ErrRateLimited, envelope.Message)
	}
	if envelope.Status != "1" {
		if isNoTransactions(envelope.Message, envelope.Result) {
			return []json.RawMessage{}, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrAPI, responseMessage(envelope.Message, envelope.Result))
	}
	var items []json.RawMessage
	if err := json.Unmarshal(envelope.Result, &items); err != nil {
		return nil, fmt.Errorf("%w: result is not an array", ErrMalformedResponse)
	}
	return items, nil
}

func (c *APIClient) get(ctx context.Context, endpoint string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := waitContext(ctx, c.config.RetryBase*time.Duration(1<<(attempt-1))); err != nil {
				return nil, err
			}
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("%w: request creation failed", ErrMalformedResponse)
		}
		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("%w: %v", ErrTransient, err)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("%w: %v", ErrTransient, readErr)
			continue
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("%w: HTTP status %d", ErrRateLimited, resp.StatusCode)
			continue
		}
		if resp.StatusCode >= http.StatusInternalServerError {
			lastErr = fmt.Errorf("%w: HTTP status %d", ErrTransient, resp.StatusCode)
			continue
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("etherscan HTTP status %d", resp.StatusCode)
		}
		if isRateLimitResponse(body) {
			lastErr = fmt.Errorf("%w: API response", ErrRateLimited)
			continue
		}
		return body, nil
	}
	return nil, c.redact(lastErr)
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isNoTransactions(message string, result json.RawMessage) bool {
	if message != "No transactions found" {
		return false
	}
	trimmed := bytes.TrimSpace(result)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return false
	}
	var items []json.RawMessage
	return json.Unmarshal(trimmed, &items) == nil && len(items) == 0
}

func responseMessage(message string, result json.RawMessage) string {
	var detail string
	if err := json.Unmarshal(result, &detail); err == nil && detail != "" {
		return message + ": " + detail
	}
	return message
}

func isRateLimited(message string, result json.RawMessage) bool {
	text := strings.ToLower(message + " " + string(result))
	return strings.Contains(text, "rate limit") || strings.Contains(text, "rate-limit") || strings.Contains(text, "max rate")
}

func isRateLimitResponse(body []byte) bool {
	var envelope struct {
		Status  string          `json:"status"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Status == "1" {
		return false
	}
	return isRateLimited(envelope.Message+" "+envelope.Error.Message, envelope.Result)
}
