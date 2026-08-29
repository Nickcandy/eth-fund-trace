package risk

import (
	"math"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

const RiskVersion = "direct-label-v1"

type Evidence struct {
	Address    string   `bson:"address" json:"address"`
	LabelType  string   `bson:"labelType" json:"labelType"`
	BaseScore  int      `bson:"baseScore" json:"baseScore"`
	Score      int      `bson:"score" json:"score"`
	Confidence float64  `bson:"confidence" json:"confidence"`
	Distance   int      `bson:"distance" json:"distance"`
	Direction  string   `bson:"direction" json:"direction"`
	Path       []string `bson:"path" json:"path"`
	TxHashes   []string `bson:"txHashes" json:"txHashes"`
	Evidence   []string `bson:"evidence,omitempty" json:"evidence,omitempty"`
	Rule       string   `bson:"rule" json:"rule"`
}

type Result struct {
	Score       int        `bson:"score" json:"score"`
	Level       string     `bson:"level" json:"level"`
	Evidence    []Evidence `bson:"evidence,omitempty" json:"evidence,omitempty"`
	RuleVersion string     `bson:"ruleVersion" json:"ruleVersion"`
}

// Analyze evaluates only labels attached directly to the seed. Transfer edges
// are intentionally ignored because trace-v1 does not propagate risk.
func Analyze(seed string, _ []store.Transfer, labels []store.Label) Result {
	seed = strings.ToLower(seed)
	result := Result{Level: "no_conclusion", RuleVersion: RiskVersion}
	for _, label := range labels {
		if !strings.EqualFold(label.Address, seed) || label.Source != "manual" && label.Source != "public-list" {
			continue
		}
		base := 40
		kind := strings.ToLower(label.Type)
		if label.RiskLevel == "high" || strings.Contains(kind, "hacker") || strings.Contains(kind, "phishing") {
			base = 70
		}
		confidence := labelConfidence(label.Confidence)
		score := int(math.Round(float64(base) * confidence))
		if score > result.Score {
			result.Score = score
		}
		result.Evidence = append(result.Evidence, Evidence{Address: seed, LabelType: label.Type, BaseScore: base, Score: score, Confidence: confidence, Direction: "direct", Path: []string{seed}, Evidence: append([]string(nil), label.Evidence...), Rule: RiskVersion})
	}
	if result.Score >= 70 {
		result.Level = "known_high"
	} else if result.Score >= 40 {
		result.Level = "suspected"
	}
	return result
}

func labelConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
