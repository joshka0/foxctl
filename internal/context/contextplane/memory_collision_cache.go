package contextplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/memorycore"
	obsidiantool "github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
	"gopkg.in/yaml.v3"
)

const (
	memoryCollisionCacheSourceLane        = "mechanism_collision_agents"
	memoryCollisionCacheMachineFence      = "foxctl-memory-collision-cache-v1"
	memoryCollisionCacheRecordVersion     = 1
	memoryCollisionCacheVaultRelativeRoot = "inbox/drafted-from-foxctl/collisions"
	memoryCollisionCacheStrategy          = "memory_collision_cache"
)

var ErrNotMemoryCollisionCacheNote = errors.New("not a memory collision cache note")

// MemoryCollisionSynthesis is one validated agent synthesis for a collision
// cell. It is intentionally the compact record needed for a reusable
// ContextWiki cache note, not the raw agent transcript.
type MemoryCollisionSynthesis struct {
	AgentIndex        int                            `json:"agent_index"`
	AgentRole         string                         `json:"agent_role"`
	AgentProvider     string                         `json:"agent_provider,omitempty"`
	AgentModel        string                         `json:"agent_model,omitempty"`
	BisociationMode   string                         `json:"bisociation_mode,omitempty"`
	SelectionMode     string                         `json:"selection_mode,omitempty"`
	PromptAbstraction string                         `json:"prompt_abstraction,omitempty"`
	Collision         MemoryCollisionCell            `json:"collision"`
	Output            MemoryCollisionAgentOutput     `json:"agent_output"`
	Validation        MemoryCollisionAgentValidation `json:"validation"`
}

// MemoryCollisionCacheInput is the pure input for planning one Obsidian note
// that caches a small collision network for later retrieval.
type MemoryCollisionCacheInput struct {
	WorkspaceID   string                     `json:"workspace_id"`
	WorkspacePath string                     `json:"workspace_path,omitempty"`
	WorkspaceSlug string                     `json:"workspace_slug,omitempty"`
	Query         MechanismQuery             `json:"query"`
	Syntheses     []MemoryCollisionSynthesis `json:"syntheses"`
	CreatedAt     time.Time                  `json:"created_at"`
}

// MemoryCollisionCacheNote is a deterministic note plan. Write explicitly with
// WriteMemoryCollisionCacheNote when the caller wants to mutate an Obsidian
// vault.
type MemoryCollisionCacheNote struct {
	DedupeKey      string    `json:"dedupe_key"`
	Title          string    `json:"title"`
	NotePath       string    `json:"note_path"`
	Content        string    `json:"content,omitempty"`
	CollisionIDs   []string  `json:"collision_ids,omitempty"`
	SynthesisCount int       `json:"synthesis_count"`
	CreatedAt      time.Time `json:"created_at"`
}

type MemoryCollisionCacheRecord struct {
	Version       int                                   `json:"version"`
	WorkspaceID   string                                `json:"workspace_id"`
	WorkspacePath string                                `json:"workspace_path,omitempty"`
	NotePath      string                                `json:"note_path"`
	DedupeKey     string                                `json:"dedupe_key"`
	Title         string                                `json:"title"`
	GeneratedAt   time.Time                             `json:"generated_at"`
	Query         MemoryCollisionCacheQueryRecord       `json:"query"`
	Syntheses     []MemoryCollisionCacheSynthesisRecord `json:"syntheses"`
}

type MemoryCollisionCacheQueryRecord struct {
	ID             string   `json:"id"`
	Domain         string   `json:"domain"`
	Text           string   `json:"text"`
	AbstractSchema string   `json:"abstract_schema,omitempty"`
	MechanismTags  []string `json:"mechanism_tags,omitempty"`
	SourceRefs     []string `json:"source_refs,omitempty"`
}

type MemoryCollisionCacheSynthesisRecord struct {
	AgentProvider     string                              `json:"agent_provider,omitempty"`
	AgentModel        string                              `json:"agent_model,omitempty"`
	BisociationMode   string                              `json:"bisociation_mode"`
	SelectionMode     string                              `json:"selection_mode"`
	PromptAbstraction string                              `json:"prompt_abstraction"`
	Collision         MemoryCollisionCacheCollisionRecord `json:"collision"`
	Output            MemoryCollisionAgentOutput          `json:"agent_output"`
}

type MemoryCollisionCacheCollisionRecord struct {
	CollisionID          string   `json:"collision_id"`
	MemoryID             string   `json:"memory_id,omitempty"`
	MemoryDomain         string   `json:"memory_domain"`
	MemorySummary        string   `json:"memory_summary,omitempty"`
	AbstractSchema       string   `json:"abstract_schema,omitempty"`
	MechanismTags        []string `json:"mechanism_tags,omitempty"`
	LiteralSimilarity    float64  `json:"literal_similarity,omitempty"`
	StructuralSimilarity float64  `json:"structural_similarity,omitempty"`
	CollisionScore       float64  `json:"collision_score,omitempty"`
	SourceRefs           []string `json:"source_refs,omitempty"`
	Reason               string   `json:"reason,omitempty"`
}

