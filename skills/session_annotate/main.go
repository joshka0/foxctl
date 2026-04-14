// Package main implements the session/annotate skill for deterministic turn annotation.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/context/sessionkit/claudejsonl"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/annotations"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/vector"
)

const (
	command            = "session/annotate"
	defaultBatchSize   = 10
	defaultLLMEndpoint = "http://localhost:1234/v1"
	defaultLLMModel    = "lfm-2.5"
	maxPreviewChars    = 200
	maxEmbeddingChars  = 8000
)

var (
	reFilePath = regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])((?:[A-Za-z0-9._-]+/)*[A-Za-z0-9._-]+\.[A-Za-z0-9_-]+(?::\d+)?)`)
	reCommand  = regexp.MustCompile(`(?m)(?:^|\n)\s*(?:[$#>]\s*)([^\n]+)`)
	reAnchor   = regexp.MustCompile(`@?[A-Za-z0-9._/-]+\.[A-Za-z0-9_-]+(?::\d+|#L\d+(?:C\d+)?)?`)
)

var allowedCategories = map[string]struct{}{
	"decision":    {},
	"howto":       {},
	"debug":       {},
	"code_change": {},
	"question":    {},
	"answer":      {},
	"context":     {},
	"config":      {},
	"test":        {},
	"refactor":    {},
}

const annotationPrompt = `You annotate ONE chat turn for a searchable semantic table-of-contents.

Rules:
- Output STRICT JSON only.
- toc_label: one short sentence, <= 140 chars
- toc_category: one of: decision, howto, debug, code_change, question, answer, context, config, test, refactor
- intent: brief description of the user's or assistant's intent

Input:
role: %s
chunk_type: %s
content_preview: %s
tools_used: %s
code_blocks: %d blocks
commands: %d commands
errors: %d errors
file_paths: %v

Return JSON:
{"toc_label": "...", "toc_category": "...", "intent": "..."}`

// Input defines skill input parameters.
type Input struct {
	SessionID      string `json:"session_id" validate:"omitempty"`
	Workspace      string `json:"workspace" validate:"omitempty"`
	Force          bool   `json:"force"`
	BatchSize      int    `json:"batch_size"`
	SkipEmbedding  bool   `json:"skip_embedding"`
	SkipLLM        bool   `json:"skip_llm"`
	LLMEndpoint    string `json:"llm_endpoint"`              // default: http://localhost:1234/v1
	EmbeddingModel string `json:"embedding_model"`           // LM Studio embedding model name
	QueueEmbedding bool   `json:"queue_embedding,omitempty"` // Queue embedding jobs for async processing
}

// Output reports annotation counters.
type Output struct {
	SessionID        string `json:"session_id"`
	TurnsProcessed   int    `json:"turns_processed"`
	TurnsSkipped     int    `json:"turns_skipped"`
	AnchorsFound     int    `json:"anchors_found"`
	EmbeddingsGen    int    `json:"embeddings_generated"`
	LLMAnnotated     int    `json:"llm_annotated"`
	AnnotationsSaved int    `json:"annotations_saved"`
	EmbeddingsQueued int    `json:"embeddings_queued"`
}

type turnAnnotation struct {
	TOCLabel    string `json:"toc_label"`
	TOCCategory string `json:"toc_category"`
	Intent      string `json:"intent"`
}

type turnFeatures struct {
	Role           string
	ChunkType      string
	ContentPreview string
	ToolsUsed      []string
	FilePaths      []string
	Commands       []string
	SliceAnchors   []string
	CodeBlocks     int
	ErrorCount     int
}

type llmClient struct {
	endpoint string
	model    string
	client   *http.Client
}

type localEmbedder struct {
	endpoint string
	model    string
	client   *http.Client
}

func (e *localEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	payload := map[string]any{
		"model": e.model,
		"input": text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer lm-studio")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("embedding: parse response: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding: empty response")
	}
	return result.Data[0].Embedding, nil
}

func (e *localEmbedder) Model() string { return e.model }

// main is the skill entry point.
func main() {
	skillmain.Main(command, run)
}

// run streams Claude NDJSON turns, extracts deterministic anchors, optionally labels with LLM, and stores annotated chunks.
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	workspace := workspaceutil.Resolve(in.Workspace, "", rc.Workspace)
	if workspace == "" {
		return skillerr.Arg("workspace is required")
	}
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatchSize
	}
	if in.LLMEndpoint == "" {
		in.LLMEndpoint = defaultLLMEndpoint
	}

	projectDir := claudejsonl.ClaudeProjectDir(workspace)
	if projectDir == "" {
		return skillerr.Arg(
			fmt.Sprintf("no Claude Code project found for workspace: %s", workspace),
			skillerr.WithHint("Ensure Claude Code has been used in this workspace."),
		)
	}

	sessionFile, sessionID := findSessionFile(projectDir, in.SessionID)
	if sessionFile == "" {
		return skillerr.Arg(
			fmt.Sprintf("no session file found in project directory: %s", projectDir),
			skillerr.WithHint("Specify session_id explicitly or run Claude Code in this workspace first."),
		)
	}

	sessionStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}
	if err := ensureSessionRecord(ctx, sessionStore, sessionID, workspace, sessionFile); err != nil {
		return skillerr.IO("ensure session record", skillerr.WithCause(err))
	}

	// Open annotations store (non-fatal if unavailable)
	annStore, annErr := annotations.Open(ctx, "")
	if annErr != nil {
		fmt.Fprintf(os.Stderr, "WARN: session/annotate: annotations store unavailable: %v\n", annErr)
	}
	if annStore != nil {
		defer annStore.Close()
	}

	// Open annotation queue if requested
	var annQueue *annotations.Queue
	if in.QueueEmbedding {
		annQueue, err = annotations.OpenQueue(ctx, rc.Config.Storage.Root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: session/annotate: annotation queue unavailable: %v\n", err)
		}
		if annQueue != nil {
			defer annQueue.Close()
		}
	}

	existingHashes := map[string]struct{}{}
	if !in.Force {
		existingChunks, err := sessionStore.GetChunks(ctx, sessionID, 0)
		if err == nil {
			for _, c := range existingChunks {
				if c.ContentHash != "" {
					existingHashes[c.ContentHash] = struct{}{}
				}
			}
		}
	}

	reader, err := claudejsonl.OpenReader(sessionFile)
	if err != nil {
		return skillerr.IO("open session file", skillerr.WithCause(err))
	}
	defer reader.Close()

	rawFile, err := os.Open(sessionFile)
	if err != nil {
		return skillerr.IO("open session file for line hashing", skillerr.WithCause(err))
	}
	defer rawFile.Close()

	type embedFunc func(ctx context.Context, text string) ([]float32, string, error)
	var doEmbed embedFunc
	if !in.SkipEmbedding {
		if in.EmbeddingModel != "" {
			// Local LM Studio embeddings
			le := &localEmbedder{
				endpoint: strings.TrimRight(in.LLMEndpoint, "/"),
				model:    in.EmbeddingModel,
				client:   &http.Client{Timeout: 30 * time.Second},
			}
			doEmbed = func(ctx context.Context, text string) ([]float32, string, error) {
				vec, err := le.Embed(ctx, text)
				return vec, le.Model(), err
			}
		} else {
			// Voyage API
			voyageKey := os.Getenv("VOYAGE_API_KEY")
			embedder, err := semantic.NewEmbedderFromConfig(
				semantic.ScopeSessions,
				rc.Config,
				semantic.WithVoyageKey(voyageKey),
				skillmain.EmbeddingGuard(rc),
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "WARN: session/annotate: embedding disabled: %v\n", err)
			} else {
				doEmbed = func(ctx context.Context, text string) ([]float32, string, error) {
					res, err := embedder.Embed(ctx, text)
					return res.Vec, embedder.Model(), err
				}
			}
		}
	}

	var llm *llmClient
	if !in.SkipLLM {
		llm = &llmClient{
			endpoint: strings.TrimRight(in.LLMEndpoint, "/"),
			model:    defaultLLMModel,
			client:   &http.Client{Timeout: 15 * time.Second},
		}
	}

	out := Output{
		SessionID: sessionID,
	}

	windowIndex := 0
	turnIndex := 0
	llmDisabled := false
	pending := make([]storage.SessionChunk, 0, in.BatchSize)

	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		if err := sessionStore.SaveChunks(ctx, pending); err != nil {
			return err
		}
		pending = pending[:0]
		return nil
	}

	for {
		rm, err := reader.Next()
		if err != nil {
			return skillerr.IO("read session NDJSON", skillerr.WithCause(err))
		}
		if rm == nil {
			break
		}

		msg := rm.Message
		if msg == nil {
			continue
		}

		chunkType := claudejsonl.Classify(msg)
		if chunkType == claudejsonl.ChunkTypeOther {
			continue
		}

		_, _, isBoundary := claudejsonl.IsCompactBoundary(msg)

		role := extractRole(msg, chunkType)
		contentPreview := claudejsonl.ExtractPreview(msg, maxPreviewChars)
		toolsUsed := normalizeStrings(claudejsonl.ExtractTools(msg))
		features := buildTurnFeatures(msg, role, string(chunkType), contentPreview, toolsUsed)

		contentHash, err := hashRawLine(rawFile, rm.ByteOffset, rm.ByteLength, msg)
		if err != nil {
			return skillerr.IO("hash line content", skillerr.WithCause(err))
		}

		if !in.Force {
			if _, exists := existingHashes[contentHash]; exists {
				out.TurnsSkipped++
				turnIndex++
				if isBoundary {
					windowIndex++
				}
				continue
			}
		}

		var ann turnAnnotation
		if llm != nil && !llmDisabled {
			var llmErr error
			ann, llmErr = llm.annotateTurn(ctx, features)
			if llmErr != nil {
				llmDisabled = true
				fmt.Fprintf(os.Stderr, "WARN: session/annotate: disabling LLM annotation after error: %v\n", llmErr)
			} else if ann.TOCLabel != "" {
				out.LLMAnnotated++
			}
		}

		chunk := storage.SessionChunk{
			ID:                 fmt.Sprintf("%s-%d", sessionID, turnIndex),
			SessionID:          sessionID,
			ChunkIndex:         turnIndex,
			ChunkType:          string(chunkType),
			ContentHash:        contentHash,
			ContentPreview:     buildAnnotatedPreview(contentPreview, ann),
			ByteOffset:         rm.ByteOffset,
			ByteLength:         rm.ByteLength,
			ToolsUsed:          toolsUsed,
			FilesTouched:       features.FilePaths,
			HasError:           features.ErrorCount > 0 || claudejsonl.HasError(msg),
			ErrorType:          claudejsonl.ExtractErrorType(msg),
			ContextWindowIndex: windowIndex,
		}

		embeddingText := buildEmbeddingText(features, ann)
		if doEmbed != nil {
			vec, model, embErr := doEmbed(ctx, embeddingText)
			if embErr != nil {
				fmt.Fprintf(os.Stderr, "WARN: session/annotate: embedding failed at turn %d: %v\n", turnIndex, embErr)
			} else {
				chunk.Embedding = vector.SerializeF32(vec)
				chunk.EmbeddingModel = model
				out.EmbeddingsGen++
			}
		}

		pending = append(pending, chunk)

		// Dual-write to annotations store
		if annStore != nil {
			turnAnn := &storage.TurnAnnotation{
				ID:                 chunk.ID,
				SessionID:          sessionID,
				TurnIndex:          turnIndex,
				ContextWindowIndex: windowIndex,
				ByteOffset:         rm.ByteOffset,
				ByteLength:         rm.ByteLength,
				LineNum:            turnIndex,
				Timestamp:          extractTurnAnnotationTime(rm),
				ChunkType:          string(chunkType),
				Role:               role,
				FilePaths:          features.FilePaths,
				ToolsUsed:          toolsUsed,
				ContentPreview:     contentPreview,
				ContentHash:        contentHash,
				TOCLabel:           ann.TOCLabel,
				TOCCategory:        ann.TOCCategory,
				Intent:             ann.Intent,
				Embedding:          chunk.Embedding,
				EmbeddingModel:     chunk.EmbeddingModel,
				EmbeddingText:      embeddingText,
				HasError:           chunk.HasError,
				IsCompactBoundary:  isBoundary,
			}
			if llm != nil && !llmDisabled {
				turnAnn.AnnotationModel = llm.model
			}
			// Convert string slices to []any for JSON array columns
			for _, cmd := range features.Commands {
				turnAnn.Commands = append(turnAnn.Commands, cmd)
			}
			for _, anchor := range features.SliceAnchors {
				turnAnn.Symbols = append(turnAnn.Symbols, anchor)
			}
			if saveErr := annStore.Save(ctx, turnAnn); saveErr != nil {
				fmt.Fprintf(os.Stderr, "WARN: session/annotate: annotation save failed (turn %d): %v\n", turnIndex, saveErr)
			} else {
				out.AnnotationsSaved++
			}
		}

		// Queue embedding job if requested, not using local model, and inline embedding didn't already succeed
		if annQueue != nil && in.EmbeddingModel == "" && embeddingText != "" && chunk.Embedding == nil {
			if qErr := annQueue.Enqueue(ctx, annotations.AnnotationEmbeddingPayload{
				SessionID:     sessionID,
				TurnIndex:     turnIndex,
				EmbeddingText: embeddingText,
			}); qErr != nil {
				fmt.Fprintf(os.Stderr, "WARN: session/annotate: queue enqueue failed (turn %d): %v\n", turnIndex, qErr)
			} else {
				out.EmbeddingsQueued++
			}
		}
		if len(pending) >= in.BatchSize {
			if err := flush(); err != nil {
				return skillerr.IO("save annotated turns", skillerr.WithCause(err))
			}
		}

		existingHashes[contentHash] = struct{}{}
		out.TurnsProcessed++
		out.AnchorsFound += len(features.SliceAnchors)
		turnIndex++
		if isBoundary {
			windowIndex++
		}
	}

	if err := flush(); err != nil {
		return skillerr.IO("save final annotated turns", skillerr.WithCause(err))
	}

	return skillout.Emit(rc, command, out)
}

