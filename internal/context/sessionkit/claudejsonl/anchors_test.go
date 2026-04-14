package claudejsonl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractCodeBlocks(t *testing.T) {
	message := &Message{
		Type: "assistant",
		Message: mustMarshalJSON(t, map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{
					"type": "text",
					"text": "Go:\n```go\nfunc Hello() {}\n```\n\nTS:\n```typescript\nexport function boot() {}\n```\n\nBash:\n```bash\necho hello\n```",
				},
			},
		}),
	}

	anchors := ExtractCodeBlocks(message)
	if len(anchors) != 3 {
		t.Fatalf("expected 3 code blocks, got %d", len(anchors))
	}

	expectedLanguages := []string{"go", "typescript", "bash"}
	for index, anchor := range anchors {
		if anchor.Type != AnchorCodeBlock {
			t.Fatalf("expected code block anchor at index %d, got %q", index, anchor.Type)
		}
		if anchor.Meta.Language != expectedLanguages[index] {
			t.Fatalf("expected language %q at index %d, got %q", expectedLanguages[index], index, anchor.Meta.Language)
		}
		if anchor.Meta.ContentHash == "" {
			t.Fatalf("expected content hash at index %d", index)
		}
		if len(anchor.Meta.Preview) > codePreviewLen {
			t.Fatalf("preview too long at index %d: %d", index, len(anchor.Meta.Preview))
		}
	}

	expectedFirstHash := sha256.Sum256([]byte("func Hello() {}"))
	if anchors[0].Meta.ContentHash != hex.EncodeToString(expectedFirstHash[:]) {
		t.Fatalf("unexpected content hash for first block: %s", anchors[0].Meta.ContentHash)
	}
}

func TestExtractCommands(t *testing.T) {
	message := &Message{
		Type: "assistant",
		ToolUse: &ToolUseInfo{
			Name:  "Bash",
			Input: mustMarshalJSON(t, BashInput{Command: "go test ./..."}),
		},
		Message: mustMarshalJSON(t, map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{
					"type":  "tool_use",
					"name":  "Bash",
					"id":    "tool-bash-2",
					"input": map[string]any{"command": "make test"},
				},
				{
					"type":  "tool_use",
					"name":  "Read",
					"id":    "tool-read-1",
					"input": map[string]any{"file_path": "internal/context/sessionkit/claudejsonl/anchors.go"},
				},
				{
					"type":  "tool_use",
					"name":  "Bash",
					"id":    "tool-bash-3",
					"input": map[string]any{"command": "make test"},
				},
			},
		}),
	}

	anchors := ExtractCommands(message)
	if len(anchors) != 2 {
		t.Fatalf("expected 2 command anchors, got %d", len(anchors))
	}

	commands := map[string]bool{}
	for _, anchor := range anchors {
		if anchor.Type != AnchorCommand {
			t.Fatalf("expected anchor type %q, got %q", AnchorCommand, anchor.Type)
		}
		if !strings.EqualFold(anchor.Meta.Tool, "bash") {
			t.Fatalf("expected tool Bash, got %q", anchor.Meta.Tool)
		}
		commands[anchor.Content] = true
	}

	for _, command := range []string{"go test ./...", "make test"} {
		if !commands[command] {
			t.Fatalf("missing command anchor for %q", command)
		}
	}
}

func TestExtractFilePaths(t *testing.T) {
	message := &Message{
		Type: "assistant",
		Message: mustMarshalJSON(t, map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{
					"type": "text",
					"text": "Update internal/context/sessionkit/claudejsonl/anchors.go and /tmp/report.txt for debug output.",
				},
				{
					"type":  "tool_use",
					"name":  "Read",
					"id":    "tool-read",
					"input": map[string]any{"file_path": "internal/context/sessionkit/claudejsonl/anchors.go"},
				},
				{
					"type":  "tool_use",
					"name":  "Write",
					"id":    "tool-write",
					"input": map[string]any{"file_path": "/tmp/output.txt", "content": "x"},
				},
				{
					"type":  "tool_use",
					"name":  "Edit",
					"id":    "tool-edit",
					"input": map[string]any{"file_path": "internal/context/sessionkit/claudejsonl/extract.go", "old_string": "a", "new_string": "b"},
				},
				{
					"type":  "tool_use",
					"name":  "Glob",
					"id":    "tool-glob",
					"input": map[string]any{"pattern": "internal/**/*.go", "path": "internal/context/sessionkit"},
				},
				{
					"type":  "tool_use",
					"name":  "Grep",
					"id":    "tool-grep",
					"input": map[string]any{"pattern": "internal/context/sessionkit/claudejsonl/.*\\.go", "path": "internal/context/sessionkit/claudejsonl"},
				},
			},
		}),
	}

	anchors := ExtractFilePaths(message)
	if len(anchors) == 0 {
		t.Fatal("expected file path anchors")
	}

	pathSet := map[string]bool{}
	for _, anchor := range anchors {
		if anchor.Type != AnchorFilePath {
			t.Fatalf("expected file path anchor type, got %q", anchor.Type)
		}
		pathSet[anchor.Content] = true
	}

	expectedPaths := []string{
		"/tmp/output.txt",
		"/tmp/report.txt",
		"internal/**/*.go",
		"internal/context/sessionkit",
		"internal/context/sessionkit/claudejsonl/.*\\.go",
		"internal/context/sessionkit/claudejsonl/anchors.go",
		"internal/context/sessionkit/claudejsonl/extract.go",
		"internal/context/sessionkit/claudejsonl",
	}
	for _, expected := range expectedPaths {
		if !pathSet[expected] {
			t.Fatalf("missing expected path anchor %q", expected)
		}
	}
}

