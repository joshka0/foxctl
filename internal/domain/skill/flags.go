package skill

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

// FlagSet wraps pflag.FlagSet with skill parameter metadata.
type FlagSet struct {
	*pflag.FlagSet
	params     []Parameter
	stringVals map[string]*string
	boolVals   map[string]*bool
	intVals    map[string]*int
	floatVals  map[string]*float64
	sliceVals  map[string]*[]string
}

// NewFlagSet creates a FlagSet from skill parameters.
// The flagSetName is used for help/usage messages.
func NewFlagSet(flagSetName string, params []Parameter) *FlagSet {
	fs := &FlagSet{
		FlagSet:    pflag.NewFlagSet(flagSetName, pflag.ContinueOnError),
		params:     params,
		stringVals: make(map[string]*string),
		boolVals:   make(map[string]*bool),
		intVals:    make(map[string]*int),
		floatVals:  make(map[string]*float64),
		sliceVals:  make(map[string]*[]string),
	}

	for _, p := range params {
		fs.addParameter(p)
	}

	return fs
}

// addParameter adds a flag for the given parameter.
func (fs *FlagSet) addParameter(p Parameter) {
	flagName := toFlagName(p.Name)
	usage := buildUsage(p)

	switch p.Type {
	case "string":
		var def string
		if p.Default != nil {
			def = fmt.Sprintf("%v", p.Default)
		}
		val := new(string)
		*val = def
		fs.stringVals[p.Name] = val
		fs.StringVar(val, flagName, def, usage)

	case "boolean", "bool":
		var def bool
		if p.Default != nil {
			switch v := p.Default.(type) {
			case bool:
				def = v
			case string:
				def, _ = strconv.ParseBool(v)
			}
		}
		val := new(bool)
		*val = def
		fs.boolVals[p.Name] = val
		fs.BoolVar(val, flagName, def, usage)

	case "integer", "int":
		var def int
		if p.Default != nil {
			switch v := p.Default.(type) {
			case int:
				def = v
			case int64:
				def = int(v)
			case float64:
				def = int(v)
			case string:
				def, _ = strconv.Atoi(v)
			}
		}
		val := new(int)
		*val = def
		fs.intVals[p.Name] = val
		fs.IntVar(val, flagName, def, usage)

	case "number", "float":
		var def float64
		if p.Default != nil {
			switch v := p.Default.(type) {
			case float64:
				def = v
			case float32:
				def = float64(v)
			case int:
				def = float64(v)
			case string:
				def, _ = strconv.ParseFloat(v, 64)
			}
		}
		val := new(float64)
		*val = def
		fs.floatVals[p.Name] = val
		fs.Float64Var(val, flagName, def, usage)

	case "array":
		// Arrays can be passed as comma-separated or repeated flags
		val := new([]string)
		fs.sliceVals[p.Name] = val
		fs.StringSliceVar(val, flagName, nil, usage+" (comma-separated or repeat flag)")

	case "object":
		// Objects are passed as JSON strings
		val := new(string)
		fs.stringVals[p.Name] = val
		fs.StringVar(val, flagName, "", usage+" (JSON)")

	default:
		// Fallback to string for unknown types
		val := new(string)
		fs.stringVals[p.Name] = val
		fs.StringVar(val, flagName, "", usage)
	}
}

// ToJSON converts the parsed flags to a JSON map.
// Only includes flags that were explicitly set or have defaults.
func (fs *FlagSet) ToJSON() map[string]any {
	result := make(map[string]any)

	for _, p := range fs.params {
		flagName := toFlagName(p.Name)
		flag := fs.Lookup(flagName)
		if flag == nil {
			continue
		}

		// Check if flag was changed (explicitly set)
		changed := flag.Changed

		switch p.Type {
		case "string":
			if val, ok := fs.stringVals[p.Name]; ok {
				if changed || p.Default != nil {
					result[p.Name] = *val
				}
			}

		case "boolean", "bool":
			if val, ok := fs.boolVals[p.Name]; ok {
				if changed || p.Default != nil {
					result[p.Name] = *val
				}
			}

		case "integer", "int":
			if val, ok := fs.intVals[p.Name]; ok {
				if changed || p.Default != nil {
					result[p.Name] = *val
				}
			}

		case "number", "float":
			if val, ok := fs.floatVals[p.Name]; ok {
				if changed || p.Default != nil {
					result[p.Name] = *val
				}
			}

		case "array":
			if val, ok := fs.sliceVals[p.Name]; ok && len(*val) > 0 {
				result[p.Name] = *val
			}

		case "object":
			if val, ok := fs.stringVals[p.Name]; ok && *val != "" {
				// Parse JSON string to object
				var obj any
				if err := json.Unmarshal([]byte(*val), &obj); err == nil {
					result[p.Name] = obj
				} else {
					// If not valid JSON, treat as string
					result[p.Name] = *val
				}
			}

		default:
			if val, ok := fs.stringVals[p.Name]; ok && *val != "" {
				result[p.Name] = *val
			}
		}
	}

	return result
}

