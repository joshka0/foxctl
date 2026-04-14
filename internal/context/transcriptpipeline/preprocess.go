package transcriptpipeline

import (
	"context"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/companion"
	actormemory "github.com/joshka0/foxctl/internal/runtime/actor/memory"
	"github.com/joshka0/foxctl/internal/storage/transcriptcache"
	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

// PreprocessOptions configures transcript artifact preprocessing.
type PreprocessOptions struct {
	Mode                       string
	Model                      string
	Provider                   string
	BaseURL                    string
	ReferencePromptVersion     string
	ToolOutputPromptVersion    string
	Timeout                    time.Duration
	MaxContextTokens           int
	ToolOutputSummaryMinTokens int
}

// PreprocessResult is the normalized transcript plus emitted artifact cache reports.
type PreprocessResult struct {
	Parsed    sourceimport.ParsedSession
	Artifacts []ArtifactCacheReport
}

// PreprocessParsedSession normalizes reference blobs and large tool outputs
// into cached prederived artifacts before later packetization and derivation.
func PreprocessParsedSession(ctx context.Context, parsed sourceimport.ParsedSession, cacheStore *transcriptcache.Store, opts PreprocessOptions) (PreprocessResult, error) {
	out := parsed
	out.Turns = make([]run.TurnRecord, len(parsed.Turns))
	reports := make([]ArtifactCacheReport, 0)

	for i, turn := range parsed.Turns {
		cloned := turn.Clone()
		prompt, report, err := preprocessTranscriptText(ctx, "user", cloned.Prompt, cacheStore, opts)
		if err != nil {
			return PreprocessResult{}, err
		}
		cloned.Prompt = prompt
		if report != nil {
			reports = append(reports, *report)
		}

		cloned.FinalOutput.Text, report, err = preprocessTranscriptText(ctx, "assistant", cloned.FinalOutput.Text, cacheStore, opts)
		if err != nil {
			return PreprocessResult{}, err
		}
		if report != nil {
			reports = append(reports, *report)
		}

		minTokens := opts.ToolOutputSummaryMinTokens
		if minTokens <= 0 {
			minTokens = 1200
		}
		for iterIdx := range cloned.Iterations {
			for toolIdx := range cloned.Iterations[iterIdx].ToolCalls {
				result := strings.TrimSpace(cloned.Iterations[iterIdx].ToolCalls[toolIdx].ResultRef.Text)
				if result == "" || actormemory.EstimateTokens(result) < minTokens {
					continue
				}
				summary, report, err := preprocessToolOutputText(ctx, result, cacheStore, opts)
				if err != nil {
					return PreprocessResult{}, err
				}
				cloned.Iterations[iterIdx].ToolCalls[toolIdx].ResultRef.Text = summary
				if report != nil {
					reports = append(reports, *report)
				}
			}
		}
		out.Turns[i] = cloned
	}

	return PreprocessResult{Parsed: out, Artifacts: reports}, nil
}

func preprocessTranscriptText(ctx context.Context, role string, text string, cacheStore *transcriptcache.Store, opts PreprocessOptions) (string, *ArtifactCacheReport, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil, nil
	}
	if companion.IsTranscriptControlText(text) {
		return "", nil, nil
	}
	if strings.TrimSpace(role) != "user" {
		return text, nil, nil
	}

	normalizedBlob, ok := companion.ExtractReferenceBlob(text)
	if !ok {
		return text, nil, nil
	}

	sourceHash := transcriptcache.DigestText(text)
	normalizedHash := transcriptcache.DigestText(normalizedBlob)
	promptVersion := strings.TrimSpace(opts.ReferencePromptVersion)
	if promptVersion == "" {
		promptVersion = "reference_blob_summary_v1"
	}

	modelID := resolveModelID(opts.Mode, WorkerConfig{
		Provider: opts.Provider,
		Model:    opts.Model,
	})
	if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, companion.TranscriptArtifactKindReferenceBlob, normalizedHash, promptVersion, modelID); err != nil {
		return "", nil, err
	} else if hit {
		return entry.Summary, &ArtifactCacheReport{
			ArtifactKind:   companion.TranscriptArtifactKindReferenceBlob,
			NormalizedHash: normalizedHash,
			SourceHash:     sourceHash,
			DerivationMode: entry.DerivationMode,
			ModelID:        entry.ModelID,
			CacheHit:       true,
			SummaryPreview: truncatePacketInline(entry.Summary, 140),
		}, nil
	}

	deterministicSummary := companion.SummarizeReferenceBlobDeterministic(normalizedBlob)
	entry := transcriptcache.Entry{
		ArtifactKind:   companion.TranscriptArtifactKindReferenceBlob,
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		PromptVersion:  promptVersion,
		SourcePreview:  truncatePacketInline(deterministicSummary, 120),
	}

	switch normalizeMode(opts.Mode) {
	case "deterministic":
		entry.DerivationMode = "deterministic"
		entry.ModelID = deterministicModelID()
		entry.Summary = deterministicSummary
	default:
		result, err := RunLLMTask(ctx, workerConfigFromPreprocess(opts), Task{
			Stage:         StageCompress,
			InputKind:     "reference_blob",
			PromptVersion: promptVersion,
			SystemPrompt:  "Summarize a pasted reference document for downstream transcript memory derivation. Return only one compact paragraph under 60 words. Focus on durable ideas, not incidental details.",
			ArtifactText:  normalizedBlob,
			MaxTokens:     180,
		})
		if err != nil {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
			entry.Summary = deterministicSummary
		} else {
			entry.DerivationMode = "lmstudio"
			entry.ModelID = result.ModelID
			entry.Summary = truncatePacketInline(result.OutputText, 240)
		}
	}
	if err := cacheStore.Put(ctx, entry); err != nil {
		return "", nil, err
	}
	return entry.Summary, &ArtifactCacheReport{
		ArtifactKind:   companion.TranscriptArtifactKindReferenceBlob,
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: entry.DerivationMode,
		ModelID:        entry.ModelID,
		CacheHit:       false,
		SummaryPreview: truncatePacketInline(entry.Summary, 140),
	}, nil
}

