package codemap

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// WindsurfCodemap matches the .codemap JSON schema produced by Windsurf.
type WindsurfCodemap struct {
	SchemaVersion  int              `json:"schemaVersion"`
	ID             string           `json:"id"`
	StableID       string           `json:"stableId"`
	Metadata       WindsurfMetadata `json:"metadata"`
	Title          string           `json:"title"`
	Description    string           `json:"description"`
	Traces         []WindsurfTrace  `json:"traces"`
	MermaidDiagram string           `json:"mermaidDiagram"`
}

type WindsurfMetadata struct {
	CascadeID           string `json:"cascadeId"`
	GenerationSource    string `json:"generationSource"`
	GenerationTimestamp string `json:"generationTimestamp"`
	Mode                string `json:"mode"`
	OriginalPrompt      string `json:"originalPrompt"`
}

type WindsurfTrace struct {
	ID               string             `json:"id"`
	Title            string             `json:"title"`
	Description      string             `json:"description"`
	Locations        []WindsurfLocation `json:"locations"`
	TraceTextDiagram string             `json:"traceTextDiagram"`
	TraceGuide       string             `json:"traceGuide"`
}

type WindsurfLocation struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	LineNumber  int    `json:"lineNumber"`
	LineContent string `json:"lineContent"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ParseWindsurfCodemap attempts to parse a Windsurf codemap.
// It returns (codemap, true, nil) when schemaVersion is present and valid.
func ParseWindsurfCodemap(data []byte) (*WindsurfCodemap, bool, error) {
	var probe struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, false, err
	}
	if probe.SchemaVersion == 0 {
		return nil, false, nil
	}
	var cm WindsurfCodemap
	if err := json.Unmarshal(data, &cm); err != nil {
		return nil, true, err
	}
	return &cm, true, nil
}

// GenerationTime returns the codemap generation time if present.
func (cm *WindsurfCodemap) GenerationTime() time.Time {
	if cm == nil {
		return time.Time{}
	}
	if cm.Metadata.GenerationTimestamp == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, cm.Metadata.GenerationTimestamp)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ToCodemap converts a Windsurf codemap into the internal codemap shape.
func (cm *WindsurfCodemap) ToCodemap() *Codemap {
	if cm == nil {
		return nil
	}

	converted := &Codemap{
		ID:          cm.ID,
		Title:       cm.Title,
		Description: cm.Description,
		Query:       cm.Metadata.OriginalPrompt,
		CreatedAt:   cm.GenerationTime(),
	}

	fileSet := make(map[string]struct{})
	locationCount := 0
	for i, trace := range cm.Traces {
		convertedTrace := Trace{
			Number:  i + 1,
			Title:   trace.Title,
			Summary: trace.Description,
			Tree:    trace.TraceTextDiagram,
		}

		for j, loc := range trace.Locations {
			label := loc.ID
			if label == "" {
				label = fmt.Sprintf("%d%s", i+1, alphabetLabel(j))
			}

			path := strings.TrimPrefix(loc.Path, "@")
			if loc.LineNumber > 0 {
				path = fmt.Sprintf("%s:%d", path, loc.LineNumber)
			}
			if path != "" && !strings.HasPrefix(path, "@") {
				path = "@" + path
			}

			if path != "" {
				fileSet[strings.TrimPrefix(strings.SplitN(path, ":", 2)[0], "@")] = struct{}{}
			}

			convertedTrace.Annotations = append(convertedTrace.Annotations, Annotation{
				Label:       label,
				Title:       loc.Title,
				Description: loc.Description,
				Path:        path,
			})
			locationCount++
		}
		converted.Traces = append(converted.Traces, convertedTrace)
	}

	converted.FileCount = len(fileSet)
	converted.SymbolCount = locationCount
	return converted
}

func alphabetLabel(idx int) string {
	if idx < 0 {
		return "a"
	}
	return string(rune('a' + (idx % 26)))
}
