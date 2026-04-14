package sourceimport

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/todosync"
	"github.com/joshka0/foxctl/internal/v2/adapters/libsql/turns"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

const (
	defaultEmbeddingDims = 1024
	hashEmbeddingModel   = "deterministic-hash-v1"
)

// BuildArtifacts derives annotation/classification/learning/embedding artifacts for parsed turns.
func BuildArtifacts(ctx context.Context, parsed ParsedSession, opts ArtifactBuildOptions) ArtifactBuildResult {
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	artifactSource := strings.TrimSpace(opts.ArtifactSource)
	if artifactSource == "" {
		artifactSource = "sourceimport"
	}
	includeEmbedding := opts.IncludeEmbedding
	embedder := opts.Embedder
	if includeEmbedding && embedder == nil {
		embedder = NewHashEmbedder(defaultEmbeddingDims)
	}

	todoStats := summarizeTodos(opts.Todos)
	artifacts := make([]turns.Artifact, 0, len(parsed.Turns)*4)
	warnings := make([]string, 0)

	for _, turn := range parsed.Turns {
		turn = turn.Clone()
		baseMeta := map[string]any{
			"provider":      string(parsed.Provider),
			"session_id":    parsed.SessionID,
			"source_path":   parsed.SourcePath,
			"workspace":     parsed.WorkspacePath,
			"turn_id":       turn.ID,
			"turn_index":    turn.TurnIndex,
			"tool_calls":    countToolCalls(turn),
			"has_error":     turnHasError(turn),
			"todo_total":    todoStats.Total,
			"todo_pending":  todoStats.Pending,
			"todo_active":   todoStats.InProgress,
			"todo_done":     todoStats.Completed,
			"artifact_from": artifactSource,
		}

		annotationContent := map[string]any{
			"prompt":        strings.TrimSpace(turn.Prompt),
			"final_output":  strings.TrimSpace(turn.FinalOutput.Text),
			"tools":         turnToolNames(turn),
			"tool_results":  turnToolResults(turn),
			"iteration_cnt": len(turn.Iterations),
		}
		annotationSummary := buildAnnotationSummary(turn, parsed.Provider)
		artifacts = append(artifacts, turns.Artifact{
			TurnID:          turn.ID,
			ArtifactType:    turns.ArtifactTypeAnnotation,
			ArtifactVersion: "v1",
			Ref:             turns.BuildArtifactRef(turn.ID, turns.ArtifactTypeAnnotation, "v1"),
			Summary:         annotationSummary,
			ContentJSON:     mustJSON(annotationContent),
			MetadataJSON:    mustJSON(baseMeta),
			CreatedAt:       now().UTC(),
			UpdatedAt:       now().UTC(),
		})

		labels := classifyTurn(turn, parsed.Provider, todoStats)
		classificationContent := map[string]any{
			"labels":        labels,
			"provider":      string(parsed.Provider),
			"tool_calls":    countToolCalls(turn),
			"has_error":     turnHasError(turn),
			"todo_snapshot": todoStats,
		}
		artifacts = append(artifacts, turns.Artifact{
			TurnID:          turn.ID,
			ArtifactType:    turns.ArtifactTypeClassification,
			ArtifactVersion: "v1",
			Ref:             turns.BuildArtifactRef(turn.ID, turns.ArtifactTypeClassification, "v1"),
			Summary:         strings.Join(labels, ", "),
			ContentJSON:     mustJSON(classificationContent),
			MetadataJSON:    mustJSON(baseMeta),
			CreatedAt:       now().UTC(),
			UpdatedAt:       now().UTC(),
		})

		learnings := deriveLearnings(turn, todoStats)
		learningSummary := "turn captured for continuity"
		if len(learnings) > 0 {
			learningSummary = learnings[0]
		}
		learningContent := map[string]any{
			"learnings":     learnings,
			"todo_snapshot": todoStats,
			"labels":        labels,
		}
		artifacts = append(artifacts, turns.Artifact{
			TurnID:          turn.ID,
			ArtifactType:    turns.ArtifactTypeLearning,
			ArtifactVersion: "v1",
			Ref:             turns.BuildArtifactRef(turn.ID, turns.ArtifactTypeLearning, "v1"),
			Summary:         truncate(learningSummary, 240),
			ContentJSON:     mustJSON(learningContent),
			MetadataJSON:    mustJSON(baseMeta),
			CreatedAt:       now().UTC(),
			UpdatedAt:       now().UTC(),
		})

		if !includeEmbedding || embedder == nil {
			continue
		}

		embedText := buildEmbeddingText(turn, labels, learnings, todoStats, parsed.Provider)
		embeddingRes, err := embedder.Embed(ctx, embedText)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("turn %s embedding: %v", turn.ID, err))
			continue
		}
		if len(embeddingRes.Vector) == 0 {
			continue
		}

		embeddingContent := map[string]any{
			"text_preview": truncate(embedText, 512),
			"dims":         len(embeddingRes.Vector),
		}
		embeddingMeta := cloneMap(baseMeta)
		embeddingMeta["embedding_model"] = strings.TrimSpace(embeddingRes.Model)
		embeddingMeta["embedding_dims"] = len(embeddingRes.Vector)

		artifacts = append(artifacts, turns.Artifact{
			TurnID:          turn.ID,
			ArtifactType:    turns.ArtifactTypeEmbedding,
			ArtifactVersion: "v1",
			Ref:             turns.BuildArtifactRef(turn.ID, turns.ArtifactTypeEmbedding, "v1"),
			Summary:         truncate(embedText, 240),
			ContentJSON:     mustJSON(embeddingContent),
			MetadataJSON:    mustJSON(embeddingMeta),
			Embedding:       append([]float32(nil), embeddingRes.Vector...),
			EmbeddingModel:  strings.TrimSpace(embeddingRes.Model),
			CreatedAt:       now().UTC(),
			UpdatedAt:       now().UTC(),
		})
	}

	return ArtifactBuildResult{
		Artifacts: artifacts,
		Warnings:  warnings,
		TodoStats: todoStats,
	}
}

