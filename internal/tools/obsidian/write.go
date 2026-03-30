package obsidian

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Writer performs safe write-side operations through the Obsidian CLI.
type Writer struct {
	CLIPath         string
	VaultName       string
	VaultPath       string
	Policy          Policy
	PostCreateDelay time.Duration
}

// NewWriter returns a write-side adapter with conservative defaults.
func NewWriter(cliPath, vaultName string, policy Policy) *Writer {
	if strings.TrimSpace(cliPath) == "" {
		cliPath = "obsidian"
	}
	if strings.TrimSpace(policy.InboxPrefix) == "" &&
		strings.TrimSpace(policy.SessionsPrefix) == "" &&
		strings.TrimSpace(policy.OpsPrefix) == "" &&
		len(policy.CanonicalPrefixes) == 0 &&
		len(policy.AllowedAppendHeadings) == 0 {
		policy = DefaultPolicy()
	}
	return &Writer{
		CLIPath:         cliPath,
		VaultName:       vaultName,
		Policy:          policy,
		PostCreateDelay: 2 * time.Second,
	}
}

// CreateNote creates or overwrites a note in a write-allowed path.
func (w *Writer) CreateNote(ctx context.Context, notePath, content string, overwrite bool) error {
	if err := w.Policy.ValidateCreate(notePath); err != nil {
		return err
	}
	if strings.TrimSpace(w.VaultPath) != "" {
		return w.writeVaultFile(notePath, content, overwrite)
	}
	vaultName, err := ResolveVaultName(ctx, w.CLIPath, w.VaultName, w.VaultPath)
	if err != nil {
		return err
	}
	args := []string{"create", "vault=" + vaultName, "path=" + normalizeVaultPath(notePath), "content=" + content}
	if overwrite {
		args = append(args, "overwrite")
	}
	if _, err := w.run(ctx, args...); err != nil {
		return err
	}
	if w.PostCreateDelay > 0 {
		timer := time.NewTimer(w.PostCreateDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

// AppendUnderHeading appends content to a specific heading, rewriting the note safely through the CLI.
func (w *Writer) AppendUnderHeading(ctx context.Context, notePath, heading, content string) error {
	if err := w.Policy.ValidateAppend(notePath, heading); err != nil {
		return err
	}
	existing, err := w.Read(ctx, notePath)
	if err != nil {
		return err
	}
	updated := appendMarkdownUnderHeading(existing, heading, content)
	return w.writeNoteDirectOrCLI(ctx, notePath, updated, true)
}

// CaptureSession writes a session artifact into the sessions prefix.
func (w *Writer) CaptureSession(ctx context.Context, slug, content string) (string, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = time.Now().UTC().Format("2006-01-02-150405")
	}
	notePath := filepath.ToSlash(filepath.Join(w.Policy.SessionsPrefix, time.Now().UTC().Format("2006-01"), slug+".md"))
	return notePath, w.CreateNote(ctx, notePath, content, true)
}

// PromoteToEvergreen creates a durable-note draft in the inbox, not the canonical location.
func (w *Writer) PromoteToEvergreen(ctx context.Context, slug, content string) (string, error) {
	slug = safeSlug(slug)
	if slug == "" {
		slug = "promotion-draft"
	}
	notePath := filepath.ToSlash(filepath.Join(w.Policy.InboxPrefix, slug+".md"))
	return notePath, w.CreateNote(ctx, notePath, content, true)
}

// ReviewMergeDraft reads an inbox draft note and merges it into a canonical target explicitly.
func (w *Writer) ReviewMergeDraft(ctx context.Context, draftPath, targetPath, heading string) (ReviewedMergeResult, error) {
	if err := w.Policy.ValidateReviewedMerge(draftPath, targetPath, heading); err != nil {
		return ReviewedMergeResult{}, err
	}
	draft, err := w.readDraftPreferred(draftPath)
	if err != nil {
		draft, err = w.Read(ctx, draftPath)
		if err != nil {
			return ReviewedMergeResult{}, err
		}
	}
	if !strings.Contains(strings.ToLower(draft), "status: draft") {
		return ReviewedMergeResult{}, fmt.Errorf("obsidian write: reviewed merge requires a draft note at %s", normalizeVaultPath(draftPath))
	}
	return w.MergeReviewedDraftContent(ctx, targetPath, heading, draft, normalizeVaultPath(draftPath))
}

// ReviewedMergeResult describes how a reviewed draft was merged into the canonical knowledge plane.
type ReviewedMergeResult struct {
	TargetPath string `json:"target_path"`
	Heading    string `json:"heading,omitempty"`
	MergedAs   string `json:"merged_as"`
}

// MergeReviewedDraftContent explicitly merges reviewed draft content into a canonical note.
// If the canonical note does not exist, it is created in-place. Otherwise, the reviewed body
// is appended under the requested bounded heading.
func (w *Writer) MergeReviewedDraftContent(ctx context.Context, targetPath, heading, draftContent, sourceRef string) (ReviewedMergeResult, error) {
	if err := w.Policy.ValidateReviewedMergeTarget(targetPath, heading); err != nil {
		return ReviewedMergeResult{}, err
	}
	targetPath = normalizeVaultPath(targetPath)
	existing, err := w.Read(ctx, targetPath)
	if err != nil {
		if !isMissingNoteError(err) {
			return ReviewedMergeResult{}, err
		}
		canonical := canonicalizeDraftForCanonical(draftContent)
		if err := w.writeNoteDirectOrCLI(ctx, targetPath, canonical, true); err != nil {
			return ReviewedMergeResult{}, err
		}
		return ReviewedMergeResult{
			TargetPath: targetPath,
			MergedAs:   "create",
		}, nil
	}

	section := strings.TrimSpace(heading)
	if section == "" {
		section = "Review"
	}
	merged := mergeReviewedFrontmatter(existing, draftContent)
	merged = appendMarkdownUnderHeading(merged, section, renderReviewedMergeBlock(draftContent, sourceRef))
	if err := w.writeNoteDirectOrCLI(ctx, targetPath, merged, true); err != nil {
		return ReviewedMergeResult{}, err
	}
	return ReviewedMergeResult{
		TargetPath: targetPath,
		Heading:    section,
		MergedAs:   "append",
	}, nil
}

func mergeReviewedFrontmatter(existing, draftContent string) string {
	frontLists, frontValues, _, _ := parseBridgeFrontmatter(draftContent)
	merged := existing
	for _, key := range []string{"paths", "symbols", "anchor_paths", "impl_anchor_paths", "support_anchor_paths", "resource_anchor_paths"} {
		updated, _ := mergeMarkdownFrontmatterList(merged, key, frontLists[key])
		merged = updated
	}
	values := map[string]string{
		"status": "reviewed",
		"trust":  "canonical",
	}
	if primaryAnchorPath := strings.TrimSpace(frontValues["primary_anchor_path"]); primaryAnchorPath != "" {
		values["primary_anchor_path"] = primaryAnchorPath
	}
	merged = setMarkdownFrontmatterValues(merged, values)
	return merged
}

// Read fetches note contents through the CLI.
func (w *Writer) Read(ctx context.Context, notePath string) (string, error) {
	if strings.TrimSpace(w.VaultPath) != "" {
		return w.readVaultFile(notePath)
	}
	result, err := Read(ctx, ReadOptions{
		BinaryPath: w.CLIPath,
		VaultName:  w.VaultName,
		VaultPath:  w.VaultPath,
		NotePath:   normalizeVaultPath(notePath),
	})
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (w *Writer) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, w.CLIPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("obsidian write: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func appendMarkdownUnderHeading(markdown, heading, content string) string {
	lines := strings.Split(markdown, "\n")
	target := strings.TrimSpace(heading)
	insert := strings.TrimRight(content, "\n") + "\n"
	for i, line := range lines {
		level, title, ok := headingLine(line)
		if !ok || title != target {
			continue
		}
		j := i + 1
		for j < len(lines) {
			nextLevel, _, ok := headingLine(lines[j])
			if ok && nextLevel <= level {
				break
			}
			j++
		}
		out := append([]string{}, lines[:j]...)
		out = append(out, strings.Split(strings.TrimRight(insert, "\n"), "\n")...)
		out = append(out, lines[j:]...)
		return strings.Join(out, "\n")
	}
	trimmed := strings.TrimRight(markdown, "\n")
	if trimmed != "" {
		trimmed += "\n\n"
	}
	return trimmed + "## " + target + "\n\n" + insert
}

func renderReviewedMergeBlock(draftContent, sourceRef string) string {
	var b strings.Builder
	stamp := time.Now().UTC().Format(time.RFC3339)
	b.WriteString("### Reviewed Merge\n\n")
	b.WriteString("- Reviewed at: `" + stamp + "`\n")
	if trimmed := strings.TrimSpace(sourceRef); trimmed != "" {
		b.WriteString("- Source draft: `" + trimmed + "`\n")
	}
	b.WriteString("\n")
	body := extractDraftBody(draftContent)
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

func canonicalizeDraftForCanonical(markdown string) string {
	lines := strings.Split(markdown, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return strings.TrimRight(markdown, "\n") + "\n"
	}
	out := make([]string, 0, len(lines)+2)
	out = append(out, "---")
	i := 1
	statusSeen := false
	trustSeen := false
	for ; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			break
		}
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "status:"):
			out = append(out, "status: reviewed")
			statusSeen = true
		case strings.HasPrefix(lower, "trust:"):
			out = append(out, "trust: canonical")
			trustSeen = true
		default:
			out = append(out, line)
		}
	}
	if !statusSeen {
		out = append(out, "status: reviewed")
	}
	if !trustSeen {
		out = append(out, "trust: canonical")
	}
	out = append(out, "---")
	if i < len(lines) && strings.TrimSpace(lines[i]) == "---" {
		i++
	}
	out = append(out, lines[i:]...)
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

func extractDraftBody(markdown string) string {
	markdown = strings.TrimSpace(stripMarkdownFrontmatter(markdown))
	if markdown == "" {
		return ""
	}
	lines := strings.Split(markdown, "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "# ") {
		lines = lines[1:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func stripMarkdownFrontmatter(markdown string) string {
	if !strings.HasPrefix(markdown, "---\n") {
		return markdown
	}
	parts := strings.SplitN(markdown, "\n---\n", 2)
	if len(parts) != 2 {
		return markdown
	}
	return parts[1]
}

func isMissingNoteError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "file not found") || strings.Contains(msg, "no such file") || strings.Contains(msg, os.ErrNotExist.Error())
}

func (w *Writer) readDraftPreferred(draftPath string) (string, error) {
	return w.readVaultFile(draftPath)
}

func (w *Writer) readVaultFile(notePath string) (string, error) {
	if strings.TrimSpace(w.VaultPath) == "" {
		return "", os.ErrNotExist
	}
	full := filepath.Join(w.VaultPath, filepath.FromSlash(normalizeVaultPath(notePath)))
	body, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (w *Writer) writeVaultFile(notePath, content string, overwrite bool) error {
	if strings.TrimSpace(w.VaultPath) == "" {
		return os.ErrNotExist
	}
	full := filepath.Join(w.VaultPath, filepath.FromSlash(normalizeVaultPath(notePath)))
	if !overwrite {
		if _, err := os.Stat(full); err == nil {
			return fmt.Errorf("obsidian write: file already exists: %s", normalizeVaultPath(notePath))
		}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

func (w *Writer) writeNoteDirectOrCLI(ctx context.Context, notePath, content string, overwrite bool) error {
	if strings.TrimSpace(w.VaultPath) != "" {
		return w.writeVaultFile(notePath, content, overwrite)
	}
	vaultName, err := ResolveVaultName(ctx, w.CLIPath, w.VaultName, w.VaultPath)
	if err != nil {
		return err
	}
	args := []string{"create", "vault=" + vaultName, "path=" + normalizeVaultPath(notePath), "content=" + content}
	if overwrite {
		args = append(args, "overwrite")
	}
	_, err = w.run(ctx, args...)
	return err
}

func headingLine(line string) (int, string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	title := strings.TrimSpace(line[level:])
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}

func safeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}
