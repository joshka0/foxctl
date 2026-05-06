// Package parser extracts Index metadata from doc comments for repoindex edges.
package parser

import (
	"sort"
	"strings"
)

// DocIndex captures parsed Index metadata from doc comments.
type DocIndex struct {
	Purpose      string   `json:"purpose,omitempty"`
	Keywords     []string `json:"keywords,omitempty"`
	Related      []string `json:"related,omitempty"`
	Flow         []string `json:"flow,omitempty"`
	Resources    []string `json:"resources,omitempty"`
	Events       []string `json:"events,omitempty"`
	OutputFields []string `json:"output_fields,omitempty"`
}

// Empty reports whether the index metadata contains any entries.
func (d DocIndex) Empty() bool {
	return d.Purpose == "" && len(d.Keywords) == 0 && len(d.Related) == 0 &&
		len(d.Flow) == 0 && len(d.Resources) == 0 && len(d.Events) == 0 &&
		len(d.OutputFields) == 0
}

// ParsedDoc is the parsed documentation with Index metadata removed.
type ParsedDoc struct {
	Doc      string
	Index    DocIndex
	HasIndex bool
}

// Parse extracts Index metadata from doc comments and returns the remaining doc text.
//
// Supported forms:
//
//	Index: kw1, kw2
//	Index:
//	  Purpose: ...
//	  Related: ...
//	  Flow: ...
//	  Keywords: ...
//	  Events: ...
//	  Resources: ...
//	  OutputFields: ...
//
// Field lines may optionally be prefixed with '-' or '*'.
//
// Index:
//   Purpose: Parse structured Index blocks and keyword lists from doc comments
//   Keywords: doc_index, parse, keywords, purpose, related, flow, resources, events, output_fields
//   Related: DocIndex, ParsedDoc, normalizeIndex
//   Flow: split lines → detect Index: → parse fields → normalize → return ParsedDoc
//   Resources: repoindex comment edges
//   Events: index-parse
//   OutputFields: Doc, Index, HasIndex
//
// [[protocol:index-block-parsing]]
// [[invariant:empty-index-reported-correctly]]
func Parse(doc string) ParsedDoc {
	doc = strings.ReplaceAll(doc, "\r\n", "\n")
	doc = strings.ReplaceAll(doc, "\r", "\n")
	lines := strings.Split(doc, "\n")
	var out []string
	var idx DocIndex
	var hasIndex bool
	inIndex := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inIndex {
			if strings.HasPrefix(trimmed, "Index:") {
				hasIndex = true
				after := strings.TrimSpace(strings.TrimPrefix(trimmed, "Index:"))
				if after != "" {
					idx.Keywords = append(idx.Keywords, splitList(after)...)
					continue
				}
				inIndex = true
				continue
			}
			out = append(out, line)
			continue
		}

		if trimmed == "" {
			inIndex = false
			continue
		}

		field, value, ok := parseField(trimmed)
		if ok {
			applyField(&idx, field, value)
			continue
		}

		// Unknown line: stop index parsing and treat as doc.
		inIndex = false
		out = append(out, line)
	}

	idx = normalizeIndex(idx)
	return ParsedDoc{
		Doc:      strings.TrimSpace(strings.Join(out, "\n")),
		Index:    idx,
		HasIndex: hasIndex,
	}
}

func parseField(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
		line = strings.TrimSpace(line[1:])
	}
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	field := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if field == "" {
		return "", "", false
	}
	return strings.ToLower(field), value, true
}

func applyField(idx *DocIndex, field, value string) {
	if idx == nil {
		return
	}
	switch field {
	case "purpose":
		if idx.Purpose == "" {
			idx.Purpose = value
		}
	case "keyword", "keywords":
		idx.Keywords = append(idx.Keywords, splitList(value)...)
	case "related":
		idx.Related = append(idx.Related, splitList(value)...)
	case "flow":
		idx.Flow = append(idx.Flow, splitList(value)...)
	case "resource", "resources":
		idx.Resources = append(idx.Resources, splitList(value)...)
	case "event", "events":
		idx.Events = append(idx.Events, splitList(value)...)
	case "output", "outputs", "outputfields", "outputfield", "output_fields":
		idx.OutputFields = append(idx.OutputFields, splitList(value)...)
	}
}

func splitList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';'
	})
	if len(parts) == 0 {
		return nil
	}
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		items = append(items, item)
	}
	return items
}

func normalizeIndex(idx DocIndex) DocIndex {
	idx.Keywords = normalizeTokens(idx.Keywords)
	idx.Resources = normalizeTokens(idx.Resources)
	idx.Events = normalizeTokens(idx.Events)
	idx.OutputFields = normalizeTokens(idx.OutputFields)
	idx.Related = normalizeRefs(idx.Related)
	idx.Flow = normalizeRefs(idx.Flow)
	idx.Purpose = strings.TrimSpace(idx.Purpose)
	return idx
}

func normalizeTokens(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	set := make(map[string]struct{})
	for _, item := range items {
		norm := normalizeToken(item)
		if norm == "" {
			continue
		}
		set[norm] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func normalizeRefs(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	set := make(map[string]struct{})
	for _, item := range items {
		clean := strings.TrimSpace(item)
		if clean == "" {
			continue
		}
		set[clean] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func normalizeToken(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'")
	value = strings.ToLower(value)
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.' || r == '-' || r == '_' || r == '/':
			return r
		case r == ' ' || r == '\t':
			return '_'
		default:
			return -1
		}
	}, value)
	value = strings.Trim(value, "_")
	return value
}