// HashEmbedder generates deterministic local embeddings with no external dependencies.
type HashEmbedder struct {
	dimensions int
	model      string
}

// NewHashEmbedder builds a deterministic hashing embedder.
func NewHashEmbedder(dimensions int) *HashEmbedder {
	if dimensions <= 0 {
		dimensions = defaultEmbeddingDims
	}
	return &HashEmbedder{
		dimensions: dimensions,
		model:      hashEmbeddingModel,
	}
}

// Embed converts text to a deterministic float32 vector.
func (h *HashEmbedder) Embed(_ context.Context, text string) (EmbeddingResult, error) {
	if h == nil {
		return EmbeddingResult{}, nil
	}
	return EmbeddingResult{
		Vector: hashVector(text, h.dimensions),
		Model:  h.model,
	}, nil
}

func hashVector(text string, dims int) []float32 {
	text = strings.ToLower(strings.TrimSpace(text))
	if dims <= 0 {
		dims = defaultEmbeddingDims
	}
	if text == "" {
		return make([]float32, dims)
	}

	vec := make([]float32, dims)
	tokens := tokenStream(text)
	if len(tokens) == 0 {
		return vec
	}
	for _, token := range tokens {
		idx, sign := bucket(token, dims)
		vec[idx] += sign
	}

	// L2 normalize.
	var sum float64
	for _, v := range vec {
		sum += float64(v * v)
	}
	if sum <= 0 {
		return vec
	}
	scale := float32(1.0 / math.Sqrt(sum))
	for i := range vec {
		vec[i] *= scale
	}
	return vec
}

func bucket(token string, dims int) (int, float32) {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(token))
	v := hasher.Sum64()
	idx := int(v % uint64(dims))
	sign := float32(1.0)
	if v&1 == 1 {
		sign = -1.0
	}
	return idx, sign
}

func tokenStream(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	out := make([]string, 0, len(parts)*2)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
		if len(p) >= 3 {
			for i := 0; i <= len(p)-3; i++ {
				out = append(out, "tri:"+p[i:i+3])
			}
		}
	}
	return out
}

// Dimensions returns the configured deterministic embedding width.
func (h *HashEmbedder) Dimensions() int {
	if h == nil || h.dimensions <= 0 {
		return 0
	}
	return h.dimensions
}

func summarizeTodos(todosIn []todosync.ClaudeTodo) TodoStats {
	stats := TodoStats{Total: len(todosIn)}
	for _, todo := range todosIn {
		switch strings.TrimSpace(strings.ToLower(todo.Status)) {
		case "completed":
			stats.Completed++
		case "in_progress":
			stats.InProgress++
		default:
			stats.Pending++
		}
	}
	return stats
}

func buildAnnotationSummary(turn run.TurnRecord, provider Provider) string {
	final := strings.TrimSpace(turn.FinalOutput.Text)
	if final != "" {
		return truncate(final, 240)
	}
	prompt := strings.TrimSpace(turn.Prompt)
	if prompt != "" {
		return truncate(prompt, 240)
	}
	return fmt.Sprintf("%s turn %d", provider, turn.TurnIndex)
}

