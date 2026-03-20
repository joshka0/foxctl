package optimization

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// PromptPreferenceExample is one ranked preference example derived from prompt comparisons.
type PromptPreferenceExample struct {
	RecordType string                      `json:"record_type"`
	Input      PromptPreferenceInput       `json:"input"`
	Chosen     PromptPreferenceCandidate   `json:"chosen"`
	Rejected   PromptPreferenceCandidate   `json:"rejected"`
	Metadata   PromptPreferenceExampleMeta `json:"metadata"`
}

type PromptPreferenceInput struct {
	Question       string `json:"question"`
	Context        string `json:"context,omitempty"`
	TargetResponse string `json:"target_response,omitempty"`
	EvalCaseID     string `json:"eval_case_id,omitempty"`
	Category       string `json:"category,omitempty"`
}

type PromptPreferenceCandidate struct {
	VariantID      string                  `json:"variant_id"`
	AgentRole      string                  `json:"agent_role"`
	Mode           string                  `json:"mode"`
	Prompt         string                  `json:"prompt"`
	MeanScore      float64                 `json:"mean_score"`
	WorstScore     float64                 `json:"worst_score"`
	PassCount      int                     `json:"pass_count"`
	OutputsByModel []PromptPreferenceModel `json:"outputs_by_model,omitempty"`
}

type PromptPreferenceModel struct {
	Model  string  `json:"model"`
	Output string  `json:"output,omitempty"`
	Error  string  `json:"error,omitempty"`
	Score  float64 `json:"score"`
	Passed bool    `json:"passed"`
}

type PromptPreferenceExampleMeta struct {
	RunID          string         `json:"run_id"`
	ArtifactDigest string         `json:"artifact_digest"`
	Provider       string         `json:"provider"`
	BaseURL        string         `json:"base_url,omitempty"`
	Granularity    string         `json:"granularity,omitempty"`
	EvalCaseID     string         `json:"eval_case_id,omitempty"`
	Category       string         `json:"category,omitempty"`
	Scoring        map[string]any `json:"scoring,omitempty"`
}

// WritePromptPreferenceDatasetJSONL writes one preference example per line.
func WritePromptPreferenceDatasetJSONL(w io.Writer, examples []PromptPreferenceExample) error {
	enc := json.NewEncoder(w)
	for _, example := range examples {
		if err := enc.Encode(example); err != nil {
			return fmt.Errorf("encode prompt preference example: %w", err)
		}
	}
	return nil
}

// BuildPromptPreferenceDatasetJSONL returns JSONL bytes for the preference dataset.
func BuildPromptPreferenceDatasetJSONL(examples []PromptPreferenceExample) ([]byte, error) {
	var builder strings.Builder
	if err := WritePromptPreferenceDatasetJSONL(&builder, examples); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

// ParsePromptPreferenceDatasetJSONL decodes JSONL preference examples.
func ParsePromptPreferenceDatasetJSONL(r io.Reader) ([]PromptPreferenceExample, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10<<20)
	examples := []PromptPreferenceExample{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var example PromptPreferenceExample
		if err := json.Unmarshal([]byte(line), &example); err != nil {
			return nil, fmt.Errorf("decode prompt preference example: %w", err)
		}
		examples = append(examples, example)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan prompt preference dataset: %w", err)
	}
	return examples, nil
}

// SavePromptPreferenceDatasetFile writes preference examples to JSONL.
func SavePromptPreferenceDatasetFile(path string, examples []PromptPreferenceExample) error {
	body, err := BuildPromptPreferenceDatasetJSONL(examples)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		if existingMode := info.Mode().Perm(); existingMode != 0 && (mode&^existingMode) != 0 {
			mode = existingMode
		}
	}
	return os.WriteFile(path, body, mode)
}
