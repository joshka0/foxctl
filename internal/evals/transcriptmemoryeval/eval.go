package transcriptmemoryeval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/transcriptpipeline"
	"gopkg.in/yaml.v3"
)

type ClaimExpectation struct {
	Text    string   `yaml:"text" json:"text"`
	Kind    string   `yaml:"kind,omitempty" json:"kind,omitempty"`
	Aliases []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`
}

type Case struct {
	ID                    string             `yaml:"id" json:"id"`
	Mode                  string             `yaml:"mode" json:"mode"`
	Provider              string             `yaml:"provider,omitempty" json:"provider,omitempty"`
	SourceFile            string             `yaml:"source_file,omitempty" json:"source_file,omitempty"`
	SourceFiles           []string           `yaml:"source_files,omitempty" json:"source_files,omitempty"`
	Workspace             string             `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	ExpectedDurableClaims []ClaimExpectation `yaml:"expected_durable_claims,omitempty" json:"expected_durable_claims,omitempty"`
	ForbiddenClaims       []ClaimExpectation `yaml:"forbidden_claims,omitempty" json:"forbidden_claims,omitempty"`
	ExpectedKinds         []string           `yaml:"expected_kinds,omitempty" json:"expected_kinds,omitempty"`
	MinPersisted          int                `yaml:"min_persisted,omitempty" json:"min_persisted,omitempty"`
	MaxPersisted          int                `yaml:"max_persisted,omitempty" json:"max_persisted,omitempty"`
	Notes                 string             `yaml:"notes,omitempty" json:"notes,omitempty"`
}

type Suite struct {
	Name  string `yaml:"name" json:"name"`
	Cases []Case `yaml:"cases" json:"cases"`
}

type ActualClaim struct {
	Text       string   `json:"text"`
	Kind       string   `json:"kind"`
	Durability string   `json:"durability"`
	Tags       []string `json:"tags,omitempty"`
	GroupKeys  []string `json:"group_keys,omitempty"`
}

type CaseResult struct {
	ID               string        `json:"id"`
	Mode             string        `json:"mode"`
	Notes            string        `json:"notes,omitempty"`
	Precision        float64       `json:"precision"`
	Recall           float64       `json:"recall"`
	KindAccuracy     float64       `json:"kind_accuracy"`
	FallbackRate     float64       `json:"fallback_rate"`
	ForbiddenHits    int           `json:"forbidden_hits"`
	PersistedCount   int           `json:"persisted_count"`
	PersistedInRange bool          `json:"persisted_in_range"`
	Score            float64       `json:"score"`
	ActualClaims     []ActualClaim `json:"actual_claims,omitempty"`
	PersistedMemory  []string      `json:"persisted_memory,omitempty"`
}

type Summary struct {
	Cases                int     `json:"cases"`
	MeanScore            float64 `json:"mean_score"`
	MeanPrecision        float64 `json:"mean_precision"`
	MeanRecall           float64 `json:"mean_recall"`
	MeanKindAccuracy     float64 `json:"mean_kind_accuracy"`
	MeanFallbackRate     float64 `json:"mean_fallback_rate"`
	ForbiddenHitRate     float64 `json:"forbidden_hit_rate"`
	PersistedInRangeRate float64 `json:"persisted_in_range_rate"`
}

type RunResult struct {
	Suite       string       `json:"suite"`
	Cases       []CaseResult `json:"cases"`
	Summary     Summary      `json:"summary"`
	GeneratedAt time.Time    `json:"generated_at"`
}

type RunOptions struct {
	Runtime transcriptpipeline.LocalModelRuntime
}

func LoadSuite(path string) (Suite, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Suite{}, err
	}
	var suite Suite
	if err := yaml.Unmarshal(body, &suite); err != nil {
		return Suite{}, fmt.Errorf("decode suite yaml: %w", err)
	}
	if strings.TrimSpace(suite.Name) == "" {
		suite.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return suite, nil
}

