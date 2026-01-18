// Package main implements the codemap/get skill.
package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/codemap"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

const command = "codemap/get"

const (
	DefaultMaxTraceContent = 500
	DefaultTimeout         = 5 * time.Second
)

type Input struct {
	ID              string `json:"id" validate:"required"`
	IncludeTraces   *bool  `json:"include_traces,omitempty"`
	MaxTraceContent int    `json:"max_trace_content,omitempty"`
}

type Output struct {
	Codemap *CodemapData `json:"codemap,omitempty"`
	Found   bool         `json:"found"`
	Stats   Stats        `json:"stats"`
}

type CodemapData struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Files       []string `json:"files,omitempty"`
	Traces      []Trace  `json:"traces,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	Summary     string   `json:"summary,omitempty"`
}

type Trace struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content,omitempty"`
	File        string `json:"file,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
}

type Stats struct {
	LatencyMS int `json:"latency_ms"`
}

type StoredCodemap struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Files       []string      `json:"files"`
	Traces      []StoredTrace `json:"traces"`
}

type StoredTrace struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	File        string `json:"file"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if in.MaxTraceContent <= 0 {
		in.MaxTraceContent = DefaultMaxTraceContent
	}
	if in.IncludeTraces == nil {
		defaultTrue := true
		in.IncludeTraces = &defaultTrue
	}

	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	start := time.Now()
	out := &Output{
		Found: false,
	}

	memStore, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		return skillerr.WrapIO("open memory store", err)
	}
	defer func() { errs.Ignore(memStore.Close(), "close memory store") }()

	// Normalize the ID - try with and without codemap:// prefix
	searchIDs := []string{in.ID}
	if !strings.HasPrefix(in.ID, "codemap://") {
		searchIDs = append(searchIDs, "codemap://"+in.ID)
	} else {
		searchIDs = append(searchIDs, strings.TrimPrefix(in.ID, "codemap://"))
	}

	// List all codemaps and find matching one
	entries, err := memStore.List(ctx, rc.Workspace, 500)
	if err != nil {
		return skillerr.WrapIO("list memories", err)
	}

	var foundEntry *storage.NamedEntry
	for i := range entries {
		if entries[i].Type != "codemap" {
			continue
		}
		for _, searchID := range searchIDs {
			if entries[i].Name == searchID {
				foundEntry = &entries[i]
				break
			}
		}
		if foundEntry != nil {
			break
		}
	}

	if foundEntry == nil {
		out.Stats.LatencyMS = int(time.Since(start).Milliseconds())
		return skillout.Emit(rc, command, out)
	}

	out.Found = true
	out.Codemap = &CodemapData{
		ID:      foundEntry.Name,
		Summary: foundEntry.Summary,
	}

	if !foundEntry.CreatedAt.IsZero() {
		out.Codemap.CreatedAt = foundEntry.CreatedAt.Format(time.RFC3339)
	}
	if !foundEntry.UpdatedAt.IsZero() {
		out.Codemap.UpdatedAt = foundEntry.UpdatedAt.Format(time.RFC3339)
	}

	if foundEntry.Result != nil {
		if ws, ok, err := codemap.ParseWindsurfCodemap(foundEntry.Result); err == nil && ok {
			out.Codemap.Title = ws.Title
			out.Codemap.Description = ws.Description
			out.Codemap.Files = extractWindsurfFiles(ws)

			if *in.IncludeTraces {
				for _, t := range ws.Traces {
					content := t.TraceTextDiagram
					if content == "" {
						content = t.TraceGuide
					}

					file := ""
					startLine := 0
					endLine := 0
					if len(t.Locations) > 0 {
						file = t.Locations[0].Path
						startLine = t.Locations[0].LineNumber
						endLine = t.Locations[0].LineNumber
					}

					out.Codemap.Traces = append(out.Codemap.Traces, Trace{
						Name:        t.Title,
						Description: t.Description,
						Content:     skillout.TruncateRunes(content, in.MaxTraceContent),
						File:        file,
						StartLine:   startLine,
						EndLine:     endLine,
					})
				}
			}
		} else {
			var stored StoredCodemap
			if err := json.Unmarshal(foundEntry.Result, &stored); err == nil {
				out.Codemap.Title = stored.Title
				out.Codemap.Description = stored.Description
				out.Codemap.Files = stored.Files

				if *in.IncludeTraces {
					for _, st := range stored.Traces {
						out.Codemap.Traces = append(out.Codemap.Traces, Trace{
							Name:        st.Name,
							Description: st.Description,
							Content:     skillout.TruncateRunes(st.Content, in.MaxTraceContent),
							File:        st.File,
							StartLine:   st.StartLine,
							EndLine:     st.EndLine,
						})
					}
				}
			}
		}
	}

	// Ensure non-nil slices for JSON output
	if out.Codemap.Files == nil {
		out.Codemap.Files = []string{}
	}
	if out.Codemap.Traces == nil {
		out.Codemap.Traces = []Trace{}
	}

	out.Stats.LatencyMS = int(time.Since(start).Milliseconds())
	return skillout.Emit(rc, command, out)
}

func extractWindsurfFiles(ws *codemap.WindsurfCodemap) []string {
	if ws == nil {
		return []string{}
	}
	pathSet := make(map[string]struct{})
	for _, trace := range ws.Traces {
		for _, loc := range trace.Locations {
			if loc.Path == "" {
				continue
			}
			path := strings.TrimPrefix(loc.Path, "./")
			pathSet[path] = struct{}{}
		}
	}
	if len(pathSet) == 0 {
		return []string{}
	}
	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}
