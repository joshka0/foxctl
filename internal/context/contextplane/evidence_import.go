package contextplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/verification"
	"github.com/joshka0/foxctl/internal/platform/timeutil"
	llmproviders "github.com/joshka0/foxctl/internal/providers/llm"
	"github.com/joshka0/foxctl/internal/storage/cas"
	obsidiantool "github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
	"gopkg.in/yaml.v3"
)

const evidenceArtifactInlineLimit = 8 * 1024

type EvidenceImportInput struct {
	Title      string
	SourceKind string
	SourceRef  string
	Content    string
}

type EvidenceExtraction struct {
	Summary        string   `json:"summary"`
	Claims         []string `json:"claims,omitempty"`
	Frameworks     []string `json:"frameworks,omitempty"`
	ActionItems    []string `json:"action_items,omitempty"`
	OpenQuestions  []string `json:"open_questions,omitempty"`
	ProcessorKind  string   `json:"processor_kind,omitempty"`
	ProcessorModel string   `json:"processor_model,omitempty"`
}

type EvidenceImportResult struct {
	Run        EvidenceImportRun  `json:"run"`
	Extraction EvidenceExtraction `json:"extraction"`
	Proposal   MemoryProposal     `json:"proposal"`
}

type EvidenceTargetSuggestion struct {
	Path    string `json:"path,omitempty"`
	Heading string `json:"heading,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func (s *WorkspaceStore) ImportEvidence(ctx context.Context, casRoot, vaultPath string, in EvidenceImportInput) (EvidenceImportResult, error) {
	if strings.TrimSpace(vaultPath) == "" {
		return EvidenceImportResult{}, fmt.Errorf("vault path is required")
	}
	if _, err := s.EnsureLayout(); err != nil {
		return EvidenceImportResult{}, err
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return EvidenceImportResult{}, fmt.Errorf("content is required")
	}
	sourceKind := firstNonEmpty(strings.TrimSpace(in.SourceKind), "text")
	sourceRef := firstNonEmpty(strings.TrimSpace(in.SourceRef), sourceKind)
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = suggestedEvidenceTitle(sourceKind, sourceRef, content)
	}

	extraction, err := summarizeEvidenceWithFallback(ctx, content)
	if err != nil {
		return EvidenceImportResult{}, err
	}
	if strings.TrimSpace(extraction.Summary) == "" {
		extraction.Summary = truncateSentences(content, 2, 280)
	}

	artifactDigest := ""
	if len(content) > evidenceArtifactInlineLimit {
		if strings.TrimSpace(casRoot) == "" {
			return EvidenceImportResult{}, fmt.Errorf("cas root is required when evidence exceeds inline limit")
		}
		artifactDigest, err = persistEvidenceContentArtifact(ctx, casRoot, content)
		if err != nil {
			return EvidenceImportResult{}, err
		}
		if strings.TrimSpace(artifactDigest) == "" {
			return EvidenceImportResult{}, fmt.Errorf("persist evidence content artifact returned empty digest")
		}
	}

	writer := obsidiantool.NewWriter("", "", obsidiantool.DefaultPolicy())
	writer.VaultPath = vaultPath
	project := filepath.Base(strings.TrimSpace(s.layout.WorkspacePath))
	draftPath := filepath.ToSlash(filepath.Join(writer.Policy.InboxPrefix, "external-evidence", project, fmt.Sprintf("%s-%s.md", safeFileSlug(title, "evidence-import"), timeutil.NowUTC().Format("20060102T150405Z"))))
	if err := writer.CreateNote(ctx, draftPath, renderEvidenceImportDraft(title, sourceKind, sourceRef, artifactDigest, extraction, content), true); err != nil {
		return EvidenceImportResult{}, err
	}

	run := EvidenceImportRun{
		ID:             buildRecordID("E", timeutil.NowUTC()),
		SourceKind:     sourceKind,
		SourceRef:      sourceRef,
		Title:          title,
		DraftPath:      draftPath,
		ArtifactDigest: artifactDigest,
		ProcessorKind:  extraction.ProcessorKind,
		ProcessorModel: extraction.ProcessorModel,
		Summary:        extraction.Summary,
		Status:         "drafted",
		CreatedAt:      timeutil.NowUTC(),
	}
	target, err := suggestEvidenceMergeTarget(ctx, vaultPath, run, extraction)
	if err != nil {
		return EvidenceImportResult{}, err
	}
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return EvidenceImportResult{}, err
	}
	defer func() { _ = closeFn() }()
	if err := insertEvidenceImportRunRow(ctx, db, run); err != nil {
		return EvidenceImportResult{}, fmt.Errorf("record evidence import run: %w", err)
	}
	proposal, err := s.RecordEvidenceImportProposal(ctx, run, extraction, target)
	if err != nil {
		return EvidenceImportResult{}, fmt.Errorf("record evidence import proposal: %w", err)
	}
	return EvidenceImportResult{
		Run:        run,
		Extraction: extraction,
		Proposal:   proposal,
	}, nil
}

func summarizeEvidenceWithFallback(ctx context.Context, content string) (EvidenceExtraction, error) {
	if client, ok := newLocalEvidenceLLMClient(); ok {
		out, err := summarizeEvidenceWithLocalModel(ctx, client, content)
		if err == nil && strings.TrimSpace(out.Summary) != "" {
			return out, nil
		}
	}
	return deterministicEvidenceExtraction(content), nil
}

func newLocalEvidenceLLMClient() (verification.LLMClient, bool) {
	baseURL := strings.TrimSpace(os.Getenv("FOXCTL_ACA_L6_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("FOXCTL_OPENAI_COMPAT_BASE_URL"))
	}
	if baseURL == "" {
		return nil, false
	}
	model := strings.TrimSpace(os.Getenv("FOXCTL_ACA_L6_MODEL"))
	if model == "" {
		model = strings.TrimSpace(os.Getenv("FOXCTL_OPENAI_COMPAT_MODEL"))
	}
	if model == "" {
		model = llmproviders.DefaultModelForProvider("lmstudio")
	}
	apiKey := strings.TrimSpace(os.Getenv("FOXCTL_ACA_L6_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("FOXCTL_OPENAI_COMPAT_API_KEY"))
	}
	if apiKey == "" && (strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1")) {
		apiKey = "lm-studio"
	}
	client, err := verification.NewOpenAIClient(verification.OpenAIConfig{
		Provider: "lmstudio",
		BaseURL:  baseURL,
		APIKey:   apiKey,
		Model:    model,
		Timeout:  45 * time.Second,
	})
	if err != nil {
		return nil, false
	}
	return client, true
}

func summarizeEvidenceWithLocalModel(ctx context.Context, client verification.LLMClient, content string) (EvidenceExtraction, error) {
	systemPrompt := `You are a local ACA L6 summarizer. Produce a lightweight evidence extraction.
Return only valid JSON with keys:
summary, claims, frameworks, action_items, open_questions.
Keep the summary to 2-4 sentences.
Use short strings in arrays.`
	userPrompt := fmt.Sprintf(`Extract durable evidence from this source text.

Source text:
%s`, truncateContentWindow(content, 12000))
	raw, err := client.Chat(ctx, systemPrompt, userPrompt, verification.LLMCallOptions{
		MaxTokens:   1200,
		Temperature: 0,
	})
	if err != nil {
		return EvidenceExtraction{}, err
	}
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "```json"))
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "```"))
	var parsed struct {
		Summary       string   `json:"summary"`
		Claims        []string `json:"claims"`
		Frameworks    []string `json:"frameworks"`
		ActionItems   []string `json:"action_items"`
		OpenQuestions []string `json:"open_questions"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return EvidenceExtraction{}, err
	}
	return EvidenceExtraction{
		Summary:        strings.TrimSpace(parsed.Summary),
		Claims:         uniqueStrings(parsed.Claims),
		Frameworks:     uniqueStrings(parsed.Frameworks),
		ActionItems:    uniqueStrings(parsed.ActionItems),
		OpenQuestions:  uniqueStrings(parsed.OpenQuestions),
		ProcessorKind:  "local_llm",
		ProcessorModel: inferLocalProcessorModel(),
	}, nil
}

func deterministicEvidenceExtraction(content string) EvidenceExtraction {
	lines := normalizedEvidenceLines(content)
	return EvidenceExtraction{
		Summary: truncateSentences(content, 2, 280),
		Claims:  takeInterestingLines(lines, func(s string) bool { return len(s) >= 40 && !strings.HasSuffix(s, "?") }, 5),
		Frameworks: takeInterestingLines(lines, func(s string) bool {
			return strings.Contains(strings.ToLower(s), "framework") || strings.Contains(strings.ToLower(s), "model") || strings.Contains(strings.ToLower(s), "pattern")
		}, 3),
		ActionItems:    takeInterestingLines(lines, looksActionableLine, 5),
		OpenQuestions:  takeInterestingLines(lines, func(s string) bool { return strings.HasSuffix(strings.TrimSpace(s), "?") }, 5),
		ProcessorKind:  "deterministic",
		ProcessorModel: "",
	}
}

func persistEvidenceContentArtifact(ctx context.Context, casRoot, content string) (string, error) {
	casRoot = strings.TrimSpace(casRoot)
	if casRoot == "" {
		return "", nil
	}
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()
	obj, err := store.Put(ctx, bytes.NewReader([]byte(content)), "text/plain", []string{"aca-external-evidence"})
	if err != nil {
		return "", err
	}
	return obj.Digest, nil
}

func renderEvidenceImportDraft(title, sourceKind, sourceRef, artifactDigest string, extraction EvidenceExtraction, rawContent string) string {
	var b strings.Builder
	frontmatter := map[string]any{
		"title":           title,
		"type":            "evidence",
		"status":          "draft",
		"trust":           "raw",
		"provenance_refs": buildEvidenceProvenanceRefs(sourceKind, sourceRef, artifactDigest),
		"updated":         timeutil.NowUTC().Format("2006-01-02"),
	}
	frontmatterYAML, err := yaml.Marshal(frontmatter)
	if err == nil {
		b.WriteString("---\n")
		b.Write(frontmatterYAML)
		b.WriteString("---\n\n")
	} else {
		b.WriteString("---\n")
		b.WriteString("title: Imported External Evidence\n")
		b.WriteString("type: evidence\nstatus: draft\ntrust: raw\n---\n\n")
	}
	b.WriteString("# " + title + "\n\n")
	b.WriteString("## Summary\n\n")
	b.WriteString(firstNonEmpty(strings.TrimSpace(extraction.Summary), "No summary generated.") + "\n\n")
	writeDraftSection(&b, "Claims", extraction.Claims)
	writeDraftSection(&b, "Frameworks", extraction.Frameworks)
	writeDraftSection(&b, "Action Items", extraction.ActionItems)
	writeDraftSection(&b, "Open Questions", extraction.OpenQuestions)
	b.WriteString("## Provenance\n\n")
	b.WriteString(fmt.Sprintf("- Source kind: `%s`\n", sourceKind))
	b.WriteString(fmt.Sprintf("- Source ref: `%s`\n", sourceRef))
	if strings.TrimSpace(extraction.ProcessorKind) != "" {
		b.WriteString(fmt.Sprintf("- Processor: `%s`\n", extraction.ProcessorKind))
	}
	if strings.TrimSpace(extraction.ProcessorModel) != "" {
		b.WriteString(fmt.Sprintf("- Processor model: `%s`\n", extraction.ProcessorModel))
	}
	if strings.TrimSpace(artifactDigest) != "" {
		b.WriteString(fmt.Sprintf("- Artifact digest: `%s`\n", artifactDigest))
	}
	b.WriteString("\n## Raw Excerpt\n\n")
	if strings.TrimSpace(artifactDigest) != "" {
		b.WriteString("Full source content is stored in CAS.\n\n")
	}
	b.WriteString("```\n")
	b.WriteString(truncateContentWindow(rawContent, 2000))
	b.WriteString("\n```\n\n")
	b.WriteString("## Promotion Notes\n\n")
	b.WriteString("- Review extracted claims before merging into canonical notes.\n")
	b.WriteString("- Preserve external provenance when promoting durable knowledge.\n")
	return b.String()
}

