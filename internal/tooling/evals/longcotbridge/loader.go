package longcotbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/tooling/evals/longcoteval"
)

type PythonLoaderConfig struct {
	RepoPath  string
	PythonBin string
}

type PythonQuestionLoader struct {
	cfg PythonLoaderConfig
}

func NewPythonQuestionLoader(cfg PythonLoaderConfig) *PythonQuestionLoader {
	return &PythonQuestionLoader{cfg: cfg}
}

func (l *PythonQuestionLoader) LoadQuestions(ctx context.Context, request longcoteval.LoadRequest) ([]longcoteval.Question, error) {
	_ = request // foxctl applies filtering after official loading.

	repoPath, err := resolveLongCoTRepoPath(l.cfg.RepoPath)
	if err != nil {
		return nil, err
	}
	cmdName, cmdArgs, err := resolvePythonVerifierCommand(strings.TrimSpace(l.cfg.PythonBin))
	if err != nil {
		return nil, err
	}

	script := `
import dataclasses
import json
import longcot
import sys

def jsonable(value):
    if value is None:
        return None
    if dataclasses.is_dataclass(value):
        return dataclasses.asdict(value)
    try:
        json.dumps(value)
        return value
    except TypeError:
        return str(value)

questions = longcot.load_questions()
for q in questions:
    row = {
        "question_id": getattr(q, "question_id", ""),
        "domain": getattr(q, "domain", ""),
        "difficulty": getattr(q, "difficulty", ""),
        "template": getattr(q, "template", ""),
        "prompt": getattr(q, "prompt", ""),
        "answer": jsonable(getattr(q, "answer", None)),
        "canary": getattr(q, "canary", ""),
    }
    sys.stdout.write(json.dumps(row, ensure_ascii=False) + "\n")
`
	cmdArgs = append(cmdArgs, "-c", script)
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...) //nolint:gosec // explicit argv; repo path is user-provided
	cmd.Dir = repoPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf(
			"official LongCoT question loader failed: %w (stdout=%q stderr=%q)",
			err,
			strings.TrimSpace(stdout.String()),
			strings.TrimSpace(stderr.String()),
		)
	}

	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	questions := make([]longcoteval.Question, 0, 512)
	for {
		var row officialQuestionRow
		if err := decoder.Decode(&row); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode official LongCoT question row: %w", err)
		}
		if strings.TrimSpace(row.QuestionID) == "" || strings.TrimSpace(row.Prompt) == "" {
			return nil, fmt.Errorf("official LongCoT loader returned row missing question_id or prompt")
		}
		questions = append(questions, longcoteval.Question{
			ID:           strings.TrimSpace(row.QuestionID),
			Domain:       strings.TrimSpace(row.Domain),
			Difficulty:   strings.TrimSpace(row.Difficulty),
			Template:     strings.TrimSpace(row.Template),
			PromptText:   strings.TrimSpace(row.Prompt),
			Answer:       strings.TrimSpace(string(row.Answer)),
			Canary:       strings.TrimSpace(row.Canary),
			QuestionHash: hashText(row.Prompt),
		})
	}
	return questions, nil
}

type officialQuestionRow struct {
	QuestionID string          `json:"question_id"`
	Domain     string          `json:"domain"`
	Difficulty string          `json:"difficulty"`
	Template   string          `json:"template"`
	Prompt     string          `json:"prompt"`
	Answer     json.RawMessage `json:"answer"`
	Canary     string          `json:"canary"`
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func resolveLongCoTRepoPath(repoPath string) (string, error) {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		repoPath = strings.TrimSpace(os.Getenv(envLongCoTRepo))
	}
	if repoPath == "" {
		repoPath = strings.TrimSpace(os.Getenv(EnvLongCoTRepo))
	}
	if repoPath == "" {
		return "", fmt.Errorf(
			"official LongCoT unavailable: set --longcot-repo (or %s/%s) to a checkout of https://github.com/LongHorizonReasoning/longcot",
			envLongCoTRepo,
			EnvLongCoTRepo,
		)
	}
	repoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return "", fmt.Errorf("resolve longcot repo path: %w", err)
	}
	if _, err := os.Stat(filepath.Join(repoPath, "run_eval.py")); err != nil {
		return "", fmt.Errorf(
			"official LongCoT unavailable: %s is missing run_eval.py: %w",
			repoPath,
			err,
		)
	}
	return repoPath, nil
}
