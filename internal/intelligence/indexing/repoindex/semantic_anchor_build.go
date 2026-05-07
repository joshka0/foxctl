package repoindex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semanticanchors"
)

// [[protocol:semantic-anchor-graph-emission]]
// [[test:internal/intelligence/indexing/repoindex/semantic_anchor_build_test.go#TestBuilderEmitsSemanticAnchorEdgesBehindFlag]]
func applySemanticAnchorEdges(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge) error {
	resolver := newSemanticAnchorBuildResolver(opts, nodes)
	targets := semanticAnchorFileTargetResolver{repoRoot: opts.RepoRoot}
	policy := semanticanchors.DefaultAnchorPolicy(opts.RepoKey, nil)
	now := time.Now().UTC()

	files := make([]Node, 0)
	for _, node := range nodes {
		if node.Kind == NodeFile && strings.TrimSpace(node.File) != "" {
			files = append(files, node)
		}
	}
	for _, file := range files {
		fullPath := filepath.Join(opts.RepoRoot, filepath.FromSlash(file.File))
		src, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		result, err := semanticanchors.ExtractAnchorsFromSource(ctx, policy, resolver, file.File, src)
		if err != nil {
			return fmt.Errorf("repoindex: extract semantic anchors from %s: %w", file.File, err)
		}
		if result.Support != semanticanchors.AnchorSupportGraphBinding {
			continue
		}
		if hasSemanticAnchorErrorFinding(result.Findings) {
			continue
		}
		for _, occ := range result.Occurrences {
			owner, ok := resolver.ownerForBinding(occ.OwnerBinding)
			if !ok {
				continue
			}
			resolution, err := semanticanchors.ResolveAnchorOccurrence(ctx, policy, occ, targets)
			if err != nil {
				continue
			}
			edge, err := NewSemanticAnchorEdge(resolution, owner)
			if err != nil {
				continue
			}
			meta, _, err := DecodeAndValidateSemanticAnchorEdge(edge)
			if err != nil {
				continue
			}
			addNode(nodes, Node{
				ID:        edge.Dst,
				Kind:      NodeConcept,
				Pkg:       file.Pkg,
				File:      file.File,
				Name:      semanticAnchorConceptName(meta),
				Summary:   semanticAnchorConceptSummary(meta),
				UpdatedAt: now,
			})
			addEdge(edges, edge)
		}
	}
	return nil
}

func hasSemanticAnchorErrorFinding(findings []semanticanchors.Finding) bool {
	for _, finding := range findings {
		if finding.Severity == semanticanchors.AnchorFindingError {
			return true
		}
	}
	return false
}

type semanticAnchorBuildResolver struct {
	filesByPath   map[string]Node
	symbolsByPath map[string][]Node
	ownersByID    map[string]semanticanchors.AnchorOwner
}

func newSemanticAnchorBuildResolver(opts BuildOptions, nodes map[string]Node) *semanticAnchorBuildResolver {
	r := &semanticAnchorBuildResolver{
		filesByPath:   make(map[string]Node),
		symbolsByPath: make(map[string][]Node),
		ownersByID:    make(map[string]semanticanchors.AnchorOwner),
	}
	for _, node := range nodes {
		switch node.Kind {
		case NodeFile:
			if node.File != "" {
				r.filesByPath[filepath.ToSlash(node.File)] = node
			}
		case NodeSymbol:
			if node.File != "" {
				path := filepath.ToSlash(node.File)
				r.symbolsByPath[path] = append(r.symbolsByPath[path], node)
			}
		}
	}
	for path, file := range r.filesByPath {
		owner := anchorOwnerForNode(file, "file:"+path)
		r.ownersByID[owner.NodeID] = owner
	}
	for _, symbols := range r.symbolsByPath {
		for _, symbol := range symbols {
			owner := anchorOwnerForNode(symbol, semanticAnchorSymbolStableKey(symbol))
			r.ownersByID[owner.NodeID] = owner
		}
	}
	_ = opts
	return r
}

func (r *semanticAnchorBuildResolver) ResolveFileOwner(path string) semanticanchors.AnchorOwner {
	if r == nil {
		return semanticanchors.AnchorOwner{}
	}
	node, ok := r.filesByPath[filepath.ToSlash(path)]
	if !ok {
		return semanticanchors.AnchorOwner{}
	}
	return anchorOwnerForNode(node, "file:"+filepath.ToSlash(path))
}