// MergeWithInput merges flag values with input JSON.
// Flags take precedence over input JSON values.
func (fs *FlagSet) MergeWithInput(inputJSON []byte) ([]byte, error) {
	// Start with defaults from parameters
	base := make(map[string]any)

	// Add defaults from manifest
	for _, p := range fs.params {
		if p.Default != nil {
			base[p.Name] = p.Default
		}
	}

	// Layer input JSON on top
	if len(inputJSON) > 0 && string(inputJSON) != "{}" {
		var input map[string]any
		if err := json.Unmarshal(inputJSON, &input); err != nil {
			return nil, fmt.Errorf("parse input JSON: %w", err)
		}
		for k, v := range input {
			base[k] = v
		}
	}

	// Layer explicit flags on top (only if changed)
	flagVals := fs.getChangedValues()
	for k, v := range flagVals {
		base[k] = v
	}

	return json.Marshal(base)
}

// getChangedValues returns only flags that were explicitly set.
func (fs *FlagSet) getChangedValues() map[string]any {
	result := make(map[string]any)

	for _, p := range fs.params {
		flagName := toFlagName(p.Name)
		flag := fs.Lookup(flagName)
		if flag == nil || !flag.Changed {
			continue
		}

		switch p.Type {
		case "string":
			if val, ok := fs.stringVals[p.Name]; ok {
				result[p.Name] = *val
			}
		case "boolean", "bool":
			if val, ok := fs.boolVals[p.Name]; ok {
				result[p.Name] = *val
			}
		case "integer", "int":
			if val, ok := fs.intVals[p.Name]; ok {
				result[p.Name] = *val
			}
		case "number", "float":
			if val, ok := fs.floatVals[p.Name]; ok {
				result[p.Name] = *val
			}
		case "array":
			if val, ok := fs.sliceVals[p.Name]; ok {
				result[p.Name] = *val
			}
		case "object":
			if val, ok := fs.stringVals[p.Name]; ok && *val != "" {
				var obj any
				if err := json.Unmarshal([]byte(*val), &obj); err == nil {
					result[p.Name] = obj
				} else {
					result[p.Name] = *val
				}
			}
		default:
			if val, ok := fs.stringVals[p.Name]; ok {
				result[p.Name] = *val
			}
		}
	}

	return result
}

// Validate checks that required parameters are present and enum values are valid.
func (fs *FlagSet) Validate(merged map[string]any) error {
	var errs []string

	for _, p := range fs.params {
		val, exists := merged[p.Name]

		// Check required
		if p.Required && !exists {
			errs = append(errs, fmt.Sprintf("required parameter %q is missing", p.Name))
			continue
		}

		// Check enum
		if exists && len(p.Enum) > 0 {
			strVal := fmt.Sprintf("%v", val)
			valid := false
			for _, e := range p.Enum {
				if strVal == e {
					valid = true
					break
				}
			}
			if !valid {
				errs = append(errs, fmt.Sprintf("parameter %q must be one of: %s (got %q)",
					p.Name, strings.Join(p.Enum, ", "), strVal))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation errors:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// toFlagName converts a parameter name to a CLI flag name.
// e.g., "analysis_mode" -> "analysis-mode"
func toFlagName(paramName string) string {
	return strings.ReplaceAll(paramName, "_", "-")
}

// buildUsage creates a usage string for the parameter.
func buildUsage(p Parameter) string {
	var parts []string
	parts = append(parts, p.Description)

	if p.Required {
		parts = append(parts, "(required)")
	}

	if len(p.Enum) > 0 {
		parts = append(parts, fmt.Sprintf("[%s]", strings.Join(p.Enum, "|")))
	}

	if p.Default != nil && !p.Required {
		parts = append(parts, fmt.Sprintf("(default: %v)", p.Default))
	}

	return strings.Join(parts, " ")
}

// GenerateParameterHelp returns formatted help text for parameters.
func GenerateParameterHelp(params []Parameter) string {
	if len(params) == 0 {
		return "  (no parameters)\n"
	}

	var sb strings.Builder
	for _, p := range params {
		flagName := toFlagName(p.Name)

		// Flag name and type
		typeStr := p.Type
		if typeStr == "boolean" {
			typeStr = "bool"
		}
		sb.WriteString(fmt.Sprintf("  --%s <%s>\n", flagName, typeStr))

		// Description
		if p.Description != "" {
			sb.WriteString(fmt.Sprintf("        %s\n", p.Description))
		}

		// Required indicator
		if p.Required {
			sb.WriteString("        (required)\n")
		}

		// Enum values
		if len(p.Enum) > 0 {
			sb.WriteString(fmt.Sprintf("        allowed: %s\n", strings.Join(p.Enum, ", ")))
		}

		// Default value
		if p.Default != nil {
			sb.WriteString(fmt.Sprintf("        default: %v\n", p.Default))
		}

		sb.WriteString("\n")
	}

	return sb.String()
}