type MemoryCollisionCacheLoadOptions struct {
	WorkspaceID        string `json:"workspace_id,omitempty"`
	QueryID            string `json:"query_id,omitempty"`
	QueryDomain        string `json:"query_domain,omitempty"`
	BisociationMode    string `json:"bisociation_mode,omitempty"`
	SelectionMode      string `json:"selection_mode,omitempty"`
	PromptAbstraction  string `json:"prompt_abstraction,omitempty"`
	MemoryDomain       string `json:"memory_domain,omitempty"`
	MechanismTag       string `json:"mechanism_tag,omitempty"`
	AgentModel         string `json:"agent_model,omitempty"`
	Limit              int    `json:"limit,omitempty"`
	IncludeUnsupported bool   `json:"include_unsupported,omitempty"`
}

// PlanMemoryCollisionCacheNote renders one lightweight ContextWiki/Obsidian
// cache note for a bounded set of successful agent syntheses.
func PlanMemoryCollisionCacheNote(input MemoryCollisionCacheInput) (MemoryCollisionCacheNote, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	workspaceSlug := strings.TrimSpace(input.WorkspaceSlug)
	if workspaceSlug == "" {
		workspaceSlug = workspaceMemorySlug(input.WorkspacePath, workspaceID)
	}
	if workspaceID == "" {
		workspaceID = workspaceSlug
	}
	query := input.Query
	query.ID = strings.TrimSpace(query.ID)
	query.Domain = strings.TrimSpace(query.Domain)
	query.Text = strings.TrimSpace(query.Text)
	if workspaceID == "" {
		return MemoryCollisionCacheNote{}, fmt.Errorf("memory collision cache: workspace_id is required")
	}
	if query.ID == "" || query.Domain == "" || query.Text == "" {
		return MemoryCollisionCacheNote{}, fmt.Errorf("memory collision cache: query id, domain, and text are required")
	}

	createdAt := input.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	syntheses := successfulMemoryCollisionSyntheses(input.Syntheses)
	if len(syntheses) == 0 {
		return MemoryCollisionCacheNote{}, fmt.Errorf("memory collision cache: at least one valid synthesis is required")
	}

	collisionIDs := memoryCollisionSynthesisIDs(syntheses)
	dedupeKey := "memory_collision_cache:" + digestParts(append([]string{workspaceID, query.ID}, memoryCollisionSynthesisDedupeParts(syntheses)...)...)
	title := "Collision cache: " + memoryCollisionCacheQueryTitle(query)
	slug := safeMemorySlug(title + " " + dedupeKey[len(dedupeKey)-12:])
	if slug == "" {
		slug = "collision-cache-" + dedupeKey[len(dedupeKey)-12:]
	}
	notePath := filepath.ToSlash(filepath.Join("inbox", "drafted-from-foxctl", "collisions", workspaceSlug, createdAt.Format("2006-01-02"), slug+".md"))
	note := MemoryCollisionCacheNote{
		DedupeKey:      dedupeKey,
		Title:          title,
		NotePath:       notePath,
		CollisionIDs:   collisionIDs,
		SynthesisCount: len(syntheses),
		CreatedAt:      createdAt,
	}
	note.Content = renderMemoryCollisionCacheNote(workspaceID, note, query, syntheses)
	return note, nil
}

// WriteMemoryCollisionCacheNote writes a planned cache note into an Obsidian
// vault using the same direct writer path as ContextWiki drafts.
func WriteMemoryCollisionCacheNote(ctx context.Context, vaultPath string, note MemoryCollisionCacheNote) error {
	vaultPath = strings.TrimSpace(vaultPath)
	if vaultPath == "" {
		return fmt.Errorf("memory collision cache: vault_path is required")
	}
	if strings.TrimSpace(note.NotePath) == "" || strings.TrimSpace(note.Content) == "" {
		return fmt.Errorf("memory collision cache: note path and content are required")
	}
	writer := obsidiantool.NewWriter("", filepath.Base(vaultPath), obsidiantool.DefaultPolicy())
	writer.VaultPath = vaultPath
	return writer.CreateNote(ctx, note.NotePath, note.Content, true)
}

func renderMemoryCollisionCacheNote(workspaceID string, note MemoryCollisionCacheNote, query MechanismQuery, syntheses []MemoryCollisionSynthesis) string {
	var b strings.Builder
	writeMemoryCollisionCacheFrontmatter(&b, workspaceID, note, query, syntheses)
	b.WriteString("# ")
	b.WriteString(note.Title)
	b.WriteString("\n\n")
	b.WriteString("## Summary\n")
	if len(syntheses) == 1 {
		b.WriteString("Cached 1 agent synthesis")
	} else {
		fmt.Fprintf(&b, "Cached %d agent syntheses", len(syntheses))
	}
	b.WriteString(" for one mechanism query and its cross-domain collision network.\n\n")
	writeMemoryCollisionCacheMachineBlock(&b, workspaceID, note, query, syntheses)
	b.WriteString("## Query Shape\n")
	b.WriteString("- Domain: `")
	b.WriteString(escapeInlineCode(query.Domain))
	b.WriteString("`\n")
	b.WriteString("- Query: ")
	b.WriteString(markdownOneLine(query.Text))
	b.WriteString("\n")
	if len(query.MechanismTags) > 0 {
		b.WriteString("- Mechanism tags: ")
		b.WriteString(strings.Join(normalizeMechanismTags(query.MechanismTags), ", "))
		b.WriteString("\n")
	}
	if len(query.SourceRefs) > 0 {
		b.WriteString("- Source refs: ")
		b.WriteString(strings.Join(formatEvidenceRefs(query.SourceRefs), ", "))
		b.WriteString("\n")
	}
	b.WriteString("\n## Collision Network\n")
	for i, synthesis := range syntheses {
		writeMemoryCollisionSynthesisSection(&b, i+1, synthesis)
	}
	b.WriteString("## Review Notes\n")
	b.WriteString("Treat this as a lightweight retrieval cache. Promote only the durable mechanism or implementation insight after review.\n")
	return b.String()
}

