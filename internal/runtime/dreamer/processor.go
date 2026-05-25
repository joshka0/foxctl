package dreamer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/context/transcriptpipeline"
	historypkg "github.com/joshka0/foxctl/internal/context/transcriptpipeline/history"
	"github.com/joshka0/foxctl/internal/platform/config"
	memstore "github.com/joshka0/foxctl/internal/storage/memory"
	obsidiantool "github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
)

type EmbedFunc historypkg.HistoryRecordEmbedFunc

type SingleInsightProcessorConfig struct {
	Runtime       transcriptpipeline.LocalModelRuntime
	StorageRoot   string
	CASPath       string
	ActorID       string
	FrameLimit    int
	MemoryStore   *memstore.Store
	Embed         EmbedFunc
	NoteWriter    DreamNoteWriter
	NoteIndexer   DreamNoteIndexer
	BlurAgent     contextplane.MemoryBlurAgent
	BlurAgentName string
	Now           func() time.Time
}

type DreamNoteWriter interface {
	CreateNote(ctx context.Context, notePath, content string, overwrite bool) error
}

type DreamNoteIndexer interface {
	IndexDreamNote(ctx context.Context, note contextplane.TranscriptDreamNote) error
}

type SingleInsightProcessor struct {
	cfg SingleInsightProcessorConfig
}

