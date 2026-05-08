package symbol

import (
	"regexp"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semanticanchors"
)

var inlineSemanticAnchorPattern = regexp.MustCompile(`\[\[[^\]\r\n]{1,512}\]\]`)

func semanticAnchorHints(repoKey string, values ...string) []string {
	policy := semanticanchors.DefaultAnchorPolicy(repoKey, nil)
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for _, value := range values {
		for _, syntax := range inlineSemanticAnchorPattern.FindAllString(value, -1) {
			occ, findings := semanticanchors.ParseInlineAnchor(policy, syntax)
			if len(findings) != 0 {
				continue
			}
			hint := semanticAnchorHint(occ)
			if hint == "" {
				continue
			}
			if _, ok := seen[hint]; ok {
				continue
			}
			seen[hint] = struct{}{}
			out = append(out, hint)
		}
	}
	sort.Strings(out)
	return out
}

func semanticAnchorHint(occ semanticanchors.AnchorOccurrence) string {
	target := strings.TrimSpace(occ.Target)
	if target == "" {
		return ""
	}
	if occ.Scope == semanticanchors.RepoLocalAnchorScope {
		return string(occ.Type) + ":" + target
	}
	return string(occ.Scope) + ":" + string(occ.Type) + "/" + target
}
