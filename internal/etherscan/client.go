package etherscan

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

	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"golang.org/x/time/rate"
)

var (
	ErrRateLimited       = errors.New("etherscan rate limited")
	ErrAPI               = errors.New("etherscan API error")
	ErrMalformedResponse = errors.New("malformed etherscan response")
	ErrPageLimit         = errors.New("etherscan page limit exceeded")
	ErrTransient         = errors.New("transient etherscan error")
)

type Client interface {
	ListTransactions(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error)
	ListInternalTransactions(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error)
	ListTokenTransfers(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error)
	LatestBlock(ctx context.Context) (int64, error)
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
	HTTPClient        *http.Client
	Limiter           *rate.Limiter
}

type APIClient struct {
	config  Config
	client  *http.Client
	limiter *rate.Limiter
}

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
		config.MaxPages = 100
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
	return c.list(ctx, address, startBlock, endBlock, "txlist")
}

func (c *APIClient) ListInternalTransactions(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error) {
	return c.list(ctx, address, startBlock, endBlock, "txlistinternal")
}

func (c *APIClient) ListTokenTransfers(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error) {
	return c.list(ctx, address, startBlock, endBlock, "tokentx")
}

func (c *APIClient) list(ctx context.Context, address string, startBlock, endBlock int64, action string) ([]store.Transfer, error) {
	if address == "" {
		return nil, fmt.Errorf("%w: empty address", ErrMalformedResponse)
	}
	if startBlock < 0 || endBlock < 0 || startBlock > endBlock {
		return nil, fmt.Errorf("%w: invalid block range", ErrMalformedResponse)
	}
	var transfers []store.Transfer
	tokenOccurrences := make(map[string]int64)
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
		if len(items) < c.config.PageSize {
			return transfers, nil
		}
	}
	return nil, fmt.Errorf("%w: action=%s max_pages=%d", ErrPageLimit, action, c.config.MaxPages)
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
	query.Set("sort", "asc")
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
	return nil, lastErr
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