func preprocessToolOutputText(ctx context.Context, text string, cacheStore *transcriptcache.Store, opts PreprocessOptions) (string, *ArtifactCacheReport, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil, nil
	}
	sourceHash := transcriptcache.DigestText(text)
	normalizedHash := transcriptcache.DigestText(text)
	promptVersion := strings.TrimSpace(opts.ToolOutputPromptVersion)
	if promptVersion == "" {
		promptVersion = "tool_output_summary_v1"
	}
	modelID := resolveModelID(opts.Mode, WorkerConfig{
		Provider: opts.Provider,
		Model:    opts.Model,
	})

	if entry, hit, err := cacheStore.GetByNormalizedHash(ctx, "tool_output", normalizedHash, promptVersion, modelID); err != nil {
		return "", nil, err
	} else if hit {
		return entry.Summary, &ArtifactCacheReport{
			ArtifactKind:   "tool_output",
			NormalizedHash: normalizedHash,
			SourceHash:     sourceHash,
			DerivationMode: entry.DerivationMode,
			ModelID:        entry.ModelID,
			CacheHit:       true,
			SummaryPreview: truncatePacketInline(entry.Summary, 140),
		}, nil
	}

	deterministicSummary := summarizeToolOutputDeterministic(text)
	entry := transcriptcache.Entry{
		ArtifactKind:   "tool_output",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		PromptVersion:  promptVersion,
		SourcePreview:  truncatePacketInline(deterministicSummary, 120),
	}

	switch normalizeMode(opts.Mode) {
	case "deterministic":
		entry.DerivationMode = "deterministic"
		entry.ModelID = deterministicModelID()
		entry.Summary = deterministicSummary
	default:
		result, err := RunLLMTask(ctx, workerConfigFromPreprocess(opts), Task{
			Stage:         StageCompress,
			InputKind:     "tool_output",
			PromptVersion: promptVersion,
			SystemPrompt:  "Summarize a large tool output for downstream transcript memory derivation. Return one compact paragraph under 60 words. Capture only the durable result, error, or state change.",
			ArtifactText:  text,
			MaxTokens:     180,
		})
		if err != nil {
			entry.DerivationMode = "deterministic_fallback"
			entry.ModelID = deterministicModelID()
			entry.Summary = deterministicSummary
		} else {
			entry.DerivationMode = "lmstudio"
			entry.ModelID = result.ModelID
			entry.Summary = truncatePacketInline(result.OutputText, 240)
		}
	}
	if err := cacheStore.Put(ctx, entry); err != nil {
		return "", nil, err
	}
	return entry.Summary, &ArtifactCacheReport{
		ArtifactKind:   "tool_output",
		NormalizedHash: normalizedHash,
		SourceHash:     sourceHash,
		DerivationMode: entry.DerivationMode,
		ModelID:        entry.ModelID,
		CacheHit:       false,
		SummaryPreview: truncatePacketInline(entry.Summary, 140),
	}, nil
}

func workerConfigFromPreprocess(opts PreprocessOptions) WorkerConfig {
	maxContext := opts.MaxContextTokens
	if maxContext <= 0 {
		maxContext = 100000
	}
	return WorkerConfig{
		Provider:         firstNonEmpty(strings.TrimSpace(opts.Provider), "lmstudio"),
		Model:            strings.TrimSpace(opts.Model),
		BaseURL:          strings.TrimSpace(opts.BaseURL),
		MaxContextTokens: maxContext,
		Timeout:          opts.Timeout,
	}
}

func summarizeToolOutputDeterministic(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	parts := make([]string, 0, 3)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, truncatePacketInline(line, 120))
		if len(parts) >= 3 {
			break
		}
	}
	prefix := "Tool output summary"
	lower := strings.ToLower(text)
	if strings.Contains(lower, "error") || strings.Contains(lower, "fail") {
		prefix = "Tool output summary (error)"
	}
	if len(parts) == 0 {
		return prefix + ": large tool output omitted"
	}
	return prefix + ": " + strings.Join(parts, " | ")
}