func RunSuite(ctx context.Context, suite Suite, opts RunOptions) (RunResult, error) {
	results := make([]CaseResult, 0, len(suite.Cases))
	for _, c := range suite.Cases {
		result, err := runCase(ctx, c, opts)
		if err != nil {
			return RunResult{}, err
		}
		results = append(results, result)
	}
	return RunResult{
		Suite:       suite.Name,
		Cases:       results,
		Summary:     summarize(results),
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func runCase(ctx context.Context, c Case, opts RunOptions) (CaseResult, error) {
	tmpRoot, err := os.MkdirTemp("", "agentctl-transcriptmemoryeval-*")
	if err != nil {
		return CaseResult{}, err
	}
	defer os.RemoveAll(tmpRoot)
	casPath := filepath.Join(tmpRoot, "cas")
	if err := os.MkdirAll(casPath, 0o755); err != nil {
		return CaseResult{}, err
	}

	classifier := transcriptpipeline.NewCachedClaimClassifier(opts.Runtime)
	result := CaseResult{
		ID:    c.ID,
		Mode:  strings.TrimSpace(c.Mode),
		Notes: c.Notes,
	}

	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "grouped_doctrine":
		opts := transcriptpipeline.GroupRunOptions{
			StorageRoot:   tmpRoot,
			CASPath:       casPath,
			SourceFiles:   expandPaths(c.SourceFiles),
			Workspace:     expandPath(c.Workspace),
			Runtime:       opts.Runtime,
			PersistMemory: false,
		}
		if _, err := transcriptpipeline.RunGroupedDoctrine(ctx, opts); err != nil {
			return CaseResult{}, err
		}
		run, err := transcriptpipeline.RunGroupedDoctrine(ctx, opts)
		if err != nil {
			return CaseResult{}, err
		}
		if len(run.Groups) == 0 {
			return CaseResult{}, fmt.Errorf("transcriptmemoryeval: grouped_doctrine case %q returned no groups", c.ID)
		}
		group := run.Groups[0]
		result.PersistedMemory = persistedSummaries(group.PersistedMemory)
		result.ActualClaims = claimsFromDoctrine(group.DoctrineClaims, group.AlignedClaims)
		result.FallbackRate = doctrineFallbackRate(group.DoctrineSeedArtifact, group.DoctrineArtifact, group.AlignmentArtifact)
	case "grouped":
		run, err := transcriptpipeline.RunGrouped(ctx, transcriptpipeline.GroupRunOptions{
			StorageRoot:   tmpRoot,
			CASPath:       casPath,
			SourceFiles:   expandPaths(c.SourceFiles),
			Workspace:     expandPath(c.Workspace),
			Runtime:       opts.Runtime,
			Classifier:    classifier,
			PersistMemory: true,
		})
		if err != nil {
			return CaseResult{}, err
		}
		if len(run.Groups) == 0 {
			return CaseResult{}, fmt.Errorf("transcriptmemoryeval: grouped case %q returned no groups", c.ID)
		}
		group := run.Groups[0]
		result.PersistedMemory = persistedSummaries(group.PersistedMemory)
		result.ActualClaims = claimsFromPersisted(group.PersistedMemory)
		if len(result.ActualClaims) == 0 {
			result.ActualClaims = claimsFromReviewedOrConsolidated(group.ReviewedClaims, group.ConsolidatedClaims)
		}
		result.FallbackRate = fallbackRate(group.ClassificationArtifacts, group.ReviewArtifact)
	case "single_doctrine":
		provider := strings.TrimSpace(c.Provider)
		if provider == "" {
			provider = "auto"
		}
		opts := transcriptpipeline.SingleRunOptions{
			StorageRoot:   tmpRoot,
			CASPath:       casPath,
			Provider:      provider,
			SourceFile:    expandPath(c.SourceFile),
			Workspace:     expandPath(c.Workspace),
			Runtime:       opts.Runtime,
			PersistMemory: false,
		}
		if _, err := transcriptpipeline.RunSingleDoctrine(ctx, opts); err != nil {
			return CaseResult{}, err
		}
		run, err := transcriptpipeline.RunSingleDoctrine(ctx, opts)
		if err != nil {
			return CaseResult{}, err
		}
		result.PersistedMemory = persistedSummaries(run.PersistedMemory)
		result.ActualClaims = claimsFromDoctrine(run.DoctrineClaims, run.AlignedClaims)
		result.FallbackRate = doctrineFallbackRate(run.DoctrineSeedArtifact, run.DoctrineArtifact, run.AlignmentArtifact)
	case "", "single":
		provider := strings.TrimSpace(c.Provider)
		if provider == "" {
			provider = "auto"
		}
		run, err := transcriptpipeline.RunSingle(ctx, transcriptpipeline.SingleRunOptions{
			StorageRoot:   tmpRoot,
			CASPath:       casPath,
			Provider:      provider,
			SourceFile:    expandPath(c.SourceFile),
			Workspace:     expandPath(c.Workspace),
			Runtime:       opts.Runtime,
			Classifier:    classifier,
			PersistMemory: true,
		})
		if err != nil {
			return CaseResult{}, err
		}
		result.PersistedMemory = persistedSummaries(run.PersistedMemory)
		result.ActualClaims = claimsFromPersisted(run.PersistedMemory)
		if len(result.ActualClaims) == 0 {
			result.ActualClaims = claimsFromReviewedOrConsolidated(run.ReviewedClaims, run.ConsolidatedClaims)
		}
		result.FallbackRate = fallbackRate(run.ClassificationArtifacts, run.ReviewArtifact)
	default:
		return CaseResult{}, fmt.Errorf("transcriptmemoryeval: unsupported case mode %q", c.Mode)
	}

	result.PersistedCount = len(result.PersistedMemory)
	result.PersistedInRange = persistedCountInRange(result.PersistedCount, c.MinPersisted, c.MaxPersisted)
	result.Precision, result.Recall, result.KindAccuracy = claimMetrics(result.ActualClaims, c.ExpectedDurableClaims)
	result.ForbiddenHits = forbiddenHits(result.ActualClaims, c.ForbiddenClaims)
	result.Score = scoreCase(result)
	return result, nil
}

func claimsFromDoctrine(doctrine, aligned []transcriptpipeline.ClassifiedClaim) []ActualClaim {
	source := aligned
	if len(source) == 0 {
		source = doctrine
	}
	out := make([]ActualClaim, 0, len(source))
	for _, claim := range source {
		out = append(out, ActualClaim{
			Text:       strings.TrimSpace(claim.Text),
			Kind:       string(claim.Kind),
			Durability: string(claim.Durability),
			Tags:       append([]string(nil), claim.Tags...),
			GroupKeys:  append([]string(nil), claim.GroupKeys...),
		})
	}
	return out
}

func doctrineFallbackRate(artifacts ...*transcriptpipeline.ArtifactCacheReport) float64 {
	total := 0
	fallback := 0
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		total++
		if strings.TrimSpace(artifact.DerivationMode) != "lmstudio" {
			fallback++
		}
	}
	if total == 0 {
		return 1
	}
	return float64(fallback) / float64(total)
}

