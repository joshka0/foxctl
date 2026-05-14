package shellreduce

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	actormemory "github.com/joshka0/foxctl/internal/runtime/actor/memory"
	tiktoken "github.com/pkoukk/tiktoken-go"
)

// MeasureOptions controls raw-vs-reduced output comparison.
type MeasureOptions struct {
	TokenModel                   string
	ReducedDuration              time.Duration
	InputTokenPricePerMillionUSD float64
}

// Measure compares raw command output against reduced summary text.
func Measure(ctx context.Context, workspace string, argv []string, reducedSummary string, opts MeasureOptions) map[string]any {
	counter := newTokenCounter(opts.TokenModel)
	raw := executeRaw(ctx, workspace, argv)

	rawText := strings.TrimSpace(raw.Combined)
	reducedText := strings.TrimSpace(reducedSummary)
	rawTokens := counter.Count(rawText)
	reducedTokens := counter.Count(reducedText)
	rawBytes := len(rawText)
	reducedBytes := len(reducedText)
	reducedDurationMS := int(opts.ReducedDuration.Milliseconds())
	durationSavedMS := raw.DurationMS - reducedDurationMS
	rawCostUSD := tokenCostUSD(rawTokens, opts.InputTokenPricePerMillionUSD)
	reducedCostUSD := tokenCostUSD(reducedTokens, opts.InputTokenPricePerMillionUSD)
	advice := recommendReduction(rawBytes, reducedBytes, rawTokens, reducedTokens, raw.Error)

	return map[string]any{
		"tokenizer": map[string]any{
			"name":      counter.Name,
			"requested": strings.TrimSpace(opts.TokenModel),
			"kind":      counter.Kind,
		},
		"raw": map[string]any{
			"command":                  JoinCommand(raw.Argv),
			"stdout_bytes":             len(raw.Stdout),
			"stderr_bytes":             len(raw.Stderr),
			"combined_bytes":           rawBytes,
			"combined_tokens":          rawTokens,
			"duration_ms":              raw.DurationMS,
			"exit_code":                raw.ExitCode,
			"error":                    raw.Error,
			"estimated_input_cost_usd": rawCostUSD,
		},
		"reduced": map[string]any{
			"bytes":                    reducedBytes,
			"tokens":                   reducedTokens,
			"duration_ms":              reducedDurationMS,
			"estimated_input_cost_usd": reducedCostUSD,
		},
		"savings": map[string]any{
			"bytes_saved":                        rawBytes - reducedBytes,
			"tokens_saved":                       rawTokens - reducedTokens,
			"bytes_saved_percent":                percentSaved(rawBytes, reducedBytes),
			"tokens_saved_percent":               percentSaved(rawTokens, reducedTokens),
			"duration_saved_ms":                  durationSavedMS,
			"duration_saved_percent":             percentSaved(raw.DurationMS, reducedDurationMS),
			"estimated_input_cost_saved_usd":     rawCostUSD - reducedCostUSD,
			"estimated_input_cost_saved_percent": percentSavedFloat(rawCostUSD, reducedCostUSD),
		},
		"pricing": map[string]any{
			"input_token_price_per_million_usd": opts.InputTokenPricePerMillionUSD,
		},
		"advice": advice,
	}
}

type rawExecResult struct {
	Argv       []string
	Stdout     string
	Stderr     string
	Combined   string
	ExitCode   int
	DurationMS int
	Error      string
}

