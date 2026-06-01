package run

import (
	"bytes"
	"strings"
	"testing"
	"testing/quick"
)

func TestEffectStatusesTerminalOnlyAfterDurableResult(t *testing.T) {
	t.Parallel()

	modelTests := []struct {
		status ModelEffectStatus
		want   bool
	}{
		{status: ModelEffectIntent, want: false},
		{status: ModelEffectSucceeded, want: true},
		{status: ModelEffectFailed, want: true},
		{status: "", want: false},
		{status: ModelEffectStatus("cancelled"), want: false},
	}
	for _, tt := range modelTests {
		tt := tt
		t.Run("model_"+string(tt.status), func(t *testing.T) {
			t.Parallel()

			if got := tt.status.IsTerminal(); got != tt.want {
				t.Fatalf("ModelEffectStatus(%q).IsTerminal()=%v want %v", tt.status, got, tt.want)
			}
		})
	}

	toolTests := []struct {
		status ToolEffectStatus
		want   bool
	}{
		{status: ToolEffectIntent, want: false},
		{status: ToolEffectSucceeded, want: true},
		{status: ToolEffectFailed, want: true},
		{status: "", want: false},
		{status: ToolEffectStatus("cancelled"), want: false},
	}
	for _, tt := range toolTests {
		tt := tt
		t.Run("tool_"+string(tt.status), func(t *testing.T) {
			t.Parallel()

			if got := tt.status.IsTerminal(); got != tt.want {
				t.Fatalf("ToolEffectStatus(%q).IsTerminal()=%v want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestTurnRequestStatusTerminalOnlyAfterCompletion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status TurnRequestStatus
		want   bool
	}{
		{status: TurnRequestRunning, want: false},
		{status: TurnRequestSucceeded, want: true},
		{status: TurnRequestFailed, want: true},
		{status: TurnRequestCanceled, want: true},
		{status: "", want: false},
		{status: TurnRequestStatus("cancelled"), want: false},
		{status: TurnRequestStatus("retrying"), want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()

			if got := tt.status.IsTerminal(); got != tt.want {
				t.Fatalf("TurnRequestStatus(%q).IsTerminal()=%v want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestTurnRequestRecordClonePropertyCopiesJSONPayloads(t *testing.T) {
	t.Parallel()

	property := func(outputRaw, errorRaw []byte) bool {
		record := TurnRequestRecord{
			RunID:      "run",
			RequestID:  "request",
			TurnID:     "turn",
			Status:     TurnRequestSucceeded,
			OutputJSON: append([]byte(nil), outputRaw...),
			ErrorJSON:  append([]byte(nil), errorRaw...),
		}
		clone := record.Clone()

		if !bytes.Equal(clone.OutputJSON, record.OutputJSON) || !bytes.Equal(clone.ErrorJSON, record.ErrorJSON) {
			return false
		}
		if len(record.OutputJSON) > 0 && &clone.OutputJSON[0] == &record.OutputJSON[0] {
			return false
		}
		if len(record.ErrorJSON) > 0 && &clone.ErrorJSON[0] == &record.ErrorJSON[0] {
			return false
		}

		if len(clone.OutputJSON) > 0 {
			clone.OutputJSON[0] ^= 0xff
			if bytes.Equal(clone.OutputJSON, record.OutputJSON) {
				return false
			}
		}
		if len(clone.ErrorJSON) > 0 {
			clone.ErrorJSON[0] ^= 0xff
			if bytes.Equal(clone.ErrorJSON, record.ErrorJSON) {
				return false
			}
		}

		return record.RunID == "run" &&
			record.RequestID == "request" &&
			record.TurnID == "turn" &&
			record.Status == TurnRequestSucceeded &&
			bytes.Equal(record.OutputJSON, outputRaw) &&
			bytes.Equal(record.ErrorJSON, errorRaw)
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatalf("turn request clone property failed: %v", err)
	}
}

func TestRunRecordClonesDeepCopyNestedMutableFields(t *testing.T) {
	t.Parallel()

	t.Run("turn record copies nested tool call args", func(t *testing.T) {
		t.Parallel()

		record := TurnRecord{
			ID: "turn-1",
			Iterations: []IterationRecord{{
				IterationIndex: 1,
				ToolCalls: []ToolCallRecord{{
					CallID:   "call-1",
					Name:     "tool/run",
					ArgsJSON: jsonBytes(`{"path":"a.go"}`),
				}},
			}},
		}

		clone := record.Clone()
		clone.Iterations[0].ToolCalls[0].ArgsJSON[0] = '{' ^ 0xff
		clone.Iterations[0].ToolCalls[0].CallID = "changed"

		if string(record.Iterations[0].ToolCalls[0].ArgsJSON) != `{"path":"a.go"}` {
			t.Fatalf("mutating clone args changed original: %s", record.Iterations[0].ToolCalls[0].ArgsJSON)
		}
		if record.Iterations[0].ToolCalls[0].CallID != "call-1" {
			t.Fatalf("mutating clone scalar changed original call id: %q", record.Iterations[0].ToolCalls[0].CallID)
		}
	})

	t.Run("episode record copies anchors", func(t *testing.T) {
		t.Parallel()

		record := EpisodeRecord{ID: "episode-1", AnchorRefs: []string{"turn:a", "artifact:b"}}
		clone := record.Clone()
		clone.AnchorRefs[0] = "changed"

		if got := record.AnchorRefs[0]; got != "turn:a" {
			t.Fatalf("mutating clone anchors changed original: %q", got)
		}
	})

	t.Run("narrative record copies top-level and claim anchors", func(t *testing.T) {
		t.Parallel()

		record := NarrativeRecord{
			SessionID:  "session-1",
			AnchorRefs: []string{"turn:a"},
			Claims: []NarrativeClaim{{
				Text:       "claim",
				AnchorRefs: []string{"artifact:b"},
			}},
		}
		clone := record.Clone()
		clone.AnchorRefs[0] = "changed-top"
		clone.Claims[0].AnchorRefs[0] = "changed-claim"
		clone.Claims[0].Text = "changed text"

		if got := record.AnchorRefs[0]; got != "turn:a" {
			t.Fatalf("mutating clone top-level anchors changed original: %q", got)
		}
		if got := record.Claims[0].AnchorRefs[0]; got != "artifact:b" {
			t.Fatalf("mutating clone claim anchors changed original: %q", got)
		}
		if got := record.Claims[0].Text; got != "claim" {
			t.Fatalf("mutating clone claim scalar changed original: %q", got)
		}
	})

	t.Run("scored artifact copies metadata json", func(t *testing.T) {
		t.Parallel()

		record := ScoredArtifact{Ref: "artifact-1", MetadataJSON: jsonBytes(`{"score":1}`)}
		clone := record.Clone()
		clone.MetadataJSON[0] = '{' ^ 0xff

		if string(record.MetadataJSON) != `{"score":1}` {
			t.Fatalf("mutating clone metadata changed original: %s", record.MetadataJSON)
		}
	})
}

func TestNormalizeTurnBackendCanonicalizesKnownBackends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw       TurnBackend
		want      TurnBackend
		supported bool
	}{
		{raw: "", want: TurnBackendLLMChat, supported: true},
		{raw: " llm_chat ", want: TurnBackendLLMChat, supported: true},
		{raw: "LLM_CHAT", want: TurnBackendLLMChat, supported: true},
		{raw: " rlm_repl ", want: TurnBackendRLMREPL, supported: true},
		{raw: "RLM_REPL", want: TurnBackendRLMREPL, supported: true},
		{raw: " custom_backend ", want: TurnBackend("custom_backend"), supported: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.raw), func(t *testing.T) {
			t.Parallel()

			if got := NormalizeTurnBackend(tt.raw); got != tt.want {
				t.Fatalf("NormalizeTurnBackend(%q)=%q want %q", tt.raw, got, tt.want)
			}
			if got := IsSupportedTurnBackend(tt.raw); got != tt.supported {
				t.Fatalf("IsSupportedTurnBackend(%q)=%v want %v", tt.raw, got, tt.supported)
			}
		})
	}
}

func jsonBytes(raw string) []byte {
	return []byte(raw)
}

func TestNormalizeTurnBackendPropertyIsTrimmedLowercaseAndIdempotent(t *testing.T) {
	t.Parallel()

	property := func(input string) bool {
		raw := strings.ToValidUTF8(input, "?")
		got := NormalizeTurnBackend(TurnBackend(raw))
		if got != NormalizeTurnBackend(got) {
			return false
		}
		trimmedLower := strings.ToLower(strings.TrimSpace(raw))
		switch trimmedLower {
		case "", string(TurnBackendLLMChat):
			return got == TurnBackendLLMChat && IsSupportedTurnBackend(TurnBackend(raw))
		case string(TurnBackendRLMREPL):
			return got == TurnBackendRLMREPL && IsSupportedTurnBackend(TurnBackend(raw))
		default:
			return string(got) == trimmedLower && !IsSupportedTurnBackend(TurnBackend(raw))
		}
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 256}); err != nil {
		t.Fatalf("turn backend normalization property failed: %v", err)
	}
}
