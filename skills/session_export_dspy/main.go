// Package main implements the session/export-dspy skill for exporting sessions as DSPy training data.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// Input defines the skill input parameters.
type Input struct {
	SessionIDs   []string `json:"session_ids,omitempty"`
	Project      string   `json:"project,omitempty"`
	IncludeTools bool     `json:"include_tools,omitempty"`
	IncludeFiles bool     `json:"include_files,omitempty"`
	OutputFile   string   `json:"output_file,omitempty"`
	Format       string   `json:"format,omitempty"` // "dspy", "jsonl", "csv"
	Limit        int      `json:"limit,omitempty"`
}

// Output defines the skill output.
type Output struct {
	ExamplesCount int           `json:"examples_count"`
	SessionsUsed  int           `json:"sessions_used"`
	OutputFile    string        `json:"output_file,omitempty"`
	Examples      []DSPyExample `json:"examples,omitempty"`
	Status        string        `json:"status"`
	Message       string        `json:"message"`
}

// DSPyExample represents a training example in DSPy format.
type DSPyExample struct {
	Input    ExampleInput  `json:"input"`
	Output   ExampleOutput `json:"output"`
	Metadata ExampleMeta   `json:"metadata,omitempty"`
}

// ExampleInput is the input portion of an example.
type ExampleInput struct {
	UserRequest string   `json:"user_request"`
	Context     string   `json:"context,omitempty"`
	Files       []string `json:"files,omitempty"`
}

// ExampleOutput is the output portion of an example.
type ExampleOutput struct {
	Response    string   `json:"response"`
	ToolsUsed   []string `json:"tools_used,omitempty"`
	FilesEdited []string `json:"files_edited,omitempty"`
}

// ExampleMeta provides metadata about the example.
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

func main() {
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("DECODE_ERROR", fmt.Errorf("decode input: %w", err))
	}

	if len(input.SessionIDs) == 0 && input.Project == "" {
		fail("INVALID_INPUT", fmt.Errorf("either session_ids or project is required"))
	}

	if input.Limit <= 0 {
		input.Limit = defaultLimit
	}

	if input.Format == "" {
		input.Format = "dspy"
	}

	// Get agentctl home
	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		homeDir, _ := os.UserHomeDir()
		agentctlHome = filepath.Join(homeDir, ".agentctl")
	}

	// Open sessions store
	storageRoot := filepath.Join(agentctlHome, "storage")
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		fail("STORE_ERROR", fmt.Errorf("open sessions store: %w", err))
	}
	defer func() { errs.Ignore(sessionStore.Close(), "close sessions store") }()

	// Gather sessions
	var sessionList []sessions.Session

	if len(input.SessionIDs) > 0 {
		for _, id := range input.SessionIDs {
			sess, err := sessionStore.Get(ctx, id)
			if err == nil {
				sessionList = append(sessionList, sess)
			}
		}
	} else if input.Project != "" {
		// Search by project
		all, err := sessionStore.List(ctx, sessions.ListOptions{Limit: 100})
		if err != nil {
			fail("LIST_ERROR", fmt.Errorf("list sessions: %w", err))
		}
		for _, s := range all {
			if strings.EqualFold(s.ProjectName, input.Project) {
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
		env := envelope.OK(command, output)
		errs.Ignore(envelope.Write(os.Stdout, env), "emit session/export-dspy result")
		return
	}

	// Extract examples from sessions
	examples := []DSPyExample{}
	for _, sess := range sessionList {
		// Get turns for this session
		turns, err := sessionStore.GetTurns(ctx, sess.ID, sessions.TurnListOptions{
			SessionID: sess.ID,
			Limit:     input.Limit,
		})
		if err != nil || len(turns) == 0 {
			continue
		}

		// Pair user requests with assistant responses
		examples = append(examples, extractExamples(sess, turns, input)...)

		if len(examples) >= input.Limit {
			examples = examples[:input.Limit]
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
	if input.OutputFile != "" {
		if err := writeExamples(input.OutputFile, examples, input.Format); err != nil {
			fail("WRITE_ERROR", fmt.Errorf("write output: %w", err))
		}
		output.OutputFile = input.OutputFile
	} else {
		output.Examples = examples
	}

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/export-dspy result")
}

// extractExamples extracts DSPy examples from session turns.
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

// writeExamples writes examples to a file in the specified format.
func writeExamples(path string, examples []DSPyExample, format string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)

	switch format {
	case "jsonl":
		for _, ex := range examples {
			if err := encoder.Encode(ex); err != nil {
				return err
			}
		}
	case "csv":
		// Simple CSV with request/response
		if _, err := file.WriteString("user_request,response\n"); err != nil {
			return fmt.Errorf("writing CSV header: %w", err)
		}
		for i, ex := range examples {
			req := escapeCSV(ex.Input.UserRequest)
			resp := escapeCSV(ex.Output.Response)
			if _, err := fmt.Fprintf(file, "%s,%s\n", req, resp); err != nil {
				return fmt.Errorf("writing CSV row %d: %w", i, err)
			}
		}
	default: // "dspy" - single JSON array
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(examples); err != nil {
			return err
		}
	}

	return nil
}

// escapeCSV escapes a string for CSV output.
func escapeCSV(s string) string {
	s = strings.ReplaceAll(s, "\"", "\"\"")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if strings.Contains(s, ",") || strings.Contains(s, "\"") {
		s = "\"" + s + "\""
	}
	return s
}

// unique returns unique values from a slice.
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

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/export-dspy failure")
	os.Exit(1)
}