func ensureSessionRecord(ctx context.Context, store *sessions.Store, sessionID, workspace, sessionFile string) error {
	if _, err := store.Get(ctx, sessionID); err == nil {
		return nil
	}
	_, err := store.Save(ctx, storage.Session{
		ID:            sessionID,
		WorkspacePath: workspace,
		ProjectName:   filepath.Base(workspace),
		RawJSONLPath:  sessionFile,
		AgentType:     "claude",
	})
	return err
}

func findSessionFile(projectDir, requestedSessionID string) (string, string) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", ""
	}

	type sessionFile struct {
		path    string
		id      string
		modTime time.Time
	}

	var files []sessionFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") || strings.HasPrefix(name, "agent-") {
			continue
		}

		id := strings.TrimSuffix(name, ".jsonl")
		if requestedSessionID != "" && id != requestedSessionID {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, sessionFile{
			path:    filepath.Join(projectDir, name),
			id:      id,
			modTime: info.ModTime(),
		})
	}

	if len(files) == 0 {
		return "", ""
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	return files[0].path, files[0].id
}

func hashRawLine(f *os.File, offset, length int64, msg *claudejsonl.Message) (string, error) {
	if f == nil || length <= 0 {
		return hashMessageFallback(msg)
	}
	buf := make([]byte, length)
	n, err := f.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return "", err
	}
	if int64(n) < length && err != io.EOF {
		return hashMessageFallback(msg)
	}
	line := bytes.TrimRight(buf[:n], "\r\n")
	if len(line) == 0 {
		return hashMessageFallback(msg)
	}
	sum := sha256.Sum256(line)
	return hex.EncodeToString(sum[:]), nil
}

