package etherscan

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestClientPagesAndNormalizesAllActions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("chainid") != "1" || q.Get("module") != "account" || q.Get("apikey") != "secret" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		page, _ := strconv.Atoi(q.Get("page"))
		var item map[string]string
		switch q.Get("action") {
		case "txlist":
			item = map[string]string{"blockNumber": "10", "timeStamp": "1700000000", "hash": "0xnormal", "from": "0xfrom", "to": "0xto", "value": "100000000000000000000"}
		case "txlistinternal":
			item = map[string]string{"blockNumber": "11", "timeStamp": "1700000001", "hash": "0xinternal", "from": "0xfrom", "to": "0xto", "value": "2", "traceId": "0_1"}
		case "tokentx":
			item = map[string]string{"blockNumber": "12", "timeStamp": "1700000002", "hash": "0xtoken", "from": "0xfrom", "to": "0xto", "value": "999999999999999999999", "contractAddress": "0xasset", "tokenSymbol": "USDC", "tokenDecimal": "6", "logIndex": "7"}
		default:
			t.Fatal("unexpected action")
		}
		result := []map[string]string{item}
		if page == 2 {
			item["hash"] += "-page2"
			result = []map[string]string{item}
		}
		if page >= 3 {
			result = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "1", "message": "OK", "result": result})
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "secret", BaseURL: server.URL, PageSize: 1, MaxPages: 3, HTTPClient: server.Client()})
	ctx := context.Background()
	for _, test := range []struct {
		name string
		call func(context.Context) ([]store.Transfer, error)
	}{
		{name: "normal", call: func(ctx context.Context) ([]store.Transfer, error) {
			return client.ListTransactions(ctx, "0xseed", 0, 99)
		}},
		{name: "internal", call: func(ctx context.Context) ([]store.Transfer, error) {
			return client.ListInternalTransactions(ctx, "0xseed", 0, 99)
		}},
		{name: "token", call: func(ctx context.Context) ([]store.Transfer, error) {
			return client.ListTokenTransfers(ctx, "0xseed", 0, 99)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			transfers, err := test.call(ctx)
			if err != nil || len(transfers) != 2 {
				t.Fatalf("transfers=%d err=%v", len(transfers), err)
			}
			if transfers[0].Chain != "ethereum" || transfers[0].ChainID != 1 || transfers[0].ObservedAt.IsZero() {
				t.Fatalf("unexpected common fields: %+v", transfers[0])
			}
			switch test.name {
			case "normal":
				if transfers[0].AssetType != "eth" || transfers[0].Asset != "ETH" || transfers[0].Amount != "100000000000000000000" {
					t.Fatalf("unexpected normal transfer: %+v", transfers[0])
				}
			case "internal":
				if transfers[0].Source != "txlistinternal" || transfers[0].TraceID != "0_1" {
					t.Fatalf("unexpected internal transfer: %+v", transfers[0])
				}
			case "token":
				if transfers[0].AssetType != "erc20" || transfers[0].Asset != "0xasset" || transfers[0].TokenValue != "999999999999999999999" || transfers[0].Symbol != "USDC" || transfers[0].Decimals != 6 || transfers[0].LogIndex != 7 {
					t.Fatalf("unexpected token transfer: %+v", transfers[0])
				}
			}
		})
	}
}

func TestClientErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       error
	}{
		{name: "rate status", statusCode: http.StatusTooManyRequests, body: `{}`, want: ErrRateLimited},
		{name: "rate message", statusCode: http.StatusOK, body: `{"status":"0","message":"Max rate limit reached","result":""}`, want: ErrRateLimited},
		{name: "api", statusCode: http.StatusOK, body: `{"status":"0","message":"Invalid API Key","result":""}`, want: ErrAPI},
		{name: "no transactions without array", statusCode: http.StatusOK, body: `{"status":"0","message":"No transactions found","result":null}`, want: ErrAPI},
		{name: "malformed", statusCode: http.StatusOK, body: `{`, want: ErrMalformedResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			_, err := NewClient(Config{APIKey: "secret", BaseURL: server.URL, HTTPClient: server.Client()}).ListTransactions(context.Background(), "0xseed", 0, 1)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("error leaked API key")
			}
		})
	}
}

