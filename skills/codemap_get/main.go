// Package main implements the codemap/get skill.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
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

	memStore, err := memory.Open(ctx, rc.Config.Storage.Root, rc.Config.Paths.CAS)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
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
		return fmt.Errorf("list memories: %w", err)
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
						Content:     truncate(st.Content, in.MaxTraceContent),
						File:        st.File,
						StartLine:   st.StartLine,
						EndLine:     st.EndLine,
					})
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

func truncate(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