func NewSingleInsightProcessor(cfg SingleInsightProcessorConfig) (*SingleInsightProcessor, error) {
	if cfg.MemoryStore == nil {
		return nil, fmt.Errorf("dreamer: memory store is required")
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &SingleInsightProcessor{cfg: cfg}, nil
}

func NewSingleInsightProcessorFromConfig(ctx context.Context, cfg config.Config, opts SingleInsightProcessorConfig) (*SingleInsightProcessor, func() error, error) {
	store, err := memstore.OpenFromConfig(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	opts.MemoryStore = store
	if opts.StorageRoot == "" {
		opts.StorageRoot = cfg.Storage.Root
	}
	if opts.CASPath == "" {
		opts.CASPath = cfg.Paths.CAS
	}
	processor, err := NewSingleInsightProcessor(opts)
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	return processor, store.Close, nil
}

func (p *SingleInsightProcessor) Process(ctx context.Context, source Source) (ProcessResult, error) {
	source = normalizeSource(source)
	result, err := transcriptpipeline.RunSingleInsight(ctx, transcriptpipeline.SingleRunOptions{
		StorageRoot: p.cfg.StorageRoot,
		CASPath:     p.cfg.CASPath,
		Provider:    source.Provider,
		SourceFile:  source.Path,
		SessionID:   source.SessionID,
		Workspace:   source.WorkspacePath,
		ActorID:     firstNonEmpty(p.cfg.ActorID, "actor:system:dreamer"),
		FrameLimit:  p.cfg.FrameLimit,
		Runtime:     p.cfg.Runtime,
	})
	if err != nil {
		return ProcessResult{}, err
	}
	persisted, err := historypkg.PersistSingleInsightHistory(ctx, p.cfg.MemoryStore, historypkg.SingleInsightHistory{
		WorkspacePath:  result.Parsed.WorkspacePath,
		SessionID:      result.Parsed.SessionID,
		ConversationID: result.ConversationID,
		Records:        result.HistoryRecords,
	}, historypkg.HistoryRecordEmbedFunc(p.cfg.Embed))
	if err != nil {
		return ProcessResult{}, err
	}

	dreamNotes := 0
	indexedDreamNotes := 0
	blurred := false
	if p.cfg.NoteWriter != nil && len(persisted.Persisted) > 0 {
		noteInput := PlanSingleInsightDreamNoteInput(result, persisted.Persisted, p.cfg.Now(), source)
		if p.cfg.BlurAgent != nil {
			var ok bool
			noteInput, ok, err = blurSingleInsightDreamNoteInput(ctx, p.cfg.BlurAgent, p.cfg.BlurAgentName, noteInput)
			if err != nil {
				return ProcessResult{}, err
			}
			blurred = ok
		}
		note, err := contextplane.PlanTranscriptDreamNote(noteInput)
		if err != nil {
			return ProcessResult{}, err
		}
		if err := writeDreamNote(ctx, p.cfg.NoteWriter, note); err != nil {
			return ProcessResult{}, err
		}
		dreamNotes = 1
		if p.cfg.NoteIndexer != nil {
			if err := indexDreamNote(ctx, p.cfg.NoteIndexer, note); err != nil {
				return ProcessResult{}, err
			}
			indexedDreamNotes = 1
		}
	}
	return ProcessResult{
		HistoryRecords:    len(persisted.Persisted),
		DreamNotes:        dreamNotes,
		IndexedDreamNotes: indexedDreamNotes,
		Blurred:           blurred,
	}, nil
}

func PlanSingleInsightDreamNote(result transcriptpipeline.SingleRunResult, persisted []historypkg.PersistedHistoryRecord, now time.Time, source Source) (contextplane.TranscriptDreamNote, error) {
	return contextplane.PlanTranscriptDreamNote(PlanSingleInsightDreamNoteInput(result, persisted, now, source))
}

func PlanSingleInsightDreamNoteInput(result transcriptpipeline.SingleRunResult, persisted []historypkg.PersistedHistoryRecord, now time.Time, source Source) contextplane.TranscriptDreamNoteInput {
	sourceRefs := make([]contextplane.TranscriptDreamSourceRef, 0, len(result.HistoryRecords))
	for _, record := range result.HistoryRecords {
		if !persistedContainsRecord(persisted, record.RecordID) {
			continue
		}
		sourceRefs = append(sourceRefs, contextplane.TranscriptDreamSourceRef{
			RecordID:        record.RecordID,
			Kind:            record.Kind,
			ConversationID:  record.ConversationID,
			SessionIDs:      record.SessionIDs,
			SourceStartedAt: record.SourceStartedAt,
			Summary:         record.Summary,
			EvidenceRefs:    record.EvidenceRefs,
		})
	}
	mechanisms := dreamMechanismsFromResult(result)
	return contextplane.TranscriptDreamNoteInput{
		Project:           firstNonEmpty(result.Parsed.WorkspacePath, result.WorkspaceFamilyPath, "workspace"),
		WorkspaceID:       compactDreamTag(firstNonEmpty(result.Parsed.WorkspacePath, result.WorkspaceFamilyPath, "workspace")),
		Provider:          firstNonEmpty(source.Provider, string(result.Parsed.Provider)),
		SessionID:         firstNonEmpty(source.SessionID, result.Parsed.SessionID),
		SourceDigest:      source.Fingerprint,
		Now:               now,
		Title:             firstNonEmpty(singleInsightDreamTitle(result), "Transcript dream"),
		DistilledSummary:  singleInsightDreamSummary(result),
		BlurredMechanisms: mechanisms,
		SourceRefs:        sourceRefs,
		Review: contextplane.TranscriptDreamReview{
			Required:    true,
			GeneratedBy: "foxctl dreamer",
		},
	}
}

func blurSingleInsightDreamNoteInput(ctx context.Context, agent contextplane.MemoryBlurAgent, agentName string, input contextplane.TranscriptDreamNoteInput) (contextplane.TranscriptDreamNoteInput, bool, error) {
	if agent == nil {
		return input, false, nil
	}
	promptInput, err := contextplane.BuildTranscriptDreamBlurAgentPromptInput(input)
	if err != nil {
		return input, false, err
	}
	output, _, err := agent.BlurMemory(ctx, promptInput)
	if err != nil {
		return input, false, fmt.Errorf("dream blur agent: %w", err)
	}
	blurred, validation := contextplane.ApplyTranscriptDreamAgentBlur(input, promptInput, agentName, output)
	if !validation.Valid {
		if len(validation.LeakedTerms) > 0 {
			return input, false, fmt.Errorf("dream blur agent validation failed: %s (leaked_terms=%s)", strings.Join(validation.Errors, "; "), strings.Join(validation.LeakedTerms, ", "))
		}
		return input, false, fmt.Errorf("dream blur agent validation failed: %s", strings.Join(validation.Errors, "; "))
	}
	return blurred, true, nil
}

func NewObsidianDreamNoteWriter(vaultPath string) *obsidiantool.Writer {
	writer := obsidiantool.NewWriter("", "", obsidiantool.DefaultPolicy())
	writer.VaultPath = strings.TrimSpace(vaultPath)
	writer.PostCreateDelay = 0
	return writer
}

func writeDreamNote(ctx context.Context, writer DreamNoteWriter, note contextplane.TranscriptDreamNote) error {
	return writer.CreateNote(ctx, note.DraftPath, note.Content, true)
}

func indexDreamNote(ctx context.Context, indexer DreamNoteIndexer, note contextplane.TranscriptDreamNote) error {
	return indexer.IndexDreamNote(ctx, note)
}

func persistedContainsRecord(persisted []historypkg.PersistedHistoryRecord, recordID string) bool {
	recordID = strings.TrimSpace(recordID)
	for _, item := range persisted {
		if strings.TrimSpace(item.RecordID) == recordID {
			return true
		}
	}
	return false
}

func dreamMechanismsFromResult(result transcriptpipeline.SingleRunResult) []contextplane.TranscriptDreamMechanism {
	out := make([]contextplane.TranscriptDreamMechanism, 0, len(result.NotableInsights)+len(result.Insights))
	for _, notable := range result.NotableInsights {
		out = append(out, contextplane.TranscriptDreamMechanism{
			Name:    string(notable.Kind),
			Summary: firstNonEmpty(notable.WhyItMatters, notable.Headline),
			Tags:    compactDreamTags([]string{string(notable.Kind), "notable_window"}),
		})
	}
	for _, insight := range result.Insights {
		out = append(out, contextplane.TranscriptDreamMechanism{
			Name:       string(insight.Kind),
			Summary:    insight.Summary,
			Confidence: insight.Confidence,
			Tags:       compactDreamTags(append([]string{string(insight.Kind), string(insight.Status)}, insight.Tags...)),
		})
	}
	if len(out) == 0 && result.InsightBrief != nil {
		out = append(out, contextplane.TranscriptDreamMechanism{
			Name:    "insight_brief",
			Summary: result.InsightBrief.Overview,
			Tags:    []string{"insight_brief"},
		})
	}
	return out
}

func singleInsightDreamTitle(result transcriptpipeline.SingleRunResult) string {
	if result.Objective != nil && strings.TrimSpace(result.Objective.Objective) != "" {
		return result.Objective.Objective
	}
	if result.InsightBrief != nil && strings.TrimSpace(result.InsightBrief.Overview) != "" {
		return result.InsightBrief.Overview
	}
	return result.Parsed.SessionID
}

func singleInsightDreamSummary(result transcriptpipeline.SingleRunResult) string {
	if result.InsightBrief != nil && strings.TrimSpace(result.InsightBrief.Overview) != "" {
		return result.InsightBrief.Overview
	}
	for _, insight := range result.Insights {
		if strings.TrimSpace(insight.Summary) != "" {
			return insight.Summary
		}
	}
	for _, record := range result.HistoryRecords {
		if strings.TrimSpace(record.Summary) != "" {
			return record.Summary
		}
	}
	return "Transcript-derived history records were distilled for review."
}

func compactDreamTags(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = compactDreamTag(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func compactDreamTag(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "/", "_")
	return strings.Trim(value, "_")
}

var _ Processor = (*SingleInsightProcessor)(nil)
