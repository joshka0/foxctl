package frontmatter

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// ErrInvalidFrontMatter indicates malformed YAML frontmatter framing.
	ErrInvalidFrontMatter = errors.New("workflow frontmatter: invalid frontmatter")
	// ErrFrontMatterNotMap indicates decoded YAML root was not an object/map.
	ErrFrontMatterNotMap = errors.New("workflow frontmatter: frontmatter must decode to a map")
)

const delimiter = "---"

// Parse parses WORKFLOW.md bytes into frontmatter config + prompt body.
//
// Rules:
//  1. If content starts with "---" on the first line, parse frontmatter until the next "---" line.
//  2. Frontmatter YAML must decode to map/object.
//  3. Prompt template is the remaining markdown body, trimmed.
//  4. If no frontmatter fence exists, entire file is treated as prompt body.
func Parse(data []byte) (Document, error) {
	text := normalizeNewlines(string(stripUTF8BOM(data)))
	lines := strings.Split(text, "\n")

	// No frontmatter: whole content is prompt body.
	if len(lines) == 0 || !isFenceLine(lines[0]) {
		return Document{
			Config:         map[string]any{},
			PromptTemplate: strings.TrimSpace(text),
			HasFrontMatter: false,
		}, nil
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if isFenceLine(lines[i]) {
			end = i
			break
		}
	}
	if end < 0 {
		return Document{}, fmt.Errorf("%w: missing closing '---' fence", ErrInvalidFrontMatter)
	}

	fmText := strings.Join(lines[1:end], "\n")
	cfgMap := map[string]any{}
	if strings.TrimSpace(fmText) != "" {
		var fm any
		if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
			return Document{}, fmt.Errorf("%w: %v", ErrInvalidFrontMatter, err)
		}
		var ok bool
		cfgMap, ok = fm.(map[string]any)
		if !ok {
			return Document{}, ErrFrontMatterNotMap
		}
	}

	body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return Document{
		Config:         cfgMap,
		PromptTemplate: body,
		HasFrontMatter: true,
	}, nil
}

// ParseFile reads and parses a WORKFLOW.md file.
func ParseFile(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, fmt.Errorf("workflow frontmatter: read file: %w", err)
	}
	return Parse(data)
}

func stripUTF8BOM(in []byte) []byte {
	return bytes.TrimPrefix(in, []byte{0xEF, 0xBB, 0xBF})
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func isFenceLine(line string) bool {
	if !strings.HasPrefix(line, delimiter) {
		return false
	}
	rest := strings.TrimPrefix(line, delimiter)
	return strings.Trim(rest, " \t") == ""
}
