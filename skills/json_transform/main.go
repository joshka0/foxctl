// Package main implements the json/transform skill for comprehensive JSON data manipulation and analysis.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const command = "json/transform"

var allowedOps = []string{"extract", "merge", "validate", "format", "keys"}

// input defines the skill input parameters for JSON transformation operations with multiple operation types.
type input struct {
	Operation string `json:"operation"`
	Input     string `json:"input"`
	Path      string `json:"path"`
	MergeWith string `json:"merge_with"`
	Indent    int    `json:"indent"`
	Compact   bool   `json:"compact"`
}

// main is the skill entry point for json/transform with comprehensive JSON manipulation capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates JSON transformation operations including extract, merge, validate, format, and keys.
//
// Index:
//   Purpose: Transform and analyze JSON data with multiple operations: path extraction, deep merging, validation, formatting, and key enumeration
//   Keywords: json/transform, json_manipulation, path_extraction, deep_merge, json_validation, formatting
//   Related: extractOperation, mergeOperation, validateOperation, formatOperation, keysOperation
//   Flow: validate input → parse JSON → dispatch operation → execute specific transformation → return structured result
//   Resources: JSON parser; bytes buffer
//   Events: json-transformed
//   OutputFields: operation, result, formatted, keys, error
//
// [[domain:json-manipulation]]
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

// extractOperation extracts values from JSON data using dot notation paths with array index support.
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

// extractPath navigates through JSON structure using dot notation and array indices with proper error handling.
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

// mergeOperation performs deep merging of JSON objects with recursive conflict resolution and validation.
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

// deepMerge recursively merges two JSON values with maps taking precedence over primitive types and nested handling.
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

// validateOperation analyzes JSON structure and provides detailed validation information with type detection.
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

// formatOperation formats JSON data with configurable indentation and compact/pretty modes with size tracking.
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

// keysOperation extracts and enumerates all keys from JSON objects including nested structures with full path tracking.
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

// getKeys returns a sorted list of keys from a JSON object map with deterministic ordering.
func getKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// collectAllKeys recursively collects all keys from JSON structures with dot notation and array index formatting.
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

// getJSONType returns the JSON type name for a given Go value with proper type mapping.
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

// calculateDepth computes the maximum nesting depth of a JSON structure with recursive traversal.
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
