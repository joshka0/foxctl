package analysisflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	gitcochange "github.com/joshka0/foxctl/internal/intelligence/indexing/cochange"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semanticanchors"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
)

type Dependencies struct {
	RunSkill lifecycle.SkillRunner
}

func NewDependencies(deps lifecycle.Dependencies) Dependencies {
	return Dependencies{RunSkill: deps.RunSkill}
}

type Payload struct {
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
	} `json:"tool_input"`
}

type Request struct {
	Workspace string
	Payload   Payload
}

type Response struct {
	Decision string `json:"decision"`
	Context  string `json:"context,omitempty"`
	FilePath string `json:"file_path,omitempty"`
}

type SemanticAnchorResponse struct {
	Decision      string                         `json:"decision"`
	Context       string                         `json:"context,omitempty"`
	FilePath      string                         `json:"file_path,omitempty"`
	Warnings      []string                       `json:"warnings,omitempty"`
	TestContracts []string                       `json:"test_contracts,omitempty"`
	GraphDiff     []SemanticAnchorGraphDiffEntry `json:"graph_diff,omitempty"`
}

type SemanticAnchorGraphDiffEntry struct {
	OwnerNodeID    string `json:"owner_node_id,omitempty"`
	OwnerStableKey string `json:"owner_stable_key,omitempty"`
	Anchor         string `json:"anchor"`
	TargetID       string `json:"target_id,omitempty"`
	Relation       string `json:"relation,omitempty"`
	LineStart      int    `json:"line_start,omitempty"`
	WouldEmit      bool   `json:"would_emit"`
}

type complexityEnvelope struct {
	Data struct {
		Results []struct {
			Function             string `json:"function"`
			Line                 int    `json:"line"`
			CyclomaticComplexity int    `json:"cyclomatic_complexity"`
			RiskLevel            string `json:"risk_level"`
		} `json:"results"`
	} `json:"data"`
}

type impactEnvelope struct {
	Data struct {
		HookOutput hooks.Output `json:"hook_output"`
	} `json:"data"`
}

func AnalyzeEditedFile(ctx context.Context, deps Dependencies, req Request) (Response, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return Response{}, fmt.Errorf("detect workspace")
	}
	response := Response{Decision: "approve"}
	filePath := strings.TrimSpace(req.Payload.ToolInput.FilePath)
	if filePath == "" {
		return response, nil
	}
	response.FilePath = filePath
	if envEnabled("FOXCTL_CODE_ANALYSIS_DISABLED") || !isCodeFile(filePath) || isTestFile(filePath) {
		return response, nil
	}
	if deps.RunSkill == nil {
		return response, nil
	}

	parts := []string{}

	if !envEnabled("FOXCTL_COMPLEXITY_DISABLED") {
		var env complexityEnvelope
		if err := deps.RunSkill(ctx, "code/complexity", map[string]any{
			"path":          filePath,
			"threshold":     envInt("FOXCTL_COMPLEXITY_THRESHOLD", 15),
			"analysis_mode": "hotspots",
			"max_results":   5,
		}, target, &env); err == nil {
			highRisk := make([]string, 0, 2)
			riskCount := 0
			for _, item := range env.Data.Results {
				if item.RiskLevel != "high" && item.RiskLevel != "medium" {
					continue
				}
				riskCount++
				if len(highRisk) < 2 {
					highRisk = append(highRisk, fmt.Sprintf("- `%s` (line %d): cyclomatic=%d", item.Function, item.Line, item.CyclomaticComplexity))
				}
			}
			if riskCount > 0 {
				msg := fmt.Sprintf("**Complexity:** %d function(s) with elevated complexity", riskCount)
				if len(highRisk) > 0 {
					msg += "\n" + strings.Join(highRisk, "\n")
				}
				parts = append(parts, msg)
			}
		}
	}

	if !envEnabled("FOXCTL_IMPACT_DISABLED") {
		var env impactEnvelope
		if err := deps.RunSkill(ctx, "hooks/impact_analysis", hooks.Input{
			Event:         hooks.EventPostToolUse,
			WorkspaceRoot: target,
			ToolName:      "Edit",
			ToolInput:     []byte(fmt.Sprintf(`{"file_path":%q}`, filePath)),
		}, target, &env); err == nil {
			context := strings.TrimSpace(env.Data.HookOutput.Context)
			if context != "" {
				parts = append(parts, context)
			}
		}
	}

	if len(parts) > 0 {
		response.Context = strings.Join(parts, "\n\n")
	}
	return response, nil
}

