package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type promptEvalSnippet struct {
	Path      string   `json:"path,omitempty"`
	StartLine int      `json:"start_line,omitempty"`
	EndLine   int      `json:"end_line,omitempty"`
	Contains  []string `json:"contains,omitempty"`
}

type evalObservedSnippet struct {
	Path      string
	StartLine int
	EndLine   int
}

func hasCodeCorrectnessExpectations(evalCase promptEvalCase) bool {
	return len(evalCase.ExpectedPaths) > 0 || len(evalCase.ExpectedSymbols) > 0 || len(evalCase.ExpectedSnippets) > 0 || len(evalCase.RequiredFacts) > 0
}

func scoreExpectedSymbols(expected, observed []string) ([]string, float64) {
	normalized := normalizeExpectedSymbols(expected)
	if len(normalized) == 0 {
		return nil, 0
	}
	observedExact, observedNames := buildObservedSymbolSets(observed)
	matched := make([]string, 0, len(normalized))
	for _, want := range normalized {
		if strings.Contains(want, "::") {
			if _, ok := observedExact[strings.ToLower(want)]; ok {
				matched = append(matched, want)
			}
			continue
		}
		if _, ok := observedNames[strings.ToLower(want)]; ok {
			matched = append(matched, want)
		}
	}
	return matched, float64(len(matched)) / float64(len(normalized))
}

func scoreRequiredFacts(required []string, blob string) ([]string, float64) {
	normalized := normalizeExpectedFacts(required)
	if len(normalized) == 0 {
		return nil, 0
	}
	blob = strings.ToLower(filepath.ToSlash(strings.TrimSpace(blob)))
	matched := make([]string, 0, len(normalized))
	for _, want := range normalized {
		if strings.Contains(blob, strings.ToLower(want)) {
			matched = append(matched, want)
		}
	}
	return matched, float64(len(matched)) / float64(len(normalized))
}

func scoreExpectedSnippets(workspace string, expected []promptEvalSnippet, observed []evalObservedSnippet) ([]string, float64) {
	normalized := normalizeExpectedSnippets(expected)
	if len(normalized) == 0 {
		return nil, 0
	}
	observed = normalizeObservedSnippets(observed)
	matched := make([]string, 0, len(normalized))
	contentCache := map[string]string{}
	for _, want := range normalized {
		for _, got := range observed {
			if want.Path != "" && normalizeCodeSearchPath(got.Path) != want.Path {
				continue
			}
			if !snippetRangeMatches(want, got) {
				continue
			}
			if len(want.Contains) > 0 {
				content := observedSnippetContent(workspace, got, contentCache)
				if content == "" || !containsAllFold(content, want.Contains) {
					continue
				}
			}
			matched = append(matched, describeExpectedSnippet(want))
			break
		}
	}
	return matched, float64(len(matched)) / float64(len(normalized))
}

func blendedCorrectnessScore(pathRecall, symbolRecall, snippetRecall, factRecall float64, hasPaths, hasSymbols, hasSnippets, hasFacts bool) float64 {
	weights := []struct {
		present bool
		score   float64
		weight  float64
	}{
		{present: hasPaths, score: pathRecall, weight: 0.40},
		{present: hasSymbols, score: symbolRecall, weight: 0.20},
		{present: hasSnippets, score: snippetRecall, weight: 0.25},
		{present: hasFacts, score: factRecall, weight: 0.15},
	}
	var totalWeight float64
	var total float64
	for _, item := range weights {
		if !item.present {
			continue
		}
		totalWeight += item.weight
		total += clampEvalScore(item.score) * item.weight
	}
	if totalWeight == 0 {
		return 0
	}
	return total / totalWeight
}

func normalizeExpectedSymbols(symbols []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			continue
		}
		if strings.Contains(symbol, "::") {
			parts := strings.SplitN(symbol, "::", 2)
			symbol = normalizeCodeSearchPath(parts[0]) + "::" + strings.TrimSpace(parts[1])
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	sort.Strings(out)
	return out
}

func normalizeExpectedFacts(facts []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(facts))
	for _, fact := range facts {
		fact = strings.TrimSpace(fact)
		if fact == "" {
			continue
		}
		if _, ok := seen[fact]; ok {
			continue
		}
		seen[fact] = struct{}{}
		out = append(out, fact)
	}
	sort.Strings(out)
	return out
}

