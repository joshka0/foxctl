// Package artifacts extracts CAS digests from envelopes.
package artifacts

import "encoding/json"

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
	if s, ok := env.Data["artifact"].(string); ok && isDigest(s) {
		out = append(out, s)
	}
	if arr, ok := env.Data["artifacts"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok && isDigest(s) {
				out = append(out, s)
			}
		}
	}
	return out
}

func isDigest(s string) bool {
	return len(s) > 7 && s[:7] == "sha256:"
}
