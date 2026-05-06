// Package main implements the session/export-dspy skill for exporting sessions as DSPy training data.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/storage/sessions"
)

// Input defines the skill input parameters for DSPy training data export with flexible filtering options.
type Input struct {
	SessionIDs   []string `json:"session_ids,omitempty"`
	Project      string   `json:"project,omitempty"`
	IncludeTools bool     `json:"include_tools,omitempty"`
	IncludeFiles bool     `json:"include_files,omitempty"`
	OutputFile   string   `json:"output_file,omitempty"`
	Format       string   `json:"format,omitempty"` // "dspy", "jsonl", "csv"
	Limit        int      `json:"limit,omitempty"`
}

// Output defines the skill output with comprehensive export statistics and data.
type Output struct {
	ExamplesCount int           `json:"examples_count"`
	SessionsUsed  int           `json:"sessions_used"`
	OutputFile    string        `json:"output_file,omitempty"`
	Examples      []DSPyExample `json:"examples,omitempty"`
	Status        string        `json:"status"`
	Message       string        `json:"message"`
}

// DSPyExample represents a training example in DSPy format with input/output pairs and metadata.
type DSPyExample struct {
	Input    ExampleInput  `json:"input"`
	Output   ExampleOutput `json:"output"`
	Metadata ExampleMeta   `json:"metadata,omitempty"`
}

// ExampleInput is the input portion of an example with user request and contextual information.
type ExampleInput struct {
	UserRequest string   `json:"user_request"`
	Context     string   `json:"context,omitempty"`
	Files       []string `json:"files,omitempty"`
}

// ExampleOutput is the output portion of an example with response and tool usage tracking.
type ExampleOutput struct {
	Response    string   `json:"response"`
	ToolsUsed   []string `json:"tools_used,omitempty"`
	FilesEdited []string `json:"files_edited,omitempty"`
}

// ExampleMeta provides metadata about the example for training context and analysis.
type ExampleMeta struct {
	SessionID   string `json:"session_id"`
	ProjectName string `json:"project_name,omitempty"`
	TurnIndex   int    `json:"turn_index"`
	HasError    bool   `json:"has_error,omitempty"`
}

const (
	command      = "session/export-dspy"
	defaultLimit = 1000
)

// main is the skill entry point for session/export-dspy with training data generation capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates DSPy training data export from sessions with multiple output formats and filtering options.
//
// Index:
//   Purpose: Export session data as DSPy training examples with user-assistant pairing and tool usage tracking
//   Keywords: session/export-dspy, dspy_training, data_export, machine_learning, session_processing
//   Related: extractExamples, writeExamples, unique, escapeCSV
//   Flow: validate input → open sessions store → gather sessions → extract examples → write output → emit results
//   Resources: session store, output file (optional)
//   Events: DSPy export events
//   OutputFields: examples_count, sessions_used, output_file, examples
// [[domain:dspy-training-export]]
// [[protocol:user-assistant-pairing]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if len(in.SessionIDs) == 0 && in.Project == "" {
		return skillerr.Arg("either session_ids or project is required")
	}

	if in.Limit <= 0 {
		in.Limit = defaultLimit
	}

	if in.Format == "" {
		in.Format = "dspy"
	}

	// Open sessions store
	sessionStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}

	// Gather sessions
	var sessionList []sessions.Session

	if len(in.SessionIDs) > 0 {
		for _, id := range in.SessionIDs {
			sess, err := sessionStore.Get(ctx, id)
			if err == nil {
				sessionList = append(sessionList, sess)
			}
		}
	} else if in.Project != "" {
		// Search by project
		all, err := sessionStore.List(ctx, sessions.ListOptions{Limit: 100})
		if err != nil {
			return skillerr.IO("list sessions", skillerr.WithCause(err))
		}
		for _, s := range all {
			if strings.EqualFold(s.ProjectName, in.Project) {
				sessionList = append(sessionList, s)
			}
		}
	}

	if len(sessionList) == 0 {
		output := Output{
			ExamplesCount: 0,
			SessionsUsed:  0,
			Examples:      []DSPyExample{},
			Status:        "no_sessions",
			Message:       "No sessions found matching criteria",
		}
		return skillout.Emit(rc, command, output)
	}

	// Extract examples from sessions
	examples := []DSPyExample{}
	for _, sess := range sessionList {
		// Get turns for this session
		turns, err := sessionStore.GetTurns(ctx, sess.ID, sessions.TurnListOptions{
			SessionID: sess.ID,
			Limit:     in.Limit,
		})
		if err != nil || len(turns) == 0 {
			continue
		}

		// Pair user requests with assistant responses
		examples = append(examples, extractExamples(sess, turns, in)...)

		if len(examples) >= in.Limit {
			examples = examples[:in.Limit]
			break
		}
	}

	output := Output{
		ExamplesCount: len(examples),
		SessionsUsed:  len(sessionList),
		Status:        "ok",
		Message:       fmt.Sprintf("Exported %d examples from %d sessions", len(examples), len(sessionList)),
	}

	// Write to file or return inline
	if in.OutputFile != "" {
		if err := writeExamples(in.OutputFile, examples, in.Format); err != nil {
			return err
		}
		output.OutputFile = in.OutputFile
	} else {
		output.Examples = examples
	}

	return skillout.Emit(rc, command, output)
}