func buildEvidenceProvenanceRefs(sourceKind, sourceRef, artifactDigest string) []string {
	refs := []string{
		fmt.Sprintf("external:%s:%s", strings.TrimSpace(sourceKind), strings.TrimSpace(sourceRef)),
	}
	if strings.TrimSpace(artifactDigest) != "" {
		refs = append(refs, "artifact:"+strings.TrimSpace(artifactDigest))
	}
	return refs
}

func suggestedEvidenceTitle(sourceKind, sourceRef, content string) string {
	if trimmed := strings.TrimSpace(sourceRef); trimmed != "" {
		base := strings.TrimSuffix(filepath.Base(trimmed), filepath.Ext(trimmed))
		base = strings.TrimSpace(strings.ReplaceAll(base, "-", " "))
		if base != "" {
			return titleCaseWords(base)
		}
	}
	switch strings.TrimSpace(sourceKind) {
	case "transcript":
		return "Imported Transcript Evidence"
	case "file":
		return "Imported File Evidence"
	default:
		summary := truncateSentences(content, 1, 80)
		if summary != "" {
			return summary
		}
		return "Imported External Evidence"
	}
}

func inferLocalProcessorModel() string {
	if model := strings.TrimSpace(os.Getenv("FOXCTL_ACA_L6_MODEL")); model != "" {
		return model
	}
	if model := strings.TrimSpace(os.Getenv("FOXCTL_OPENAI_COMPAT_MODEL")); model != "" {
		return model
	}
	if model := strings.TrimSpace(os.Getenv("LMSTUDIO_MODEL")); model != "" {
		return model
	}
	return llmproviders.DefaultModelForProvider("lmstudio")
}