func hashMessageFallback(msg *claudejsonl.Message) (string, error) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func extractRole(msg *claudejsonl.Message, chunkType claudejsonl.ChunkType) string {
	if msg == nil {
		return ""
	}
	if role := strings.TrimSpace(msg.Role); role != "" {
		return role
	}
	if len(msg.Message) > 0 {
		var nested claudejsonl.NestedMessage
		if err := json.Unmarshal(msg.Message, &nested); err == nil {
			if role := strings.TrimSpace(nested.Role); role != "" {
				return role
			}
		}
	}
	switch chunkType {
	case claudejsonl.ChunkTypeUserRequest, claudejsonl.ChunkTypeToolOutput, claudejsonl.ChunkTypeError:
		return "user"
	case claudejsonl.ChunkTypeAssistantResponse, claudejsonl.ChunkTypeToolUse:
		return "assistant"
	case claudejsonl.ChunkTypeCompactBoundary:
		return "system"
	default:
		return "system"
	}
}

func extractTurnAnnotationTime(rm *claudejsonl.ReadMessage) time.Time {
	if rm == nil {
		return time.Time{}
	}
	if !rm.Timestamp.IsZero() {
		return rm.Timestamp.UTC()
	}
	if rm.Message == nil {
		return time.Time{}
	}
	raw := strings.TrimSpace(rm.Message.Timestamp)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func buildTurnFeatures(msg *claudejsonl.Message, role, chunkType, contentPreview string, toolsUsed []string) turnFeatures {
	rawPreview := claudejsonl.ExtractPreview(msg, 4000)
	paths := extractFilePaths(rawPreview)
	commands := extractCommands(rawPreview)
	anchors := extractSliceAnchors(rawPreview)

	// Pull additional anchors from tool input payloads.
	toolPaths, toolCommands := extractToolInputAnchors(msg)
	paths = append(paths, toolPaths...)
	commands = append(commands, toolCommands...)
	anchors = append(anchors, toolPaths...)

	paths = normalizeStrings(paths)
	commands = normalizeStrings(commands)
	anchors = normalizeStrings(anchors)

	errorCount := 0
	if claudejsonl.HasError(msg) {
		errorCount++
	}
	lower := strings.ToLower(rawPreview)
	if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
		errorCount++
	}

	return turnFeatures{
		Role:           role,
		ChunkType:      chunkType,
		ContentPreview: contentPreview,
		ToolsUsed:      toolsUsed,
		FilePaths:      paths,
		Commands:       commands,
		SliceAnchors:   anchors,
		CodeBlocks:     strings.Count(rawPreview, "```") / 2,
		ErrorCount:     errorCount,
	}
}

