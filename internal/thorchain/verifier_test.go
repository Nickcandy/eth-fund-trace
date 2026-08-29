package thorchain

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestVerifierConfirmsMatchingBitcoinOutbound(t *testing.T) {
	const (
		hash = "d6cc09af0fe3b51225cc8db073eb4353ba657cf7041be6877c78c993e383a4c2"
		btc  = "bc1phghtqqpfwgw3jzaefey0z0epesyd77nyncllcer9s9rs0t4t4ttqhcpgd4"
		out  = "f9e417b7db1d7128327387c0971e377e5a694d28758fea4d43c64d3fae098634"
		memo = "=:b:" + btc + ":725679114/1/0:-_/bgw:20/30"
	)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/thorchain/tx/status/" + "D6CC09AF0FE3B51225CC8DB073EB4353BA657CF7041BE6877C78C993E383A4C2":
			_, _ = response.Write([]byte(`{"tx":{"id":"` + hash + `","chain":"ETH","from_address":"0x1476be0c524e15c735a1b3927bba993f03a59790","to_address":"0x3986fd2fd3669fd32584b960cda8f82e0f772c35","coins":[{"asset":"ETH.ETH","amount":"20065000000"}],"memo":"` + memo + `"},"planned_out_txs":[{"chain":"BTC","to_address":"` + btc + `","coin":{"asset":"BTC.BTC","amount":"729777955"},"refund":false}],"out_txs":[{"id":"` + out + `","chain":"BTC","from_address":"bc1qvault","to_address":"` + btc + `","coins":[{"asset":"BTC.BTC","amount":"729777955"}],"memo":"OUT:` + hash + `"}],"stages":{"inbound_observed":{"completed":true},"inbound_finalised":{"completed":true},"swap_status":{"pending":false},"swap_finalised":{"completed":true},"outbound_signed":{"completed":true}}}`))
		case "/api/tx/" + out:
			_, _ = response.Write([]byte(`{"txid":"` + out + `","vout":[{"scriptpubkey_address":"` + btc + `","value":729777955}],"status":{"confirmed":true,"block_height":917213}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	verifier := New(Config{StatusBaseURL: server.URL, BitcoinBaseURL: server.URL, HTTPClient: server.Client()})
	analysis := store.TransactionAnalysis{Chain: "ethereum", TxHash: "0x" + hash, From: "0x1476be0c524e15c735a1b3927bba993f03a59790", To: "0x3986fd2fd3669fd32584b960cda8f82e0f772c35", Value: "200650000000000000000", Succeeded: true, ProtocolAction: "router_inbound", ProtocolMemo: memo, ProtocolDestination: btc, ProtocolAsset: "BTC.BTC"}
	transfer, ok, err := verifier.Verify(context.Background(), analysis)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || transfer.TargetChain != "bitcoin" || transfer.To != btc || transfer.Amount != "729777955" || transfer.TxHash != out || transfer.BlockNumber != 917213 {
		t.Fatalf("transfer=%+v ok=%v", transfer, ok)
	}
}

func TestVerifierRejectsPendingOrRefundedOutbound(t *testing.T) {
	const (
		hash = "d6cc09af0fe3b51225cc8db073eb4353ba657cf7041be6877c78c993e383a4c2"
		btc  = "bc1phghtqqpfwgw3jzaefey0z0epesyd77nyncllcer9s9rs0t4t4ttqhcpgd4"
		memo = "=:b:" + btc + ":725679114/1/0:-_/bgw:20/30"
	)
	for _, test := range []struct {
		name    string
		pending bool
		refund  bool
	}{{name: "pending", pending: true}, {name: "refund", refund: true}} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"tx":{"id":"` + hash + `","chain":"ETH","from_address":"0x1476be0c524e15c735a1b3927bba993f03a59790","to_address":"0x3986fd2fd3669fd32584b960cda8f82e0f772c35","coins":[{"asset":"ETH.ETH","amount":"20065000000"}],"memo":"` + memo + `"},"planned_out_txs":[{"chain":"BTC","to_address":"` + btc + `","coin":{"asset":"BTC.BTC","amount":"729777955"},"refund":` + fmt.Sprint(test.refund) + `}],"out_txs":[],"stages":{"inbound_observed":{"completed":true},"inbound_finalised":{"completed":true},"swap_status":{"pending":` + fmt.Sprint(test.pending) + `},"swap_finalised":{"completed":true},"outbound_signed":{"completed":true}}}`))
			}))
			defer server.Close()
			analysis := store.TransactionAnalysis{Chain: "ethereum", TxHash: "0x" + hash, From: "0x1476be0c524e15c735a1b3927bba993f03a59790", To: "0x3986fd2fd3669fd32584b960cda8f82e0f772c35", Value: "200650000000000000000", Succeeded: true, ProtocolAction: "router_inbound", ProtocolMemo: memo, ProtocolDestination: btc, ProtocolAsset: "BTC.BTC"}
			_, ok, err := New(Config{StatusBaseURL: server.URL, BitcoinBaseURL: server.URL, HTTPClient: server.Client()}).Verify(context.Background(), analysis)
			if err != nil || ok {
				t.Fatalf("ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestVerifierRetriesTemporaryStatusFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		attempts++
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	analysis := store.TransactionAnalysis{Chain: "ethereum", TxHash: "0x" + strings.Repeat("a", 64), Succeeded: true, ProtocolAction: "router_inbound", ProtocolMemo: "=:b:bc1ptestaddress", ProtocolDestination: "bc1ptestaddress", ProtocolAsset: "BTC.BTC"}
	_, _, err := New(Config{StatusBaseURL: server.URL, HTTPClient: server.Client()}).Verify(context.Background(), analysis)
	if err == nil || attempts != 3 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}