func AnalyzeSemanticAnchors(ctx context.Context, req Request) (SemanticAnchorResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return SemanticAnchorResponse{}, fmt.Errorf("detect workspace")
	}
	response := SemanticAnchorResponse{Decision: "approve"}
	filePath := strings.TrimSpace(req.Payload.ToolInput.FilePath)
	if filePath == "" {
		return response, nil
	}
	response.FilePath = filePath
	if !envEnabled("FOXCTL_SEMANTIC_ANCHORS_HOOK") || !isCodeFile(filePath) {
		return response, nil
	}

	relPath := normalizeHookRepoPath(target, filePath)
	if relPath == "" {
		return response, nil
	}
	body, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(relPath)))
	if err != nil {
		response.Warnings = append(response.Warnings, "semantic anchors: unable to read touched file: "+err.Error())
		response.Context = buildSemanticAnchorContext(nil, response.Warnings, nil, nil, nil)
		return response, nil
	}

	policy := semanticanchors.DefaultAnchorPolicy(filepath.Base(target), nil)
	extracted, err := semanticanchors.ExtractAnchorsFromSource(ctx, policy, hookAnchorOwnerResolver{}, relPath, body)
	if err != nil {
		response.Warnings = append(response.Warnings, "semantic anchors: unable to parse touched file: "+err.Error())
		response.Context = buildSemanticAnchorContext(nil, response.Warnings, nil, nil, nil)
		return response, nil
	}
	warnings := semanticAnchorWarnings(extracted)
	testContracts := linkedSemanticAnchorTestContracts(target, extracted.Occurrences)
	if semanticAnchorTrustCriticalWithoutTests(extracted.Occurrences, testContracts) {
		warnings = append(warnings, "semantic anchors: trust-critical anchors changed without linked test contracts")
		sort.Strings(warnings)
	}
	graphDiff := semanticAnchorGraphDiff(policy, extracted.Occurrences)
	neighbors := semanticAnchorCoChangeNeighbors(ctx, target, relPath)
	if len(extracted.Occurrences) == 0 && len(warnings) == 0 && len(testContracts) == 0 && len(graphDiff) == 0 && len(neighbors) == 0 {
		return response, nil
	}
	response.Warnings = warnings
	response.TestContracts = testContracts
	response.GraphDiff = graphDiff
	response.Context = buildSemanticAnchorContext(extracted.Occurrences, warnings, testContracts, graphDiff, neighbors)
	return response, nil
}

func isCodeFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".c", ".cpp", ".rs":
		return true
	default:
		return false
	}
}

func isTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.js") ||
		strings.HasSuffix(lower, ".spec.ts") ||
		strings.HasSuffix(lower, ".spec.js") ||
		strings.Contains(lower, "__test__")
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

type hookAnchorOwnerResolver struct{}

func (hookAnchorOwnerResolver) ResolveFileOwner(path string) semanticanchors.AnchorOwner {
	return semanticanchors.AnchorOwner{
		NodeID:    "file:" + path,
		Kind:      "file",
		StableKey: path,
		Path:      path,
	}
}

func (hookAnchorOwnerResolver) ResolveSymbolOwner(path string, lang string, span semanticanchors.Span, qualifiedName string) (semanticanchors.AnchorOwner, bool) {
	name := strings.TrimSpace(qualifiedName)
	if path == "" || name == "" {
		return semanticanchors.AnchorOwner{}, false
	}
	return semanticanchors.AnchorOwner{
		NodeID:    "symbol:" + path + ":" + name,
		Kind:      "symbol",
		StableKey: path + ":" + name,
		Path:      path,
		Name:      name,
		StartLine: span.LineStart,
		EndLine:   span.LineEnd,
	}, true
}