func writeMemoryCollisionCacheMachineBlock(b *strings.Builder, workspaceID string, note MemoryCollisionCacheNote, query MechanismQuery, syntheses []MemoryCollisionSynthesis) {
	record := memoryCollisionCacheRecordFromSyntheses(workspaceID, note, query, syntheses)
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return
	}
	b.WriteString("```")
	b.WriteString(memoryCollisionCacheMachineFence)
	b.WriteString("\n")
	b.Write(body)
	b.WriteString("\n```\n\n")
}

func writeMemoryCollisionCacheFrontmatter(b *strings.Builder, workspaceID string, note MemoryCollisionCacheNote, query MechanismQuery, syntheses []MemoryCollisionSynthesis) {
	b.WriteString("---\n")
	writeYAMLString(b, "type", "memory_collision_cache")
	writeYAMLString(b, "status", "cache")
	writeYAMLString(b, "trust", "raw")
	writeYAMLString(b, "lifecycle", string(memorycore.LifecycleStateCandidate))
	writeYAMLString(b, "review_status", string(memorycore.ReviewStatusNeedsReview))
	writeYAMLString(b, "source_lane", memoryCollisionCacheSourceLane)
	writeYAMLString(b, "workspace_id", workspaceID)
	writeYAMLString(b, "dedupe_key", note.DedupeKey)
	writeYAMLString(b, "title", note.Title)
	writeYAMLString(b, "generated_at", note.CreatedAt.UTC().Format(time.RFC3339))
	writeYAMLString(b, "query_id", query.ID)
	writeYAMLString(b, "query_domain", query.Domain)
	writeYAMLString(b, "query_text", query.Text)
	writeYAMLStringSlice(b, "collision_ids", note.CollisionIDs)
	writeYAMLStringSlice(b, "memory_ids", memoryCollisionSynthesisMemoryIDs(syntheses))
	writeYAMLStringSlice(b, "memory_domains", memoryCollisionSynthesisMemoryDomains(syntheses))
	writeYAMLStringSlice(b, "mechanism_tags", memoryCollisionCacheMechanismTags(query, syntheses))
	writeYAMLStringSlice(b, "bisociation_modes", memoryCollisionSynthesisModes(syntheses))
	writeYAMLStringSlice(b, "selection_modes", memoryCollisionSynthesisSelectionModes(syntheses))
	writeYAMLStringSlice(b, "prompt_abstractions", memoryCollisionSynthesisPromptAbstractions(syntheses))
	writeYAMLStringSlice(b, "agent_roles", memoryCollisionSynthesisAgentRoles(syntheses))
	writeYAMLStringSlice(b, "agent_models", memoryCollisionSynthesisAgentModels(syntheses))
	writeYAMLStringSlice(b, "source_refs", formatEvidenceRefs(memoryCollisionCacheSourceRefs(query, syntheses)))
	writeYAMLStringSlice(b, "tags", []string{
		"foxctl/collision-cache",
		"foxctl/contextwiki",
		"foxctl/mechanism-memory",
		"foxctl/pi-agent",
	})
	b.WriteString("---\n\n")
}

