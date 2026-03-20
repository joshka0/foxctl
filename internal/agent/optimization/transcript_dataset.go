package optimization

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/sessionkit/codexjsonl"
	"github.com/jkatigb/agentctl/internal/storage"
)

// TranscriptDatasetRequest configures transcript export into optimizer-friendly examples.
type TranscriptDatasetRequest struct {
	SessionIDs      []string
	WorkspacePath   string
	Project         string
	Source          string
	Category        string
	IncludeTools    bool
	IncludeFiles    bool
	IncludeFeedback bool
	Limit           int
}

// TranscriptTrainingExample is one transcript-derived training example.
type TranscriptTrainingExample struct {
	Input    TranscriptTrainingInput    `json:"input"`
	Output   TranscriptTrainingOutput   `json:"output"`
	Metadata TranscriptTrainingMetadata `json:"metadata"`
}

// TranscriptTrainingInput captures the user-side input for one example.
type TranscriptTrainingInput struct {
	UserRequest string   `json:"user_request"`
	Context     string   `json:"context,omitempty"`
	Files       []string `json:"files,omitempty"`
}

// TranscriptTrainingOutput captures the assistant-side target.
type TranscriptTrainingOutput struct {
	Response    string   `json:"response"`
	ToolsUsed   []string `json:"tools_used,omitempty"`
	FilesEdited []string `json:"files_edited,omitempty"`
}

// TranscriptTrainingMetadata carries session and weak-supervision metadata.
type TranscriptTrainingMetadata struct {
	SessionID    string    `json:"session_id"`
	AgentType    string    `json:"agent_type,omitempty"`
	Category     string    `json:"category,omitempty"`
	ProjectName  string    `json:"project_name,omitempty"`
	RawJSONLPath string    `json:"raw_jsonl_path,omitempty"`
	TurnIndex    int       `json:"turn_index"`
	HasError     bool      `json:"has_error,omitempty"`
	Prompt       string    `json:"prompt,omitempty"`
	PromptHash   string    `json:"prompt_hash,omitempty"`
	LLMProvider  string    `json:"llm_provider,omitempty"`
	LLMModel     string    `json:"llm_model,omitempty"`
	Rating       int       `json:"rating,omitempty"`
	Outcome      string    `json:"outcome,omitempty"`
	Notes        string    `json:"notes,omitempty"`
	Timestamp    time.Time `json:"timestamp,omitempty"`
}

