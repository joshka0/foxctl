package embeddingtext

import (
	"regexp"
	"strings"
)

var (
	goFieldPattern      = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*(?:\s*,\s*[A-Za-z_][A-Za-z0-9_]*)*)\s+[*\[\]A-Za-z_][A-Za-z0-9_\.\[\]\*]*`)
	tsMemberPattern     = regexp.MustCompile(`^\s*(?:public|private|protected|readonly|static|abstract|declare|override|async|\s)*([A-Za-z_$][A-Za-z0-9_$]*)\??\s*(?:[(:=]|$)`)
	pythonMemberPattern = regexp.MustCompile(`^\s*self\.([A-Za-z_][A-Za-z0-9_]*)\s*=`)
	elixirKeyPattern    = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*):`)
)

func memberKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "class", "struct", "interface", "type":
		return true
	default:
		return false
	}
}

func goMemberHints(code, kind string) []string {
	if !memberKind(kind) {
		return nil
	}
	return extractLineMatches(code, goFieldPattern, func(match []string) []string {
		return strings.Split(match[1], ",")
	})
}

func tsMemberHints(code, kind string) []string {
	if !memberKind(kind) {
		return nil
	}
	return extractLineMatches(code, tsMemberPattern, func(match []string) []string {
		switch match[1] {
		case "constructor", "if", "for", "while", "switch", "return":
			return nil
		default:
			return []string{match[1]}
		}
	})
}

func pythonMemberHints(code, kind string) []string {
	if !memberKind(kind) {
		return nil
	}
	return extractLineMatches(code, pythonMemberPattern, func(match []string) []string {
		return []string{match[1]}
	})
}

func elixirMemberHints(code, kind string) []string {
	if !memberKind(kind) {
		return nil
	}
	var out []string
	for _, match := range elixirKeyPattern.FindAllStringSubmatch(code, -1) {
		out = append(out, match[1])
	}
	return out
}

func extractLineMatches(code string, pattern *regexp.Regexp, values func([]string) []string) []string {
	var out []string
	for _, line := range strings.Split(code, "\n") {
		match := pattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		for _, value := range values(match) {
			value = strings.TrimSpace(value)
			if value != "" && !skipMemberHint(value) {
				out = append(out, value)
			}
		}
	}
	return out
}

func skipMemberHint(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "case", "class", "const", "def", "default", "do", "else", "end", "export", "for", "func", "function", "if", "interface", "return", "self", "struct", "switch", "type", "var", "while":
		return true
	default:
		return false
	}
}