func classifyTurn(turn run.TurnRecord, provider Provider, todoStats TodoStats) []string {
	labels := []string{
		fmt.Sprintf("provider:%s", provider),
		fmt.Sprintf("turn_index:%d", turn.TurnIndex),
	}
	toolCalls := countToolCalls(turn)
	switch {
	case toolCalls == 0:
		labels = append(labels, "tools:none")
	case toolCalls <= 2:
		labels = append(labels, "tools:few")
	default:
		labels = append(labels, "tools:many")
	}
	if turnHasError(turn) {
		labels = append(labels, "error:true")
	} else {
		labels = append(labels, "error:false")
	}
	if todoStats.Pending > 0 || todoStats.InProgress > 0 {
		labels = append(labels, "todos:open")
	} else {
		labels = append(labels, "todos:clear")
	}
	sort.Strings(labels)
	return labels
}

func deriveLearnings(turn run.TurnRecord, todoStats TodoStats) []string {
	notes := make([]string, 0, 4)
	if turnHasError(turn) {
		notes = append(notes, "Tool execution encountered at least one error; keep remediation context.")
	}
	if countToolCalls(turn) > 0 {
		notes = append(notes, fmt.Sprintf("Turn used %d tool calls; preserve command/result trace for replay.", countToolCalls(turn)))
	}
	if strings.TrimSpace(turn.FinalOutput.Text) == "" {
		notes = append(notes, "No explicit assistant final output captured; rely on tool results and prompt context.")
	}
	if todoStats.Pending > 0 || todoStats.InProgress > 0 {
		notes = append(notes, fmt.Sprintf("Todo state indicates unfinished work (pending=%d in_progress=%d).", todoStats.Pending, todoStats.InProgress))
	}
	if len(notes) == 0 {
		notes = append(notes, "No explicit issues detected; preserve turn as baseline context.")
	}
	return notes
}

func turnHasError(turn run.TurnRecord) bool {
	for _, iter := range turn.Iterations {
		for _, call := range iter.ToolCalls {
			if strings.EqualFold(strings.TrimSpace(call.Status), "error") {
				return true
			}
		}
	}
	return false
}

func countToolCalls(turn run.TurnRecord) int {
	total := 0
	for _, iter := range turn.Iterations {
		total += len(iter.ToolCalls)
	}
	return total
}

func turnToolNames(turn run.TurnRecord) []string {
	set := map[string]struct{}{}
	for _, iter := range turn.Iterations {
		for _, call := range iter.ToolCalls {
			name := strings.TrimSpace(call.Name)
			if name == "" {
				continue
			}
			set[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func turnToolResults(turn run.TurnRecord) []string {
	out := make([]string, 0, 4)
	for _, iter := range turn.Iterations {
		for _, call := range iter.ToolCalls {
			text := strings.TrimSpace(call.ResultRef.Text)
			if text == "" {
				continue
			}
			out = append(out, truncate(text, 180))
			if len(out) >= 6 {
				return out
			}
		}
	}
	return out
}

func buildEmbeddingText(turn run.TurnRecord, labels, learnings []string, todos TodoStats, provider Provider) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("provider=%s ", provider))
	if prompt := strings.TrimSpace(turn.Prompt); prompt != "" {
		sb.WriteString("prompt=")
		sb.WriteString(prompt)
		sb.WriteString(" ")
	}
	if final := strings.TrimSpace(turn.FinalOutput.Text); final != "" {
		sb.WriteString("final=")
		sb.WriteString(final)
		sb.WriteString(" ")
	}
	if len(labels) > 0 {
		sb.WriteString("labels=")
		sb.WriteString(strings.Join(labels, ","))
		sb.WriteString(" ")
	}
	if len(learnings) > 0 {
		sb.WriteString("learn=")
		sb.WriteString(strings.Join(learnings, " | "))
		sb.WriteString(" ")
	}
	sb.WriteString(fmt.Sprintf("todos total=%d pending=%d active=%d completed=%d",
		todos.Total, todos.Pending, todos.InProgress, todos.Completed))
	return strings.TrimSpace(sb.String())
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		fallback := sha256.Sum256([]byte(fmt.Sprintf("%v", v)))
		msg := map[string]any{
			"error":  "marshal_failed",
			"digest": fmt.Sprintf("%x", fallback[:8]),
		}
		b, _ = json.Marshal(msg)
	}
	return json.RawMessage(b)
}

// BinaryDigest returns a stable digest for embedding vectors.
func BinaryDigest(vec []float32) string {
	if len(vec) == 0 {
		return ""
	}
	raw := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(raw[i*4:], math.Float32bits(v))
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:8])
}
