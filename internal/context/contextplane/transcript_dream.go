package contextplane

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/transcriptpipeline/history"
)

const transcriptDreamSourceLane = "transcript_dream"

// TranscriptDreamNoteInput is the Obsidian-facing contract for a distilled
// transcript dream. Callers must pass summaries and abstract mechanisms, never
// raw transcript turns.
type TranscriptDreamNoteInput struct {
	Project           string
	WorkspaceID       string
	Provider          string
	SessionID         string
	SourceDigest      string
	Now               time.Time
	Title             string
	DistilledSummary  string
	BlurredMechanisms []TranscriptDreamMechanism
	SourceRefs        []TranscriptDreamSourceRef
	AgentBlur         *TranscriptDreamAgentBlur
	Review            TranscriptDreamReview
}

type TranscriptDreamMechanism struct {
	Name       string
	Summary    string
	Confidence float64
	Tags       []string
}

type TranscriptDreamSourceRef struct {
	RecordID        string
	Kind            history.HistoryRecordKind
	ConversationID  string
	SessionIDs      []string
	SourceStartedAt time.Time
	Summary         string
	EvidenceRefs    []string
}

type TranscriptDreamReview struct {
	Status      string
	Reviewer    string
	Required    bool
	GeneratedBy string
	Notes       []string
}

type TranscriptDreamAgentBlur struct {
	Agent          string
	AbstractSchema string
	MechanismTags  []string
	DomainsToAvoid []string
	Confidence     float64
	LeakageRisk    float64
	Validation     MemoryBlurValidation
}

type TranscriptDreamNote struct {
	SourceLane   string
	Project      string
	WorkspaceID  string
	Provider     string
	SessionID    string
	SourceDigest string
	DedupeKey    string
	Title        string
	DraftPath    string
	Content      string
	CreatedAt    time.Time
}

// PlanTranscriptDreamNote owns the stable markdown/frontmatter contract for
// transcript_dream notes consumed by Obsidian review workflows.
func PlanTranscriptDreamNote(input TranscriptDreamNoteInput) (TranscriptDreamNote, error) {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	project := transcriptDreamSlug(input.Project)
	if project == "" {
		project = "workspace"
	}
	summary := strings.TrimSpace(input.DistilledSummary)
	if summary == "" {
		return TranscriptDreamNote{}, fmt.Errorf("distilled summary is required")
	}
	mechanisms := normalizeTranscriptDreamMechanisms(input.BlurredMechanisms)
	if len(mechanisms) == 0 {
		return TranscriptDreamNote{}, fmt.Errorf("at least one blurred mechanism is required")
	}
	sourceRefs := normalizeTranscriptDreamSourceRefs(input.SourceRefs)
	if len(sourceRefs) == 0 {
		return TranscriptDreamNote{}, fmt.Errorf("at least one source reference is required")
	}
	agentBlur, err := normalizeTranscriptDreamAgentBlur(input.AgentBlur)
	if err != nil {
		return TranscriptDreamNote{}, err
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "Transcript dream: " + truncatePlain(summary, 72)
	}
	dedupeKey := transcriptDreamDedupeKey(project, summary, sourceRefs)
	slug := transcriptDreamSlug(title + " " + dedupeKey[len(dedupeKey)-12:])
	draftPath := filepath.ToSlash(filepath.Join("inbox/drafted-from-foxctl/dreams", project, now.Format("2006-01-02"), slug+".md"))
	review := normalizeTranscriptDreamReview(input.Review)

	note := TranscriptDreamNote{
		SourceLane:   transcriptDreamSourceLane,
		Project:      project,
		WorkspaceID:  strings.TrimSpace(input.WorkspaceID),
		Provider:     strings.TrimSpace(input.Provider),
		SessionID:    strings.TrimSpace(input.SessionID),
		SourceDigest: strings.TrimSpace(input.SourceDigest),
		DedupeKey:    dedupeKey,
		Title:        title,
		DraftPath:    draftPath,
		CreatedAt:    now,
	}
	note.Content = renderTranscriptDreamNote(note, summary, mechanisms, sourceRefs, review, agentBlur)
	return note, nil
}

