package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

// ---------------------------------------------------------------------------
// Transform function type and registry
// ---------------------------------------------------------------------------

// TransformFunc is a pure function that transforms input data.
// The config string is transform-specific JSON.
// All transforms return Go errors; the caller wraps them in error envelopes.
type TransformFunc func(ctx context.Context, input any, config string) (any, error)

// transformRegistry maps TransformKind values to their implementations.
var transformRegistry = map[TransformKind]TransformFunc{
	TransformPassthrough: passthroughTransform,
	TransformRegex:       regexExtractTransform,
	TransformTemplate:    templateTransform,
	TransformJQ:          jqFilterTransform,
	TransformSplitLines:  splitLinesTransform,
	TransformMapFields:   mapFieldsTransform,
}

// GetTransform returns the TransformFunc for the given kind, or an error if
// the kind is not registered.
func GetTransform(kind TransformKind) (TransformFunc, error) {
	fn, ok := transformRegistry[kind]
	if !ok {
		return nil, fmt.Errorf("transform: unknown kind %q", kind)
	}
	return fn, nil
}

// ApplyTransform looks up the transform by kind and applies it to the input.
// This is the primary entry point for the evaluator.
func ApplyTransform(ctx context.Context, kind TransformKind, config string, input any) (any, error) {
	fn, err := GetTransform(kind)
	if err != nil {
		return nil, err
	}
	return fn(ctx, input, config)
}

// ---------------------------------------------------------------------------
// Passthrough (identity)
// ---------------------------------------------------------------------------

func passthroughTransform(_ context.Context, input any, _ string) (any, error) {
	return input, nil
}

// ---------------------------------------------------------------------------
// RegexExtract
// ---------------------------------------------------------------------------

// regexExtractConfig is the typed config for the regex_extract transform.
type regexExtractConfig struct {
	Pattern string `json:"pattern"`
	Group   any    `json:"group"` // int or string
}

func regexExtractTransform(_ context.Context, input any, configStr string) (any, error) {
	// Validate input type.
	s, ok := input.(string)
	if !ok {
		return nil, fmt.Errorf("transform regex_extract: input must be string, got %T", input)
	}

	// Parse config.
	var cfg regexExtractConfig
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		return nil, fmt.Errorf("transform regex_extract: invalid config: %w", err)
	}
	if cfg.Pattern == "" {
		return nil, fmt.Errorf("transform regex_extract: pattern is required")
	}

	// Compile regex.
	re, err := regexp.Compile(cfg.Pattern)
	if err != nil {
		return nil, fmt.Errorf("transform regex_extract: invalid pattern: %w", err)
	}

	// Execute match.
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return nil, fmt.Errorf("transform regex_extract: no match for pattern %q in %q", cfg.Pattern, s)
	}

	// Resolve group (default 0).
	switch g := cfg.Group.(type) {
	case nil:
		// Default to full match (group 0).
		return matches[0], nil
	case float64:
		idx := int(g)
		if idx < 0 || idx >= len(matches) {
			return nil, fmt.Errorf("transform regex_extract: group %d out of range (0..%d)", idx, len(matches)-1)
		}
		return matches[idx], nil
	case string:
		// Named group: look up in subexp names.
		names := re.SubexpNames()
		for i, name := range names {
			if name == g {
				return matches[i], nil
			}
		}
		return nil, fmt.Errorf("transform regex_extract: named group %q not found", g)
	default:
		return nil, fmt.Errorf("transform regex_extract: group must be int or string, got %T", g)
	}
}

// ---------------------------------------------------------------------------
// Template
// ---------------------------------------------------------------------------

// templateConfig is the typed config for the template transform.
type templateConfig struct {
	Template string `json:"template"`
}

