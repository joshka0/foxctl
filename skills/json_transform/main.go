// Package main implements the json/transform skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	Operation string `json:"operation"`
	Input     string `json:"input"`
	Path      string `json:"path"`
	MergeWith string `json:"merge_with"`
	Indent    int    `json:"indent"`
	Compact   bool   `json:"compact"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("json/transform", "ERUNTIME", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("json/transform", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("json/transform", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("json/transform", "ERUNTIME", err)
	}
}

func run(_ context.Context, rc *runner.RunnerContext, in input) error {
	// Parse input JSON
	var data any
	if err := json.Unmarshal([]byte(in.Input), &data); err != nil {
		return fmt.Errorf("invalid JSON input: %w", err)
	}

	var result map[string]any

	switch in.Operation {
	case "extract":
		result = extractOperation(data, in.Path)
	case "merge":
		result = mergeOperation(data, in.MergeWith)
	case "validate":
		result = validateOperation(data)
	case "format":
		result = formatOperation(data, in.Indent, in.Compact)
	case "keys":
		result = keysOperation(data)
	default:
		return fmt.Errorf("invalid operation: %s", in.Operation)
	}

	return rc.Emit("json/transform", result, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}

	if in.Operation == "" {
		return input{}, fmt.Errorf("operation is required")
	}

	if in.Input == "" {
		return input{}, fmt.Errorf("input is required")
	}

	if in.Indent <= 0 {
		in.Indent = 2
	}

	return in, nil
}

func extractOperation(data any, path string) map[string]any {
	if path == "" || path == "." {
		return map[string]any{
			"operation": "extract",
			"result":    data,
		}
	}

	// Simple path extraction (supports .key and .key.subkey)
	extracted := extractPath(data, path)

	return map[string]any{
		"operation": "extract",
		"path":      path,
		"result":    extracted,
		"found":     extracted != nil,
	}
}

func extractPath(data any, path string) any {
	parts := strings.Split(strings.TrimPrefix(path, "."), ".")
	current := data

	for _, part := range parts {
		if part == "" {
			continue
		}

		// Handle array index [n]
		if strings.HasSuffix(part, "]") && strings.Contains(part, "[") {
			keyPart := part[:strings.Index(part, "[")]
			indexPart := part[strings.Index(part, "[")+1 : len(part)-1]

			// Get the key first
			if keyPart != "" {
				m, ok := current.(map[string]any)
				if !ok {
					return nil
				}
				current = m[keyPart]
			}

			// Then handle array index
			arr, ok := current.([]any)
			if !ok {
				return nil
			}

			var idx int
			if _, err := fmt.Sscanf(indexPart, "%d", &idx); err != nil {
				return nil
			}

			if idx < 0 || idx >= len(arr) {
				return nil
			}

			current = arr[idx]
			continue
		}

		// Handle regular key
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}

		val, exists := m[part]
		if !exists {
			return nil
		}

		current = val
	}

	return current
}

func mergeOperation(data any, mergeWithJSON string) map[string]any {
	if mergeWithJSON == "" {
		return map[string]any{
			"operation": "merge",
			"error":     "merge_with is required",
		}
	}

	var mergeData any
	if err := json.Unmarshal([]byte(mergeWithJSON), &mergeData); err != nil {
		return map[string]any{
			"operation": "merge",
			"error":     fmt.Sprintf("invalid merge_with JSON: %v", err),
		}
	}

	merged := deepMerge(data, mergeData)

	return map[string]any{
		"operation": "merge",
		"result":    merged,
	}
}

func deepMerge(dst, src any) any {
	dstMap, dstIsMap := dst.(map[string]any)
	srcMap, srcIsMap := src.(map[string]any)

	if dstIsMap && srcIsMap {
		result := make(map[string]any)
		// Copy dst
		for k, v := range dstMap {
			result[k] = v
		}
		// Merge src
		for k, v := range srcMap {
			if existingVal, exists := result[k]; exists {
				result[k] = deepMerge(existingVal, v)
			} else {
				result[k] = v
			}
		}
		return result
	}

	// If not both maps, src overwrites dst
	return src
}

func validateOperation(data any) map[string]any {
	result := map[string]any{
		"operation": "validate",
		"valid":     true,
	}

	// Analyze structure
	dataType := getJSONType(data)
	result["type"] = dataType

	switch v := data.(type) {
	case map[string]any:
		result["key_count"] = len(v)
		result["keys"] = getKeys(v)
	case []any:
		result["array_length"] = len(v)
		if len(v) > 0 {
			result["first_element_type"] = getJSONType(v[0])
		}
	case string:
		result["string_length"] = len(v)
	case float64:
		result["is_integer"] = v == float64(int64(v))
	}

	// Check depth
	depth := calculateDepth(data)
	result["max_depth"] = depth

	return result
}

func formatOperation(data any, indent int, compact bool) map[string]any {
	var formatted string
	var err error

	if compact {
		buf := &bytes.Buffer{}
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		if err = enc.Encode(data); err == nil {
			formatted = strings.TrimSpace(buf.String())
		}
	} else {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", strings.Repeat(" ", indent))
		if err = enc.Encode(data); err == nil {
			formatted = strings.TrimSpace(buf.String())
		}
	}

	result := map[string]any{
		"operation": "format",
		"compact":   compact,
		"indent":    indent,
	}

	if err != nil {
		result["error"] = err.Error()
	} else {
		result["formatted"] = formatted
		result["size"] = len(formatted)
	}

	return result
}

func keysOperation(data any) map[string]any {
	result := map[string]any{
		"operation": "keys",
	}

	m, ok := data.(map[string]any)
	if !ok {
		result["error"] = "input is not a JSON object"
		return result
	}

	keys := getKeys(m)
	result["keys"] = keys
	result["count"] = len(keys)

	// Collect all nested keys
	allKeys := collectAllKeys(data, "")
	result["all_keys"] = allKeys
	result["total_key_count"] = len(allKeys)

	return result
}

func getKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func collectAllKeys(data any, prefix string) []string {
	var keys []string

	switch v := data.(type) {
	case map[string]any:
		for k, val := range v {
			fullKey := k
			if prefix != "" {
				fullKey = prefix + "." + k
			}
			keys = append(keys, fullKey)
			keys = append(keys, collectAllKeys(val, fullKey)...)
		}
	case []any:
		for i, val := range v {
			fullKey := fmt.Sprintf("%s[%d]", prefix, i)
			keys = append(keys, collectAllKeys(val, fullKey)...)
		}
	}

	return keys
}

func getJSONType(data any) string {
	if data == nil {
		return "null"
	}

	switch data.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	default:
		return reflect.TypeOf(data).String()
	}
}

func calculateDepth(data any) int {
	switch v := data.(type) {
	case map[string]any:
		maxDepth := 0
		for _, val := range v {
			depth := calculateDepth(val)
			if depth > maxDepth {
				maxDepth = depth
			}
		}
		return maxDepth + 1
	case []any:
		maxDepth := 0
		for _, val := range v {
			depth := calculateDepth(val)
			if depth > maxDepth {
				maxDepth = depth
			}
		}
		return maxDepth + 1
	default:
		return 1
	}
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit json/transform failure")
	os.Exit(1)
}
