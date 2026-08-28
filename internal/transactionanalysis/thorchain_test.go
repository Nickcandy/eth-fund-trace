package transactionanalysis

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestParseTHORChainTransferOutMemo(t *testing.T) {
	// transferOut(to, native asset, amount, "MIGRATE:25385637")
	input := "0x574da717" +
		"00000000000000000000000052425d6e839582BDDa85D4bcA83D347504de3ACd" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000001" +
		"0000000000000000000000000000000000000000000000000000000000000080" +
		"0000000000000000000000000000000000000000000000000000000000000010" +
		"4d4947524154453a3235333835363337" +
		"0000000000000000000000000000000000000000000000000000000000000000"
	analysis := store.TransactionAnalysis{To: THORChainRouter, Input: input, Succeeded: true}
	parseTHORChainCall(&analysis)
	if analysis.ProtocolAction != "vault_migration" || analysis.ProtocolMemo != "MIGRATE:25385637" || analysis.ProtocolDestination != "0x52425d6e839582bdda85d4bca83d347504de3acd" || analysis.ProtocolAsset != "ETH" || analysis.ProtocolAmount != "1" {
		t.Fatalf("decoded THORChain call = %+v", analysis)
	}
}

func TestVerifyTHORChainMigrationRequiresMatchingETHInternalCall(t *testing.T) {
	analysis := store.TransactionAnalysis{
		Succeeded: true, To: THORChainRouter, ProtocolAction: "vault_migration", ProtocolDestination: "0x00000000000000000000000000000000000000a1", ProtocolAsset: "ETH", ProtocolAmount: "25",
		InternalCalls: []store.InternalCall{{From: THORChainRouter, To: "0x00000000000000000000000000000000000000a1", Value: "25"}},
	}
	if !verifiedTHORChainMigration(analysis) {
		t.Fatal("matching internal ETH call should verify migration")
	}
	analysis.InternalCalls[0].Value = "24"
	if verifiedTHORChainMigration(analysis) {
		t.Fatal("mismatched internal ETH amount must not verify migration")
	}
}

func TestVerifyTHORChainMigrationRequiresMatchingERC20Transfer(t *testing.T) {
	const token = "0x00000000000000000000000000000000000000b1"
	analysis := store.TransactionAnalysis{
		Succeeded: true, To: THORChainRouter, ProtocolAction: "vault_migration", ProtocolDestination: "0x00000000000000000000000000000000000000a1", ProtocolAsset: token, ProtocolAmount: "25",
		Transfers: []store.ReceiptTransfer{{Token: token, From: THORChainRouter, To: "0x00000000000000000000000000000000000000a1", Amount: "25"}},
	}
	if !verifiedTHORChainMigration(analysis) {
		t.Fatal("matching ERC-20 receipt transfer should verify migration")
	}
	analysis.Transfers[0].To = "0x00000000000000000000000000000000000000a2"
	if verifiedTHORChainMigration(analysis) {
		t.Fatal("mismatched ERC-20 destination must not verify migration")
	}
}

func TestParseTHORChainTransferOutERC20AssetAndAmount(t *testing.T) {
	const (
		destination = "0x00000000000000000000000000000000000000a1"
		token       = "0x00000000000000000000000000000000000000b1"
	)
	analysis := store.TransactionAnalysis{To: THORChainRouter, Input: thorchainTransferOutInput(destination, token, big.NewInt(25), "MIGRATE:42"), Succeeded: true}
	if !parseTHORChainCall(&analysis) || analysis.ProtocolDestination != destination || analysis.ProtocolAsset != token || analysis.ProtocolAmount != "25" || analysis.ProtocolAction != "vault_migration" {
		t.Fatalf("decoded THORChain ERC-20 call = %+v", analysis)
	}
}

func TestAnalyzeTHORChainMigrationMarksMismatchedEvidencePartial(t *testing.T) {
	destination := "0x00000000000000000000000000000000000000a1"
	amount := big.NewInt(25)
	source := &sourceStub{
		tx:        etherscan.RPCTransaction{Hash: testHash, From: testUser, To: THORChainRouter, Value: "0x19", Input: thorchainTransferOutInput(destination, zeroAddress, amount, "MIGRATE:42")},
		receipt:   etherscan.RPCReceipt{TransactionHash: testHash, BlockNumber: "0x10", Status: "0x1"},
		internals: []etherscan.InternalTransaction{{From: THORChainRouter, To: destination, Value: "24", Type: "call"}},
	}
	analysis, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ProtocolAmount != "25" || analysis.Quality.Status != "partial" || !strings.Contains(strings.Join(analysis.Quality.Issues, " "), "evidence mismatch") {
		t.Fatalf("analysis=%+v", analysis)
	}
}

func TestParseTHORChainCallRejectsOversizedMemoWithoutPanic(t *testing.T) {
	input := "0x574da717" +
		"00000000000000000000000052425d6e839582BDDa85D4bcA83D347504de3ACd" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000001" +
		"0000000000000000000000000000000000000000000000000000000000000080" +
		strings.Repeat("f", 64) +
		"4d4947524154453a3235333835363337" +
		"0000000000000000000000000000000000000000000000000000000000000000"
	analysis := store.TransactionAnalysis{To: THORChainRouter, Input: input, Succeeded: true}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("parseTHORChainCall panicked: %v", recovered)
		}
	}()
	if parseTHORChainCall(&analysis) {
		t.Fatal("oversized memo should be rejected")
	}
}

func TestParseTHORChainCallRejectsFailedTransaction(t *testing.T) {
	analysis := store.TransactionAnalysis{To: THORChainRouter, Input: "0x574da717", Succeeded: false}
	if parseTHORChainCall(&analysis) {
		t.Fatal("failed transaction should not produce protocol evidence")
	}
}

func thorchainTransferOutInput(destination, asset string, amount *big.Int, memo string) string {
	addressWord := func(value string) string {
		return fmt.Sprintf("%064s", strings.TrimPrefix(strings.ToLower(value), "0x"))
	}
	memoHex := fmt.Sprintf("%x", []byte(memo))
	memoHex += strings.Repeat("0", (64-len(memoHex)%64)%64)
	return "0x574da717" + addressWord(destination) + addressWord(asset) + fmt.Sprintf("%064x", amount) + fmt.Sprintf("%064x", 128) + fmt.Sprintf("%064x", len(memo)) + memoHex
}