func (r *semanticAnchorBuildResolver) ResolveSymbolOwner(path, lang string, span semanticanchors.Span, qualifiedName string) (semanticanchors.AnchorOwner, bool) {
	if r == nil {
		return semanticanchors.AnchorOwner{}, false
	}
	candidates := r.symbolsByPath[filepath.ToSlash(path)]
	for _, node := range candidates {
		if node.Name == qualifiedName {
			return anchorOwnerForNode(node, semanticAnchorSymbolStableKey(node)), true
		}
	}
	for _, node := range candidates {
		if node.SpanStart == span.LineStart || (node.SpanStart <= span.LineStart && node.SpanEnd >= span.LineStart) {
			return anchorOwnerForNode(node, semanticAnchorSymbolStableKey(node)), true
		}
	}
	return semanticanchors.AnchorOwner{}, false
}

func (r *semanticAnchorBuildResolver) ownerForBinding(binding semanticanchors.AnchorOwnerBinding) (semanticanchors.AnchorOwner, bool) {
	if r == nil || binding.OwnerNodeID == "" {
		return semanticanchors.AnchorOwner{}, false
	}
	owner, ok := r.ownersByID[binding.OwnerNodeID]
	return owner, ok
}

func anchorOwnerForNode(node Node, stableKey string) semanticanchors.AnchorOwner {
	return semanticanchors.AnchorOwner{
		NodeID:    node.ID,
		Kind:      string(node.Kind),
		StableKey: stableKey,
		Path:      node.File,
		Name:      node.Name,
		StartLine: node.SpanStart,
		EndLine:   node.SpanEnd,
	}
}

func semanticAnchorSymbolStableKey(node Node) string {
	return strings.Join([]string{"symbol", node.Pkg, filepath.ToSlash(node.File), node.Name}, ":")
}

type semanticAnchorFileTargetResolver struct {
	repoRoot string
}

func (r semanticAnchorFileTargetResolver) ResolveAnchorTarget(_ context.Context, occ semanticanchors.AnchorOccurrence) (semanticanchors.TargetResolution, error) {
	if occ.Type != semanticanchors.AnchorTypeDoc && occ.Type != semanticanchors.AnchorTypeTest {
		return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationEvidenceOnly}, nil
	}
	targetPath, fragment, _ := strings.Cut(occ.Target, "#")
	fullPath := filepath.Join(r.repoRoot, filepath.FromSlash(targetPath))
	rel, err := filepath.Rel(r.repoRoot, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationLintError}, nil
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return missingAnchorTarget(semanticanchors.AnchorFindingMissingTarget), nil
	}
	if strings.TrimSpace(fragment) == "" {
		return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationValidReference}, nil
	}
	switch occ.Type {
	case semanticanchors.AnchorTypeDoc:
		if markdownHeadingExists(string(content), fragment) {
			return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationValidReference}, nil
		}
	case semanticanchors.AnchorTypeTest:
		if testSymbolExists(string(content), fragment) {
			return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationValidReference}, nil
		}
	}
	return missingAnchorTarget(semanticanchors.AnchorFindingUnresolvedFragment), nil
}

func missingAnchorTarget(reason semanticanchors.AnchorFindingReason) semanticanchors.TargetResolution {
	finding := semanticanchors.Finding{
		ID:       "anchor-finding:" + string(reason),
		Reason:   reason,
		Severity: semanticanchors.AnchorFindingWarning,
		Message:  "semantic anchor " + string(reason),
	}
	return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationMissingTarget, Finding: &finding}
}

func markdownHeadingExists(content, fragment string) bool {
	want := markdownAnchorSlug(fragment)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if markdownAnchorSlug(heading) == want {
			return true
		}
	}
	return false
}

func markdownAnchorSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || unicode.IsSpace(r):
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func testSymbolExists(content, fragment string) bool {
	fragment = regexp.QuoteMeta(strings.TrimSpace(fragment))
	if fragment == "" {
		return true
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\bfunc\s+` + fragment + `\s*\(`),
		regexp.MustCompile(`\bdef\s+` + fragment + `\s*\(`),
		regexp.MustCompile(`\b` + fragment + `\s*[:=]\s*(?:async\s*)?\(?`),
	}
	for _, pattern := range patterns {
		if pattern.MatchString(content) {
			return true
		}
	}
	return false
}

func semanticAnchorConceptName(meta semanticanchors.SemanticAnchorEdgeMeta) string {
	if meta.TargetDisplay != "" {
		return meta.TargetDisplay
	}
	return string(meta.TargetID)
}

func semanticAnchorConceptSummary(meta semanticanchors.SemanticAnchorEdgeMeta) string {
	return strings.TrimSpace(string(meta.Relation) + " " + string(meta.TargetID))
}
