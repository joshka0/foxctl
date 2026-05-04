package repoindex

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
)

// ComputeDelta compares stored file_state rows with the current workspace.
func (s *Store) ComputeDelta(ctx context.Context) (WorkspaceDelta, error) {
	meta, _ := s.GetMeta(ctx)
	current := ResolveGitSnapshot(ctx, s.repoRoot)
	states, err := s.ListFileStates(ctx)
	if err != nil {
		return WorkspaceDelta{}, err
	}
	delta := WorkspaceDelta{
		BaseHeadSHA:     meta.HeadSHA,
		CurrentHeadSHA:  current.HeadSHA,
		DirtyStatusHash: current.DirtyStatusHash,
	}
	stored := make(map[string]FileState, len(states))
	for _, state := range states {
		pathValue := normalizeDeltaPath(state.Path)
		if pathValue != "" {
			stored[pathValue] = state
		}
	}
	currentFiles := map[string]struct{}{}
	for pathValue, state := range stored {
		currentState, ok := fileStateForPath(s.repoRoot, pathValue, current.HeadSHA)
		if !ok {
			delta.Deleted = append(delta.Deleted, pathValue)
			continue
		}
		currentFiles[pathValue] = struct{}{}
		if currentState.ContentHash != state.ContentHash || currentState.SizeBytes != state.SizeBytes {
			delta.Modified = append(delta.Modified, pathValue)
		} else {
			delta.Unchanged++
		}
	}
	untracked, modifiedFromGit := gitDeltaPaths(ctx, s.repoRoot)
	for _, pathValue := range modifiedFromGit {
		pathValue = normalizeDeltaPath(pathValue)
		if pathValue == "" {
			continue
		}
		if _, ok := stored[pathValue]; !ok {
			if _, exists := fileStateForPath(s.repoRoot, pathValue, current.HeadSHA); exists {
				delta.Added = append(delta.Added, pathValue)
			}
			continue
		}
		if _, ok := currentFiles[pathValue]; !ok {
			continue
		}
		delta.Modified = appendUniqueDeltaPath(delta.Modified, pathValue)
	}
	for _, pathValue := range untracked {
		pathValue = normalizeDeltaPath(pathValue)
		if pathValue == "" {
			continue
		}
		if _, ok := stored[pathValue]; ok {
			continue
		}
		if _, exists := fileStateForPath(s.repoRoot, pathValue, current.HeadSHA); exists {
			delta.Untracked = appendUniqueDeltaPath(delta.Untracked, pathValue)
		}
	}
	sortDelta(&delta)
	return delta, nil
}

func gitDeltaPaths(ctx context.Context, repoRoot string) (untracked []string, modified []string) {
	status := ResolveGitStatusPorcelain(ctx, repoRoot)
	if strings.TrimSpace(status) == "" {
		return nil, nil
	}
	for _, entry := range strings.Split(status, "\x00") {
		entry = strings.TrimSpace(entry)
		if len(entry) < 4 {
			continue
		}
		code := strings.TrimSpace(entry[:2])
		pathValue := strings.TrimSpace(entry[3:])
		if idx := strings.Index(pathValue, " -> "); idx >= 0 {
			pathValue = strings.TrimSpace(pathValue[idx+4:])
		}
		switch code {
		case "??":
			untracked = appendUniqueDeltaPath(untracked, pathValue)
		default:
			modified = appendUniqueDeltaPath(modified, pathValue)
		}
	}
	return untracked, modified
}

func normalizeDeltaPath(pathValue string) string {
	pathValue = filepath.ToSlash(strings.TrimSpace(pathValue))
	pathValue = strings.TrimPrefix(pathValue, "./")
	return strings.Trim(pathValue, "/")
}

func appendUniqueDeltaPath(paths []string, pathValue string) []string {
	pathValue = normalizeDeltaPath(pathValue)
	if pathValue == "" {
		return paths
	}
	for _, existing := range paths {
		if existing == pathValue {
			return paths
		}
	}
	return append(paths, pathValue)
}

func sortDelta(delta *WorkspaceDelta) {
	sort.Strings(delta.Added)
	sort.Strings(delta.Modified)
	sort.Strings(delta.Deleted)
	sort.Strings(delta.Untracked)
}