func writeMemoryCollisionSynthesisSection(b *strings.Builder, ordinal int, synthesis MemoryCollisionSynthesis) {
	cell := synthesis.Collision
	output := synthesis.Output
	role := firstNonEmpty(strings.TrimSpace(synthesis.AgentRole), defaultMemoryCollisionAgentRole)
	fmt.Fprintf(b, "### %d. %s -> %s\n", ordinal, markdownOneLine(cell.QueryDomain), markdownOneLine(cell.MemoryDomain))
	b.WriteString("- Agent role: `")
	b.WriteString(escapeInlineCode(role))
	b.WriteString("`\n")
	if model := memoryCollisionSynthesisModelLabel(synthesis); model != "" {
		b.WriteString("- Agent model: `")
		b.WriteString(escapeInlineCode(model))
		b.WriteString("`\n")
	}
	if mode := strings.TrimSpace(synthesis.BisociationMode); mode != "" {
		b.WriteString("- Bisociation mode: `")
		b.WriteString(escapeInlineCode(mode))
		b.WriteString("`\n")
	}
	b.WriteString("- Collision: `")
	b.WriteString(escapeInlineCode(cell.CollisionID))
	b.WriteString("`\n")
	b.WriteString("- Memory: `")
	b.WriteString(escapeInlineCode(cell.MemoryID))
	b.WriteString("`\n")
	fmt.Fprintf(b, "- Scores: collision %.4f, structural %.4f, literal %.4f\n", cell.CollisionScore, cell.StructuralSimilarity, cell.LiteralSimilarity)
	if strings.TrimSpace(cell.Reason) != "" {
		b.WriteString("- Reason: ")
		b.WriteString(markdownOneLine(cell.Reason))
		b.WriteString("\n")
	}
	if len(cell.MechanismTags) > 0 {
		b.WriteString("- Memory tags: ")
		b.WriteString(strings.Join(normalizeMechanismTags(cell.MechanismTags), ", "))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if strings.TrimSpace(cell.MemorySummary) != "" {
		b.WriteString("Memory summary: ")
		b.WriteString(markdownOneLine(cell.MemorySummary))
		b.WriteString("\n\n")
	}
	writeMarkdownCodeBlock(b, "Abstract schema", cell.AbstractSchema)
	b.WriteString("Bridge schema: ")
	b.WriteString(markdownOneLine(output.BridgeSchema))
	b.WriteString("\n\n")
	b.WriteString("New collision: ")
	b.WriteString(markdownOneLine(output.NewCollision))
	b.WriteString("\n\n")
	if len(output.TransferSteps) > 0 {
		b.WriteString("Transfer steps:\n")
		for _, step := range output.TransferSteps {
			b.WriteString("- ")
			b.WriteString(markdownOneLine(step))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if strings.TrimSpace(output.StressTest) != "" {
		b.WriteString("Stress test: ")
		b.WriteString(markdownOneLine(output.StressTest))
		b.WriteString("\n\n")
	}
	if len(output.Risks) > 0 {
		b.WriteString("Risks:\n")
		for _, risk := range output.Risks {
			b.WriteString("- ")
			b.WriteString(markdownOneLine(risk))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "Confidence: %.2f; novelty: %.2f\n\n", output.Confidence, output.NoveltyConfidence)
	if len(cell.SourceRefs) > 0 {
		b.WriteString("Source refs:\n")
		for _, ref := range formatEvidenceRefs(cell.SourceRefs) {
			b.WriteString("- `")
			b.WriteString(escapeInlineCode(ref))
			b.WriteString("`\n")
		}
		b.WriteString("\n")
	}
}

func ParseMemoryCollisionCacheNote(notePath string, content string) (MemoryCollisionCacheRecord, error) {
	meta, ok, err := parseMemoryCollisionCacheFrontmatter(content)
	if err != nil {
		return MemoryCollisionCacheRecord{}, err
	}
	if !ok {
		return MemoryCollisionCacheRecord{}, ErrNotMemoryCollisionCacheNote
	}
	if noteType := strings.TrimSpace(fmt.Sprint(meta["type"])); noteType != "memory_collision_cache" {
		return MemoryCollisionCacheRecord{}, ErrNotMemoryCollisionCacheNote
	}
	block, ok := extractMemoryCollisionCacheMachineBlock(content)
	if !ok {
		return MemoryCollisionCacheRecord{}, fmt.Errorf("memory collision cache: missing %s block", memoryCollisionCacheMachineFence)
	}
	var record MemoryCollisionCacheRecord
	if err := json.Unmarshal([]byte(block), &record); err != nil {
		return MemoryCollisionCacheRecord{}, fmt.Errorf("memory collision cache: decode machine block: %w", err)
	}
	if record.NotePath == "" {
		record.NotePath = filepath.ToSlash(strings.TrimSpace(notePath))
	}
	if err := normalizeMemoryCollisionCacheRecord(&record); err != nil {
		return MemoryCollisionCacheRecord{}, err
	}
	return record, nil
}

func LoadMemoryCollisionCacheRecords(ctx context.Context, vaultPath string, opts MemoryCollisionCacheLoadOptions) ([]MemoryCollisionCacheRecord, error) {
	vaultPath = strings.TrimSpace(vaultPath)
	if vaultPath == "" {
		return nil, fmt.Errorf("memory collision cache: vault_path is required")
	}
	root := filepath.Join(vaultPath, filepath.FromSlash(memoryCollisionCacheVaultRelativeRoot))
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var records []MemoryCollisionCacheRecord
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(vaultPath, path)
		if err != nil {
			rel = path
		}
		record, err := ParseMemoryCollisionCacheNote(filepath.ToSlash(rel), string(body))
		if err != nil {
			if errors.Is(err, ErrNotMemoryCollisionCacheNote) || opts.IncludeUnsupported {
				return nil
			}
			return err
		}
		if memoryCollisionCacheRecordMatches(record, opts) {
			records = append(records, record)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool {
		left, right := records[i], records[j]
		if !left.GeneratedAt.Equal(right.GeneratedAt) {
			return left.GeneratedAt.After(right.GeneratedAt)
		}
		return left.NotePath < right.NotePath
	})
	if opts.Limit > 0 && len(records) > opts.Limit {
		records = records[:opts.Limit]
	}
	return records, nil
}

func MemoryCollisionCacheRecordsToCells(workspaceID string, query MechanismQuery, records []MemoryCollisionCacheRecord) []MemoryCollisionCell {
	var cells []MemoryCollisionCell
	seen := map[string]struct{}{}
	for _, record := range records {
		for _, synthesis := range record.Syntheses {
			cell := memoryCollisionCacheSynthesisCell(workspaceID, query, record, synthesis)
			if strings.TrimSpace(cell.DedupeKey) == "" {
				continue
			}
			if _, exists := seen[cell.DedupeKey]; exists {
				continue
			}
			seen[cell.DedupeKey] = struct{}{}
			cells = append(cells, cell)
		}
	}
	return cells
}

func memoryCollisionCacheRecordFromSyntheses(workspaceID string, note MemoryCollisionCacheNote, query MechanismQuery, syntheses []MemoryCollisionSynthesis) MemoryCollisionCacheRecord {
	record := MemoryCollisionCacheRecord{
		Version:     memoryCollisionCacheRecordVersion,
		WorkspaceID: strings.TrimSpace(workspaceID),
		NotePath:    strings.TrimSpace(note.NotePath),
		DedupeKey:   strings.TrimSpace(note.DedupeKey),
		Title:       strings.TrimSpace(note.Title),
		GeneratedAt: note.CreatedAt.UTC(),
		Query: MemoryCollisionCacheQueryRecord{
			ID:             strings.TrimSpace(query.ID),
			Domain:         strings.TrimSpace(query.Domain),
			Text:           strings.TrimSpace(query.Text),
			AbstractSchema: strings.TrimSpace(query.AbstractSchema),
			MechanismTags:  normalizeMechanismTags(query.MechanismTags),
			SourceRefs:     formatEvidenceRefs(query.SourceRefs),
		},
		Syntheses: make([]MemoryCollisionCacheSynthesisRecord, 0, len(syntheses)),
	}
	for _, synthesis := range syntheses {
		record.Syntheses = append(record.Syntheses, memoryCollisionCacheSynthesisRecord(synthesis))
	}
	return record
}

func memoryCollisionCacheSynthesisRecord(synthesis MemoryCollisionSynthesis) MemoryCollisionCacheSynthesisRecord {
	cell := synthesis.Collision
	mode := NormalizeMemoryCollisionAgentMode(synthesis.BisociationMode)
	return MemoryCollisionCacheSynthesisRecord{
		AgentProvider:     strings.TrimSpace(synthesis.AgentProvider),
		AgentModel:        strings.TrimSpace(synthesis.AgentModel),
		BisociationMode:   mode,
		SelectionMode:     firstNonEmpty(strings.TrimSpace(synthesis.SelectionMode), memoryCollisionCacheSelectionMode(mode)),
		PromptAbstraction: firstNonEmpty(strings.TrimSpace(synthesis.PromptAbstraction), memoryCollisionCachePromptAbstraction(mode)),
		Collision: MemoryCollisionCacheCollisionRecord{
			CollisionID:          strings.TrimSpace(cell.CollisionID),
			MemoryID:             strings.TrimSpace(cell.MemoryID),
			MemoryDomain:         strings.TrimSpace(cell.MemoryDomain),
			MemorySummary:        strings.TrimSpace(cell.MemorySummary),
			AbstractSchema:       strings.TrimSpace(cell.AbstractSchema),
			MechanismTags:        normalizeMechanismTags(cell.MechanismTags),
			LiteralSimilarity:    cell.LiteralSimilarity,
			StructuralSimilarity: cell.StructuralSimilarity,
			CollisionScore:       cell.CollisionScore,
			SourceRefs:           formatEvidenceRefs(cell.SourceRefs),
			Reason:               strings.TrimSpace(cell.Reason),
		},
		Output: synthesis.Output,
	}
}

func parseMemoryCollisionCacheFrontmatter(content string) (map[string]any, bool, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return nil, false, nil
	}
	end := strings.Index(normalized[len("---\n"):], "\n---\n")
	if end < 0 {
		return nil, false, nil
	}
	raw := normalized[len("---\n") : len("---\n")+end]
	meta := map[string]any{}
	if err := yaml.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, false, fmt.Errorf("memory collision cache: parse frontmatter: %w", err)
	}
	return meta, true, nil
}

func extractMemoryCollisionCacheMachineBlock(content string) (string, bool) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	start := "```" + memoryCollisionCacheMachineFence
	var b strings.Builder
	inBlock := false
	for _, line := range lines {
		switch {
		case !inBlock && strings.TrimSpace(line) == start:
			inBlock = true
		case inBlock && strings.TrimSpace(line) == "```":
			return b.String(), true
		case inBlock:
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return "", false
}

func normalizeMemoryCollisionCacheRecord(record *MemoryCollisionCacheRecord) error {
	record.Version = int(record.Version)
	if record.Version != memoryCollisionCacheRecordVersion {
		return fmt.Errorf("memory collision cache: unsupported record version %d", record.Version)
	}
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	record.WorkspacePath = strings.TrimSpace(record.WorkspacePath)
	record.NotePath = filepath.ToSlash(strings.TrimSpace(record.NotePath))
	record.DedupeKey = strings.TrimSpace(record.DedupeKey)
	record.Title = strings.TrimSpace(record.Title)
	record.Query.ID = strings.TrimSpace(record.Query.ID)
	record.Query.Domain = strings.TrimSpace(record.Query.Domain)
	record.Query.Text = strings.TrimSpace(record.Query.Text)
	record.Query.AbstractSchema = strings.TrimSpace(record.Query.AbstractSchema)
	record.Query.MechanismTags = normalizeMechanismTags(record.Query.MechanismTags)
	record.Query.SourceRefs = compactStrings(record.Query.SourceRefs)
	if record.WorkspaceID == "" || record.DedupeKey == "" || record.Query.ID == "" || record.Query.Domain == "" || record.Query.Text == "" {
		return fmt.Errorf("memory collision cache: workspace, dedupe key, query id, query domain, and query text are required")
	}
	if len(record.Syntheses) == 0 {
		return fmt.Errorf("memory collision cache: at least one synthesis is required")
	}
	for i := range record.Syntheses {
		normalizeMemoryCollisionCacheSynthesisRecord(&record.Syntheses[i])
		if record.Syntheses[i].Output.NewCollision == "" {
			return fmt.Errorf("memory collision cache: synthesis %d missing new_collision", i)
		}
		if record.Syntheses[i].Collision.MemoryDomain == "" {
			return fmt.Errorf("memory collision cache: synthesis %d missing memory_domain", i)
		}
	}
	return nil
}

func normalizeMemoryCollisionCacheSynthesisRecord(synthesis *MemoryCollisionCacheSynthesisRecord) {
	synthesis.AgentProvider = strings.TrimSpace(synthesis.AgentProvider)
	synthesis.AgentModel = strings.TrimSpace(synthesis.AgentModel)
	synthesis.BisociationMode = NormalizeMemoryCollisionAgentMode(synthesis.BisociationMode)
	synthesis.SelectionMode = firstNonEmpty(strings.TrimSpace(synthesis.SelectionMode), memoryCollisionCacheSelectionMode(synthesis.BisociationMode))
	synthesis.PromptAbstraction = firstNonEmpty(strings.TrimSpace(synthesis.PromptAbstraction), memoryCollisionCachePromptAbstraction(synthesis.BisociationMode))
	synthesis.Collision.CollisionID = strings.TrimSpace(synthesis.Collision.CollisionID)
	synthesis.Collision.MemoryID = strings.TrimSpace(synthesis.Collision.MemoryID)
	synthesis.Collision.MemoryDomain = strings.TrimSpace(synthesis.Collision.MemoryDomain)
	synthesis.Collision.MemorySummary = strings.TrimSpace(synthesis.Collision.MemorySummary)
	synthesis.Collision.AbstractSchema = strings.TrimSpace(synthesis.Collision.AbstractSchema)
	synthesis.Collision.MechanismTags = normalizeMechanismTags(synthesis.Collision.MechanismTags)
	synthesis.Collision.SourceRefs = compactStrings(synthesis.Collision.SourceRefs)
	synthesis.Collision.Reason = strings.TrimSpace(synthesis.Collision.Reason)
	synthesis.Output.BridgeSchema = strings.TrimSpace(synthesis.Output.BridgeSchema)
	synthesis.Output.NewCollision = strings.TrimSpace(synthesis.Output.NewCollision)
	synthesis.Output.TransferSteps = compactStrings(synthesis.Output.TransferSteps)
	synthesis.Output.Risks = compactStrings(synthesis.Output.Risks)
	synthesis.Output.StressTest = strings.TrimSpace(synthesis.Output.StressTest)
}

func memoryCollisionCacheRecordMatches(record MemoryCollisionCacheRecord, opts MemoryCollisionCacheLoadOptions) bool {
	if opts.WorkspaceID != "" && strings.TrimSpace(opts.WorkspaceID) != record.WorkspaceID {
		return false
	}
	if opts.QueryID != "" && strings.TrimSpace(opts.QueryID) != record.Query.ID {
		return false
	}
	if opts.QueryDomain != "" && strings.TrimSpace(opts.QueryDomain) != record.Query.Domain {
		return false
	}
	if opts.MechanismTag != "" && !memoryCollisionCacheRecordHasTag(record, opts.MechanismTag) {
		return false
	}
	if opts.BisociationMode == "" && opts.SelectionMode == "" && opts.PromptAbstraction == "" && opts.MemoryDomain == "" && opts.AgentModel == "" {
		return true
	}
	for _, synthesis := range record.Syntheses {
		if memoryCollisionCacheSynthesisMatches(synthesis, opts) {
			return true
		}
	}
	return false
}

func memoryCollisionCacheSynthesisMatches(synthesis MemoryCollisionCacheSynthesisRecord, opts MemoryCollisionCacheLoadOptions) bool {
	if opts.BisociationMode != "" && NormalizeMemoryCollisionAgentMode(opts.BisociationMode) != synthesis.BisociationMode {
		return false
	}
	if opts.SelectionMode != "" && strings.TrimSpace(opts.SelectionMode) != synthesis.SelectionMode {
		return false
	}
	if opts.PromptAbstraction != "" && strings.TrimSpace(opts.PromptAbstraction) != synthesis.PromptAbstraction {
		return false
	}
	if opts.MemoryDomain != "" && strings.TrimSpace(opts.MemoryDomain) != synthesis.Collision.MemoryDomain {
		return false
	}
	if opts.AgentModel != "" && strings.TrimSpace(opts.AgentModel) != memoryCollisionCacheSynthesisModelLabelRecord(synthesis) {
		return false
	}
	return true
}

func memoryCollisionCacheRecordHasTag(record MemoryCollisionCacheRecord, tag string) bool {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return true
	}
	for _, candidate := range record.Query.MechanismTags {
		if candidate == tag {
			return true
		}
	}
	for _, synthesis := range record.Syntheses {
		for _, candidate := range synthesis.Collision.MechanismTags {
			if candidate == tag {
				return true
			}
		}
	}
	return false
}

func memoryCollisionCacheSynthesisCell(workspaceID string, query MechanismQuery, record MemoryCollisionCacheRecord, synthesis MemoryCollisionCacheSynthesisRecord) MemoryCollisionCell {
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = record.WorkspaceID
	}
	dedupeKey := "memory_collision_cache_cell:" + digestParts(
		workspaceID,
		query.ID,
		record.DedupeKey,
		synthesis.Collision.CollisionID,
		synthesis.BisociationMode,
		synthesis.Output.NewCollision,
	)
	score := synthesis.Collision.CollisionScore
	if !validCollisionScore(score) || score <= 0 {
		score = defaultMemoryCollisionThreshold
	}
	if score > 0.89 {
		score = 0.89
	}
	structural := synthesis.Collision.StructuralSimilarity
	if !validCollisionScore(structural) || structural == 0 {
		structural = 0.80
	}
	literal := synthesis.Collision.LiteralSimilarity
	if !validCollisionScore(literal) {
		literal = 0
	}
	sourceRefs := stringsToEvidenceRefs(append(append([]string(nil), record.Query.SourceRefs...), synthesis.Collision.SourceRefs...))
	if record.NotePath != "" {
		sourceRefs = append(sourceRefs, contextengine.EvidenceRef{Type: contextengine.RefTypeNote, Ref: record.NotePath})
	}
	return MemoryCollisionCell{
		DedupeKey:            dedupeKey,
		CollisionID:          "memory_collision_cache:" + digestParts(workspaceID, dedupeKey)[:24],
		TextID:               stableCollisionComponentID("text", firstNonEmpty(query.ID, query.Text)),
		SetID:                stableCollisionComponentID("set", record.DedupeKey),
		Strategy:             memoryCollisionCacheStrategy,
		QueryDomain:          strings.TrimSpace(query.Domain),
		MemoryDomain:         strings.TrimSpace(synthesis.Collision.MemoryDomain),
		QueryText:            strings.TrimSpace(query.Text),
		MemoryID:             "collision_cache:" + digestParts(record.DedupeKey, synthesis.Output.NewCollision)[:16],
		MemorySummary:        strings.TrimSpace(synthesis.Output.NewCollision),
		AbstractSchema:       firstNonEmpty(strings.TrimSpace(synthesis.Output.BridgeSchema), strings.TrimSpace(synthesis.Collision.AbstractSchema)),
		MechanismTags:        normalizeMechanismTags(append(append([]string(nil), record.Query.MechanismTags...), synthesis.Collision.MechanismTags...)),
		LiteralSimilarity:    roundCollisionScore(literal),
		StructuralSimilarity: roundCollisionScore(structural),
		CollisionScore:       roundCollisionScore(score),
		SourceRefs:           compactEvidenceRefs(sourceRefs),
		Reason:               "cached collision synthesis from " + record.NotePath + " using " + synthesis.BisociationMode + " mode",
	}
}

func memoryCollisionCacheSynthesisModelLabelRecord(synthesis MemoryCollisionCacheSynthesisRecord) string {
	provider := strings.TrimSpace(synthesis.AgentProvider)
	model := strings.TrimSpace(synthesis.AgentModel)
	switch {
	case provider != "" && model != "":
		return provider + "/" + model
	case model != "":
		return model
	case provider != "":
		return provider
	default:
		return ""
	}
}

func successfulMemoryCollisionSyntheses(in []MemoryCollisionSynthesis) []MemoryCollisionSynthesis {
	out := make([]MemoryCollisionSynthesis, 0, len(in))
	seen := map[string]struct{}{}
	for _, synthesis := range in {
		if !synthesis.Validation.Valid {
			continue
		}
		collisionID := strings.TrimSpace(synthesis.Collision.CollisionID)
		if collisionID == "" || strings.TrimSpace(synthesis.Output.NewCollision) == "" {
			continue
		}
		if _, ok := seen[collisionID]; ok {
			continue
		}
		seen[collisionID] = struct{}{}
		synthesis.AgentRole = firstNonEmpty(strings.TrimSpace(synthesis.AgentRole), defaultMemoryCollisionAgentRole)
		synthesis.BisociationMode = NormalizeMemoryCollisionAgentMode(synthesis.BisociationMode)
		synthesis.SelectionMode = firstNonEmpty(strings.TrimSpace(synthesis.SelectionMode), memoryCollisionCacheSelectionMode(synthesis.BisociationMode))
		synthesis.PromptAbstraction = firstNonEmpty(strings.TrimSpace(synthesis.PromptAbstraction), memoryCollisionCachePromptAbstraction(synthesis.BisociationMode))
		out = append(out, synthesis)
	}
	return out
}

func memoryCollisionSynthesisDedupeParts(syntheses []MemoryCollisionSynthesis) []string {
	values := make([]string, 0, len(syntheses)*4)
	for _, synthesis := range syntheses {
		values = append(
			values,
			synthesis.Collision.CollisionID,
			NormalizeMemoryCollisionAgentMode(synthesis.BisociationMode),
			memoryCollisionSynthesisModelLabel(synthesis),
			synthesis.Output.NewCollision,
		)
	}
	return compactStrings(values)
}

func memoryCollisionSynthesisIDs(syntheses []MemoryCollisionSynthesis) []string {
	values := make([]string, 0, len(syntheses))
	for _, synthesis := range syntheses {
		values = append(values, synthesis.Collision.CollisionID)
	}
	return compactStrings(values)
}

func memoryCollisionSynthesisMemoryIDs(syntheses []MemoryCollisionSynthesis) []string {
	values := make([]string, 0, len(syntheses))
	for _, synthesis := range syntheses {
		values = append(values, synthesis.Collision.MemoryID)
	}
	return compactStrings(values)
}

func memoryCollisionSynthesisMemoryDomains(syntheses []MemoryCollisionSynthesis) []string {
	values := make([]string, 0, len(syntheses))
	for _, synthesis := range syntheses {
		values = append(values, synthesis.Collision.MemoryDomain)
	}
	return compactStrings(values)
}

func memoryCollisionSynthesisAgentRoles(syntheses []MemoryCollisionSynthesis) []string {
	values := make([]string, 0, len(syntheses))
	for _, synthesis := range syntheses {
		values = append(values, synthesis.AgentRole)
	}
	return compactStrings(values)
}

func memoryCollisionSynthesisAgentModels(syntheses []MemoryCollisionSynthesis) []string {
	values := make([]string, 0, len(syntheses))
	for _, synthesis := range syntheses {
		values = append(values, memoryCollisionSynthesisModelLabel(synthesis))
	}
	return compactStrings(values)
}

func memoryCollisionSynthesisModes(syntheses []MemoryCollisionSynthesis) []string {
	values := make([]string, 0, len(syntheses))
	for _, synthesis := range syntheses {
		values = append(values, NormalizeMemoryCollisionAgentMode(synthesis.BisociationMode))
	}
	return compactStrings(values)
}

func memoryCollisionSynthesisSelectionModes(syntheses []MemoryCollisionSynthesis) []string {
	values := make([]string, 0, len(syntheses))
	for _, synthesis := range syntheses {
		values = append(values, firstNonEmpty(synthesis.SelectionMode, memoryCollisionCacheSelectionMode(synthesis.BisociationMode)))
	}
	return compactStrings(values)
}

func memoryCollisionSynthesisPromptAbstractions(syntheses []MemoryCollisionSynthesis) []string {
	values := make([]string, 0, len(syntheses))
	for _, synthesis := range syntheses {
		values = append(values, firstNonEmpty(synthesis.PromptAbstraction, memoryCollisionCachePromptAbstraction(synthesis.BisociationMode)))
	}
	return compactStrings(values)
}

func memoryCollisionCacheSelectionMode(mode string) string {
	switch NormalizeMemoryCollisionAgentMode(mode) {
	case MemoryCollisionAgentModeFar, MemoryCollisionAgentModeFarAlien:
		return "far"
	default:
		return "balanced"
	}
}

func memoryCollisionCachePromptAbstraction(mode string) string {
	switch NormalizeMemoryCollisionAgentMode(mode) {
	case MemoryCollisionAgentModeAlien, MemoryCollisionAgentModeFarAlien:
		return "alien"
	default:
		return "grounded"
	}
}

func memoryCollisionSynthesisModelLabel(synthesis MemoryCollisionSynthesis) string {
	provider := strings.TrimSpace(synthesis.AgentProvider)
	model := strings.TrimSpace(synthesis.AgentModel)
	switch {
	case provider != "" && model != "":
		return provider + "/" + model
	case model != "":
		return model
	case provider != "":
		return provider
	default:
		return ""
	}
}

func memoryCollisionCacheMechanismTags(query MechanismQuery, syntheses []MemoryCollisionSynthesis) []string {
	values := append([]string(nil), query.MechanismTags...)
	for _, synthesis := range syntheses {
		values = append(values, synthesis.Collision.MechanismTags...)
	}
	return normalizeMechanismTags(values)
}

func memoryCollisionCacheSourceRefs(query MechanismQuery, syntheses []MemoryCollisionSynthesis) []contextengine.EvidenceRef {
	refs := append([]contextengine.EvidenceRef(nil), query.SourceRefs...)
	for _, synthesis := range syntheses {
		refs = append(refs, synthesis.Collision.SourceRefs...)
	}
	return compactEvidenceRefs(refs)
}

func memoryCollisionCacheQueryTitle(query MechanismQuery) string {
	name := functionNameFromSignature(query.Text)
	if name == "" {
		name = truncatePlain(query.Text, 72)
	}
	if name == "" {
		name = lastDomainComponent(query.ID)
	}
	domain := lastDomainComponent(query.Domain)
	if domain != "" {
		return truncatePlain(name+" ("+domain+")", 96)
	}
	return truncatePlain(name, 96)
}

func functionNameFromSignature(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "func ") {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(text, "func "))
	if strings.HasPrefix(rest, "(") {
		end := strings.Index(rest, ")")
		if end < 0 || end+1 >= len(rest) {
			return ""
		}
		rest = strings.TrimSpace(rest[end+1:])
	}
	if idx := strings.Index(rest, "("); idx >= 0 {
		return strings.TrimSpace(rest[:idx])
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func lastDomainComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ':', '/', '\\':
			return true
		default:
			return false
		}
	})
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part != "" {
			return part
		}
	}
	return value
}

func writeMarkdownCodeBlock(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	if value == "" {
		return
	}
	b.WriteString(label)
	b.WriteString(":\n\n```text\n")
	b.WriteString(value)
	b.WriteString("\n```\n\n")
}

func markdownOneLine(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func escapeInlineCode(value string) string {
	return strings.ReplaceAll(markdownOneLine(value), "`", "'")
}