func extractFilePaths(content string) []string {
	if content == "" {
		return nil
	}
	matches := reFilePath.FindAllStringSubmatch(content, -1)
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		candidate := strings.TrimSpace(match[1])
		candidate = strings.Trim(candidate, "[](){}'\"`")
		if candidate == "" {
			continue
		}
		paths = append(paths, candidate)
	}
	return normalizeStrings(paths)
}

func extractCommands(content string) []string {
	if content == "" {
		return nil
	}
	matches := reCommand.FindAllStringSubmatch(content, -1)
	cmds := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		cmd := strings.TrimSpace(match[1])
		if cmd != "" {
			cmds = append(cmds, cmd)
		}
	}
	return normalizeStrings(cmds)
}

func extractSliceAnchors(content string) []string {
	if content == "" {
		return nil
	}
	matches := reAnchor.FindAllString(content, -1)
	anchors := make([]string, 0, len(matches))
	for _, m := range matches {
		candidate := strings.TrimSpace(strings.TrimPrefix(m, "@"))
		candidate = strings.Trim(candidate, "[](){}'\"`")
		if candidate == "" {
			continue
		}
		anchors = append(anchors, candidate)
	}
	return normalizeStrings(anchors)
}

func extractToolInputAnchors(msg *claudejsonl.Message) (filePaths []string, commands []string) {
	if msg == nil {
		return nil, nil
	}

	if msg.ToolUse != nil && len(msg.ToolUse.Input) > 0 {
		var payload any
		if json.Unmarshal(msg.ToolUse.Input, &payload) == nil {
			filePaths = append(filePaths, walkForPaths(payload)...)
			commands = append(commands, walkForCommands(payload)...)
		}
	}

	if len(msg.Message) > 0 {
		var nested struct {
			Content []map[string]any `json:"content"`
		}
		if json.Unmarshal(msg.Message, &nested) == nil {
			for _, block := range nested.Content {
				blockType, _ := block["type"].(string)
				if blockType != "tool_use" {
					continue
				}
				if input, ok := block["input"]; ok {
					filePaths = append(filePaths, walkForPaths(input)...)
					commands = append(commands, walkForCommands(input)...)
				}
			}
		}
	}

	return normalizeStrings(filePaths), normalizeStrings(commands)
}