func claimsFromReviewedOrConsolidated(reviewed, consolidated []transcriptpipeline.ClassifiedClaim) []ActualClaim {
	source := reviewed
	if len(source) == 0 {
		source = consolidated
	}
	out := make([]ActualClaim, 0, len(source))
	for _, claim := range source {
		out = append(out, ActualClaim{
			Text:       strings.TrimSpace(claim.Text),
			Kind:       string(claim.Kind),
			Durability: string(claim.Durability),
			Tags:       append([]string(nil), claim.Tags...),
			GroupKeys:  append([]string(nil), claim.GroupKeys...),
		})
	}
	return out
}

func persistedSummaries(items []transcriptpipeline.PersistedMemory) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, strings.TrimSpace(item.Summary))
	}
	return out
}

func claimsFromPersisted(items []transcriptpipeline.PersistedMemory) []ActualClaim {
	out := make([]ActualClaim, 0, len(items))
	for _, item := range items {
		kind, ok := claimKindFromPersisted(item)
		if !ok {
			continue
		}
		out = append(out, ActualClaim{
			Text:       strings.TrimSpace(item.Summary),
			Kind:       kind,
			Durability: "durable",
		})
	}
	return out
}

func claimKindFromPersisted(item transcriptpipeline.PersistedMemory) (string, bool) {
	candidateType := strings.TrimSpace(item.CandidateType)
	switch {
	case strings.HasPrefix(candidateType, "classified_claim:"):
		kind := strings.TrimPrefix(candidateType, "classified_claim:")
		if kind != "" {
			return kind, true
		}
	case candidateType == "group_topline_claim":
		return "architecture", true
	}
	switch strings.TrimSpace(item.Type) {
	case "preference":
		return "preference", true
	case "decision":
		return "decision", true
	case "learning":
		return "technical_context", true
	default:
		return "", false
	}
}

