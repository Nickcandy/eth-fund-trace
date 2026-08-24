package store

import "testing"

func TestMergeCrossChainLinkPreservesCompletedEvidence(t *testing.T) {
	existing := CrossChainLink{Status: "completed", TargetTxHash: "0xtarget", TargetLogIndex: 4, Evidence: []string{"source", "target"}}
	incoming := CrossChainLink{Status: "initiated", Evidence: []string{"source", "retry"}}
	merged := mergeCrossChainLink(existing, incoming)
	if merged.Status != "completed" || merged.TargetTxHash != "0xtarget" || len(merged.Evidence) != 3 {
		t.Fatalf("merged=%+v", merged)
	}
}