func TestClientNormalizesContractCreation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[{"blockNumber":"341505","timeStamp":"1439471609","hash":"0xcreate","from":"0xcreator","to":"","contractAddress":"0xcontract","value":"0","isError":"0"}]}`))
	}))
	defer server.Close()

	transfers, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()}).ListTransactions(context.Background(), "0xcreator", 341505, 341505)
	if err != nil {
		t.Fatal(err)
	}
	if len(transfers) != 1 || transfers[0].To != "0xcontract" {
		t.Fatalf("transfers = %+v, want contract creation target", transfers)
	}
}

func TestClientRejectsTransactionWithoutTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[{"blockNumber":"1","timeStamp":"1","hash":"0xmissing","from":"0xfrom","to":"","contractAddress":"","value":"1","isError":"0"}]}`))
	}))
	defer server.Close()

	_, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()}).ListTransactions(context.Background(), "0xfrom", 0, 1)
	if !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("error = %v, want malformed response", err)
	}
}

func TestClientFiltersFailedTransactions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[` +
			`{"blockNumber":"1","timeStamp":"1","hash":"0xsuccess","from":"0xfrom","to":"0xto","value":"1","isError":"0","txreceipt_status":"1"},` +
			`{"blockNumber":"2","timeStamp":"2","hash":"0xerror","from":"0xfrom","to":"0xto","value":"2","isError":"1","txreceipt_status":"1"},` +
			`{"blockNumber":"3","timeStamp":"3","hash":"0xreceipt","from":"0xfrom","to":"0xto","value":"3","isError":"0","txreceipt_status":"0"}]}`))
	}))
	defer server.Close()

	transfers, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()}).ListTransactions(context.Background(), "0xfrom", 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(transfers) != 1 || transfers[0].TxHash != "0xsuccess" {
		t.Fatalf("transfers = %+v, want only successful transaction", transfers)
	}
}

func TestClientFiltersFailedInternalTransactions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[` +
			`{"blockNumber":"1","timeStamp":"1","hash":"0xsuccess","from":"0xfrom","to":"0xto","value":"1","traceId":"0","isError":"0"},` +
			`{"blockNumber":"2","timeStamp":"2","hash":"0xerror","from":"0xfrom","to":"0xto","value":"2","traceId":"1","isError":"1"}]}`))
	}))
	defer server.Close()

	transfers, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()}).ListInternalTransactions(context.Background(), "0xfrom", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(transfers) != 1 || transfers[0].TxHash != "0xsuccess" {
		t.Fatalf("transfers = %+v, want only successful internal transaction", transfers)
	}
}

func TestClientTreatsNoTransactionsAsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"0","message":"No transactions found","result":[]}`))
	}))
	defer server.Close()

	transfers, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()}).ListTransactions(context.Background(), "0xempty", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(transfers) != 0 {
		t.Fatalf("transfers = %+v, want empty result", transfers)
	}
}

func TestClientAssignsSyntheticIndexWhenTokenLogIndexIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[` +
			`{"blockNumber":"1545550","timeStamp":"1462441651","hash":"0xtoken","from":"0xfrom","to":"0xto","value":"340000000000000000000","contractAddress":"0xasset","tokenSymbol":"","tokenDecimal":"1"},` +
			`{"blockNumber":"1545550","timeStamp":"1462441651","hash":"0xtoken","from":"0xfrom","to":"0xto","value":"340000000000000000000","contractAddress":"0xasset","tokenSymbol":"","tokenDecimal":"1"}]}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	first, err := client.ListTokenTransfers(context.Background(), "0xfrom", 1545550, 1545550)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.ListTokenTransfers(context.Background(), "0xfrom", 1545550, 1545550)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].LogIndex >= 0 || first[1].LogIndex >= 0 || first[0].LogIndex == first[1].LogIndex {
		t.Fatalf("transfers = %+v, want distinct negative synthetic indexes", first)
	}
	if len(second) != 2 || first[0].LogIndex != second[0].LogIndex || first[1].LogIndex != second[1].LogIndex {
		t.Fatalf("first = %+v, second = %+v, want deterministic synthetic indexes", first, second)
	}
}

func TestClientContinuesPagingWhenPageIsFullyFiltered(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[{"blockNumber":"1","timeStamp":"1","hash":"0xfailed","from":"0xfrom","to":"0xto","value":"1","isError":"1"}]}`))
		case "2":
			_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[{"blockNumber":"2","timeStamp":"2","hash":"0xsuccess","from":"0xfrom","to":"0xto","value":"2","isError":"0"}]}`))
		default:
			_, _ = w.Write([]byte(`{"status":"0","message":"No transactions found","result":[]}`))
		}
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, PageSize: 1, MaxPages: 3, HTTPClient: server.Client()})
	transfers, err := client.ListTransactions(context.Background(), "0xfrom", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 3 || len(transfers) != 1 || transfers[0].TxHash != "0xsuccess" {
		t.Fatalf("requests = %d, transfers = %+v, want three pages and successful transaction", requests, transfers)
	}
}

func TestClientPageLimitAndContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[{"blockNumber":"1","timeStamp":"1","hash":"0xhash","from":"0xfrom","to":"0xto","value":"1"}]}`))
	}))
	defer server.Close()
	client := NewClient(Config{APIKey: "secret", BaseURL: server.URL, PageSize: 1, MaxPages: 1, HTTPClient: server.Client()})
	_, err := client.ListTransactions(context.Background(), "0xseed", 0, 1)
	if !errors.Is(err, ErrPageLimit) {
		t.Fatalf("error = %v, want page limit", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewClient(Config{APIKey: "secret", BaseURL: server.URL, RequestsPerSecond: 1, HTTPClient: server.Client()}).ListTransactions(ctx, "0xseed", 0, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestNormalizeRejectsInvalidFields(t *testing.T) {
	_, err := normalize([]json.RawMessage{json.RawMessage(`{"blockNumber":"bad"}`)}, "txlist")
	if !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("error = %v, want malformed response", err)
	}
}

func TestClientReturnsLatestBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("module") != "proxy" || r.URL.Query().Get("action") != "eth_blockNumber" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x12d687"}`))
	}))
	defer server.Close()

	block, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client(), RequestsPerSecond: 1000}).LatestBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if block != 1234567 {
		t.Fatalf("block = %d, want 1234567", block)
	}
}

func TestClientSharesRateLimitAcrossConcurrentActions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"0","message":"No transactions found","result":[]}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client(), RequestsPerSecond: 20, Burst: 1})
	started := time.Now()
	var wg sync.WaitGroup
	for _, call := range []func(context.Context, string, int64, int64) ([]store.Transfer, error){client.ListTransactions, client.ListInternalTransactions} {
		wg.Add(1)
		go func(call func(context.Context, string, int64, int64) ([]store.Transfer, error)) {
			defer wg.Done()
			if _, err := call(context.Background(), "0xseed", 0, 1); err != nil {
				t.Errorf("call failed: %v", err)
			}
		}(call)
	}
	wg.Wait()
	if elapsed := time.Since(started); elapsed < 35*time.Millisecond {
		t.Fatalf("elapsed = %v, want shared rate limiter delay", elapsed)
	}
}

func TestClientRetriesTransientHTTPFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"status":"0","message":"No transactions found","result":[]}`))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client(), RequestsPerSecond: 1000, MaxRetries: 3, RetryBase: time.Millisecond})
	if _, err := client.ListTransactions(context.Background(), "0xseed", 0, 1); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}