func templateTransform(_ context.Context, input any, configStr string) (any, error) {
	// Parse config.
	var cfg templateConfig
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		return nil, fmt.Errorf("transform template: invalid config: %w", err)
	}
	if cfg.Template == "" {
		return nil, fmt.Errorf("transform template: template is required")
	}

	// Parse and execute template.
	// Use Option "missingkey=error" so missing fields produce errors.
	tmpl, err := template.New("transform").Option("missingkey=error").Parse(cfg.Template)
	if err != nil {
		return nil, fmt.Errorf("transform template: invalid template syntax: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, input); err != nil {
		return nil, fmt.Errorf("transform template: execution error: %w", err)
	}

	return buf.String(), nil
}

// ---------------------------------------------------------------------------
// JQFilter (basic JSON path navigation)
// ---------------------------------------------------------------------------

// jqFilterConfig is the typed config for the jq_filter transform.
type jqFilterConfig struct {
	Filter string `json:"filter"`
}

// jqFilterTransform implements basic jq-like path navigation.
// Supported expressions:
//   - .field          → navigate to field in object
//   - .[]             → iterate array elements
//   - .field1.field2  → nested navigation
//   - .[].field       → iterate array then extract field
//   - .a.b.c          → deeply nested navigation
//
// No external jq library is used; this is a pure Go implementation
// using encoding/json.
func jqFilterTransform(_ context.Context, input any, configStr string) (any, error) {
	// Parse config.
	var cfg jqFilterConfig
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		return nil, fmt.Errorf("transform jq_filter: invalid config: %w", err)
	}
	if cfg.Filter == "" {
		return nil, fmt.Errorf("transform jq_filter: filter is required")
	}

	filter := cfg.Filter
	if !strings.HasPrefix(filter, ".") {
		return nil, fmt.Errorf("transform jq_filter: filter must start with '.', got %q", filter)
	}

	// Parse the filter into path segments.
	segments, err := parseJQPath(filter)
	if err != nil {
		return nil, fmt.Errorf("transform jq_filter: %w", err)
	}

	// Navigate the path.
	result, err := navigateJQPath(input, segments)
	if err != nil {
		return nil, fmt.Errorf("transform jq_filter: %w", err)
	}

	return result, nil
}

// jqSegment represents one step in a jq path.
type jqSegment struct {
	field string // field name (empty for array iterator)
	iter  bool   // true if this is an array iterator "[]"
}

// parseJQPath parses a jq-like filter expression into segments.
// Examples:
//
//	".name"          → [{field:"name"}]
//	".items[]"       → [{field:"items"}, {iter:true}]
//	".a.b.c"         → [{field:"a"}, {field:"b"}, {field:"c"}]
//	".[].file"       → [{iter:true}, {field:"file"}]
//	".items[].name"  → [{field:"items"}, {iter:true}, {field:"name"}]
func parseJQPath(filter string) ([]jqSegment, error) {
	if filter == "." {
		return nil, nil // identity — return input as-is (shouldn't normally happen)
	}

	// Strip leading dot.
	path := filter[1:]
	if path == "" {
		return nil, nil
	}

	var segments []jqSegment
	parts := strings.Split(path, ".")

	for i, part := range parts {
		if part == "" {
			// ".." is not supported.
			if i > 0 && parts[i-1] == "" {
				return nil, fmt.Errorf("unsupported path expression %q (double dot)", filter)
			}
			continue
		}

		if strings.HasSuffix(part, "[]") {
			// "items[]" → field "items" then iterator.
			fieldName := strings.TrimSuffix(part, "[]")
			if fieldName != "" {
				segments = append(segments, jqSegment{field: fieldName})
			}
			segments = append(segments, jqSegment{iter: true})
		} else if part == "[]" {
			segments = append(segments, jqSegment{iter: true})
		} else {
			segments = append(segments, jqSegment{field: part})
		}
	}

	if len(segments) == 0 {
		return nil, fmt.Errorf("empty path in filter %q", filter)
	}

	return segments, nil
}

