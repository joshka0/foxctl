package symbol

import (
	"reflect"
	"testing"
)

func TestSemanticAnchorHintsParsesValidAnchors(t *testing.T) {
	got := semanticAnchorHints("example-repo", `
// [[risk:agent-terminal-desync]]
// [[invariant:no-send-without-read]]
// [[invariant:no-send-without-read]]
`)
	want := []string{"invariant:no-send-without-read", "risk:agent-terminal-desync"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("semanticAnchorHints()=%v want %v", got, want)
	}
}

func TestSemanticAnchorHintsSkipsInvalidAnchors(t *testing.T) {
	got := semanticAnchorHints("example-repo", `
// [[doc:https://example.com/raw?token=abc]]
// [[unknown:thing]]
`)
	if len(got) != 0 {
		t.Fatalf("semanticAnchorHints()=%v want empty", got)
	}
}