func executeRaw(ctx context.Context, workspace string, argv []string) rawExecResult {
	rawArgv, normalizeErr := normalizeRawArgv(argv)
	if len(rawArgv) == 0 {
		return rawExecResult{Argv: argv, ExitCode: -1, Error: normalizeErr}
	}
	if normalizeErr != "" {
		return rawExecResult{Argv: rawArgv, ExitCode: -1, Error: normalizeErr}
	}

	cmd := exec.CommandContext(ctx, rawArgv[0], rawArgv[1:]...)
	if strings.TrimSpace(workspace) != "" {
		cmd.Dir = workspace
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	err := cmd.Run()
	durationMS := int(time.Since(started).Milliseconds())

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	stdoutText := stdout.String()
	stderrText := stderr.String()
	combined := strings.TrimSpace(stdoutText)
	if strings.TrimSpace(stderrText) != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += strings.TrimSpace(stderrText)
	}

	result := rawExecResult{
		Argv:       rawArgv,
		Stdout:     stdoutText,
		Stderr:     stderrText,
		Combined:   combined,
		ExitCode:   exitCode,
		DurationMS: durationMS,
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func normalizeRawArgv(argv []string) ([]string, string) {
	if len(argv) == 0 {
		return nil, ""
	}
	if argv[0] == "read" {
		return append([]string{"cat"}, argv[1:]...), ""
	}
	if argv[0] == "find" && containsArg(argv[1:], "-maxdepth") {
		if path, err := exec.LookPath("gfind"); err == nil && strings.TrimSpace(path) != "" {
			return append([]string{path}, argv[1:]...), ""
		}
		return append([]string(nil), argv...), "raw measurement for find -maxdepth requires GNU find (gfind) on this platform"
	}
	return append([]string(nil), argv...), ""
}

func containsArg(argv []string, target string) bool {
	for _, arg := range argv {
		if strings.TrimSpace(arg) == target {
			return true
		}
	}
	return false
}

func percentSaved(rawSize, reducedSize int) float64 {
	if rawSize <= 0 {
		return 0
	}
	return float64(rawSize-reducedSize) * 100 / float64(rawSize)
}

func percentSavedFloat(rawSize, reducedSize float64) float64 {
	if rawSize <= 0 {
		return 0
	}
	return (rawSize - reducedSize) * 100 / rawSize
}

func tokenCostUSD(tokens int, pricePerMillion float64) float64 {
	if tokens <= 0 || pricePerMillion <= 0 {
		return 0
	}
	return float64(tokens) * pricePerMillion / 1_000_000
}

type tokenCounter struct {
	Name string
	Kind string
	enc  *tiktoken.Tiktoken
}

func recommendReduction(rawBytes, reducedBytes, rawTokens, reducedTokens int, rawError string) map[string]any {
	rawError = strings.TrimSpace(rawError)
	if rawError != "" {
		return map[string]any{
			"mode":   "raw_unavailable",
			"reason": rawError,
		}
	}
	tokenSavedPct := percentSaved(rawTokens, reducedTokens)
	byteSavedPct := percentSaved(rawBytes, reducedBytes)
	switch {
	case tokenSavedPct >= 10 || byteSavedPct >= 10:
		return map[string]any{
			"mode":   "reduce",
			"reason": "reduced output saves at least 10% tokens or bytes",
		}
	case reducedTokens >= rawTokens && reducedBytes >= rawBytes:
		return map[string]any{
			"mode":   "keep_raw",
			"reason": "raw output is already as compact as the reduced form",
		}
	default:
		return map[string]any{
			"mode":   "either",
			"reason": "difference is small; choose based on readability",
		}
	}
}

// RecommendationMode extracts the recommendation mode from a measurement payload.
func RecommendationMode(measure map[string]any) string {
	return stringValue(asMap(measure["advice"])["mode"])
}

func newTokenCounter(requested string) tokenCounter {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = "cl100k_base"
	}

	modelName := normalizeTokenizerRequest(requested)
	if enc, err := tiktoken.EncodingForModel(modelName); err == nil && enc != nil {
		return tokenCounter{Name: modelName, Kind: "model", enc: enc}
	}
	if enc, err := tiktoken.GetEncoding(requested); err == nil && enc != nil {
		return tokenCounter{Name: requested, Kind: "encoding", enc: enc}
	}
	if enc, err := tiktoken.GetEncoding("cl100k_base"); err == nil && enc != nil {
		return tokenCounter{Name: "cl100k_base", Kind: "encoding", enc: enc}
	}
	return tokenCounter{Name: "heuristic", Kind: "heuristic"}
}

func normalizeTokenizerRequest(request string) string {
	request = strings.TrimSpace(request)
	if request == "" {
		return request
	}
	if strings.Contains(request, "/") {
		parts := strings.Split(request, "/")
		request = parts[len(parts)-1]
	}
	return strings.TrimSpace(request)
}

func (c tokenCounter) Count(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	if c.enc == nil {
		return actormemory.EstimateTokens(text)
	}
	return len(c.enc.Encode(text, nil, nil))
}

// MeasureSummaryLine renders a compact human-readable measurement summary.
func MeasureSummaryLine(measure map[string]any) string {
	if len(measure) == 0 {
		return ""
	}
	raw := asMap(measure["raw"])
	reduced := asMap(measure["reduced"])
	savings := asMap(measure["savings"])
	if len(raw) == 0 || len(reduced) == 0 || len(savings) == 0 {
		return ""
	}
	line := fmt.Sprintf(
		"Tokens: raw %d -> reduced %d (%.0f%% saved); bytes: raw %d -> reduced %d (%.0f%% saved)",
		intValue(raw["combined_tokens"]),
		intValue(reduced["tokens"]),
		floatValue(savings["tokens_saved_percent"]),
		intValue(raw["combined_bytes"]),
		intValue(reduced["bytes"]),
		floatValue(savings["bytes_saved_percent"]),
	)
	if rawMS := intValue(raw["duration_ms"]); rawMS > 0 || intValue(reduced["duration_ms"]) > 0 {
		line += fmt.Sprintf("; ms: raw %d -> reduced %d (%.0f%% saved)", rawMS, intValue(reduced["duration_ms"]), floatValue(savings["duration_saved_percent"]))
	}
	if rawCost := floatValue(raw["estimated_input_cost_usd"]); rawCost > 0 {
		line += fmt.Sprintf("; input cost: $%.6f -> $%.6f (%.0f%% saved)", rawCost, floatValue(reduced["estimated_input_cost_usd"]), floatValue(savings["estimated_input_cost_saved_percent"]))
	}
	advice := asMap(measure["advice"])
	mode := stringValue(advice["mode"])
	if mode == "" {
		return line
	}
	return line + fmt.Sprintf(" [%s]", mode)
}
