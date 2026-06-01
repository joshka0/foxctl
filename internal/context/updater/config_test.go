package updater

import (
	"math"
	"testing"
	"testing/quick"
)

func TestConfigValidateDefaultsInvalidThresholds(t *testing.T) {
	defaults := DefaultConfig()

	tests := []struct {
		name          string
		drift         float32
		confidenceMin float32
	}{
		{name: "zero", drift: 0, confidenceMin: 0},
		{name: "negative", drift: -0.1, confidenceMin: -0.1},
		{name: "above one", drift: 1.1, confidenceMin: 1.1},
		{name: "nan", drift: float32(math.NaN()), confidenceMin: float32(math.NaN())},
		{name: "positive infinity", drift: float32(math.Inf(1)), confidenceMin: float32(math.Inf(1))},
		{name: "negative infinity", drift: float32(math.Inf(-1)), confidenceMin: float32(math.Inf(-1))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.DriftThreshold = tt.drift
			cfg.ConfidenceMin = tt.confidenceMin

			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}

			if cfg.DriftThreshold != defaults.DriftThreshold {
				t.Fatalf("DriftThreshold = %v, want default %v", cfg.DriftThreshold, defaults.DriftThreshold)
			}
			if cfg.ConfidenceMin != defaults.ConfidenceMin {
				t.Fatalf("ConfidenceMin = %v, want default %v", cfg.ConfidenceMin, defaults.ConfidenceMin)
			}
		})
	}
}

func TestConfigValidatePropertyPreservesValidThresholds(t *testing.T) {
	property := func(driftRaw, confidenceRaw uint8) bool {
		drift := float32(driftRaw%100+1) / 100
		confidenceMin := float32(confidenceRaw%100+1) / 100

		cfg := DefaultConfig()
		cfg.DriftThreshold = drift
		cfg.ConfidenceMin = confidenceMin

		if err := cfg.Validate(); err != nil {
			return false
		}
		return cfg.DriftThreshold == drift && cfg.ConfidenceMin == confidenceMin
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("Validate threshold preservation property failed: %v", err)
	}
}