func normalizeHookRepoPath(workspacePath, filePath string) string {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return ""
	}
	if filepath.IsAbs(filePath) {
		rel, err := filepath.Rel(workspacePath, filePath)
		if err != nil || strings.HasPrefix(rel, "..") {
			return ""
		}
		filePath = rel
	}
	return filepath.ToSlash(filepath.Clean(filePath))
}

func semanticAnchorWarnings(extracted semanticanchors.ExtractionResult) []string {
	seen := map[string]struct{}{}
	var warnings []string
	add := func(prefix string, finding semanticanchors.Finding) {
		if finding.Severity == "" || finding.Severity == semanticanchors.AnchorFindingInfo {
			return
		}
		msg := prefix + string(finding.Reason)
		if _, ok := seen[msg]; ok {
			return
		}
		seen[msg] = struct{}{}
		warnings = append(warnings, msg)
	}
	for _, finding := range extracted.Findings {
		add("semantic anchors: ", finding)
	}
	for _, occ := range extracted.Occurrences {
		for _, finding := range occ.Findings {
			prefix := "semantic anchors"
			if display := safeSemanticAnchorDisplay(occ); display != "" {
				prefix += " " + display
			}
			add(prefix+": ", finding)
		}
	}
	sort.Strings(warnings)
	return warnings
}

func semanticAnchorGraphDiff(policy semanticanchors.AnchorPolicy, occurrences []semanticanchors.AnchorOccurrence) []SemanticAnchorGraphDiffEntry {
	if len(occurrences) == 0 {
		return nil
	}
	out := make([]SemanticAnchorGraphDiffEntry, 0, len(occurrences))
	for _, occ := range occurrences {
		typePolicy := policy.TypePolicies[occ.Type]
		out = append(out, SemanticAnchorGraphDiffEntry{
			OwnerNodeID:    occ.OwnerBinding.OwnerNodeID,
			OwnerStableKey: occ.OwnerBinding.OwnerStableKey,
			Anchor:         safeSemanticAnchorDisplay(occ),
			TargetID:       string(occ.TargetID),
			Relation:       string(typePolicy.Relation),
			LineStart:      occ.Span.LineStart,
			WouldEmit:      typePolicy.Indexable && occ.OwnerBinding.OwnerNodeID != "" && !semanticAnchorHasError(occ.Findings),
		})
	}
	return out
}

func semanticAnchorTrustCriticalWithoutTests(occurrences []semanticanchors.AnchorOccurrence, testContracts []string) bool {
	if len(testContracts) > 0 {
		return false
	}
	hasTrustCritical := false
	for _, occ := range occurrences {
		switch occ.Type {
		case semanticanchors.AnchorTypeTest, semanticanchors.AnchorTypeTestContract:
			return false
		case semanticanchors.AnchorTypeInvariant, semanticanchors.AnchorTypeRisk, semanticanchors.AnchorTypeProtocol, semanticanchors.AnchorTypeDecision:
			hasTrustCritical = true
		}
	}
	return hasTrustCritical
}

func safeSemanticAnchorDisplay(occ semanticanchors.AnchorOccurrence) string {
	for _, finding := range occ.Findings {
		switch finding.Reason {
		case semanticanchors.AnchorFindingUnsafeURL,
			semanticanchors.AnchorFindingAbsolutePath,
			semanticanchors.AnchorFindingPathTraversal,
			semanticanchors.AnchorFindingBackslashPath,
			semanticanchors.AnchorFindingControlChar,
			semanticanchors.AnchorFindingEnvVarExpansion,
			semanticanchors.AnchorFindingSecretLike,
			semanticanchors.AnchorFindingSessionLike,
			semanticanchors.AnchorFindingPIILike:
			return "[[redacted:" + string(finding.Reason) + "]]"
		}
	}
	return occ.DisplaySyntax
}