func TestExtractErrors(t *testing.T) {
	topLevel := &Message{
		Type: "tool_result",
		ToolUse: &ToolUseInfo{
			Name: "Bash",
			ID:   "tool-1",
		},
		ToolResult: &ToolResultInfo{
			ToolUseID: "tool-1",
			IsError:   true,
			Content:   "TypeError: invalid argument",
		},
	}

	topLevelAnchors := ExtractErrors(topLevel)
	if len(topLevelAnchors) != 1 {
		t.Fatalf("expected 1 top-level error anchor, got %d", len(topLevelAnchors))
	}
	if topLevelAnchors[0].Meta.Tool != "Bash" {
		t.Fatalf("expected tool Bash, got %q", topLevelAnchors[0].Meta.Tool)
	}
	if topLevelAnchors[0].Meta.ErrorType != "TypeError" {
		t.Fatalf("expected TypeError classification, got %q", topLevelAnchors[0].Meta.ErrorType)
	}

	longContent := "SyntaxError: " + strings.Repeat("x", errorPreviewLen+50)
	nested := &Message{
		Type: "user",
		Message: mustMarshalJSON(t, map[string]any{
			"role": "user",
			"content": []map[string]any{
				{
					"type":  "tool_use",
					"name":  "Read",
					"id":    "tool-2",
					"input": map[string]any{"file_path": "internal/context/sessionkit/claudejsonl/anchors.go"},
				},
				{
					"type":        "tool_result",
					"tool_use_id": "tool-2",
					"is_error":    true,
					"content":     longContent,
				},
			},
		}),
	}

	nestedAnchors := ExtractErrors(nested)
	if len(nestedAnchors) != 1 {
		t.Fatalf("expected 1 nested error anchor, got %d", len(nestedAnchors))
	}
	if nestedAnchors[0].Meta.Tool != "Read" {
		t.Fatalf("expected tool Read, got %q", nestedAnchors[0].Meta.Tool)
	}
	if nestedAnchors[0].Meta.ErrorType != "SyntaxError" {
		t.Fatalf("expected SyntaxError classification, got %q", nestedAnchors[0].Meta.ErrorType)
	}
	if len(nestedAnchors[0].Meta.Preview) != errorPreviewLen {
		t.Fatalf("expected preview length %d, got %d", errorPreviewLen, len(nestedAnchors[0].Meta.Preview))
	}
}

func TestExtractSymbols(t *testing.T) {
	message := &Message{
		Type: "assistant",
		Message: mustMarshalJSON(t, map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{
					"type": "text",
					"text": "File: internal/context/sessionkit/claudejsonl/anchors.go\n```go\ntype Runner struct{}\nconst DefaultName = \"runner\"\nvar Enabled = true\nfunc Build() {}\n```\n\nFile: internal/ui/app.ts\n```typescript\nexport interface Config {}\nexport class App {}\nexport function boot() {}\nexport const VERSION = \"1.0.0\"\n```",
				},
			},
		}),
	}

	anchors := ExtractSymbols(message)
	if len(anchors) == 0 {
		t.Fatal("expected symbol anchors")
	}

	type symbolExpectation struct {
		Name string
		Kind string
		Path string
	}

	expectations := []symbolExpectation{
		{Name: "Build", Kind: "func", Path: "internal/context/sessionkit/claudejsonl/anchors.go"},
		{Name: "Runner", Kind: "struct", Path: "internal/context/sessionkit/claudejsonl/anchors.go"},
		{Name: "DefaultName", Kind: "const", Path: "internal/context/sessionkit/claudejsonl/anchors.go"},
		{Name: "Enabled", Kind: "var", Path: "internal/context/sessionkit/claudejsonl/anchors.go"},
		{Name: "Config", Kind: "interface", Path: "internal/ui/app.ts"},
		{Name: "App", Kind: "class", Path: "internal/ui/app.ts"},
		{Name: "boot", Kind: "function", Path: "internal/ui/app.ts"},
		{Name: "VERSION", Kind: "export", Path: "internal/ui/app.ts"},
	}

	index := map[string]SliceAnchor{}
	for _, anchor := range anchors {
		if anchor.Type != AnchorSymbol {
			t.Fatalf("expected symbol anchor type, got %q", anchor.Type)
		}
		index[anchor.Meta.SymbolKind+"|"+anchor.Content] = anchor
	}

	for _, expected := range expectations {
		key := expected.Kind + "|" + expected.Name
		anchor, ok := index[key]
		if !ok {
			t.Fatalf("missing symbol %s", key)
		}
		if anchor.Meta.FilePath != expected.Path {
			t.Fatalf("expected path %q for %s, got %q", expected.Path, key, anchor.Meta.FilePath)
		}
	}
}

