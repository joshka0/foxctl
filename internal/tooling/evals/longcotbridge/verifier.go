package longcotbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/tooling/evals/longcoteval"
)

const (
	EnvLongCoTRepo = "LONGCOT_REPO"
	envLongCoTRepo = "FOXCTL_LONGCOT_REPO"
)

type PythonVerifierConfig struct {
	RepoPath   string
	PythonBin  string
	NoFallback bool
}

type PythonVerifier struct {
	cfg PythonVerifierConfig
}

func NewPythonVerifier(cfg PythonVerifierConfig) *PythonVerifier {
	return &PythonVerifier{cfg: cfg}
}

func (v *PythonVerifier) Verify(ctx context.Context, request longcoteval.VerifyRequest) (longcoteval.VerifyResult, error) {
	responsesPath := strings.TrimSpace(request.ResponsesPath)
	if responsesPath == "" {
		return longcoteval.VerifyResult{}, fmt.Errorf("verify requires responses_path")
	}
	responsesPath, err := filepath.Abs(responsesPath)
	if err != nil {
		return longcoteval.VerifyResult{}, fmt.Errorf("resolve responses path: %w", err)
	}
	if _, err := os.Stat(responsesPath); err != nil {
		return longcoteval.VerifyResult{}, fmt.Errorf("responses JSONL not found: %w", err)
	}

	repoPath := strings.TrimSpace(v.cfg.RepoPath)
	if repoPath == "" {
		repoPath = strings.TrimSpace(os.Getenv(envLongCoTRepo))
	}
	if repoPath == "" {
		repoPath = strings.TrimSpace(os.Getenv(EnvLongCoTRepo))
	}
	if repoPath == "" {
		return longcoteval.VerifyResult{}, fmt.Errorf(
			"official LongCoT verifier unavailable: set --longcot-repo (or %s/%s) to a checkout of https://github.com/LongHorizonReasoning/longcot",
			envLongCoTRepo,
			EnvLongCoTRepo,
		)
	}

	repoPath, err = filepath.Abs(repoPath)
	if err != nil {
		return longcoteval.VerifyResult{}, fmt.Errorf("resolve longcot repo path: %w", err)
	}
	scriptPath := filepath.Join(repoPath, "run_eval.py")
	if _, err := os.Stat(scriptPath); err != nil {
		return longcoteval.VerifyResult{}, fmt.Errorf(
			"official LongCoT verifier unavailable: %s is missing run_eval.py: %w",
			repoPath,
			err,
		)
	}

	outputPath := strings.TrimSpace(request.OutputPath)
	if outputPath == "" {
		tmpDir, err := os.MkdirTemp("", "foxctl-longcot-verify-*")
		if err != nil {
			return longcoteval.VerifyResult{}, fmt.Errorf("create verifier temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)
		outputPath = filepath.Join(tmpDir, "results.json")
	} else {
		outputPath, err = filepath.Abs(outputPath)
		if err != nil {
			return longcoteval.VerifyResult{}, fmt.Errorf("resolve verify output path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return longcoteval.VerifyResult{}, fmt.Errorf("prepare verify output dir: %w", err)
		}
	}

	cmdName, cmdArgs, err := resolvePythonVerifierCommand(strings.TrimSpace(v.cfg.PythonBin))
	if err != nil {
		return longcoteval.VerifyResult{}, err
	}
	cmdArgs = append(cmdArgs, "run_eval.py", responsesPath, "--output", outputPath)
	if v.cfg.NoFallback {
		cmdArgs = append(cmdArgs, "--no-fallback")
	}

	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...) //nolint:gosec // explicit argv and repo-local script
	cmd.Dir = repoPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return longcoteval.VerifyResult{}, fmt.Errorf(
			"official LongCoT verifier failed: %w (stdout=%q stderr=%q)",
			err,
			strings.TrimSpace(stdout.String()),
			strings.TrimSpace(stderr.String()),
		)
	}

	body, err := os.ReadFile(outputPath)
	if err != nil {
		return longcoteval.VerifyResult{}, fmt.Errorf("read verifier output: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return longcoteval.VerifyResult{}, fmt.Errorf("decode verifier output JSON: %w", err)
	}

	result := longcoteval.VerifyResult{
		VerifierName: "longcot.run_eval.py",
		Counts:       extractVerifyCounts(raw),
		Rows:         extractVerifyRows(raw),
		Raw:          raw,
	}
	return result, nil
}

func resolvePythonVerifierCommand(requestedPython string) (string, []string, error) {
	// Prefer `uv run python` when available because official LongCoT uses uv for
	// dependency management and reproducible lockfile execution.
	if _, err := exec.LookPath("uv"); err == nil {
		python := "python"
		if requestedPython != "" {
			python = requestedPython
		}
		return "uv", []string{"run", python}, nil
	}
	candidates := []string{}
	if requestedPython != "" {
		candidates = append(candidates, requestedPython)
	}
	candidates = append(candidates, "python3", "python")
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate, nil, nil
		}
	}
	return "", nil, fmt.Errorf(
		"official LongCoT verifier unavailable: missing python runtime (install uv or set --longcot-python to a working python3)",
	)
}

func extractVerifyCounts(raw map[string]any) map[string]int {
	out := map[string]int{}
	for _, key := range []string{"total", "correct", "incorrect", "failed", "wrong_formatting"} {
		if value, ok := raw[key]; ok {
			out[key] = intFromAny(value)
		}
	}
	return out
}

func extractVerifyRows(raw map[string]any) []longcoteval.VerifyRow {
	rawDetails, ok := raw["details"].([]any)
	if !ok {
		return nil
	}
	rows := make([]longcoteval.VerifyRow, 0, len(rawDetails))
	for _, detailRaw := range rawDetails {
		detail, ok := detailRaw.(map[string]any)
		if !ok {
			continue
		}
		status := strings.TrimSpace(fmt.Sprint(detail["status"]))
		row := longcoteval.VerifyRow{
			QuestionID:      strings.TrimSpace(fmt.Sprint(detail["question_id"])),
			Status:          status,
			WrongFormatting: boolFromAny(detail["wrong_formatting"]),
			NormalizedAnswer: strings.TrimSpace(
				fmt.Sprint(firstNonNil(detail["normalized_answer"], detail["parsed_solution"])),
			),
		}
		switch status {
		case longcoteval.VerifierStatusCorrect:
			row.Correct = true
		case "":
			row.Status = longcoteval.VerifierStatusFailed
		}
		if errMsg := strings.TrimSpace(fmt.Sprint(firstNonNil(detail["error"], detail["reason"]))); errMsg != "" {
			row.VerificationError = errMsg
		}
		rows = append(rows, row)
	}
	return rows
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value == nil {
			continue
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		return value
	}
	return nil
}

func intFromAny(value any) int {
	switch raw := value.(type) {
	case int:
		return raw
	case int8:
		return int(raw)
	case int16:
		return int(raw)
	case int32:
		return int(raw)
	case int64:
		return int(raw)
	case uint:
		return int(raw)
	case uint8:
		return int(raw)
	case uint16:
		return int(raw)
	case uint32:
		return int(raw)
	case uint64:
		return int(raw)
	case float32:
		return int(raw)
	case float64:
		return int(raw)
	case json.Number:
		if n, err := raw.Int64(); err == nil {
			return int(n)
		}
	}
	return 0
}

func boolFromAny(value any) bool {
	switch raw := value.(type) {
	case bool:
		return raw
	case string:
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "true", "yes":
			return true
		}
	}
	return false
}