func semanticAnchorHasError(findings []semanticanchors.Finding) bool {
	for _, finding := range findings {
		if finding.Severity == semanticanchors.AnchorFindingError {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func linkedSemanticAnchorTestContracts(workspacePath string, occurrences []semanticanchors.AnchorOccurrence) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, occ := range occurrences {
		if occ.Type != semanticanchors.AnchorTypeTest && occ.Type != semanticanchors.AnchorTypeTestContract {
			continue
		}
		target := strings.Split(strings.TrimSpace(occ.Target), "#")[0]
		if target == "" {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(target))
		if strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		if info, err := os.Stat(filepath.Join(workspacePath, filepath.FromSlash(clean))); err == nil && !info.IsDir() {
			seen[clean] = struct{}{}
			out = append(out, clean)
		}
	}
	sort.Strings(out)
	return out
}

func semanticAnchorCoChangeNeighbors(ctx context.Context, workspacePath, relPath string) []string {
	if !envEnabled("FOXCTL_SEMANTIC_ANCHORS_COCHANGE") {
		return nil
	}
	commits, err := gitcochange.CollectGitCommits(ctx, workspacePath, []string{relPath}, gitcochange.Config{
		CommitLimit:          envInt("FOXCTL_SEMANTIC_ANCHORS_COCHANGE_COMMITS", 200),
		MaxFilesPerCommit:    50,
		HalfLifeDays:         90,
		TopKPerFile:          5,
		Now:                  time.Now().UTC(),
		SkipGenerated:        true,
		SkipLockfiles:        true,
		GiantCommitSoftLimit: 200,
		GiantCommitHardLimit: 1000,
	})
	if err != nil {
		return nil
	}
	scored := gitcochange.Score(commits, gitcochange.Config{TopKPerFile: 5, Now: time.Now().UTC(), SkipGenerated: true, SkipLockfiles: true})
	neighbors := scored[relPath]
	out := make([]string, 0, len(neighbors))
	for _, neighbor := range neighbors {
		if neighbor.Path != "" {
			out = append(out, neighbor.Path)
		}
	}
	return out
}

func buildSemanticAnchorContext(occurrences []semanticanchors.AnchorOccurrence, warnings, testContracts []string, graphDiff []SemanticAnchorGraphDiffEntry, neighbors []string) string {
	var parts []string
	if len(occurrences) > 0 {
		lines := []string{fmt.Sprintf("**Semantic anchors:** %d anchor(s) in touched file", len(occurrences))}
		for i, occ := range occurrences {
			if i >= 5 {
				lines = append(lines, fmt.Sprintf("- ...and %d more", len(occurrences)-i))
				break
			}
			line := fmt.Sprintf("- `%s` line %d", safeSemanticAnchorDisplay(occ), occ.Span.LineStart)
			if occ.ValidationStatus != "" {
				line += " (" + string(occ.ValidationStatus) + ")"
			}
			lines = append(lines, line)
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	if len(graphDiff) > 0 {
		lines := []string{"**Semantic anchor graph diff:**"}
		for _, entry := range graphDiff {
			state := "review-only"
			if entry.WouldEmit {
				state = "would-emit"
			}
			owner := firstNonEmpty(entry.OwnerStableKey, entry.OwnerNodeID, "unbound")
			lines = append(lines, fmt.Sprintf("- `%s` --%s--> `%s` (%s, line %d)", owner, entry.Relation, entry.TargetID, state, entry.LineStart))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	if len(testContracts) > 0 {
		lines := []string{"**Linked test contracts:**"}
		for _, path := range testContracts {
			lines = append(lines, "- `"+path+"`")
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	if len(neighbors) > 0 {
		lines := []string{"**Likely co-change neighbors:**"}
		for _, path := range neighbors {
			lines = append(lines, "- `"+path+"`")
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	if len(warnings) > 0 {
		lines := []string{"**Semantic anchor warnings:**"}
		for _, warning := range warnings {
			lines = append(lines, "- "+warning)
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	return strings.Join(parts, "\n\n")
}
