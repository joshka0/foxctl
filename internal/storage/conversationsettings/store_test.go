package conversationsettings

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

func TestStore_PatchAndGet(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	convID := "conv-1"

	_, err = store.Get(ctx, convID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	provider := "openrouter"
	model := "openai/gpt-4o-mini"
	execMode := "story"
	presenceEnabled := false
	tools := []string{"rlm_context_query", " rlm_context_query ", "rlm_context_put", ""}

	got, err := store.Patch(ctx, convID, Patch{
		LLMProvider:     &provider,
		LLMModel:        &model,
		ExecMode:        &execMode,
		ToolsAllow:      &tools,
		PresenceEnabled: &presenceEnabled,
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if got.ConversationID != convID {
		t.Fatalf("ConversationID: got %q", got.ConversationID)
	}
	if got.LLMProvider != provider {
		t.Fatalf("LLMProvider: got %q", got.LLMProvider)
	}
	if got.LLMModel != model {
		t.Fatalf("LLMModel: got %q", got.LLMModel)
	}
	if got.ExecMode != execMode {
		t.Fatalf("ExecMode: got %q", got.ExecMode)
	}
	if len(got.ToolsAllow) != 2 || got.ToolsAllow[0] != "rlm_context_put" || got.ToolsAllow[1] != "rlm_context_query" {
		t.Fatalf("ToolsAllow: got %#v", got.ToolsAllow)
	}
	if got.UpdatedAt == "" {
		t.Fatalf("UpdatedAt: expected non-empty")
	}
	if got.PresenceEnabled == nil || *got.PresenceEnabled {
		t.Fatalf("PresenceEnabled: got %#v", got.PresenceEnabled)
	}
	if got.IsPresenceEnabled() {
		t.Fatalf("IsPresenceEnabled expected false")
	}

	roundTrip, err := store.Get(ctx, convID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if roundTrip.LLMProvider != provider || roundTrip.LLMModel != model || roundTrip.ExecMode != execMode {
		t.Fatalf("roundTrip mismatch: %#v", roundTrip)
	}
	if roundTrip.PresenceEnabled == nil || *roundTrip.PresenceEnabled {
		t.Fatalf("roundTrip PresenceEnabled mismatch: %#v", roundTrip.PresenceEnabled)
	}
	if len(roundTrip.ToolsAllow) != 2 || roundTrip.ToolsAllow[0] != "rlm_context_put" || roundTrip.ToolsAllow[1] != "rlm_context_query" {
		t.Fatalf("roundTrip ToolsAllow: got %#v", roundTrip.ToolsAllow)
	}
}

func TestSettings_IsPresenceEnabled(t *testing.T) {
	var nilSettings *Settings
	if !nilSettings.IsPresenceEnabled() {
		t.Fatalf("nil settings should default to enabled")
	}

	unset := &Settings{}
	if !unset.IsPresenceEnabled() {
		t.Fatalf("unset presence should default to enabled")
	}

	enabled := true
	explicitOn := &Settings{PresenceEnabled: &enabled}
	if !explicitOn.IsPresenceEnabled() {
		t.Fatalf("explicit true should be enabled")
	}

	disabled := false
	explicitOff := &Settings{PresenceEnabled: &disabled}
	if explicitOff.IsPresenceEnabled() {
		t.Fatalf("explicit false should be disabled")
	}
}

func TestStore_PatchInvalidExecMode(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	convID := "conv-1"
	execMode := "weird"
	_, err = store.Patch(ctx, convID, Patch{ExecMode: &execMode})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}

func TestStore_GetRejectsCorruptPersistedSettings(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		corrupt    func(context.Context, *sqlStore, string) error
		wantErrSub string
	}{
		{
			name: "tools allow null json",
			corrupt: func(ctx context.Context, store *sqlStore, convID string) error {
				_, err := store.db.ExecContext(
					ctx,
					`UPDATE conversation_settings SET tools_allow_json = ? WHERE conversation_id = ?`,
					"null",
					convID,
				)
				return err
			},
			wantErrSub: "tools_allow_json",
		},
		{
			name: "exec mode unknown",
			corrupt: func(ctx context.Context, store *sqlStore, convID string) error {
				_, err := store.db.ExecContext(
					ctx,
					`UPDATE conversation_settings SET exec_mode = ? WHERE conversation_id = ?`,
					"sideways",
					convID,
				)
				return err
			},
			wantErrSub: "exec_mode",
		},
		{
			name: "presence enabled not boolean",
			corrupt: func(ctx context.Context, store *sqlStore, convID string) error {
				_, err := store.db.ExecContext(
					ctx,
					`UPDATE conversation_settings SET presence_enabled = ? WHERE conversation_id = ?`,
					2,
					convID,
				)
				return err
			},
			wantErrSub: "presence_enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			storeIface, err := Open(ctx, t.TempDir())
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer storeIface.Close()
			store := storeIface.(*sqlStore)

			convID := "conv-corrupt"
			execMode := "story"
			presenceEnabled := true
			tools := []string{"rlm_context_query"}
			if _, err := store.Patch(ctx, convID, Patch{
				ExecMode:        &execMode,
				PresenceEnabled: &presenceEnabled,
				ToolsAllow:      &tools,
			}); err != nil {
				t.Fatalf("Patch: %v", err)
			}
			if err := tc.corrupt(ctx, store, convID); err != nil {
				t.Fatalf("corrupt row: %v", err)
			}

			_, err = store.Get(ctx, convID)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("Get error = %v, want ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("Get error = %v, want substring %q", err, tc.wantErrSub)
			}
		})
	}
}

func TestNormalizeAllowlistProperty(t *testing.T) {
	prop := func(input []string) bool {
		got := normalizeAllowlist(input)
		seen := make(map[string]struct{}, len(got))
		for i, value := range got {
			if value == "" || strings.TrimSpace(value) != value {
				return false
			}
			if _, ok := seen[value]; ok {
				return false
			}
			if i > 0 && got[i-1] > value {
				return false
			}
			seen[value] = struct{}{}
		}
		return reflect.DeepEqual(got, normalizeAllowlist(got))
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("normalize allowlist property failed: %v", err)
	}
}
