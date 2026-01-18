// Package main implements the json/transform skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const command = "json/transform"

var allowedOps = []string{"extract", "merge", "validate", "format", "keys"}

type input struct {
	Operation string `json:"operation"`
	Input     string `json:"input"`
	Path      string `json:"path"`
	MergeWith string `json:"merge_with"`
	Indent    int    `json:"indent"`
	Compact   bool   `json:"compact"`
}

func main() {
	skillmain.Main(command, run)
}

func run(_ context.Context, rc *skillmain.RunContext, in input) error {
	op := oputil.Op(in.Operation)
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if op == "" {
		return skillerr.Arg("operation is required", skillerr.WithHint(opHint))
	}
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}
	if err := oputil.Require(in.Input, "input"); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint("Provide JSON input as a string."))
	}

	// Apply defaults
	if in.Indent <= 0 {
		in.Indent = 2
	}

	// Parse input JSON
	var data any
	if err := json.Unmarshal([]byte(in.Input), &data); err != nil {
		return fmt.Errorf("invalid JSON input: %w", err)
	}

	// Dispatch operation
	result, err := oputil.NewSwitch(op).
		Case("extract", func() (map[string]any, error) { return extractOperation(data, in.Path), nil }).
		Case("merge", func() (map[string]any, error) { return mergeOperation(data, in.MergeWith), nil }).
		Case("validate", func() (map[string]any, error) { return validateOperation(data), nil }).
		Case("format", func() (map[string]any, error) { return formatOperation(data, in.Indent, in.Compact), nil }).
		Case("keys", func() (map[string]any, error) { return keysOperation(data), nil }).
		Run()
	if err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}

	return skillout.Emit(rc, command, result)
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
