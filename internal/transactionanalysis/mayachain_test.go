package transactionanalysis

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
)

func TestAnalyzeMAYAChainDepositWithExpiry(t *testing.T) {
	const destination = "bc1q2yjae3xdnlwwk6hxf7jxjdt0waupl8qglv3qs7"
	memo := "=:b:" + destination + ":115252390/3/0:ns:5"
	amount, _ := new(big.Int).SetString("33000000000000000000", 10)
	source := &sourceStub{
		tx: etherscan.RPCTransaction{
			Hash: testHash, From: testUser, To: MAYAChainRouter, Value: "0x1c9f78d2893e40000",
			Input: thorchainDepositInput("0x55c302924d5616f1f571425c6cf0762c25ebae81", amount, memo),
		},
		receipt: etherscan.RPCReceipt{TransactionHash: testHash, BlockNumber: "0x10", Status: "0x1"},
	}

	analysis, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ProtocolAction != "router_inbound" || analysis.ProtocolMemo != memo || analysis.ProtocolDestination != destination || analysis.ProtocolVault != "0x55c302924d5616f1f571425c6cf0762c25ebae81" || analysis.ProtocolAsset != "BTC.BTC" || analysis.ProtocolAmount != amount.String() {
		t.Fatalf("analysis=%+v", analysis)
	}
}