// navigateJQPath walks the input data according to the path segments.
func navigateJQPath(current any, segments []jqSegment) (any, error) {
	for _, seg := range segments {
		if seg.iter {
			// Array iteration: current must be a slice.
			arr, ok := toSlice(current)
			if !ok {
				return nil, fmt.Errorf("cannot iterate non-array at segment %q", formatSegment(seg))
			}
			// If there are more segments after this, we need to collect results.
			// For now, return the array itself.
			// If there are further segments, we map over each element.
			current = arr
		} else {
			// Field navigation: current must be a map.
			if current == nil {
				return nil, fmt.Errorf("cannot navigate field %q on nil", seg.field)
			}

			// Check if current is a slice and we need to map over it.
			if arr, ok := toSlice(current); ok {
				// Map over each element and navigate the field.
				results := make([]any, 0, len(arr))
				for _, item := range arr {
					m, ok := toMap(item)
					if !ok {
						return nil, fmt.Errorf("cannot navigate field %q on array element of type %T", seg.field, item)
					}
					val, exists := m[seg.field]
					if !exists {
						return nil, fmt.Errorf("field %q not found in array element", seg.field)
					}
					results = append(results, val)
				}
				current = results
			} else {
				m, ok := toMap(current)
				if !ok {
					return nil, fmt.Errorf("cannot navigate field %q on %T", seg.field, current)
				}
				val, exists := m[seg.field]
				if !exists {
					return nil, fmt.Errorf("field %q not found", seg.field)
				}
				current = val
			}
		}
	}
	return current, nil
}

// formatSegment returns a human-readable representation of a jqSegment.
func formatSegment(seg jqSegment) string {
	if seg.iter {
		return "[]"
	}
	return "." + seg.field
}

// ---------------------------------------------------------------------------
// SplitLines
// ---------------------------------------------------------------------------

// splitLinesConfig is the typed config for the split_lines transform.
type splitLinesConfig struct {
	Delimiter string `json:"delimiter"`
}

func splitLinesTransform(_ context.Context, input any, configStr string) (any, error) {
	// Validate input type.
	s, ok := input.(string)
	if !ok {
		return nil, fmt.Errorf("transform split_lines: input must be string, got %T", input)
	}

	// Parse config.
	var cfg splitLinesConfig
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		return nil, fmt.Errorf("transform split_lines: invalid config: %w", err)
	}

	// Default delimiter is newline.
	delimiter := cfg.Delimiter
	if delimiter == "" {
		delimiter = "\n"
	}

	// Empty string returns empty array.
	if s == "" {
		return []string{}, nil
	}

	result := strings.Split(s, delimiter)
	return result, nil
}

// ---------------------------------------------------------------------------
// MapFields
// ---------------------------------------------------------------------------

// mapFieldsConfig is the typed config for the map_fields transform.
type mapFieldsConfig struct {
	Mapping map[string]string `json:"mapping"`
}

func mapFieldsTransform(_ context.Context, input any, configStr string) (any, error) {
	// Validate input type — must be a map (object).
	m, ok := toMap(input)
	if !ok {
		return nil, fmt.Errorf("transform map_fields: input must be object, got %T", input)
	}

	// Parse config.
	var cfg mapFieldsConfig
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		return nil, fmt.Errorf("transform map_fields: invalid config: %w", err)
	}
	if cfg.Mapping == nil {
		return nil, fmt.Errorf("transform map_fields: mapping is required")
	}

	// Apply renames using a two-pass approach to handle overwrites correctly.
	// Pass 1: copy keys that are NOT in the mapping.
	// Pass 2: apply the renames (which may overwrite existing keys).
	result := make(map[string]any, len(m))
	for key, val := range m {
		if _, mapped := cfg.Mapping[key]; !mapped {
			result[key] = val
		}
	}
	for oldName, newName := range cfg.Mapping {
		if val, exists := m[oldName]; exists {
			result[newName] = val
		}
	}

	return result, nil
}
