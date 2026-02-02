package skilltest

import (
	"encoding/json"
	"io"
)

// ParseInput decodes JSON from r into the provided input struct.
// This is a thin wrapper around json.Decoder for consistency across skill tests.
// After parsing, call your skill's applyDefaults function to set default values.
func ParseInput[I any](r io.Reader) (I, error) {
	var in I
	if err := json.NewDecoder(r).Decode(&in); err != nil && err != io.EOF {
		return in, err
	}
	return in, nil
}

// ParseInputWithDefaults decodes JSON and applies defaults via the provided function.
// The defaults function receives a pointer to the input struct to modify.
func ParseInputWithDefaults[I any](r io.Reader, defaults func(*I)) (I, error) {
	in, err := ParseInput[I](r)
	if err != nil {
		return in, err
	}
	if defaults != nil {
		defaults(&in)
	}
	return in, nil
}
