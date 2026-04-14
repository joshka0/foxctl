package transcriptpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

// AnchoredDerivationResult is the mainline anchored-frame output for one parsed session.
type AnchoredDerivationResult struct {
	ConversationID string                               `json:"conversation_id"`
	Frames         []companion.AnchoredInteractionFrame `json:"frames"`
	Derivations    []companion.AnchoredMemoryDerivation `json:"derivations"`
}

// BuildAnchoredDerivations materializes a parsed transcript into the companion runtime
// and returns anchored frames plus conservative memory derivations.
func BuildAnchoredDerivations(ctx context.Context, parsed sourceimport.ParsedSession, frameLimit int) (AnchoredDerivationResult, error) {
	mem, closeFn, conversationID, err := buildConversationMemoryFromParsedSession(ctx, parsed)
	if err != nil {
		return AnchoredDerivationResult{}, err
	}
	defer func() {
		if closeFn != nil {
			_ = closeFn()
		}
	}()

	frames, err := mem.BuildAnchoredInteractionFrames(ctx, conversationID, frameLimit)
	if err != nil {
		return AnchoredDerivationResult{}, fmt.Errorf("build anchored frames: %w", err)
	}

	return AnchoredDerivationResult{
		ConversationID: conversationID,
		Frames:         frames,
		Derivations:    companion.DeriveMemoryCandidatesFromFrames(frames),
	}, nil
}

func buildConversationMemoryFromParsedSession(ctx context.Context, parsed sourceimport.ParsedSession) (*companion.ConversationMemory, func() error, string, error) {
	tmpFile, err := os.CreateTemp("", "foxctl-derive-memory-*.db")
	if err != nil {
		return nil, nil, "", fmt.Errorf("create temp sqlite path: %w", err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()

	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, dbdriver.DefaultSQLiteConfig(tmpPath), nil)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, nil, "", fmt.Errorf("open in-memory sqlite: %w", err)
	}
	wrappedClose := func() error {
		var firstErr error
		if closeFn != nil {
			firstErr = closeFn()
		}
		if err := os.Remove(tmpPath); firstErr == nil && err != nil && !os.IsNotExist(err) {
			firstErr = err
		}
		return firstErr
	}

	mem, err := companion.NewConversationMemory(db)
	if err != nil {
		_ = wrappedClose()
		return nil, nil, "", fmt.Errorf("new conversation memory: %w", err)
	}

	conversationID := "source:" + string(parsed.Provider) + ":" + strings.TrimSpace(parsed.SessionID)
	for idx, turn := range parsed.Turns {
		if err := appendParsedTurn(ctx, mem, conversationID, turn, idx); err != nil {
			_ = wrappedClose()
			return nil, nil, "", err
		}
	}

	if err := mem.EnsureHybridMode(ctx, conversationID); err != nil {
		_ = wrappedClose()
		return nil, nil, "", fmt.Errorf("ensure hybrid mode: %w", err)
	}
	if err := mem.BuildHybridContextLayers(ctx, conversationID); err != nil {
		_ = wrappedClose()
		return nil, nil, "", fmt.Errorf("build hybrid context layers: %w", err)
	}
	return mem, wrappedClose, conversationID, nil
}

func appendParsedTurn(ctx context.Context, mem *companion.ConversationMemory, conversationID string, turn run.TurnRecord, turnIndex int) error {
	baseTime := turn.CreatedAt.UTC()
	if baseTime.IsZero() {
		baseTime = time.Now().UTC().Add(time.Duration(turnIndex) * time.Second)
	}

	prompt := companion.NormalizeTranscriptTurnText(turn.Prompt)
	if prompt == "" && companion.IsTranscriptControlText(turn.Prompt) {
		return nil
	}
	if prompt != "" {
		if err := mem.AppendTurn(ctx, companion.ConversationTurn{
			ConversationID: conversationID,
			Role:           "user",
			Content:        prompt,
			CreatedAt:      baseTime,
		}); err != nil {
			return fmt.Errorf("append imported user turn %s: %w", turn.ID, err)
		}
	}

	for _, iter := range turn.Iterations {
		for _, tool := range iter.ToolCalls {
			callPayload, _ := json.Marshal(map[string]any{
				"name":    tool.Name,
				"summary": truncateInline(string(tool.ArgsJSON), 120),
				"status":  tool.Status,
			})
			_ = mem.InsertEvent(ctx, &companion.ConversationEvent{
				ConversationID: conversationID,
				EventType:      companion.EventTypeToolCall,
				ToolName:       tool.Name,
				ToolRunID:      tool.CallID,
				PayloadJSON:    string(callPayload),
				TokenCount:     0,
			})

			resultText := strings.TrimSpace(tool.ResultRef.Text)
			if resultText == "" && strings.TrimSpace(tool.Status) == "" {
				continue
			}
			resultPayload, _ := json.Marshal(map[string]any{
				"summary": truncateInline(firstNonEmpty(resultText, tool.Status), 160),
				"status":  tool.Status,
			})
			_ = mem.InsertEvent(ctx, &companion.ConversationEvent{
				ConversationID: conversationID,
				EventType:      companion.EventTypeToolResult,
				ToolName:       tool.Name,
				ToolRunID:      tool.CallID,
				PayloadJSON:    string(resultPayload),
				TokenCount:     0,
			})
		}
	}

	assistantText := companion.NormalizeTranscriptTurnText(turn.FinalOutput.Text)
	if assistantText != "" {
		assistantTime := turn.UpdatedAt.UTC()
		if assistantTime.IsZero() || !assistantTime.After(baseTime) {
			assistantTime = baseTime.Add(time.Millisecond)
		}
		if err := mem.AppendTurn(ctx, companion.ConversationTurn{
			ConversationID: conversationID,
			Role:           "assistant",
			Content:        assistantText,
			CreatedAt:      assistantTime,
		}); err != nil {
			return fmt.Errorf("append imported assistant turn %s: %w", turn.ID, err)
		}
	}

	return nil
}
