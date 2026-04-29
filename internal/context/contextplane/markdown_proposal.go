package contextplane

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	obsidiantool "github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
	"gopkg.in/yaml.v3"
)

// MarkdownProposalInput captures one review-first ACA draft generated from a
// structured runtime source such as room-agile state.
type MarkdownProposalInput struct {
	NoteType       string         `json:"note_type"`
	Project        string         `json:"project,omitempty"`
	Folder         string         `json:"folder,omitempty"`
	SourceKind     string         `json:"source_kind"`
	SourceID       string         `json:"source_id"`
	Title          string         `json:"title"`
	Summary        string         `json:"summary"`
	Body           string         `json:"body"`
	Frontmatter    map[string]any `json:"frontmatter,omitempty"`
	DedupeKey      string         `json:"dedupe_key,omitempty"`
	Kind           string         `json:"kind,omitempty"`
	Classification string         `json:"classification,omitempty"`
	BlastRadius    string         `json:"blast_radius,omitempty"`
	Confidence     float64        `json:"confidence,omitempty"`
	ReviewAction   string         `json:"review_action,omitempty"`
	SourceRefs     []string       `json:"source_refs,omitempty"`
	ProposedChange map[string]any `json:"proposed_change,omitempty"`
}

type MarkdownProposalResult struct {
	DraftPath      string         `json:"draft_path"`
	PromotionState string         `json:"promotion_state"`
	Proposal       MemoryProposal `json:"proposal"`
}

// DraftMarkdownProposal writes one stable ACA inbox draft and records the
// corresponding review-required memory proposal. Repeated calls with the same
// dedupe key and identical draft content return already_current.
func (s *WorkspaceStore) DraftMarkdownProposal(ctx context.Context, in MarkdownProposalInput) (MarkdownProposalResult, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return MarkdownProposalResult{}, err
	}
	noteType := strings.TrimSpace(in.NoteType)
	if noteType == "" {
		return MarkdownProposalResult{}, fmt.Errorf("note type is required")
	}
	sourceKind := strings.TrimSpace(in.SourceKind)
	if sourceKind == "" {
		return MarkdownProposalResult{}, fmt.Errorf("source kind is required")
	}
	sourceID := strings.TrimSpace(in.SourceID)
	if sourceID == "" {
		return MarkdownProposalResult{}, fmt.Errorf("source id is required")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = firstNonEmpty(sourceID, "AgentCTL ACA Draft")
	}
	project := firstNonEmpty(strings.TrimSpace(in.Project), filepath.Base(layout.WorkspacePath))
	folder := strings.Trim(strings.TrimSpace(in.Folder), "/")
	if folder == "" {
		folder = filepath.ToSlash(filepath.Join("room-agile", safeFileSlug(project, "workspace"), noteType))
	}
	draftRel := filepath.ToSlash(filepath.Join("inbox", "drafted-from-foxctl", folder, safeFileSlug(sourceID, "draft")+".md"))
	rendered, err := renderMarkdownProposalContent(noteType, title, in.Frontmatter, in.Body)
	if err != nil {
		return MarkdownProposalResult{}, err
	}

	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return MarkdownProposalResult{}, err
	}
	defer func() { _ = closeFn() }()

	dedupeKey := strings.TrimSpace(in.DedupeKey)
	if dedupeKey == "" {
		dedupeKey = fmt.Sprintf("%s|%s|%s", firstNonEmpty(strings.TrimSpace(in.Kind), "markdown_draft"), noteType, sourceID)
	}
	existingProposal, err := findMemoryProposalRowByKey(ctx, db, dedupeKey)
	if err != nil {
		return MarkdownProposalResult{}, err
	}
	draftAbs := filepath.Join(layout.TemplatesDir, filepath.FromSlash(draftRel))
	draftUnchanged := false
	if body, readErr := os.ReadFile(draftAbs); readErr == nil {
		draftUnchanged = bytes.Equal(normalizeMarkdownProposalForCompare(body), normalizeMarkdownProposalForCompare([]byte(rendered)))
	}
	if existingProposal != nil {
		if draftUnchanged {
			return MarkdownProposalResult{
				DraftPath:      draftRel,
				PromotionState: "already_current",
				Proposal:       *existingProposal,
			}, nil
		}
	}

	writer := obsidiantool.NewWriter("", "", obsidiantool.DefaultPolicy())
	writer.VaultPath = layout.TemplatesDir
	if err := writer.CreateNote(ctx, draftRel, rendered, true); err != nil {
		return MarkdownProposalResult{}, err
	}

	change := copyStringAnyMap(in.ProposedChange)
	change["draft_path"] = draftRel
	change["note_type"] = noteType
	change["source_kind"] = sourceKind
	change["source_id"] = sourceID
	change["title"] = title
	change["summary"] = strings.TrimSpace(in.Summary)
	change["review_action"] = firstNonEmpty(strings.TrimSpace(in.ReviewAction), "review_room_agile_draft")

	now := time.Now().UTC()
	proposal := MemoryProposal{
		DedupeKey:      dedupeKey,
		Kind:           PolicyKind(firstNonEmpty(strings.TrimSpace(in.Kind), "room_agile_draft")),
		Classification: firstNonEmpty(strings.TrimSpace(in.Classification), noteType),
		Status:         "open",
		ReviewRequired: true,
		Confidence:     firstNonZeroFloat64(in.Confidence, 0.82),
		BlastRadius:    firstNonEmpty(strings.TrimSpace(in.BlastRadius), "medium"),
		Summary:        firstNonEmpty(strings.TrimSpace(in.Summary), fmt.Sprintf("Review ACA draft for %s %s.", sourceKind, sourceID)),
		SourceRefs: uniqueEvidenceRefs(append([]contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "draft:" + draftRel},
			{Type: contextengine.RefTypePath, Ref: sourceKind + ":" + sourceID},
		}, stringsToEvidenceRefs(in.SourceRefs)...)),
		ProposedChange:   change,
		EvaluationStatus: "not_evaluated",
		ApplyStatus:      "pending",
		Count:            1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	stored, err := s.RecordMemoryProposal(ctx, proposal)
	if err != nil {
		return MarkdownProposalResult{}, err
	}
	state := "created"
	if draftUnchanged && stored.Count > 1 {
		state = "already_current"
	} else if existingProposal != nil || stored.Count > 1 {
		state = "refreshed"
	}
	return MarkdownProposalResult{
		DraftPath:      draftRel,
		PromotionState: state,
		Proposal:       stored,
	}, nil
}

func renderMarkdownProposalContent(noteType, title string, frontmatter map[string]any, body string) (string, error) {
	meta := copyStringAnyMap(frontmatter)
	meta["note_type"] = firstNonEmpty(strings.TrimSpace(noteType), strings.TrimSpace(fmt.Sprint(meta["note_type"])))
	if strings.TrimSpace(title) != "" && strings.TrimSpace(fmt.Sprint(meta["title"])) == "" {
		meta["title"] = strings.TrimSpace(title)
	}
	yamlBody, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal markdown proposal frontmatter: %w", err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(yamlBody)
	b.WriteString("---\n\n")
	if strings.TrimSpace(body) != "" {
		b.WriteString(strings.TrimSpace(body))
		b.WriteString("\n")
	}
	return b.String(), nil
}

func copyStringAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstNonZeroFloat64(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func normalizeMarkdownProposalForCompare(raw []byte) []byte {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "generated_at:") {
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.TrimSpace(strings.Join(out, "\n")))
}
