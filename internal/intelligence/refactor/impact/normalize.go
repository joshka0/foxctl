package impact

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/fsutil"
)

type normalizedInput struct {
	Input
	diffRequested bool
}

func normalizeInput(in Input) (normalizedInput, error) {
	in.Workspace = strings.TrimSpace(in.Workspace)
	if in.Intent == "" {
		in.Intent = IntentBehaviorPreservingCleanup
	}
	if !validIntent(in.Intent) {
		return normalizedInput{}, fmt.Errorf("invalid intent %q", in.Intent)
	}
	if in.Depth <= 0 {
		in.Depth = DefaultDepth
	}
	if in.Limit <= 0 {
		in.Limit = DefaultLimit
	}
	if in.PerTargetCap <= 0 {
		in.PerTargetCap = DefaultPerTargetCap
	}
	if in.MaxTargets <= 0 {
		in.MaxTargets = DefaultMaxTargets
	}
	if in.Diff != nil {
		in.Diff.BaseRef = strings.TrimSpace(in.Diff.BaseRef)
		if in.Diff.BaseRef == "" {
			in.Diff.BaseRef = DefaultBaseRef
		}
		in.Diff.HeadRef = strings.TrimSpace(in.Diff.HeadRef)
	}

	diffRequested := in.Diff != nil
	targets := make([]Target, 0, len(in.Targets))
	for _, target := range in.Targets {
		normalized, requestDiff, err := normalizeTarget(target, SourceExplicitTargets)
		if err != nil {
			return normalizedInput{}, err
		}
		if requestDiff {
			diffRequested = true
			continue
		}
		targets = append(targets, normalized)
	}
	if len(targets) == 0 && !diffRequested {
		return normalizedInput{}, fmt.Errorf("at least one explicit target or diff target is required")
	}
	in.Targets = dedupeTargets(targets)
	return normalizedInput{Input: in, diffRequested: diffRequested}, nil
}

func normalizeTarget(target Target, source Source) (Target, bool, error) {
	target.Kind = TargetKind(strings.TrimSpace(string(target.Kind)))
	if !validTargetKind(target.Kind) {
		return Target{}, false, fmt.Errorf("invalid target kind %q", target.Kind)
	}
	if target.Kind == TargetDiff {
		return Target{}, true, nil
	}
	target.Path = cleanPath(target.Path)
	target.OldPath = cleanPath(target.OldPath)
	target.Symbol = strings.TrimSpace(target.Symbol)
	target.Package = strings.TrimSpace(target.Package)
	target.Contract = strings.TrimSpace(target.Contract)
	target.Status = strings.TrimSpace(target.Status)
	target.Description = strings.TrimSpace(target.Description)
	target.IsTest = target.IsTest || fsutil.IsTestFile(filepath.Base(target.Path))
	target.Sources = appendSource(nil, source)
	if err := validateTarget(target); err != nil {
		return Target{}, false, err
	}
	return target, false, nil
}

func validateTarget(target Target) error {
	switch target.Kind {
	case TargetFile:
		if target.Path == "" {
			return fmt.Errorf("file target requires path")
		}
	case TargetSymbol:
		if target.Symbol == "" {
			return fmt.Errorf("symbol target requires symbol")
		}
	case TargetPackage:
		if target.Package == "" && target.Path == "" {
			return fmt.Errorf("package target requires package or path")
		}
	case TargetContract:
		if target.Contract == "" {
			return fmt.Errorf("contract target requires contract")
		}
	}
	return nil
}

func targetsFromChanges(changes []Change) []Target {
	targets := make([]Target, 0, len(changes)*2)
	for _, change := range changes {
		path := cleanPath(change.Path)
		if path == "" {
			continue
		}
		status := strings.TrimSpace(change.Status)
		targets = append(targets, Target{
			Kind:        TargetFile,
			Path:        path,
			OldPath:     cleanPath(change.OldPath),
			Status:      status,
			Additions:   change.Additions,
			Deletions:   change.Deletions,
			Description: strings.TrimSpace(change.Description),
			IsDeleted:   strings.HasPrefix(status, "D"),
			IsTest:      fsutil.IsTestFile(filepath.Base(path)),
			Sources:     []Source{SourceGitDiff},
		})
		oldPath := cleanPath(change.OldPath)
		if oldPath != "" && oldPath != path {
			targets = append(targets, Target{
				Kind:        TargetFile,
				Path:        oldPath,
				Status:      status,
				Description: "rename source",
				IsDeleted:   true,
				IsTest:      fsutil.IsTestFile(filepath.Base(oldPath)),
				Sources:     []Source{SourceGitDiff},
			})
		}
	}
	return targets
}

func dedupeTargets(targets []Target) []Target {
	byKey := make(map[string]Target, len(targets))
	for _, target := range targets {
		key := targetKey(target)
		if key == "" {
			continue
		}
		prev, ok := byKey[key]
		if !ok {
			byKey[key] = target
			continue
		}
		prev.Sources = appendSources(prev.Sources, target.Sources...)
		if prev.Status == "" {
			prev.Status = target.Status
		}
		if prev.OldPath == "" {
			prev.OldPath = target.OldPath
		}
		if prev.Description == "" {
			prev.Description = target.Description
		}
		prev.Additions += target.Additions
		prev.Deletions += target.Deletions
		prev.IsDeleted = prev.IsDeleted || target.IsDeleted
		prev.IsTest = prev.IsTest || target.IsTest
		byKey[key] = prev
	}
	out := make([]Target, 0, len(byKey))
	for _, target := range byKey {
		sort.Slice(target.Sources, func(i, j int) bool { return target.Sources[i] < target.Sources[j] })
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Symbol != out[j].Symbol {
			return out[i].Symbol < out[j].Symbol
		}
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Contract < out[j].Contract
	})
	return out
}

func targetKey(target Target) string {
	switch target.Kind {
	case TargetFile:
		return string(target.Kind) + "|" + target.Path
	case TargetSymbol:
		return string(target.Kind) + "|" + target.Path + "|" + target.Symbol
	case TargetPackage:
		return string(target.Kind) + "|" + target.Path + "|" + target.Package
	case TargetContract:
		return string(target.Kind) + "|" + target.Contract
	default:
		return ""
	}
}

func TargetKey(target Target) string {
	return targetKey(target)
}

func targetLabel(target Target) string {
	switch target.Kind {
	case TargetSymbol:
		if target.Path != "" {
			return target.Path + "::" + target.Symbol
		}
		return target.Symbol
	case TargetPackage:
		if target.Package != "" {
			return target.Package
		}
		return target.Path
	case TargetContract:
		return target.Contract
	default:
		return target.Path
	}
}

func TargetLabel(target Target) string {
	return targetLabel(target)
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func validTargetKind(kind TargetKind) bool {
	switch kind {
	case TargetFile, TargetSymbol, TargetPackage, TargetContract, TargetDiff:
		return true
	default:
		return false
	}
}

func validIntent(intent RefactorIntent) bool {
	switch intent {
	case IntentRename, IntentMove, IntentDelete, IntentConsolidate, IntentTypeTighten, IntentAPIContractChange, IntentBehaviorPreservingCleanup:
		return true
	default:
		return false
	}
}
