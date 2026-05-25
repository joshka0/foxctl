// Package artifacts extracts CAS digests from envelopes.
package artifacts

import (
	"encoding/json"
	"strings"
)

// Digests parses an envelope JSON document and returns any sha256 digests
// referenced via data.artifact or data.artifacts[].
func Digests(result []byte) []string {
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(result, &env); err != nil {
		return nil
	}
	var out []string
	if env.Data == nil {
		return nil
	}
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if !isDigest(value) {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if s, ok := env.Data["artifact"].(string); ok {
		add(s)
	}
	if arr, ok := env.Data["artifacts"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				add(s)
			}
		}
	}
	return out
}

func isDigest(s string) bool {
	return strings.HasPrefix(s, "sha256:")
}
