package transactionanalysis

import (
	"testing"

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
	analysis := store.TransactionAnalysis{To: THORChainRouter, Input: input}
	parseTHORChainCall(&analysis)
	if analysis.ProtocolAction != "vault_migration" || analysis.ProtocolMemo != "MIGRATE:25385637" || analysis.ProtocolDestination != "0x52425d6e839582bdda85d4bca83d347504de3acd" {
		t.Fatalf("decoded THORChain call = %+v", analysis)
	}
}