// extractExamples extracts DSPy examples from session turns with user-assistant pairing and tool tracking.
func extractExamples(sess sessions.Session, turns []sessions.SessionTurn, input Input) []DSPyExample {
	examples := []DSPyExample{}

	// Look for user-assistant pairs
	for i := 0; i < len(turns)-1; i++ {
		userTurn := turns[i]
		if userTurn.Role != "user" {
			continue
		}

		// Find the next assistant response
		var assistantTurn *sessions.SessionTurn
		var toolsUsed []string
		var filesEdited []string

		for j := i + 1; j < len(turns); j++ {
			t := turns[j]
			if t.Role == "user" {
				break
			}
			if t.Role == "assistant" {
				assistantTurn = &t
				// Collect tools from this and subsequent tool turns
				for _, tc := range t.ToolCalls {
					toolsUsed = append(toolsUsed, tc.Name)
				}
				filesEdited = append(filesEdited, t.FilesTouched...)
			}
		}

		if assistantTurn == nil {
			continue
		}

		example := DSPyExample{
			Input: ExampleInput{
				UserRequest: userTurn.ContentPreview,
			},
			Output: ExampleOutput{
				Response: assistantTurn.ContentPreview,
			},
			Metadata: ExampleMeta{
				SessionID:   sess.ID,
				ProjectName: sess.ProjectName,
				TurnIndex:   userTurn.TurnIndex,
				HasError:    assistantTurn.HasError,
			},
		}

		if input.IncludeTools && len(toolsUsed) > 0 {
			example.Output.ToolsUsed = unique(toolsUsed)
		}

		if input.IncludeFiles && len(filesEdited) > 0 {
			example.Output.FilesEdited = unique(filesEdited)
			example.Input.Files = unique(filesEdited)
		}

		examples = append(examples, example)
	}

	return examples
}

// writeExamples writes examples to a file in the specified format with proper encoding and validation.
func writeExamples(path string, examples []DSPyExample, format string) error {
	file, err := os.Create(path)
	if err != nil {
		return skillerr.WrapIO("create output file", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)

	switch format {
	case "jsonl":
		for _, ex := range examples {
			if err := encoder.Encode(ex); err != nil {
				return skillerr.WrapRuntime("encode jsonl", err)
			}
		}
	case "csv":
		// Simple CSV with request/response
		if _, err := file.WriteString("user_request,response\n"); err != nil {
			return skillerr.WrapIO("write CSV header", err)
		}
		for i, ex := range examples {
			req := escapeCSV(ex.Input.UserRequest)
			resp := escapeCSV(ex.Output.Response)
			if _, err := fmt.Fprintf(file, "%s,%s\n", req, resp); err != nil {
				return skillerr.WrapIO(fmt.Sprintf("write CSV row %d", i), err)
			}
		}
	default: // "dspy" - single JSON array
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(examples); err != nil {
			return skillerr.WrapRuntime("encode output", err)
		}
	}

	return nil
}

// escapeCSV escapes a string for CSV output with proper quoting and character handling.
func escapeCSV(s string) string {
	s = strings.ReplaceAll(s, "\"", "\"\"")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if strings.Contains(s, ",") || strings.Contains(s, "\"") {
		s = "\"" + s + "\""
	}
	return s
}

// unique returns unique values from a slice with deduplication and empty string filtering.
func unique(items []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range items {
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}
