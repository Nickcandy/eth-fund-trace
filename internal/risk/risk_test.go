package risk

import (
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

func TestAnalyzeScoresOnlyDirectSeedLabels(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	middle := "0x0000000000000000000000000000000000000002"
	target := "0x0000000000000000000000000000000000000003"
	result := Analyze(seed, []store.Transfer{
		{TxHash: "0x1", From: seed, To: middle, Asset: "ETH", Amount: "1"},
		{TxHash: "0x2", From: middle, To: target, Asset: "ETH", Amount: "1"},
	}, []store.Label{{Address: seed, Type: "hacker", Source: "manual", RiskLevel: "high", Confidence: 1}})
	if result.Level != "known_high" || result.Score != 70 {
		t.Fatalf("result=%+v", result)
	}
}

func TestAnalyzeDropsLowConfidenceAndKeepsEvidence(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	target := "0x0000000000000000000000000000000000000002"
	result := Analyze(seed, []store.Transfer{{TxHash: "0x1", From: seed, To: target, Asset: "ETH", Amount: "1"}}, []store.Label{{Address: seed, Type: "exchange", Source: "manual", RiskLevel: "high", Confidence: 0.5, Evidence: []string{"case-1"}}})
	if result.Score != 35 || result.Level != "no_conclusion" {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].Evidence[0] != "case-1" || len(result.Evidence[0].TxHashes) != 0 {
		t.Fatalf("evidence=%+v", result.Evidence)
	}
}

func TestAnalyzeIgnoresLabelsOnNeighborAddresses(t *testing.T) {
	seed := "0x0000000000000000000000000000000000000001"
	target := "0x0000000000000000000000000000000000000002"
	result := Analyze(seed, []store.Transfer{{TxHash: "0x1", From: target, To: seed}}, []store.Label{{Address: target, Type: "hacker", Source: "manual", RiskLevel: "high", Confidence: 1}})
	if result.Score != 0 || result.Level != "no_conclusion" || len(result.Evidence) != 0 {
		t.Fatalf("neighbor label propagated: %+v", result)
	}
}
