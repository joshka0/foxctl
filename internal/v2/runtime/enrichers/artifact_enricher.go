package enrichers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/v2/adapters/libsql/turns"
	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

// ErrMissingArtifactWriter indicates artifact writer dependency is nil.
var ErrMissingArtifactWriter = fmt.Errorf("v2 enrichers: missing artifact writer")

// ArtifactWriter persists one derived turn artifact.
type ArtifactWriter interface {
	SaveArtifact(ctx context.Context, artifact turns.Artifact) error
}

// ArtifactEnricherConfig configures runtime turn->artifact derivation.
type ArtifactEnricherConfig struct {
	ArtifactWriter   ArtifactWriter
	Embedder         sourceimport.Embedder
	IncludeEmbedding bool
	Provider         sourceimport.Provider
	Now              func() time.Time
	OnWarnings       func(job Job, warnings []string)
}

// ArtifactEnricher derives and persists one artifact for each enrichment job.
type ArtifactEnricher struct {
	artifactWriter   ArtifactWriter
	embedder         sourceimport.Embedder
	includeEmbedding bool
	provider         sourceimport.Provider
	now              func() time.Time
	onWarnings       func(job Job, warnings []string)
}

// NewArtifactEnricher creates a runtime artifact enricher.
func NewArtifactEnricher(cfg ArtifactEnricherConfig) *ArtifactEnricher {
	if cfg.Provider == "" {
		cfg.Provider = sourceimport.ProviderAuto
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &ArtifactEnricher{
		artifactWriter:   cfg.ArtifactWriter,
		embedder:         cfg.Embedder,
		includeEmbedding: cfg.IncludeEmbedding,
		provider:         cfg.Provider,
		now:              cfg.Now,
		onWarnings:       cfg.OnWarnings,
	}
}

// Enrich derives and writes one artifact matching the job key.
func (e *ArtifactEnricher) Enrich(ctx context.Context, job Job) error {
	if e == nil || e.artifactWriter == nil {
		return ErrMissingArtifactWriter
	}

	artifactType := strings.ToLower(strings.TrimSpace(job.ArtifactType))
	if artifactType == "" {
		return fmt.Errorf("v2 enrichers: artifact type is required")
	}
	artifactVersion := strings.TrimSpace(job.ArtifactVersion)
	if artifactVersion == "" {
		artifactVersion = "v1"
	}

	includeEmbedding := e.includeEmbedding
	if artifactType == turns.ArtifactTypeEmbedding && !e.includeEmbedding {
		return fmt.Errorf("v2 enrichers: embedding artifacts disabled")
	}

	parsed := sourceimport.ParsedSession{
		Provider:  e.provider,
		SessionID: strings.TrimSpace(job.Turn.SessionID),
		Turns:     []run.TurnRecord{job.Turn.Clone()},
	}
	derived := sourceimport.BuildArtifacts(ctx, parsed, sourceimport.ArtifactBuildOptions{
		IncludeEmbedding: includeEmbedding,
		Embedder:         e.embedder,
		ArtifactSource:   "runtime",
		Now:              e.now,
	})

	for _, artifact := range derived.Artifacts {
		if artifact.ArtifactType != artifactType {
			continue
		}
		if strings.TrimSpace(artifact.ArtifactVersion) != artifactVersion {
			continue
		}
		if err := e.artifactWriter.SaveArtifact(ctx, artifact); err != nil {
			return err
		}
		e.reportWarnings(job, derived.Warnings)
		return nil
	}

	if len(derived.Warnings) > 0 {
		return fmt.Errorf("v2 enrichers: artifact %s/%s not produced: %s", artifactType, artifactVersion, strings.Join(derived.Warnings, "; "))
	}
	return fmt.Errorf("v2 enrichers: artifact %s/%s not produced", artifactType, artifactVersion)
}

func (e *ArtifactEnricher) reportWarnings(job Job, warnings []string) {
	if e == nil || e.onWarnings == nil || len(warnings) == 0 {
		return
	}
	filtered := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" {
			continue
		}
		filtered = append(filtered, warning)
	}
	if len(filtered) == 0 {
		return
	}
	e.onWarnings(job, filtered)
}