func walkForPaths(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for key, val := range x {
			k := strings.ToLower(strings.TrimSpace(key))
			if strings.Contains(k, "path") || strings.Contains(k, "file") || strings.Contains(k, "cwd") || strings.Contains(k, "workspace") {
				out = append(out, parsePathCandidates(val)...)
			}
			out = append(out, walkForPaths(val)...)
		}
	case []any:
		for _, item := range x {
			out = append(out, walkForPaths(item)...)
		}
	case string:
		out = append(out, extractFilePaths(x)...)
	}
	return normalizeStrings(out)
}

func walkForCommands(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for key, val := range x {
			k := strings.ToLower(strings.TrimSpace(key))
			if strings.Contains(k, "command") || k == "cmd" || strings.Contains(k, "shell") {
				out = append(out, parseCommandCandidates(val)...)
			}
			out = append(out, walkForCommands(val)...)
		}
	case []any:
		for _, item := range x {
			out = append(out, walkForCommands(item)...)
		}
	case string:
		out = append(out, extractCommands("$ "+x)...)
	}
	return normalizeStrings(out)
}

func parsePathCandidates(v any) []string {
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
		paths := extractFilePaths(x)
		if len(paths) > 0 {
			return paths
		}
		candidate := strings.TrimSpace(x)
		if candidate == "" || strings.Contains(candidate, " ") {
			return nil
		}
		return []string{candidate}
	case []any:
		var out []string
		for _, item := range x {
			out = append(out, parsePathCandidates(item)...)
		}
		return normalizeStrings(out)
	default:
		return nil
	}
}

