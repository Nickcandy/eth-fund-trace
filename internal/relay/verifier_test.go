package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

const (
	targetHash = "0xdabd84d5ae85f19ec132be32e038a7b3e57156b9e9ad5cd74096cbcbb16db926"
	sourceHash = "0x75b743b66096cd63630e6c92b3e2ecdfc06de9827e7a7015bc57bc3a14ca0996"
	requestID  = "0x0ba05c68bed94b3ccd383f0392ed2736319ed820f237d2d134e45a753ad2a604"
	recipient  = "0x889b49ef0bf787c3ddc2950bfc7d1d439320004b"
)

func TestVerifyRelayArbitrumToEthereumETH(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/intents/status/v3":
			if request.URL.Query().Get("requestId") != requestID {
				t.Fatalf("requestId=%s", request.URL.Query().Get("requestId"))
			}
			_, _ = fmt.Fprintf(response, `{"status":"success","inTxHashes":[%q],"txHashes":[%q],"originChainId":42161,"destinationChainId":1}`, sourceHash, targetHash)
		case "/rpc":
			var body struct {
				Method string   `json:"method"`
				Params []string `json:"params"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body.Params) != 1 || body.Params[0] != sourceHash {
				t.Fatalf("RPC request=%+v err=%v", body, err)
			}
			switch body.Method {
			case "eth_getTransactionByHash":
				_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"hash":%q,"from":%q,"to":%q,"value":"0x6f037d2e1423f0000","input":%q}}`, sourceHash, recipient, ArbitrumRouter, originInput())
			case "eth_getTransactionReceipt":
				_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"transactionHash":%q,"status":"0x1"}}`, sourceHash)
			default:
				t.Fatalf("unexpected RPC method %s", body.Method)
			}
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	verifier := New(Config{StatusBaseURL: server.URL, ArbitrumRPC: server.URL + "/rpc", HTTPClient: server.Client()})
	analysis := destinationAnalysis()
	if !verifier.Supports(analysis) {
		t.Fatal("known Relay solver payment should be supported")
	}
	result, found, err := verifier.Verify(context.Background(), analysis)
	if err != nil {
		t.Fatal(err)
	}
	if !found || result.Protocol != "relay" || result.SourceChainID != 42161 || result.TargetChainID != 1 || result.SourceTxHash != sourceHash || result.TargetTxHash != targetHash || result.From != recipient || result.To != recipient {
		t.Fatalf("result=%+v found=%v", result, found)
	}
	if result.SourceAmount != "127990000000000000000" || result.TargetAmount != "127957978616285681831" || result.FeeAmount != "32021383714318169" {
		t.Fatalf("amounts=%+v", result)
	}
}

func TestVerifyRejectsStatusWithoutTargetHash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(response, `{"status":"success","inTxHashes":[%q],"txHashes":[],"originChainId":42161,"destinationChainId":1}`, sourceHash)
	}))
	defer server.Close()

	result, found, err := New(Config{StatusBaseURL: server.URL, HTTPClient: server.Client()}).Verify(context.Background(), destinationAnalysis())
	if err != nil || found || result.Protocol != "" {
		t.Fatalf("result=%+v found=%v err=%v", result, found, err)
	}
}

func TestSupportsRequiresKnownSolverAndRequestID(t *testing.T) {
	verifier := New(Config{})
	analysis := destinationAnalysis()
	analysis.From = recipient
	if verifier.Supports(analysis) {
		t.Fatal("unlabelled sender must not be treated as Relay")
	}
	analysis.From = Solver
	analysis.Input = "0x1234"
	if verifier.Supports(analysis) {
		t.Fatal("short calldata must not be treated as a request ID")
	}
}

func destinationAnalysis() store.TransactionAnalysis {
	return store.TransactionAnalysis{
		Chain: "ethereum", TxHash: targetHash, From: Solver, To: recipient,
		Value: "127957978616285681831", Input: requestID, Succeeded: true,
	}
}

func originInput() string {
	word := func(value string) string {
		value = strings.TrimPrefix(value, "0x")
		return strings.Repeat("0", 64-len(value)) + value
	}
	words := []string{
		fmt.Sprintf("%064x", 64), fmt.Sprintf("%064x", 512), strings.Repeat("a", 64), fmt.Sprintf("%064x", 320), fmt.Sprintf("%064x", 384),
		word("0"), word("0"), word(recipient), word("6f037d2e1423f0000"), fmt.Sprintf("%064x", 1),
		word("0"), word("0"), word("0"), word("0"), word("0"), word("0"), word(requestID), word(recipient),
	}
	return "0x" + startBridgeRelay + strings.Join(words, "")
}
