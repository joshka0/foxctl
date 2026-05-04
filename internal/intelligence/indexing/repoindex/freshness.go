package repoindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"sort"
	"strings"
)

// ResolveGitSnapshot returns best-effort commit and dirty-state metadata for repoRoot.
func ResolveGitSnapshot(ctx context.Context, repoRoot string) GitSnapshot {
	status := ResolveGitStatusPorcelain(ctx, repoRoot)
	snap := GitSnapshot{
		HeadSHA:         ResolveGitHead(ctx, repoRoot),
		WorktreeDirty:   strings.TrimSpace(status) != "",
		DirtyStatusHash: hashGitStatus(status),
	}
	snap.DefaultRef, snap.DefaultRefSHA = ResolveGitDefaultRef(ctx, repoRoot)
	if snap.HeadSHA != "" && snap.DefaultRefSHA != "" {
		snap.MergeBaseSHA = resolveGitMergeBase(ctx, repoRoot, snap.HeadSHA, snap.DefaultRefSHA)
		ahead, behind := resolveGitAheadBehind(ctx, repoRoot, snap.DefaultRefSHA, snap.HeadSHA)
		snap.CommitsAhead = ahead
		snap.CommitsBehind = behind
	}
	return snap
}

// IndexMetaFromGitSnapshot copies a snapshot into index metadata.
func IndexMetaFromGitSnapshot(meta IndexMeta, snap GitSnapshot) IndexMeta {
	meta.HeadSHA = snap.HeadSHA
	meta.WorktreeDirty = snap.WorktreeDirty
	meta.DirtyStatusHash = snap.DirtyStatusHash
	meta.DefaultRef = snap.DefaultRef
	meta.DefaultRefSHA = snap.DefaultRefSHA
	meta.MergeBaseSHA = snap.MergeBaseSHA
	meta.CommitsAhead = snap.CommitsAhead
	meta.CommitsBehind = snap.CommitsBehind
	return meta
}

// CompareIndexFreshness compares stored index metadata with current git state.
func CompareIndexFreshness(meta IndexMeta, current GitSnapshot) IndexFreshnessStatus {
	status := IndexFreshnessStatus{
		Level:            FreshnessCurrent,
		IndexHeadSHA:     meta.HeadSHA,
		CurrentHeadSHA:   current.HeadSHA,
		IndexDirty:       meta.WorktreeDirty,
		CurrentDirty:     current.WorktreeDirty,
		IndexDirtyHash:   meta.DirtyStatusHash,
		CurrentDirtyHash: current.DirtyStatusHash,
		DefaultRef:       firstNonEmpty(current.DefaultRef, meta.DefaultRef),
		DefaultRefSHA:    firstNonEmpty(current.DefaultRefSHA, meta.DefaultRefSHA),
		MergeBaseSHA:     firstNonEmpty(current.MergeBaseSHA, meta.MergeBaseSHA),
		CommitsAhead:     current.CommitsAhead,
		CommitsBehind:    current.CommitsBehind,
	}
	if meta.HeadSHA == "" || current.HeadSHA == "" {
		status.Level = FreshnessUnknown
		status.Reasons = append(status.Reasons, "head_unknown")
		return status
	}
	if meta.HeadSHA != current.HeadSHA {
		status.Level = FreshnessStale
		status.Reasons = append(status.Reasons, "head_mismatch")
	}
	if meta.WorktreeDirty != current.WorktreeDirty || (meta.DirtyStatusHash != "" && current.DirtyStatusHash != "" && meta.DirtyStatusHash != current.DirtyStatusHash) {
		if status.Level == FreshnessCurrent {
			status.Level = FreshnessDirty
		}
		status.Reasons = append(status.Reasons, "dirty_state_changed")
	}
	if current.CommitsBehind > 0 {
		if status.Level == FreshnessCurrent {
			status.Level = FreshnessBehind
		}
		status.Reasons = append(status.Reasons, "behind_default_ref")
	}
	if len(status.Reasons) == 0 {
		status.Reasons = nil
	}
	return status
}

// ResolveGitHead returns the current Git HEAD SHA for repoRoot when available.
func ResolveGitHead(ctx context.Context, repoRoot string) string {
	return runGitTrimmed(ctx, repoRoot, "rev-parse", "HEAD")
}

// ResolveGitDirty reports whether repoRoot has uncommitted Git changes.
func ResolveGitDirty(ctx context.Context, repoRoot string) bool {
	return strings.TrimSpace(ResolveGitStatusPorcelain(ctx, repoRoot)) != ""
}

// ResolveGitStatusPorcelain returns stable porcelain status for dirty-state hashing.
func ResolveGitStatusPorcelain(ctx context.Context, repoRoot string) string {
	output := runGitTrimmed(ctx, repoRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if output == "" {
		return ""
	}
	parts := strings.Split(output, "\x00")
	sort.Strings(parts)
	return strings.Join(parts, "\x00")
}

// ResolveGitDefaultRef resolves a local default/mainline ref when available.
func ResolveGitDefaultRef(ctx context.Context, repoRoot string) (string, string) {
	candidates := []string{
		strings.TrimSpace(runGitTrimmed(ctx, repoRoot, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")),
		"origin/main",
		"origin/master",
		"upstream/main",
		"upstream/master",
		"main",
		"master",
	}
	for _, ref := range candidates {
		ref = strings.TrimSpace(strings.TrimPrefix(ref, "refs/remotes/"))
		if ref == "" {
			continue
		}
		sha := runGitTrimmed(ctx, repoRoot, "rev-parse", "--verify", ref+"^{commit}")
		if sha != "" {
			return ref, sha
		}
	}
	return "", ""
}

func resolveGitMergeBase(ctx context.Context, repoRoot, left, right string) string {
	if left == "" || right == "" {
		return ""
	}
	return runGitTrimmed(ctx, repoRoot, "merge-base", left, right)
}

func resolveGitAheadBehind(ctx context.Context, repoRoot, base, head string) (int, int) {
	if base == "" || head == "" {
		return 0, 0
	}
	out := runGitTrimmed(ctx, repoRoot, "rev-list", "--left-right", "--count", base+"..."+head)
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0
	}
	behind, _ := parseInt(fields[0])
	ahead, _ := parseInt(fields[1])
	return ahead, behind
}

func runGitTrimmed(ctx context.Context, repoRoot string, args ...string) string {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func hashGitStatus(status string) string {
	if strings.TrimSpace(status) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(status))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