func parseCommandCandidates(v any) []string {
	switch x := v.(type) {
	case string:
		cmd := strings.TrimSpace(x)
		if cmd == "" {
			return nil
		}
		return []string{cmd}
	case []any:
		var out []string
		for _, item := range x {
			out = append(out, parseCommandCandidates(item)...)
		}
		return normalizeStrings(out)
	default:
		return nil
	}
}

func (c *llmClient) annotateTurn(ctx context.Context, f turnFeatures) (turnAnnotation, error) {
	if c == nil {
		return turnAnnotation{}, nil
	}
	payload := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{
				"role": "user",
				"content": fmt.Sprintf(
					annotationPrompt,
					f.Role,
					f.ChunkType,
					f.ContentPreview,
					strings.Join(f.ToolsUsed, ", "),
					f.CodeBlocks,
					len(f.Commands),
					f.ErrorCount,
					f.FilePaths,
				),
			},
		},
		"temperature": 0.1,
		"max_tokens":  256,
		"response_format": map[string]string{
			"type": "json_object",
		},
	}

	rawResp, err := c.postChatCompletion(ctx, payload)
	if err != nil {
		if isResponseFormatError(err) {
			delete(payload, "response_format")
			rawResp, err = c.postChatCompletion(ctx, payload)
		}
		if err != nil {
			return turnAnnotation{}, err
		}
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawResp, &completion); err != nil {
		return turnAnnotation{}, err
	}
	if len(completion.Choices) == 0 {
		return turnAnnotation{}, fmt.Errorf("empty choices")
	}

	ann, err := parseAnnotationJSON(completion.Choices[0].Message.Content)
	if err != nil {
		return turnAnnotation{}, err
	}
	return ann, nil
}