func fallbackRate(artifacts []transcriptpipeline.ArtifactCacheReport, review *transcriptpipeline.ArtifactCacheReport) float64 {
	total := 0
	fallbacks := 0
	for _, item := range artifacts {
		total++
		if item.DerivationMode != "lmstudio" {
			fallbacks++
		}
	}
	if review != nil {
		total++
		if review.DerivationMode != "lmstudio" {
			fallbacks++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(fallbacks) / float64(total)
}

func persistedCountInRange(count, minCount, maxCount int) bool {
	if minCount > 0 && count < minCount {
		return false
	}
	if maxCount > 0 && count > maxCount {
		return false
	}
	return true
}

func claimMetrics(actual []ActualClaim, expected []ClaimExpectation) (precision, recall, kindAccuracy float64) {
	actualDurable := make([]ActualClaim, 0, len(actual))
	for _, item := range actual {
		if item.Durability == "durable" {
			actualDurable = append(actualDurable, item)
		}
	}
	if len(expected) == 0 {
		if len(actualDurable) == 0 {
			return 1, 1, 1
		}
		return 0, 1, 1
	}
	matchedActual := make(map[int]struct{})
	matchedExpected := 0
	kindMatched := 0
	for _, exp := range expected {
		bestIdx := -1
		bestScore := 0.0
		for idx, act := range actualDurable {
			if _, used := matchedActual[idx]; used {
				continue
			}
			score := expectationMatchScore(exp, act)
			if score > bestScore {
				bestScore = score
				bestIdx = idx
			}
		}
		if bestIdx >= 0 && bestScore >= 0.55 {
			matchedActual[bestIdx] = struct{}{}
			matchedExpected++
			if exp.Kind == "" || strings.EqualFold(strings.TrimSpace(exp.Kind), strings.TrimSpace(actualDurable[bestIdx].Kind)) {
				kindMatched++
			}
		}
	}
	if len(actualDurable) > 0 {
		precision = float64(len(matchedActual)) / float64(len(actualDurable))
	}
	recall = float64(matchedExpected) / float64(len(expected))
	if matchedExpected > 0 {
		kindAccuracy = float64(kindMatched) / float64(matchedExpected)
	} else {
		kindAccuracy = 1
	}
	return precision, recall, kindAccuracy
}

func expectationMatchScore(exp ClaimExpectation, act ActualClaim) float64 {
	targets := append([]string{exp.Text}, exp.Aliases...)
	best := 0.0
	for _, target := range targets {
		score := tokenF1(target, act.Text)
		if score > best {
			best = score
		}
	}
	return best
}

func forbiddenHits(actual []ActualClaim, forbidden []ClaimExpectation) int {
	actualDurable := make([]ActualClaim, 0, len(actual))
	for _, item := range actual {
		if item.Durability == "durable" {
			actualDurable = append(actualDurable, item)
		}
	}
	hits := 0
	for _, exp := range forbidden {
		for _, act := range actualDurable {
			if expectationMatchScore(exp, act) >= 0.55 {
				hits++
				break
			}
		}
	}
	return hits
}

func scoreCase(result CaseResult) float64 {
	score := 0.0
	score += result.Precision * 0.35
	score += result.Recall * 0.30
	score += result.KindAccuracy * 0.15
	if result.PersistedInRange {
		score += 0.10
	}
	score += (1.0 - result.FallbackRate) * 0.10
	score -= minFloat(float64(result.ForbiddenHits)*0.25, 0.5)
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func summarize(results []CaseResult) Summary {
	s := Summary{Cases: len(results)}
	if len(results) == 0 {
		return s
	}
	for _, item := range results {
		s.MeanScore += item.Score
		s.MeanPrecision += item.Precision
		s.MeanRecall += item.Recall
		s.MeanKindAccuracy += item.KindAccuracy
		s.MeanFallbackRate += item.FallbackRate
		s.ForbiddenHitRate += float64(item.ForbiddenHits)
		if item.PersistedInRange {
			s.PersistedInRangeRate++
		}
	}
	div := float64(len(results))
	s.MeanScore /= div
	s.MeanPrecision /= div
	s.MeanRecall /= div
	s.MeanKindAccuracy /= div
	s.MeanFallbackRate /= div
	s.ForbiddenHitRate /= div
	s.PersistedInRangeRate /= div
	return s
}

func RenderMarkdown(result RunResult) string {
	var b strings.Builder
	b.WriteString("# Transcript Memory Eval\n\n")
	b.WriteString(fmt.Sprintf("- Suite: `%s`\n", result.Suite))
	b.WriteString(fmt.Sprintf("- Cases: `%d`\n\n", result.Summary.Cases))
	b.WriteString("## Summary\n\n")
	b.WriteString("| Score | Precision | Recall | Kind Acc | Fallback | Forbidden | Persist Range |\n")
	b.WriteString("| ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	b.WriteString(fmt.Sprintf("| %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f |\n\n",
		result.Summary.MeanScore,
		result.Summary.MeanPrecision,
		result.Summary.MeanRecall,
		result.Summary.MeanKindAccuracy,
		result.Summary.MeanFallbackRate,
		result.Summary.ForbiddenHitRate,
		result.Summary.PersistedInRangeRate,
	))
	for _, c := range result.Cases {
		b.WriteString("## " + c.ID + "\n\n")
		b.WriteString(fmt.Sprintf("- Mode: `%s`\n", c.Mode))
		b.WriteString(fmt.Sprintf("- Score: `%.2f`\n", c.Score))
		b.WriteString(fmt.Sprintf("- Precision/Recall: `%.2f / %.2f`\n", c.Precision, c.Recall))
		b.WriteString(fmt.Sprintf("- Kind Accuracy: `%.2f`\n", c.KindAccuracy))
		b.WriteString(fmt.Sprintf("- Fallback Rate: `%.2f`\n", c.FallbackRate))
		b.WriteString(fmt.Sprintf("- Forbidden Hits: `%d`\n", c.ForbiddenHits))
		b.WriteString(fmt.Sprintf("- Persisted Count: `%d` (in range: `%t`)\n", c.PersistedCount, c.PersistedInRange))
		if c.Notes != "" {
			b.WriteString("- Notes: " + c.Notes + "\n")
		}
		if len(c.ActualClaims) > 0 {
			b.WriteString("- Actual durable/session claims:\n")
			for _, claim := range c.ActualClaims {
				b.WriteString(fmt.Sprintf("  - [%s/%s] %s\n", claim.Kind, claim.Durability, claim.Text))
			}
		}
		if len(c.PersistedMemory) > 0 {
			b.WriteString("- Persisted memory:\n")
			for _, item := range c.PersistedMemory {
				b.WriteString("  - " + item + "\n")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func expandPaths(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		if path := expandPath(item); path != "" {
			out = append(out, path)
		}
	}
	return out
}

func expandPath(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	if strings.HasPrefix(in, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			in = filepath.Join(home, strings.TrimPrefix(in, "~/"))
		}
	}
	return os.ExpandEnv(in)
}

func tokenF1(a, b string) float64 {
	aTokens := normalizedTokenSet(a)
	bTokens := normalizedTokenSet(b)
	if len(aTokens) == 0 || len(bTokens) == 0 {
		return 0
	}
	intersection := 0
	for token := range aTokens {
		if _, ok := bTokens[token]; ok {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	precision := float64(intersection) / float64(len(bTokens))
	recall := float64(intersection) / float64(len(aTokens))
	return 2 * precision * recall / (precision + recall)
}

func normalizedTokenSet(in string) map[string]struct{} {
	in = strings.ToLower(strings.TrimSpace(in))
	replacer := strings.NewReplacer(
		"`", " ",
		"[", " ",
		"]", " ",
		"(", " ",
		")", " ",
		",", " ",
		".", " ",
		":", " ",
		";", " ",
		"!", " ",
		"?", " ",
		"/", " ",
		"\\", " ",
		"-", " ",
		"_", " ",
	)
	in = replacer.Replace(in)
	out := make(map[string]struct{})
	for _, token := range strings.Fields(in) {
		if len(token) < 3 {
			continue
		}
		out[token] = struct{}{}
	}
	return out
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
