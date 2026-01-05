package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

var ErrCodemapNotFound = errors.New("codemap not found")

const Command = "codemap/get"

const (
	ErrCodeInput    = "EARG"
	ErrCodeRuntime  = "ERUNTIME"
	ErrCodeNotFound = "ENOTFOUND"
)

const (
	DefaultMaxTraceContent = 500
	DefaultTimeout         = 5 * time.Second
)

type Input struct {
	ID              string `json:"id"`
	Workspace       string `json:"workspace,omitempty"`
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
	ctx := context.Background()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail(ErrCodeInput, err)
	}

	out, err := getCodemap(ctx, in)
	if err != nil {
		if errors.Is(err, ErrCodemapNotFound) {
			fail(ErrCodeNotFound, err)
		}
		fail(ErrCodeRuntime, err)
	}

	env := envelope.OK(Command, out)
	if err := json.NewEncoder(os.Stdout).Encode(env); err != nil {
		fail(ErrCodeRuntime, err)
	}
}

func parseInput(r *os.File) (*Input, error) {
	var in Input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return nil, fmt.Errorf("invalid JSON input: %w", err)
	}

	if in.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	if in.MaxTraceContent <= 0 {
		in.MaxTraceContent = DefaultMaxTraceContent
	}
	if in.IncludeTraces == nil {
		defaultTrue := true
		in.IncludeTraces = &defaultTrue
	}
	if in.Workspace == "" {
		if ws := os.Getenv("AGENTCTL_WORKSPACE"); ws != "" {
			in.Workspace = ws
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("failed to get working directory: %w", err)
			}
			in.Workspace = cwd
		}
	}

	return &in, nil
}

func getCodemap(ctx context.Context, in *Input) (*Output, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	start := time.Now()
	out := &Output{
		Found: false,
	}

	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine home directory: %w", err)
		}
		agentctlHome = filepath.Join(home, ".agentctl")
	}
	storageRoot := filepath.Join(agentctlHome, "storage")
	casRoot := filepath.Join(agentctlHome, "cas")

	workspacePath := in.Workspace
	if absPath, err := filepath.Abs(workspacePath); err == nil {
		workspacePath = absPath
	}

	memStore, err := memory.Open(ctx, storageRoot, casRoot)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
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
	entries, err := memStore.List(ctx, workspacePath, 500)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
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
		return out, nil
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
	return out, nil
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

func fail(code string, err error) {
	env := envelope.Error(Command, code, err.Error(), nil)
	_ = json.NewEncoder(os.Stdout).Encode(env)
	os.Exit(1)
}