func TestExtractAllAnchors(t *testing.T) {
	message := &Message{
		Type: "assistant",
		ToolUse: &ToolUseInfo{
			Name:  "Bash",
			ID:    "tool-bash",
			Input: mustMarshalJSON(t, BashInput{Command: "go test ./..."}),
		},
		ToolResult: &ToolResultInfo{
			ToolUseID: "tool-bash",
			IsError:   true,
			Content:   "build failed",
		},
		Message: mustMarshalJSON(t, map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{
					"type": "text",
					"text": "Edit internal/context/sessionkit/claudejsonl/anchors.go\n```go\nfunc BuildAll() {}\n```",
				},
				{
					"type":  "tool_use",
					"name":  "Bash",
					"id":    "tool-bash-2",
					"input": map[string]any{"command": "go test ./..."},
				},
				{
					"type":  "tool_use",
					"name":  "Read",
					"id":    "tool-read",
					"input": map[string]any{"file_path": "internal/context/sessionkit/claudejsonl/anchors.go"},
				},
			},
		}),
	}

	anchors := ExtractAllAnchors(message)
	if len(anchors) == 0 {
		t.Fatal("expected anchors from ExtractAllAnchors")
	}

	typeSet := map[AnchorType]bool{}
	filePathCount := 0
	commandCount := 0
	for _, anchor := range anchors {
		typeSet[anchor.Type] = true
		if anchor.Type == AnchorFilePath && anchor.Content == "internal/context/sessionkit/claudejsonl/anchors.go" {
			filePathCount++
		}
		if anchor.Type == AnchorCommand && anchor.Content == "go test ./..." {
			commandCount++
		}
	}

	requiredTypes := []AnchorType{
		AnchorCodeBlock,
		AnchorCommand,
		AnchorFilePath,
		AnchorError,
		AnchorSymbol,
	}
	for _, required := range requiredTypes {
		if !typeSet[required] {
			t.Fatalf("missing anchor type %q", required)
		}
	}
	if filePathCount != 1 {
		t.Fatalf("expected deduped file path anchor count 1, got %d", filePathCount)
	}
	if commandCount != 1 {
		t.Fatalf("expected deduped command anchor count 1, got %d", commandCount)
	}
}

func TestExtractorEdgeCases(t *testing.T) {
	var nilMsg *Message
	if anchors := ExtractCodeBlocks(nilMsg); anchors != nil {
		t.Fatalf("expected nil anchors for nil message, got %v", anchors)
	}
	if anchors := ExtractCommands(nilMsg); anchors != nil {
		t.Fatalf("expected nil anchors for nil message, got %v", anchors)
	}
	if anchors := ExtractFilePaths(nilMsg); anchors != nil {
		t.Fatalf("expected nil anchors for nil message, got %v", anchors)
	}
	if anchors := ExtractErrors(nilMsg); anchors != nil {
		t.Fatalf("expected nil anchors for nil message, got %v", anchors)
	}
	if anchors := ExtractSymbols(nilMsg); anchors != nil {
		t.Fatalf("expected nil anchors for nil message, got %v", anchors)
	}

	malformed := &Message{
		Type:    "assistant",
		Message: json.RawMessage(`{"role":"assistant","content":[`),
		ToolUse: &ToolUseInfo{
			Name:  "Bash",
			Input: json.RawMessage(`{"command":`),
		},
	}

	if anchors := ExtractCodeBlocks(malformed); anchors != nil {
		t.Fatalf("expected nil anchors for malformed message, got %v", anchors)
	}
	if anchors := ExtractCommands(malformed); anchors != nil {
		t.Fatalf("expected nil anchors for malformed command payloads, got %v", anchors)
	}
	if anchors := ExtractAllAnchors(malformed); anchors != nil {
		t.Fatalf("expected nil anchors for malformed message in ExtractAllAnchors, got %v", anchors)
	}

	role, blocks, text, ok := parseNestedContent(malformed)
	if ok || role != "" || blocks != nil || text != "" {
		t.Fatalf("expected parseNestedContent to fail on malformed JSON")
	}
}

func mustMarshalJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test fixture: %v", err)
	}
	return data
}
