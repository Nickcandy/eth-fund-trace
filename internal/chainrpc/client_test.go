package chainrpc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientReadsChainTransactionReceiptLogsAndCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body [4096]byte
		n, _ := r.Body.Read(body[:])
		request := string(body[:n])
		switch {
		case strings.Contains(request, `"method":"eth_chainId"`):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x2105"}`))
		case strings.Contains(request, `"method":"eth_getTransactionByHash"`):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"hash":"0x1","from":"0x0000000000000000000000000000000000000001","to":"0x0000000000000000000000000000000000000002","value":"0x1","input":"0x","blockNumber":"0xa"}}`))
		case strings.Contains(request, `"method":"eth_getTransactionReceipt"`):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"transactionHash":"0x1","blockNumber":"0xa","status":"0x1","logs":[]}}`))
		case strings.Contains(request, `"method":"eth_getLogs"`):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[{"address":"0x0000000000000000000000000000000000000002","topics":["0x1"],"data":"0x","logIndex":"0x0","transactionHash":"0x1","blockNumber":"0xa"}]}`))
		case strings.Contains(request, `"method":"eth_call"`):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x01"}`))
		}
	}))
	defer server.Close()

	client := New(Config{URL: server.URL, ChainID: 8453, HTTPClient: server.Client()})
	if err := client.ValidateChain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.TransactionByHash(context.Background(), "0x1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.TransactionReceipt(context.Background(), "0x1"); err != nil {
		t.Fatal(err)
	}
	if logs, err := client.Logs(context.Background(), LogFilter{Address: "0x2"}); err != nil || len(logs) != 1 {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
	if value, err := client.Call(context.Background(), "0x2", "0x3"); err != nil || value != "0x01" {
		t.Fatalf("value=%s err=%v", value, err)
	}
}

func TestClientMapsErrorsCancellationAndRedactsURL(t *testing.T) {
	secret := "rpc-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, secret) {
			http.Error(w, "limited", http.StatusTooManyRequests)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}))
	defer server.Close()
	client := New(Config{URL: server.URL + "/" + secret, ChainID: 1, HTTPClient: server.Client()})
	_, err := client.TransactionByHash(context.Background(), "0x1")
	if !errors.Is(err, ErrRateLimited) || strings.Contains(err.Error(), secret) {
		t.Fatalf("err=%v", err)
	}

	client = New(Config{URL: server.URL, ChainID: 1, HTTPClient: server.Client()})
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err = client.TransactionByHash(ctx, "0x1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestClientRejectsWrongChain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	defer server.Close()
	err := New(Config{URL: server.URL, ChainID: 8453, HTTPClient: server.Client()}).ValidateChain(context.Background())
	if !errors.Is(err, ErrWrongChain) {
		t.Fatalf("err=%v", err)
	}
}
