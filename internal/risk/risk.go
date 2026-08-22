package risk

import (
	"math"
	"strings"

	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

const (
	PropagationVersion = "propagation-v1"
	RiskVersion        = "risk-v1"
)

type InferredLabel struct {
	Address    string   `json:"address"`
	Type       string   `json:"type"`
	Source     string   `json:"source"`
	Confidence float64  `json:"confidence"`
	Direction  string   `json:"direction"`
	Distance   int      `json:"distance"`
	Path       []string `json:"path"`
	TxHashes   []string `json:"txHashes"`
}

type Evidence struct {
	Address    string   `json:"address"`
	LabelType  string   `json:"labelType"`
	BaseScore  int      `json:"baseScore"`
	Score      int      `json:"score"`
	Confidence float64  `json:"confidence"`
	Distance   int      `json:"distance"`
	Direction  string   `json:"direction"`
	Path       []string `json:"path"`
	TxHashes   []string `json:"txHashes"`
	Rule       string   `json:"rule"`
}

type Result struct {
	Score              int             `json:"score"`
	Level              string          `json:"level"`
	InferredLabels     []InferredLabel `json:"inferredLabels,omitempty"`
	Evidence           []Evidence      `json:"evidence,omitempty"`
	RuleVersion        string          `json:"ruleVersion"`
	PropagationVersion string          `json:"propagationVersion"`
}

type step struct {
	address   string
	distance  int
	direction string
	path      []string
	hashes    []string
}

func Analyze(seed string, edges []store.Transfer, labels []store.Label) Result {
	adj := make(map[string][]store.Transfer)
	for _, edge := range edges {
		adj[strings.ToLower(edge.From)] = append(adj[strings.ToLower(edge.From)], edge)
		adj[strings.ToLower(edge.To)] = append(adj[strings.ToLower(edge.To)], edge)
	}
	result := Result{Level: "no_conclusion", RuleVersion: RiskVersion, PropagationVersion: PropagationVersion}
	for _, label := range labels {
		if label.Source != "manual" && label.Source != "public-list" {
			continue
		}
		base := 40
		if label.RiskLevel == "high" || strings.Contains(strings.ToLower(label.Type), "hacker") || strings.Contains(strings.ToLower(label.Type), "phishing") {
			base = 70
		}
		queue := []step{{address: strings.ToLower(label.Address), distance: 0, direction: "direct", path: []string{label.Address}}}
		seen := map[string]bool{strings.ToLower(label.Address): true}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if current.address != strings.ToLower(label.Address) {
				confidence := labelConfidence(label.Confidence) * decay(current.direction, current.distance)
				if confidence >= 0.5 {
					inferred := InferredLabel{Address: current.address, Type: label.Type, Source: "propagation", Confidence: confidence, Direction: current.direction, Distance: current.distance, Path: append([]string(nil), current.path...), TxHashes: append([]string(nil), current.hashes...)}
					result.InferredLabels = append(result.InferredLabels, inferred)
				}
			}
			for _, edge := range adj[current.address] {
				next := strings.ToLower(edge.To)
				direction := "downstream"
				if strings.ToLower(edge.To) == current.address {
					next = strings.ToLower(edge.From)
					direction = "upstream"
				}
				if seen[next] || next == "" {
					continue
				}
				seen[next] = true
				path := append(append([]string(nil), current.path...), next)
				hashes := append(append([]string(nil), current.hashes...), edge.TxHash)
				queue = append(queue, step{address: next, distance: current.distance + 1, direction: direction, path: path, hashes: hashes})
			}
		}
		if label.Address == seed {
			confidence := labelConfidence(label.Confidence)
			score := int(math.Round(float64(base) * confidence))
			if score > result.Score {
				result.Score = score
			}
			result.Evidence = append(result.Evidence, Evidence{Address: seed, LabelType: label.Type, BaseScore: base, Score: score, Confidence: confidence, Distance: 0, Direction: "direct", Path: []string{seed}, TxHashes: append([]string(nil), label.Evidence...), Rule: RiskVersion})
		}
	}
	for _, inferred := range result.InferredLabels {
		base := 40
		for _, label := range labels {
			if label.Type == inferred.Type && (label.RiskLevel == "high" || strings.Contains(strings.ToLower(label.Type), "hacker") || strings.Contains(strings.ToLower(label.Type), "phishing")) {
				base = 70
			}
		}
		score := int(math.Round(float64(base) * inferred.Confidence))
		if score > result.Score {
			result.Score = score
		}
		result.Evidence = append(result.Evidence, Evidence{Address: inferred.Address, LabelType: inferred.Type, BaseScore: base, Score: score, Confidence: inferred.Confidence, Distance: inferred.Distance, Direction: inferred.Direction, Path: inferred.Path, TxHashes: inferred.TxHashes, Rule: RiskVersion})
	}
	if result.Score >= 70 {
		result.Level = "known_high"
	} else if result.Score >= 40 {
		result.Level = "suspected"
	}
	return result
}

func labelConfidence(value float64) float64 {
	if value <= 0 {
		return 1
	}
	if value > 1 {
		return 1
	}
	return value
}
func decay(direction string, distance int) float64 {
	if distance == 0 {
		return 1
	}
	factor := 0.6
	if direction == "downstream" {
		factor = 0.8
	}
	return math.Pow(factor, float64(distance))
}