type transcriptFeedback struct {
	SessionID string    `json:"session_id,omitempty"`
	Rating    int       `json:"rating"`
	Outcome   string    `json:"outcome"`
	Notes     string    `json:"notes,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ExportTranscriptDataset builds training examples from captured Claude/Codex sessions.
func ExportTranscriptDataset(
	ctx context.Context,
	sessionStore storage.SessionStore,
	memStore storage.MemoryStore,
	req TranscriptDatasetRequest,
) ([]TranscriptTrainingExample, error) {
	if sessionStore == nil {
		return nil, fmt.Errorf("session store is required")
	}
	if req.Limit <= 0 {
		req.Limit = 1000
	}

	sessionsList, err := gatherTranscriptSessions(ctx, sessionStore, req)
	if err != nil {
		return nil, err
	}
	feedbackBySession, err := loadTranscriptFeedback(ctx, memStore, req)
	if err != nil {
		return nil, err
	}

	examples := make([]TranscriptTrainingExample, 0, req.Limit)
	for _, session := range sessionsList {
		select {
		case <-ctx.Done():
			return examples, ctx.Err()
		default:
		}

		turns, err := sessionStore.GetTurns(ctx, session.ID, storage.SessionTurnListOptions{
			SessionID: session.ID,
		})
		if err != nil {
			return examples, fmt.Errorf("get session turns for %s: %w", session.ID, err)
		}
		if len(turns) > 0 {
			sessionFeedback := feedbackBySession[session.ID]
			examples = append(examples, extractTranscriptExamples(session, turns, sessionFeedback, req)...)
			if len(examples) >= req.Limit {
				examples = examples[:req.Limit]
				break
			}
			continue
		}

		sessionFeedback := feedbackBySession[session.ID]
		if strings.EqualFold(strings.TrimSpace(session.AgentType), "codex") {
			select {
			case <-ctx.Done():
				return examples, ctx.Err()
			default:
			}

			fallbackExamples, fallbackErr := extractCodexTranscriptExamplesFromRaw(ctx, session, sessionFeedback, req)
			if fallbackErr != nil {
				return examples, fmt.Errorf("extract codex fallback examples for %s: %w", session.ID, fallbackErr)
			}
			if len(fallbackExamples) > 0 {
				examples = append(examples, fallbackExamples...)
				if len(examples) >= req.Limit {
					examples = examples[:req.Limit]
					break
				}
			}
		}
		if len(examples) >= req.Limit {
			examples = examples[:req.Limit]
			break
		}
	}

	return examples, nil
}

func gatherTranscriptSessions(ctx context.Context, sessionStore storage.SessionStore, req TranscriptDatasetRequest) ([]storage.Session, error) {
	var sessionsList []storage.Session
	if len(req.SessionIDs) > 0 {
		for _, id := range req.SessionIDs {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			sess, err := sessionStore.Get(ctx, strings.TrimSpace(id))
			if err != nil {
				return nil, fmt.Errorf("get session %s: %w", strings.TrimSpace(id), err)
			}
			sessionsList = append(sessionsList, sess)
		}
	} else {
		opts := storage.SessionListOptions{
			WorkspacePath: strings.TrimSpace(req.WorkspacePath),
			ProjectName:   strings.TrimSpace(req.Project),
			Limit:         req.Limit,
		}
		all, err := sessionStore.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		sessionsList = append(sessionsList, all...)
	}

	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" || source == "all" {
		return sessionsList, nil
	}

	filtered := make([]storage.Session, 0, len(sessionsList))
	for _, sess := range sessionsList {
		if strings.EqualFold(strings.TrimSpace(sess.AgentType), source) {
			filtered = append(filtered, sess)
		}
	}
	return filtered, nil
}

type transcriptDatasetExampleStream func(TranscriptTrainingExample) error

func streamTranscriptDatasetJSONL(r io.Reader, limit int, emit transcriptDatasetExampleStream) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var example TranscriptTrainingExample
		if err := json.Unmarshal([]byte(line), &example); err != nil {
			return fmt.Errorf("decode transcript example: %w", err)
		}
		if emit != nil {
			if err := emit(example); err != nil {
				return err
			}
		}
		count++
		if limit > 0 && count >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan transcript dataset: %w", err)
	}
	return nil
}

func loadTranscriptFeedback(ctx context.Context, memStore storage.MemoryStore, req TranscriptDatasetRequest) (map[string]transcriptFeedback, error) {
	if !req.IncludeFeedback || memStore == nil {
		return map[string]transcriptFeedback{}, nil
	}

	feedbackLimit := max(req.Limit*10, 5000)
	entries, err := memStore.List(ctx, strings.TrimSpace(req.WorkspacePath), feedbackLimit)
	if err != nil {
		return nil, fmt.Errorf("list transcript feedback: %w", err)
	}

	feedbackBySession := make(map[string]transcriptFeedback)
	for _, entry := range entries {
		if entry.Type != "session_feedback" {
			continue
		}
		var feedback transcriptFeedback
		if err := json.Unmarshal(entry.Result, &feedback); err != nil {
			continue
		}
		if strings.TrimSpace(feedback.SessionID) == "" {
			continue
		}
		current, ok := feedbackBySession[feedback.SessionID]
		if !ok || feedback.Timestamp.After(current.Timestamp) {
			feedbackBySession[feedback.SessionID] = feedback
		}
	}
	return feedbackBySession, nil
}

func extractTranscriptExamples(
	session storage.Session,
	turns []storage.SessionTurn,
	feedback transcriptFeedback,
	req TranscriptDatasetRequest,
) []TranscriptTrainingExample {
	examples := make([]TranscriptTrainingExample, 0, len(turns)/2)
	for i := 0; i < len(turns)-1; i++ {
		userTurn := turns[i]
		if userTurn.Role != "user" {
			continue
		}

		var assistantTurn *storage.SessionTurn
		var toolsUsed []string
		var filesEdited []string

		for j := i + 1; j < len(turns); j++ {
			turn := turns[j]
			if turn.Role == "user" {
				break
			}
			if turn.Role == "assistant" {
				assistantTurn = &turn
				for _, toolCall := range turn.ToolCalls {
					toolsUsed = append(toolsUsed, toolCall.Name)
				}
				filesEdited = append(filesEdited, turn.FilesTouched...)
			}
		}
		if assistantTurn == nil {
			continue
		}

		example := TranscriptTrainingExample{
			Input: TranscriptTrainingInput{
				UserRequest: userTurn.ContentPreview,
			},
			Output: TranscriptTrainingOutput{
				Response: assistantTurn.ContentPreview,
			},
			Metadata: TranscriptTrainingMetadata{
				SessionID:    session.ID,
				AgentType:    session.AgentType,
				Category:     CategorizeTranscriptUserRequest(userTurn.ContentPreview, assistantTurn.ContentPreview),
				ProjectName:  session.ProjectName,
				RawJSONLPath: session.RawJSONLPath,
				TurnIndex:    userTurn.TurnIndex,
				HasError:     assistantTurn.HasError,
				Prompt:       session.Prompt,
				PromptHash:   session.PromptHash,
				LLMProvider:  session.LLMProvider,
				LLMModel:     session.LLMModel,
				Rating:       feedback.Rating,
				Outcome:      feedback.Outcome,
				Notes:        feedback.Notes,
				Timestamp:    assistantTurn.Timestamp,
			},
		}

		if req.IncludeTools && len(toolsUsed) > 0 {
			example.Output.ToolsUsed = uniqueSortedStrings(toolsUsed)
		}
		if req.IncludeFiles && len(filesEdited) > 0 {
			files := uniqueSortedStrings(filesEdited)
			example.Output.FilesEdited = files
			example.Input.Files = files
		}
		if !ShouldKeepTranscriptCategoryExample(example, req.Category) {
			continue
		}
		examples = append(examples, example)
	}
	return examples
}

func extractCodexTranscriptExamplesFromRaw(
	ctx context.Context,
	session storage.Session,
	feedback transcriptFeedback,
	req TranscriptDatasetRequest,
) ([]TranscriptTrainingExample, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	rawPath := strings.TrimSpace(session.RawJSONLPath)
	if rawPath == "" {
		return nil, nil
	}
	reader, err := codexjsonl.OpenReader(rawPath)
	if err != nil {
		return nil, fmt.Errorf("open codex raw transcript: %w", err)
	}
	defer reader.Close()

	messages, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read codex raw transcript: %w", err)
	}

	examples := make([]TranscriptTrainingExample, 0, 16)
	for i := 0; i < len(messages); i++ {
		select {
		case <-ctx.Done():
			return examples, ctx.Err()
		default:
		}

		rm := messages[i]
		if rm == nil || rm.Message == nil {
			continue
		}
		if codexjsonl.Classify(rm.Message) != codexjsonl.ChunkTypeUserRequest {
			continue
		}
		userRequest := strings.TrimSpace(codexjsonl.ExtractPreview(rm.Message, 400))
		if userRequest == "" {
			continue
		}

		toolsUsed := []string{}
		responseParts := []string{}
		for j := i + 1; j < len(messages); j++ {
			select {
			case <-ctx.Done():
				return examples, ctx.Err()
			default:
			}

			next := messages[j]
			if next == nil || next.Message == nil {
				continue
			}
			switch codexjsonl.Classify(next.Message) {
			case codexjsonl.ChunkTypeUserRequest:
				j = len(messages)
			case codexjsonl.ChunkTypeToolUse:
				if req.IncludeTools {
					toolsUsed = append(toolsUsed, codexjsonl.ExtractTools(next.Message)...)
				}
			case codexjsonl.ChunkTypeAssistantResponse:
				preview := strings.TrimSpace(codexjsonl.ExtractPreview(next.Message, 600))
				if preview != "" {
					responseParts = append(responseParts, preview)
				}
			}
			if len(responseParts) > 0 && j < len(messages)-1 {
				if upcoming := messages[j+1]; upcoming != nil && upcoming.Message != nil && codexjsonl.Classify(upcoming.Message) == codexjsonl.ChunkTypeUserRequest {
					break
				}
			}
		}

		response := strings.TrimSpace(strings.Join(responseParts, "\n\n"))
		if response == "" {
			continue
		}

		example := TranscriptTrainingExample{
			Input: TranscriptTrainingInput{
				UserRequest: userRequest,
			},
			Output: TranscriptTrainingOutput{
				Response: response,
			},
			Metadata: TranscriptTrainingMetadata{
				SessionID:    session.ID,
				AgentType:    session.AgentType,
				Category:     CategorizeTranscriptUserRequest(userRequest, response),
				ProjectName:  session.ProjectName,
				RawJSONLPath: session.RawJSONLPath,
				TurnIndex:    rm.LineNum,
				HasError:     false,
				Prompt:       session.Prompt,
				PromptHash:   session.PromptHash,
				LLMProvider:  session.LLMProvider,
				LLMModel:     session.LLMModel,
				Rating:       feedback.Rating,
				Outcome:      feedback.Outcome,
				Notes:        feedback.Notes,
				Timestamp:    rm.Timestamp,
			},
		}
		if req.IncludeTools && len(toolsUsed) > 0 {
			example.Output.ToolsUsed = uniqueSortedStrings(toolsUsed)
		}
		if !ShouldKeepTranscriptCategoryExample(example, req.Category) {
			continue
		}
		examples = append(examples, example)
		if len(examples) >= req.Limit {
			break
		}
	}

	return examples, nil
}

func CategorizeTranscriptUserRequest(userRequest, response string) string {
	userLower := strings.ToLower(strings.TrimSpace(userRequest))
	responseLower := strings.ToLower(strings.TrimSpace(response))

	if isTranscriptContinuationStub(userLower) {
		return "continuation"
	}
	if isTranscriptReleaseWorkflow(userLower, responseLower) {
		return "release_workflow"
	}
	if isTranscriptCoderImpl(userLower, responseLower) {
		return "coder_impl"
	}
	if isTranscriptOpsInfra(userLower, responseLower) {
		return "ops_infra"
	}
	return "other"
}

func ShouldKeepTranscriptCategoryExample(example TranscriptTrainingExample, category string) bool {
	category = NormalizeTranscriptCategory(category)
	if category == "all" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(example.Metadata.Category), category)
}

func NormalizeTranscriptCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "", "all":
		return "all"
	case "coder_impl":
		return "coder_impl"
	case "ops_infra":
		return "ops_infra"
	case "release_workflow":
		return "release_workflow"
	case "continuation":
		return "continuation"
	default:
		return "all"
	}
}

func isTranscriptContinuationStub(userLower string) bool {
	stubs := []string{
		"continue",
		"loaded",
		"keep it simple",
		"lets do 1",
		"let's do 1",
		"lets test manually",
		"let's test manually",
	}
	for _, stub := range stubs {
		if userLower == stub {
			return true
		}
	}
	return false
}

func isTranscriptReleaseWorkflow(userLower, responseLower string) bool {
	markers := []string{
		"commit",
		"merge request",
		"create an mr",
		"check mr",
		"check the job",
		"pipeline",
		"ci",
		"coderabbit",
		"greptile",
		"github.com",
		"gitlab.com",
	}
	for _, marker := range markers {
		if strings.Contains(userLower, marker) || strings.Contains(responseLower, marker) {
			return true
		}
	}
	return false
}

func isTranscriptOpsInfra(userLower, responseLower string) bool {
	markers := []string{
		"embedding",
		"annotat",
		"recall",
		"query",
		"worker",
		"queue",
		"daemon",
		"session",
		"lmstudio",
		"voyage",
		"gemma",
		"scout",
		"annotation",
		"chunk_granularity",
		"session_chunks",
		"port-forward",
		"redeploy",
		"dev-web",
		"orbstack",
		"metro",
		"cors",
		"pod ",
		"pods",
		"ghcr",
		"localhost:",
		"check the logs",
	}
	for _, marker := range markers {
		if strings.Contains(userLower, marker) || strings.Contains(responseLower, marker) {
			return true
		}
	}
	return false
}

func isTranscriptCoderImpl(userLower, responseLower string) bool {
	markers := []string{
		"review",
		"fix",
		"debug",
		"build",
		"diff",
		"format",
		"tool",
		"implementation",
		"architecture",
		"design",
		"plan",
		"prompt",
		"schema",
		"sql",
		"security",
		"encrypt",
		"docker",
		"helm",
		"k8s",
		"pvc",
		"modal",
		"gui",
		"ux",
		"noise",
		"specialized",
		"longer term",
		"longer-term",
		"what else",
		"how are the plans",
	}
	for _, marker := range markers {
		if strings.Contains(userLower, marker) || strings.Contains(responseLower, marker) {
			return true
		}
	}
	return false
}

// WriteTranscriptDatasetJSONL writes one JSON object per line.
func WriteTranscriptDatasetJSONL(w io.Writer, examples []TranscriptTrainingExample) error {
	enc := json.NewEncoder(w)
	for _, example := range examples {
		if err := enc.Encode(example); err != nil {
			return fmt.Errorf("encode transcript example: %w", err)
		}
	}
	return nil
}

// BuildTranscriptDatasetJSONL returns the JSONL payload as bytes.
func BuildTranscriptDatasetJSONL(examples []TranscriptTrainingExample) ([]byte, error) {
	var builder strings.Builder
	if err := WriteTranscriptDatasetJSONL(&builder, examples); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

// ParseTranscriptDatasetJSONL decodes JSONL transcript examples.
func ParseTranscriptDatasetJSONL(r io.Reader) ([]TranscriptTrainingExample, error) {
	examples := []TranscriptTrainingExample{}
	if err := streamTranscriptDatasetJSONL(r, 0, func(example TranscriptTrainingExample) error {
		examples = append(examples, example)
		return nil
	}); err != nil {
		return nil, err
	}
	return examples, nil
}

func SaveTranscriptDatasetFile(path string, examples []TranscriptTrainingExample) error {
	body, err := BuildTranscriptDatasetJSONL(examples)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func uniqueSortedStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
