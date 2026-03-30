package retrievaleval

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type ModeThreshold struct {
	MinHitRateAt5         float64 `yaml:"min_hit_rate_at_5,omitempty" json:"min_hit_rate_at_5,omitempty"`
	MinHitRateAt10        float64 `yaml:"min_hit_rate_at_10,omitempty" json:"min_hit_rate_at_10,omitempty"`
	MinMeanReciprocalRank float64 `yaml:"min_mrr,omitempty" json:"min_mrr,omitempty"`
}

type Policy struct {
	Suite        string                   `yaml:"suite,omitempty" json:"suite,omitempty"`
	Limit        int                      `yaml:"limit,omitempty" json:"limit,omitempty"`
	Format       string                   `yaml:"format,omitempty" json:"format,omitempty"`
	Modes        []string                 `yaml:"modes,omitempty" json:"modes,omitempty"`
	FailOnAlerts bool                     `yaml:"fail_on_alerts,omitempty" json:"fail_on_alerts,omitempty"`
	Thresholds   map[string]ModeThreshold `yaml:"thresholds,omitempty" json:"thresholds,omitempty"`
}

type Alert struct {
	Mode    string  `json:"mode"`
	Metric  string  `json:"metric"`
	Actual  float64 `json:"actual"`
	Minimum float64 `json:"minimum"`
	Message string  `json:"message"`
}

func LoadPolicy(path string) (Policy, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	var out Policy
	if err := yaml.Unmarshal(body, &out); err != nil {
		return Policy{}, fmt.Errorf("decode retrieval policy yaml: %w", err)
	}
	return out, nil
}

func BuildAlerts(summaries []Summary, policy Policy) []Alert {
	if len(policy.Thresholds) == 0 {
		return nil
	}
	thresholdsByMode := make(map[string]ModeThreshold, len(policy.Thresholds))
	for mode, threshold := range policy.Thresholds {
		key := strings.ToLower(strings.TrimSpace(mode))
		if key == "" {
			continue
		}
		thresholdsByMode[key] = threshold
	}
	if len(thresholdsByMode) == 0 {
		return nil
	}
	alerts := make([]Alert, 0, len(thresholdsByMode)*3)
	for _, summary := range summaries {
		threshold, ok := thresholdsByMode[strings.ToLower(strings.TrimSpace(summary.Mode))]
		if !ok {
			continue
		}
		if threshold.MinHitRateAt5 > 0 && summary.HitRateAt5 < threshold.MinHitRateAt5 {
			alerts = append(alerts, Alert{
				Mode:    summary.Mode,
				Metric:  "hit_rate_at_5",
				Actual:  summary.HitRateAt5,
				Minimum: threshold.MinHitRateAt5,
				Message: fmt.Sprintf("%s hit@5 %.2f below %.2f", summary.Mode, summary.HitRateAt5, threshold.MinHitRateAt5),
			})
		}
		if threshold.MinHitRateAt10 > 0 && summary.HitRateAt10 < threshold.MinHitRateAt10 {
			alerts = append(alerts, Alert{
				Mode:    summary.Mode,
				Metric:  "hit_rate_at_10",
				Actual:  summary.HitRateAt10,
				Minimum: threshold.MinHitRateAt10,
				Message: fmt.Sprintf("%s hit@10 %.2f below %.2f", summary.Mode, summary.HitRateAt10, threshold.MinHitRateAt10),
			})
		}
		if threshold.MinMeanReciprocalRank > 0 && summary.MeanReciprocalRank < threshold.MinMeanReciprocalRank {
			alerts = append(alerts, Alert{
				Mode:    summary.Mode,
				Metric:  "mean_reciprocal_rank",
				Actual:  summary.MeanReciprocalRank,
				Minimum: threshold.MinMeanReciprocalRank,
				Message: fmt.Sprintf("%s MRR %.2f below %.2f", summary.Mode, summary.MeanReciprocalRank, threshold.MinMeanReciprocalRank),
			})
		}
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		if alerts[i].Mode == alerts[j].Mode {
			return alerts[i].Metric < alerts[j].Metric
		}
		return alerts[i].Mode < alerts[j].Mode
	})
	return alerts
}
