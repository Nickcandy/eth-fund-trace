package mayachain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestVerifierConfirmsMatchingBitcoinOutbound(t *testing.T) {
	const (
		hash = "2b6fa2acfb7e87f3ca39331b071f3479ee6cff8a2967d0fddbb6459172a03afc"
		btc  = "bc1q2yjae3xdnlwwk6hxf7jxjdt0waupl8qglv3qs7"
		out  = "046992e1705d36a84dc5c36322e6272e94f1ba2dcd49c6be7295ae51f0769901"
		memo = "=:b:" + btc + ":115252390/3/0:ns:5"
	)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/mayachain/tx/details/2B6FA2ACFB7E87F3CA39331B071F3479EE6CFF8A2967D0FDDBB6459172A03AFC":
			_, _ = response.Write([]byte(`{"tx":{"tx":{"id":"` + hash + `","chain":"ETH","from_address":"0x9951544f600e95c2ca5f967c0567b9bf50de72d0","to_address":"0x55c302924d5616f1f571425c6cf0762c25ebae81","coins":[{"asset":"ETH.ETH","amount":"3300000000"}],"memo":"` + memo + `"},"status":"done"},"out_txs":[{"id":"` + out + `","chain":"BTC","from_address":"bc1qvault","to_address":"` + btc + `","coins":[{"asset":"BTC.BTC","amount":"120934527"}],"memo":"OUT:2B6FA2ACFB7E87F3CA39331B071F3479EE6CFF8A2967D0FDDBB6459172A03AFC"}]}`))
		case "/api/tx/" + out:
			_, _ = response.Write([]byte(`{"txid":"` + out + `","vout":[{"scriptpubkey_address":"` + btc + `","value":120934527}],"status":{"confirmed":true,"block_height":917224}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	verifier := New(Config{StatusBaseURL: server.URL, BitcoinBaseURL: server.URL, HTTPClient: server.Client()})
	analysis := store.TransactionAnalysis{Chain: "ethereum", TxHash: "0x" + hash, From: "0x9951544f600e95c2ca5f967c0567b9bf50de72d0", To: "0xe3985e6b61b814f7cdb188766562ba71b446b46d", Value: "33000000000000000000", Succeeded: true, ProtocolAction: "router_inbound", ProtocolMemo: memo, ProtocolDestination: btc, ProtocolVault: "0x55c302924d5616f1f571425c6cf0762c25ebae81", ProtocolAsset: "BTC.BTC"}
	transfer, ok, err := verifier.Verify(context.Background(), analysis)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || transfer.Protocol != "mayachain" || transfer.TargetChain != "bitcoin" || transfer.To != btc || transfer.Amount != "120934527" || transfer.TxHash != out || transfer.BlockNumber != 917224 {
		t.Fatalf("transfer=%+v ok=%v", transfer, ok)
	}
}