func renderTranscriptDreamNote(note TranscriptDreamNote, summary string, mechanisms []TranscriptDreamMechanism, sourceRefs []TranscriptDreamSourceRef, review TranscriptDreamReview, agentBlur *TranscriptDreamAgentBlur) string {
	var b strings.Builder
	writeTranscriptDreamFrontmatter(&b, note, mechanisms, sourceRefs, review, agentBlur)
	b.WriteString("# ")
	b.WriteString(note.Title)
	b.WriteString("\n\n")
	b.WriteString("## Distilled Summary\n")
	b.WriteString(summary)
	b.WriteString("\n\n")
	b.WriteString("## Blurred Mechanisms\n")
	for _, mechanism := range mechanisms {
		b.WriteString("- **")
		b.WriteString(mechanism.Name)
		b.WriteString("**: ")
		b.WriteString(mechanism.Summary)
		if mechanism.Confidence > 0 {
			b.WriteString(fmt.Sprintf(" _(confidence %.2f)_", mechanism.Confidence))
		}
		b.WriteString("\n")
	}
	if agentBlur != nil {
		b.WriteString("\n## Agent Blurred Mechanism\n")
		b.WriteString(agentBlur.AbstractSchema)
		b.WriteString("\n\n")
		if len(agentBlur.MechanismTags) > 0 {
			b.WriteString("Mechanism tags: ")
			b.WriteString(strings.Join(agentBlur.MechanismTags, ", "))
			b.WriteString("\n\n")
		}
		b.WriteString(fmt.Sprintf("Confidence: %.2f; leakage risk: %.2f; agent: `%s`\n", agentBlur.Confidence, agentBlur.LeakageRisk, agentBlur.Agent))
	}
	b.WriteString("\n## Source References\n")
	for _, ref := range sourceRefs {
		b.WriteString("- `")
		b.WriteString(ref.RecordID)
		b.WriteString("`")
		if ref.Kind != "" {
			b.WriteString(" (")
			b.WriteString(string(ref.Kind))
			b.WriteString(")")
		}
		if !ref.SourceStartedAt.IsZero() {
			b.WriteString(" from ")
			b.WriteString(ref.SourceStartedAt.UTC().Format(time.RFC3339))
		}
		if ref.Summary != "" {
			b.WriteString(": ")
			b.WriteString(ref.Summary)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n## Review Metadata\n")
	b.WriteString("- Status: ")
	b.WriteString(review.Status)
	b.WriteString("\n")
	b.WriteString("- Review required: ")
	b.WriteString(fmt.Sprintf("%t", review.Required))
	b.WriteString("\n")
	if review.Reviewer != "" {
		b.WriteString("- Reviewer: ")
		b.WriteString(review.Reviewer)
		b.WriteString("\n")
	}
	if review.GeneratedBy != "" {
		b.WriteString("- Generated by: ")
		b.WriteString(review.GeneratedBy)
		b.WriteString("\n")
	}
	for _, note := range review.Notes {
		b.WriteString("- Note: ")
		b.WriteString(note)
		b.WriteString("\n")
	}
	return b.String()
}

func writeTranscriptDreamFrontmatter(b *strings.Builder, note TranscriptDreamNote, mechanisms []TranscriptDreamMechanism, sourceRefs []TranscriptDreamSourceRef, review TranscriptDreamReview, agentBlur *TranscriptDreamAgentBlur) {
	b.WriteString("---\n")
	writeYAMLString(b, "type", "memory")
	writeYAMLString(b, "status", "draft")
	writeYAMLString(b, "trust", "raw")
	writeYAMLString(b, "source_lane", transcriptDreamSourceLane)
	writeYAMLString(b, "project", note.Project)
	writeYAMLString(b, "workspace_id", note.WorkspaceID)
	writeYAMLString(b, "provider", note.Provider)
	writeYAMLString(b, "session_id", note.SessionID)
	writeYAMLString(b, "source_digest", note.SourceDigest)
	writeYAMLString(b, "dedupe_key", note.DedupeKey)
	writeYAMLString(b, "created_at", note.CreatedAt.UTC().Format(time.RFC3339))
	writeYAMLString(b, "review_status", review.Status)
	writeYAMLString(b, "review_required", fmt.Sprintf("%t", review.Required))
	writeYAMLString(b, "reviewer", review.Reviewer)
	writeYAMLString(b, "generated_by", review.GeneratedBy)
	mechanismTags := transcriptDreamMechanismTags(mechanisms)
	if agentBlur != nil {
		writeYAMLString(b, "agent_blurred", "true")
		writeYAMLString(b, "blur_agent", agentBlur.Agent)
		writeYAMLString(b, "blur_confidence", fmt.Sprintf("%.2f", agentBlur.Confidence))
		writeYAMLString(b, "blur_leakage_risk", fmt.Sprintf("%.2f", agentBlur.LeakageRisk))
		mechanismTags = append(mechanismTags, agentBlur.MechanismTags...)
	}
	writeYAMLStringSlice(b, "mechanism_tags", mechanismTags)
	writeYAMLStringSlice(b, "source_refs", transcriptDreamSourceIDs(sourceRefs))
	writeYAMLStringSlice(b, "source_sessions", transcriptDreamSessionIDs(sourceRefs))
	tags := []string{"foxctl/transcript-dream", "foxctl/dream", "foxctl/review-required"}
	if agentBlur != nil {
		tags = append(tags, "foxctl/agent-blurred")
	}
	writeYAMLStringSlice(b, "tags", tags)
	b.WriteString("---\n\n")
}

func normalizeTranscriptDreamMechanisms(in []TranscriptDreamMechanism) []TranscriptDreamMechanism {
	out := make([]TranscriptDreamMechanism, 0, len(in))
	for _, item := range in {
		name := strings.TrimSpace(item.Name)
		summary := strings.TrimSpace(item.Summary)
		if name == "" || summary == "" {
			continue
		}
		item.Name = name
		item.Summary = summary
		item.Tags = compactStrings(item.Tags)
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func normalizeTranscriptDreamSourceRefs(in []TranscriptDreamSourceRef) []TranscriptDreamSourceRef {
	out := make([]TranscriptDreamSourceRef, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		item.RecordID = strings.TrimSpace(item.RecordID)
		if item.RecordID == "" {
			continue
		}
		if _, ok := seen[item.RecordID]; ok {
			continue
		}
		seen[item.RecordID] = struct{}{}
		item.ConversationID = strings.TrimSpace(item.ConversationID)
		item.SessionIDs = compactStrings(item.SessionIDs)
		item.Summary = strings.TrimSpace(item.Summary)
		item.EvidenceRefs = compactStrings(item.EvidenceRefs)
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].RecordID < out[j].RecordID
	})
	return out
}

func normalizeTranscriptDreamReview(in TranscriptDreamReview) TranscriptDreamReview {
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "needs_review"
	}
	return TranscriptDreamReview{
		Status:      status,
		Reviewer:    strings.TrimSpace(in.Reviewer),
		Required:    true,
		GeneratedBy: strings.TrimSpace(in.GeneratedBy),
		Notes:       compactStringsInOrder(in.Notes),
	}
}

func normalizeTranscriptDreamAgentBlur(in *TranscriptDreamAgentBlur) (*TranscriptDreamAgentBlur, error) {
	if in == nil {
		return nil, nil
	}
	out := *in
	out.Agent = strings.TrimSpace(out.Agent)
	out.AbstractSchema = strings.TrimSpace(out.AbstractSchema)
	out.MechanismTags = normalizeMechanismTags(out.MechanismTags)
	out.DomainsToAvoid = compactStrings(out.DomainsToAvoid)
	if out.Agent == "" {
		out.Agent = "agent"
	}
	if out.AbstractSchema == "" {
		return nil, fmt.Errorf("agent blurred schema is required")
	}
	if len(out.MechanismTags) == 0 {
		return nil, fmt.Errorf("agent blurred mechanism tags are required")
	}
	if out.Confidence <= 0 || out.Confidence > 1 {
		return nil, fmt.Errorf("agent blur confidence must be in (0,1]")
	}
	if out.LeakageRisk < 0 || out.LeakageRisk > 1 {
		return nil, fmt.Errorf("agent blur leakage risk must be in [0,1]")
	}
	return &out, nil
}

func BuildTranscriptDreamBlurAgentPromptInput(input TranscriptDreamNoteInput) (MemoryBlurAgentPromptInput, error) {
	summary := strings.TrimSpace(input.DistilledSummary)
	if summary == "" {
		return MemoryBlurAgentPromptInput{}, fmt.Errorf("transcript dream blur: distilled summary is required")
	}
	mechanisms := normalizeTranscriptDreamMechanisms(input.BlurredMechanisms)
	if len(mechanisms) == 0 {
		return MemoryBlurAgentPromptInput{}, fmt.Errorf("transcript dream blur: at least one mechanism is required")
	}
	sourceRefs := normalizeTranscriptDreamSourceRefs(input.SourceRefs)
	if len(sourceRefs) == 0 {
		return MemoryBlurAgentPromptInput{}, fmt.Errorf("transcript dream blur: at least one source reference is required")
	}
	originalDomain := "transcript_dream"
	if provider := strings.TrimSpace(input.Provider); provider != "" {
		originalDomain = provider + ":transcript_dream"
	}
	return MemoryBlurAgentPromptInput{
		ID:             "transcript-dream",
		OriginalDomain: originalDomain,
		Summary:        summary,
		LiteralText:    transcriptDreamBlurLiteralText(input, summary, mechanisms, sourceRefs),
		Shape:          transcriptDreamBlurShape(mechanisms, sourceRefs),
		ForbiddenTerms: transcriptDreamBlurForbiddenTerms(input, summary, mechanisms, sourceRefs),
	}, nil
}

func ApplyTranscriptDreamAgentBlur(input TranscriptDreamNoteInput, promptInput MemoryBlurAgentPromptInput, agentName string, output MemoryBlurAgentOutput) (TranscriptDreamNoteInput, MemoryBlurValidation) {
	validation := ValidateMemoryBlurAgentOutput(promptInput, output)
	if !validation.Valid {
		return input, validation
	}
	input.AgentBlur = &TranscriptDreamAgentBlur{
		Agent:          firstNonEmpty(strings.TrimSpace(agentName), "agent"),
		AbstractSchema: strings.TrimSpace(output.AbstractSchema),
		MechanismTags:  normalizeMechanismTags(output.MechanismTags),
		DomainsToAvoid: compactStrings(output.DomainsToAvoid),
		Confidence:     output.Confidence,
		LeakageRisk:    output.LeakageRisk,
		Validation:     validation,
	}
	return input, validation
}

func transcriptDreamBlurLiteralText(input TranscriptDreamNoteInput, summary string, mechanisms []TranscriptDreamMechanism, sourceRefs []TranscriptDreamSourceRef) string {
	var b strings.Builder
	writeBlurredLine(&b, "title", input.Title)
	writeBlurredLine(&b, "summary", summary)
	for _, mechanism := range mechanisms {
		writeBlurredLine(&b, "mechanism", mechanism.Name+": "+mechanism.Summary)
	}
	for _, ref := range sourceRefs {
		writeBlurredLine(&b, "source_summary", ref.Summary)
	}
	return strings.TrimSpace(b.String())
}

func transcriptDreamBlurShape(mechanisms []TranscriptDreamMechanism, sourceRefs []TranscriptDreamSourceRef) MemoryStructuralShape {
	shape := MemoryStructuralShape{
		Mechanism: "transcript evidence distillation into review-gated memory",
		Actors: []string{
			"conversation source",
			"history record",
			"compression pass",
			"review queue",
			"memory note",
		},
		Operations: []string{
			"compress transcript signals",
			"deduplicate repeated mechanisms",
			"discard command and output noise",
			"preserve provenance outside the abstraction",
			"prepare review-gated memory",
		},
		Flows: []string{
			"conversation source to typed history records",
			"history records to rough dream note",
			"rough dream note to lossy structural abstraction",
			"structural abstraction to review queue",
		},
		Constraints: []string{
			"no raw transcript turns",
			"no literal identifier leakage",
			"review-gated persistence",
			"source-bound provenance",
		},
		Signals: []string{
			"mechanism count",
			"source reference count",
			"history record kinds",
			"confidence scores",
		},
		Graph: &MemoryGraphShape{
			NodeKind: "transcript_dream_note",
			Outgoing: map[string]int{
				"MECHANISM":  len(mechanisms),
				"SOURCE_REF": len(sourceRefs),
			},
			Incoming: map[string]int{
				"TRANSCRIPT_SOURCE": 1,
			},
			NeighborMix: len(mechanisms) + len(sourceRefs),
		},
	}
	return shape
}

func transcriptDreamBlurForbiddenTerms(input TranscriptDreamNoteInput, summary string, mechanisms []TranscriptDreamMechanism, sourceRefs []TranscriptDreamSourceRef) []string {
	var values []string
	for _, value := range []string{input.Project, input.WorkspaceID, input.Provider, input.SessionID, input.SourceDigest, input.Title, summary} {
		values = appendTranscriptDreamForbiddenTerms(values, value)
	}
	for _, mechanism := range mechanisms {
		values = appendTranscriptDreamForbiddenTerms(values, mechanism.Summary)
	}
	for _, ref := range sourceRefs {
		for _, value := range []string{ref.RecordID, ref.ConversationID, ref.Summary} {
			values = appendTranscriptDreamForbiddenTerms(values, value)
		}
		for _, value := range append(append([]string(nil), ref.SessionIDs...), ref.EvidenceRefs...) {
			values = appendTranscriptDreamForbiddenTerms(values, value)
		}
	}
	return compactStringsInOrder(values)
}

func appendTranscriptDreamForbiddenTerms(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	values = append(values, value)
	withoutTerminalPunctuation := strings.TrimRight(value, ".:;!?")
	if withoutTerminalPunctuation != "" && withoutTerminalPunctuation != value {
		values = append(values, withoutTerminalPunctuation)
	}
	stripped := stripAngleTaggedText(value)
	if stripped != "" && stripped != value {
		values = append(values, stripped)
	}
	return values
}

func stripAngleTaggedText(value string) string {
	var b strings.Builder
	inTag := false
	lastSpace := false
	for _, r := range strings.TrimSpace(value) {
		switch r {
		case '<':
			inTag = true
			if !lastSpace && b.Len() > 0 {
				b.WriteByte(' ')
				lastSpace = true
			}
		case '>':
			inTag = false
		default:
			if inTag {
				continue
			}
			if r == '\n' || r == '\t' || r == '\r' || r == ' ' {
				if !lastSpace && b.Len() > 0 {
					b.WriteByte(' ')
					lastSpace = true
				}
				continue
			}
			b.WriteRune(r)
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func transcriptDreamDedupeKey(project, summary string, sourceRefs []TranscriptDreamSourceRef) string {
	parts := []string{project, strings.TrimSpace(summary)}
	for _, ref := range sourceRefs {
		parts = append(parts, ref.RecordID)
	}
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "transcript_dream:" + hex.EncodeToString(hash[:])
}

func transcriptDreamSlug(value string) string {
	return safeMemorySlug(value)
}

func transcriptDreamMechanismTags(mechanisms []TranscriptDreamMechanism) []string {
	var tags []string
	for _, mechanism := range mechanisms {
		tags = append(tags, mechanism.Tags...)
	}
	return compactStrings(tags)
}

func transcriptDreamSourceIDs(sourceRefs []TranscriptDreamSourceRef) []string {
	out := make([]string, 0, len(sourceRefs))
	for _, ref := range sourceRefs {
		out = append(out, ref.RecordID)
	}
	return compactStrings(out)
}

func transcriptDreamSessionIDs(sourceRefs []TranscriptDreamSourceRef) []string {
	var out []string
	for _, ref := range sourceRefs {
		out = append(out, ref.SessionIDs...)
	}
	return compactStrings(out)
}