func normalizedEvidenceLines(content string) []string {
	fields := strings.FieldsFunc(content, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		line := strings.Join(strings.Fields(strings.TrimSpace(field)), " ")
		line = strings.TrimLeft(line, "-*0123456789. )(")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return uniqueStrings(out)
}

func takeInterestingLines(lines []string, keep func(string) bool, limit int) []string {
	out := make([]string, 0, limit)
	for _, line := range lines {
		if keep != nil && !keep(line) {
			continue
		}
		out = append(out, line)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func looksActionableLine(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "todo") ||
		strings.HasPrefix(lower, "action") ||
		strings.Contains(lower, "should ") ||
		strings.Contains(lower, "need to") ||
		strings.Contains(lower, "must ")
}

func truncateSentences(content string, maxSentences, maxChars int) string {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if content == "" {
		return ""
	}
	if maxSentences <= 0 {
		maxSentences = 1
	}
	parts := splitSimpleSentences(content)
	if len(parts) == 0 {
		return truncateContentWindow(content, maxChars)
	}
	selected := parts
	if len(selected) > maxSentences {
		selected = selected[:maxSentences]
	}
	return truncateContentWindow(strings.Join(selected, " "), maxChars)
}

func splitSimpleSentences(content string) []string {
	var out []string
	start := 0
	for i, r := range content {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		segment := strings.TrimSpace(content[start : i+1])
		if segment != "" {
			out = append(out, segment)
		}
		start = i + 1
	}
	if tail := strings.TrimSpace(content[start:]); tail != "" {
		out = append(out, tail)
	}
	return out
}

func truncateContentWindow(content string, maxChars int) string {
	if maxChars <= 0 {
		return strings.TrimSpace(content)
	}
	content = strings.TrimSpace(content)
	if len(content) <= maxChars {
		return content
	}
	return strings.TrimSpace(content[:maxChars]) + "..."
}

func normalizeEvidenceTopic(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "evidence"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "evidence"
	}
	return out
}

func suggestEvidenceMergeTarget(ctx context.Context, vaultRoot string, run EvidenceImportRun, extraction EvidenceExtraction) (EvidenceTargetSuggestion, error) {
	candidates, err := loadCanonicalEvidenceTargetCandidates(ctx, vaultRoot)
	if err != nil {
		return EvidenceTargetSuggestion{}, err
	}
	if len(candidates) == 0 {
		return EvidenceTargetSuggestion{}, nil
	}
	queryText := strings.ToLower(strings.Join([]string{
		run.Title,
		extraction.Summary,
		strings.Join(extraction.Claims, " "),
		strings.Join(extraction.Frameworks, " "),
		strings.Join(extraction.ActionItems, " "),
	}, " "))
	keywords := evidenceTargetKeywords(queryText)
	if len(keywords) == 0 {
		return EvidenceTargetSuggestion{}, nil
	}

	bestPath := ""
	bestReason := ""
	bestScore := 0
	for _, candidate := range candidates {
		score, reason := scoreEvidenceTargetCandidate(candidate, keywords)
		if score > bestScore {
			bestScore = score
			bestPath = candidate.Path
			bestReason = reason
		}
	}
	if bestScore < 8 || strings.TrimSpace(bestPath) == "" {
		return EvidenceTargetSuggestion{}, nil
	}
	return EvidenceTargetSuggestion{
		Path:    bestPath,
		Heading: "Review",
		Reason:  bestReason,
	}, nil
}

type evidenceTargetCandidate struct {
	Path  string
	Title string
	Trust string
	Type  string
	Text  string
}

func loadCanonicalEvidenceTargetCandidates(ctx context.Context, vaultRoot string) ([]evidenceTargetCandidate, error) {
	root := strings.TrimSpace(vaultRoot)
	if root == "" {
		return nil, nil
	}
	var out []evidenceTargetCandidate
	for _, prefix := range []string{"00-home", "atlas", "notes"} {
		base := filepath.Join(root, filepath.FromSlash(prefix))
		info, err := os.Stat(base)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if ctx != nil {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".md" {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "inbox/") || strings.HasPrefix(rel, "sessions/") || strings.HasPrefix(rel, "ops/") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			title, trust, noteType, noteText := parseEvidenceTargetNote(body, rel)
			out = append(out, evidenceTargetCandidate{
				Path:  rel,
				Title: title,
				Trust: trust,
				Type:  noteType,
				Text:  noteText,
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func parseEvidenceTargetNote(body []byte, rel string) (title, trust, noteType, text string) {
	text = string(body)
	title = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	if strings.HasPrefix(text, "---\n") {
		if parts := strings.SplitN(text, "\n---\n", 2); len(parts) == 2 {
			for _, line := range strings.Split(parts[0], "\n") {
				trimmed := strings.TrimSpace(line)
				switch {
				case strings.HasPrefix(trimmed, "title:"):
					title = strings.TrimSpace(strings.TrimPrefix(trimmed, "title:"))
				case strings.HasPrefix(trimmed, "trust:"):
					trust = strings.TrimSpace(strings.TrimPrefix(trimmed, "trust:"))
				case strings.HasPrefix(trimmed, "type:"):
					noteType = strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
				}
			}
			text = parts[1]
		}
	}
	return title, trust, noteType, strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func evidenceTargetKeywords(queryText string) []string {
	parts := strings.FieldsFunc(strings.ToLower(queryText), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) < 4 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func scoreEvidenceTargetCandidate(candidate evidenceTargetCandidate, keywords []string) (int, string) {
	titleText := strings.ToLower(candidate.Title)
	score := 0
	reasons := make([]string, 0, 3)
	if strings.EqualFold(strings.TrimSpace(candidate.Trust), "canonical") {
		score += 5
		reasons = append(reasons, "canonical")
	} else if strings.EqualFold(strings.TrimSpace(candidate.Trust), "reviewed") {
		score += 2
	}
	switch strings.TrimSpace(candidate.Type) {
	case "map", "pattern", "adr":
		score += 2
	}
	titleMatches := 0
	bodyMatches := 0
	for _, keyword := range keywords {
		if strings.Contains(titleText, keyword) {
			score += 4
			titleMatches++
			continue
		}
		if strings.Contains(candidate.Text, keyword) {
			score += 1
			bodyMatches++
		}
	}
	if titleMatches > 0 {
		reasons = append(reasons, fmt.Sprintf("title_match:%d", titleMatches))
	}
	if bodyMatches > 0 {
		reasons = append(reasons, fmt.Sprintf("body_match:%d", bodyMatches))
	}
	return score, strings.Join(reasons, ", ")
}

func LoadEvidenceContent(text, textFile, transcriptFile string) (string, string, string, error) {
	switch {
	case strings.TrimSpace(text) != "":
		return "text", "inline", text, nil
	case strings.TrimSpace(transcriptFile) != "":
		body, err := os.ReadFile(strings.TrimSpace(transcriptFile))
		if err != nil {
			return "", "", "", err
		}
		return "transcript", strings.TrimSpace(transcriptFile), string(body), nil
	case strings.TrimSpace(textFile) != "":
		body, err := os.ReadFile(strings.TrimSpace(textFile))
		if err != nil {
			return "", "", "", err
		}
		return "file", strings.TrimSpace(textFile), string(body), nil
	default:
		return "", "", "", io.EOF
	}
}

func titleCaseWords(input string) string {
	parts := strings.Fields(input)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