func (c *llmClient) postChatCompletion(ctx context.Context, payload map[string]any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer lm-studio")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rawResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(rawResp)))
	}
	return rawResp, nil
}

func parseAnnotationJSON(raw string) (turnAnnotation, error) {
	content := strings.TrimSpace(raw)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var ann turnAnnotation
	if err := json.Unmarshal([]byte(content), &ann); err != nil {
		start := strings.Index(content, "{")
		end := strings.LastIndex(content, "}")
		if start == -1 || end <= start {
			return turnAnnotation{}, err
		}
		if err := json.Unmarshal([]byte(content[start:end+1]), &ann); err != nil {
			return turnAnnotation{}, err
		}
	}

	ann.TOCLabel = strings.TrimSpace(skillout.TruncateSingleLine(ann.TOCLabel, 140))
	ann.Intent = strings.TrimSpace(skillout.TruncateSingleLine(ann.Intent, 200))
	ann.TOCCategory = normalizeCategory(ann.TOCCategory)
	return ann, nil
}

func normalizeCategory(category string) string {
	cat := strings.ToLower(strings.TrimSpace(category))
	if _, ok := allowedCategories[cat]; ok {
		return cat
	}
	return "context"
}

func isResponseFormatError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "response_format") ||
		strings.Contains(msg, "json_schema") ||
		strings.Contains(msg, "structured output")
}

func buildAnnotatedPreview(preview string, ann turnAnnotation) string {
	preview = strings.TrimSpace(preview)
	if ann.TOCLabel == "" {
		return preview
	}
	label := ann.TOCLabel
	if ann.TOCCategory != "" {
		label = "[" + ann.TOCCategory + "] " + label
	}
	if preview == "" {
		return label
	}
	merged := label + " | " + preview
	return skillout.TruncateSingleLine(merged, 500)
}

func buildEmbeddingText(f turnFeatures, ann turnAnnotation) string {
	parts := []string{
		"role: " + f.Role,
		"chunk_type: " + f.ChunkType,
	}
	if ann.TOCLabel != "" {
		parts = append(parts, "toc_label: "+ann.TOCLabel)
	}
	if ann.TOCCategory != "" {
		parts = append(parts, "toc_category: "+ann.TOCCategory)
	}
	if ann.Intent != "" {
		parts = append(parts, "intent: "+ann.Intent)
	}
	if f.ContentPreview != "" {
		parts = append(parts, "preview: "+f.ContentPreview)
	}
	if len(f.ToolsUsed) > 0 {
		parts = append(parts, "tools: "+strings.Join(f.ToolsUsed, ", "))
	}
	if len(f.FilePaths) > 0 {
		parts = append(parts, "files: "+strings.Join(f.FilePaths, ", "))
	}
	if len(f.Commands) > 0 {
		parts = append(parts, "commands: "+strings.Join(f.Commands, " | "))
	}
	if len(f.SliceAnchors) > 0 {
		parts = append(parts, "anchors: "+strings.Join(f.SliceAnchors, ", "))
	}

	text := strings.Join(parts, "\n")
	if len(text) > maxEmbeddingChars {
		return text[:maxEmbeddingChars]
	}
	return text
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		clean := strings.TrimSpace(v)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}