func normalizeExpectedSnippets(snippets []promptEvalSnippet) []promptEvalSnippet {
	seen := map[string]struct{}{}
	out := make([]promptEvalSnippet, 0, len(snippets))
	for _, snippet := range snippets {
		snippet.Path = normalizeCodeSearchPath(snippet.Path)
		snippet.Contains = normalizeExpectedFacts(snippet.Contains)
		if snippet.Path == "" && snippet.StartLine <= 0 && snippet.EndLine <= 0 && len(snippet.Contains) == 0 {
			continue
		}
		key := describeExpectedSnippet(snippet)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, snippet)
	}
	return out
}

func normalizeObservedSnippets(snippets []evalObservedSnippet) []evalObservedSnippet {
	seen := map[string]struct{}{}
	out := make([]evalObservedSnippet, 0, len(snippets))
	for _, snippet := range snippets {
		snippet.Path = normalizeCodeSearchPath(snippet.Path)
		if snippet.Path == "" {
			continue
		}
		if snippet.EndLine > 0 && snippet.StartLine > snippet.EndLine {
			snippet.StartLine, snippet.EndLine = snippet.EndLine, snippet.StartLine
		}
		key := fmt.Sprintf("%s:%d-%d", snippet.Path, snippet.StartLine, snippet.EndLine)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, snippet)
	}
	return out
}

func buildObservedSymbolSets(observed []string) (map[string]struct{}, map[string]struct{}) {
	exact := map[string]struct{}{}
	names := map[string]struct{}{}
	for _, item := range observed {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "::") {
			parts := strings.SplitN(item, "::", 2)
			full := strings.ToLower(normalizeCodeSearchPath(parts[0]) + "::" + strings.TrimSpace(parts[1]))
			exact[full] = struct{}{}
			names[strings.ToLower(strings.TrimSpace(parts[1]))] = struct{}{}
			continue
		}
		names[strings.ToLower(item)] = struct{}{}
	}
	return exact, names
}

func snippetRangeMatches(expected promptEvalSnippet, observed evalObservedSnippet) bool {
	if expected.StartLine <= 0 && expected.EndLine <= 0 {
		return true
	}
	expectedStart := expected.StartLine
	if expectedStart <= 0 {
		expectedStart = expected.EndLine
	}
	expectedEnd := expected.EndLine
	if expectedEnd <= 0 {
		expectedEnd = expectedStart
	}
	observedStart := observed.StartLine
	if observedStart <= 0 {
		observedStart = observed.EndLine
	}
	observedEnd := observed.EndLine
	if observedEnd <= 0 {
		observedEnd = observedStart
	}
	if observedStart <= 0 || observedEnd <= 0 {
		return false
	}
	return observedStart <= expectedEnd && expectedStart <= observedEnd
}

func observedSnippetContent(workspace string, observed evalObservedSnippet, cache map[string]string) string {
	path := normalizeCodeSearchPath(observed.Path)
	if path == "" {
		return ""
	}
	full := filepath.Join(workspace, filepath.FromSlash(path))
	body, ok := cache[full]
	if !ok {
		bytes, err := os.ReadFile(full)
		if err != nil {
			cache[full] = ""
			return ""
		}
		body = string(bytes)
		cache[full] = body
	}
	lines := strings.Split(body, "\n")
	start := observed.StartLine
	end := observed.EndLine
	if start <= 0 {
		start = 1
	}
	if end <= 0 {
		end = start
	}
	if start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n")
}

func describeExpectedSnippet(snippet promptEvalSnippet) string {
	parts := make([]string, 0, 3)
	if snippet.Path != "" {
		parts = append(parts, snippet.Path)
	}
	if snippet.StartLine > 0 || snippet.EndLine > 0 {
		start := snippet.StartLine
		if start <= 0 {
			start = snippet.EndLine
		}
		end := snippet.EndLine
		if end <= 0 {
			end = start
		}
		parts = append(parts, fmt.Sprintf("%d-%d", start, end))
	}
	if len(snippet.Contains) > 0 {
		parts = append(parts, strings.Join(snippet.Contains, " & "))
	}
	return strings.Join(parts, " :: ")
}

func containsAllFold(haystack string, needles []string) bool {
	haystack = strings.ToLower(haystack)
	for _, needle := range needles {
		if !strings.Contains(haystack, strings.ToLower(strings.TrimSpace(needle))) {
			return false
		}
	}
	return true
}

func normalizeCodeSearchPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	return path
}
