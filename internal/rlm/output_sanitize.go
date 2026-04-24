package rlm

import (
	"regexp"
	"strings"
)

var solutionLinePattern = regexp.MustCompile(`(?im)^\s*solution\s*=\s*.+\s*$`)

// OutputSanitization describes model-provider markup stripped from a final answer.
type OutputSanitization struct {
	Changed   bool     `json:"changed"`
	Artifacts []string `json:"artifacts,omitempty"`
	RawText   string   `json:"raw_text,omitempty"`
}

// SanitizeOutputText removes local-model channel/tool markers from visible answers.
func SanitizeOutputText(response string) (string, OutputSanitization) {
	raw := strings.TrimSpace(response)
	if raw == "" {
		return "", OutputSanitization{}
	}

	artifacts := DetectOutputArtifacts(raw)
	if len(artifacts) == 0 {
		return raw, OutputSanitization{}
	}

	cleaned := strings.ReplaceAll(raw, "\r\n", "\n")
	cleaned = strings.ReplaceAll(cleaned, "\r", "\n")
	cleaned = stripDelimitedBlocks(cleaned, "<|tool_call>", "<tool_call|>")
	if idx := strings.LastIndex(cleaned, "<channel|>"); idx >= 0 {
		cleaned = cleaned[idx+len("<channel|>"):]
	}
	for _, marker := range []string{"<|channel>thought", "<|channel>}", "<channel|>", "<|tool_call>", "<tool_call|>"} {
		cleaned = strings.ReplaceAll(cleaned, marker, "")
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" && !artifactsContain(artifacts, "tool_call_markup_open") && !artifactsContain(artifacts, "tool_call_markup_close") {
		cleaned = raw
	}

	return cleaned, OutputSanitization{
		Changed:   cleaned != raw,
		Artifacts: artifacts,
		RawText:   raw,
	}
}

// DetectOutputArtifacts reports known non-answer markers in local model output.
func DetectOutputArtifacts(response string) []string {
	checks := []struct {
		marker string
		label  string
	}{
		{marker: "<|channel>thought", label: "reasoning_channel_open_thought"},
		{marker: "<|channel>}", label: "reasoning_channel_open_malformed"},
		{marker: "<channel|>", label: "reasoning_channel_close"},
		{marker: "<|tool_call>", label: "tool_call_markup_open"},
		{marker: "<tool_call|>", label: "tool_call_markup_close"},
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(checks))
	for _, check := range checks {
		if !strings.Contains(response, check.marker) {
			continue
		}
		if _, ok := seen[check.label]; ok {
			continue
		}
		seen[check.label] = struct{}{}
		out = append(out, check.label)
	}
	return out
}

// ExtractSolutionLine returns the last visible solution = ... line.
func ExtractSolutionLine(response string) (string, bool) {
	matches := solutionLinePattern.FindAllString(response, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		line := strings.TrimSpace(matches[i])
		if line != "" {
			return line, true
		}
	}
	return "", false
}

func stripDelimitedBlocks(text, open, close string) string {
	for {
		start := strings.Index(text, open)
		if start < 0 {
			return text
		}
		end := strings.Index(text[start+len(open):], close)
		if end < 0 {
			return strings.TrimSpace(text[:start])
		}
		end = start + len(open) + end + len(close)
		text = text[:start] + text[end:]
	}
}

func artifactsContain(artifacts []string, want string) bool {
	for _, artifact := range artifacts {
		if artifact == want {
			return true
		}
	}
	return false
}
