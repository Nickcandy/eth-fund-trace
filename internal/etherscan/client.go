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
)

var (
	ErrRateLimited       = errors.New("etherscan rate limited")
	ErrAPI               = errors.New("etherscan API error")
	ErrMalformedResponse = errors.New("malformed etherscan response")
	ErrPageLimit         = errors.New("etherscan page limit exceeded")
)

type Client interface {
	ListTransactions(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error)
	ListInternalTransactions(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error)
	ListTokenTransfers(ctx context.Context, address string, startBlock, endBlock int64) ([]store.Transfer, error)
}

type Config struct {
	APIKey          string
	BaseURL         string
	PageSize        int
	MaxPages        int
	RequestInterval time.Duration
	HTTPClient      *http.Client
}

type APIClient struct {
	config Config
	client *http.Client
}

func NewClient(config Config) *APIClient {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.etherscan.io/v2/api"
	}
	if config.PageSize <= 0 {
		config.PageSize = 100
	}
	if config.MaxPages <= 0 {
		config.MaxPages = 100
	}
	if config.RequestInterval < 0 {
		config.RequestInterval = 0
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &APIClient{config: config, client: client}
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
		if page > 1 && c.config.RequestInterval > 0 {
			timer := time.NewTimer(c.config.RequestInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		items, err := c.fetchPage(ctx, address, startBlock, endBlock, page, action)
		if err != nil {
			return nil, err
		}
		pageTransfers, err := normalizeWithState(items, action, tokenOccurrences)
		if err != nil {
			return nil, err
		}
		transfers = append(transfers, pageTransfers...)
		if len(items) < c.config.PageSize {
			return transfers, nil
		}
	}
	return nil, fmt.Errorf("%w: action=%s max_pages=%d", ErrPageLimit, action, c.config.MaxPages)
}

func (c *APIClient) fetchPage(ctx context.Context, address string, startBlock, endBlock int64, page int, action string) ([]json.RawMessage, error) {
	endpoint, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base URL", ErrMalformedResponse)
	}
	query := endpoint.Query()
	query.Set("chainid", "1")
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: request creation failed", ErrMalformedResponse)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: HTTP status %d", ErrRateLimited, resp.StatusCode)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("etherscan HTTP status %d", resp.StatusCode)
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
