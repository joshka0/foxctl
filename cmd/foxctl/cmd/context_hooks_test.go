package cmd

import "testing"

func TestMergeACAHooks(t *testing.T) {
	settings := claudeSettings{
		Hooks: map[string][]claudeHookMatcher{
			"Stop": {
				{
					Matcher: "",
					Hooks: []claudeHookCommand{
						{Type: "command", Command: "$CLAUDE_PROJECT_DIR/configs/hooks/session-end.sh", Timeout: 15},
					},
				},
			},
		},
	}

	installed := mergeACAHooks(&settings)
	if installed["Stop"] {
		t.Fatalf("expected stop hook to be recognized as existing")
	}
	if !installed["SessionStart"] || !installed["SubagentStop"] {
		t.Fatalf("expected sessionstart and subagentstop hooks to be added: %#v", installed)
	}
	if len(settings.Hooks["SessionStart"]) != 1 {
		t.Fatalf("sessionstart hooks=%d", len(settings.Hooks["SessionStart"]))
	}
	if len(settings.Hooks["SubagentStop"]) != 1 {
		t.Fatalf("subagentstop hooks=%d", len(settings.Hooks["SubagentStop"]))
	}
	if len(settings.Hooks["Stop"]) != 1 {
		t.Fatalf("stop hooks=%d", len(settings.Hooks["Stop"]))
	}
}
