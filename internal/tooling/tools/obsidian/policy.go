package obsidian

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Policy constrains where the adapter may create or append note content.
type Policy struct {
	InboxPrefix           string
	SessionsPrefix        string
	OpsPrefix             string
	CanonicalPrefixes     []string
	AllowedAppendHeadings []string
	ReviewedMergeHeadings []string
}

// DefaultPolicy returns conservative write boundaries for vault automation.
func DefaultPolicy() Policy {
	return Policy{
		InboxPrefix:           "inbox/drafted-from-foxctl",
		SessionsPrefix:        "sessions",
		OpsPrefix:             "ops",
		CanonicalPrefixes:     []string{"00-home", "atlas", "notes"},
		AllowedAppendHeadings: []string{"Recent Findings", "Review", "Recent Sessions", "Recent Observations"},
		ReviewedMergeHeadings: []string{"Review", "Recent Findings"},
	}
}

// ValidateCreate ensures note creation is restricted to draft/ops/session paths.
func (p Policy) ValidateCreate(path string) error {
	path = normalizeVaultPath(path)
	switch {
	case hasVaultPrefix(path, p.InboxPrefix):
		return nil
	case hasVaultPrefix(path, p.SessionsPrefix):
		return nil
	case hasVaultPrefix(path, p.OpsPrefix):
		return nil
	default:
		return fmt.Errorf("obsidian policy: create denied for %s", path)
	}
}

// ValidateAppend ensures canonical appends only happen in bounded sections.
func (p Policy) ValidateAppend(path, heading string) error {
	path = normalizeVaultPath(path)
	if hasVaultPrefix(path, p.InboxPrefix) || hasVaultPrefix(path, p.SessionsPrefix) || hasVaultPrefix(path, p.OpsPrefix) {
		return nil
	}
	for _, prefix := range p.CanonicalPrefixes {
		if hasVaultPrefix(path, prefix) {
			if slices.Contains(p.AllowedAppendHeadings, strings.TrimSpace(heading)) {
				return nil
			}
			return fmt.Errorf("obsidian policy: append denied for heading %q in %s", heading, path)
		}
	}
	return fmt.Errorf("obsidian policy: append denied for %s", path)
}

// ValidateReviewedMerge ensures draft-to-canonical merges are explicit and bounded.
func (p Policy) ValidateReviewedMerge(sourcePath, targetPath, heading string) error {
	sourcePath = normalizeVaultPath(sourcePath)
	targetPath = normalizeVaultPath(targetPath)
	if !hasVaultPrefix(sourcePath, p.InboxPrefix) {
		return fmt.Errorf("obsidian policy: reviewed merge denied for source %s", sourcePath)
	}
	allowedTarget := false
	for _, prefix := range p.CanonicalPrefixes {
		if hasVaultPrefix(targetPath, prefix) {
			allowedTarget = true
			break
		}
	}
	if !allowedTarget {
		return fmt.Errorf("obsidian policy: reviewed merge denied for target %s", targetPath)
	}
	heading = strings.TrimSpace(heading)
	if heading == "" {
		return nil
	}
	if !slices.Contains(p.ReviewedMergeHeadings, heading) {
		return fmt.Errorf("obsidian policy: reviewed merge denied for heading %q in %s", heading, targetPath)
	}
	return nil
}

// ValidateReviewedMergeTarget ensures explicit reviewed merges only land in canonical areas.
func (p Policy) ValidateReviewedMergeTarget(path, heading string) error {
	path = normalizeVaultPath(path)
	trimmed := strings.TrimSpace(heading)
	for _, prefix := range p.CanonicalPrefixes {
		if !hasVaultPrefix(path, prefix) {
			continue
		}
		if trimmed == "" {
			return nil
		}
		if slices.Contains(p.ReviewedMergeHeadings, trimmed) {
			return nil
		}
		return fmt.Errorf("obsidian policy: reviewed merge denied for heading %q in %s", heading, path)
	}
	return fmt.Errorf("obsidian policy: reviewed merge denied for %s", path)
}

func normalizeVaultPath(path string) string {
	path = strings.TrimSpace(path)
	path = filepath.ToSlash(path)
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")
	return path
}

func hasVaultPrefix(path, prefix string) bool {
	prefix = normalizeVaultPath(prefix)
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
