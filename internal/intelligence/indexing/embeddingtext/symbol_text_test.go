package embeddingtext

import (
	"strings"
	"testing"
)

func TestBuildSymbolEmbeddingText(t *testing.T) {
	info := SymbolInfo{
		Name:      "SearchHybrid",
		Kind:      "function",
		Package:   "internal/storage/memory",
		Signature: "func(ctx, query, vec, workspace, limit) ([]SearchResult, error)",
		Doc:       "SearchHybrid performs combined BM25 + vector search.",
		Calls:     []string{"SearchBM25", "SearchVector"},
	}

	text := BuildSymbolEmbeddingText(info, DefaultSymbolTextOptionsSummaryOnly())
	if !strings.Contains(text, "[function] SearchHybrid") {
		t.Fatalf("expected header, got: %s", text)
	}
	if !strings.Contains(text, "Signature:") {
		t.Fatalf("expected signature, got: %s", text)
	}
	if !strings.Contains(text, "Documentation:") {
		t.Fatalf("expected documentation, got: %s", text)
	}
	if strings.Contains(text, "Calls:") {
		t.Fatalf("did not expect calls in summary-only mode: %s", text)
	}
}

func TestBuildSymbolEmbeddingText_DedupSortCalls(t *testing.T) {
	info := SymbolInfo{
		Name:  "Example",
		Kind:  "function",
		Calls: []string{"B", "A", "B"},
	}

	text := BuildSymbolEmbeddingText(info, DefaultSymbolTextOptionsDocEnriched())
	if !strings.Contains(text, "Calls: A, B") {
		t.Fatalf("expected sorted calls, got: %s", text)
	}
}

func TestBuildSymbolEmbeddingText_IncludesAliases(t *testing.T) {
	info := SymbolInfo{
		Name:    "Jido.AgentServer.SignalRouter",
		Kind:    "class",
		Aliases: []string{"jido agent server signal router", "signal_router"},
	}

	text := BuildSymbolEmbeddingText(info, DefaultSymbolTextOptionsDocEnriched())
	if !strings.Contains(text, "Aliases:") {
		t.Fatalf("expected aliases, got: %s", text)
	}
	if !strings.Contains(text, "signal_router") {
		t.Fatalf("expected alias value, got: %s", text)
	}
}

func TestBuildSymbolEmbeddingText_IncludesSemanticAnchors(t *testing.T) {
	info := SymbolInfo{
		Name:            "Guard",
		Kind:            "function",
		Doc:             "Guard protects terminal writes.",
		SemanticAnchors: []string{"risk:agent-terminal-desync", "invariant:no-send-without-read"},
	}

	text, metrics := BuildSymbolEmbeddingTextWithMetrics(info, DefaultSymbolTextOptionsDocEnriched())
	if !strings.Contains(text, "Semantic anchors: invariant:no-send-without-read, risk:agent-terminal-desync") {
		t.Fatalf("expected sorted semantic anchors, got: %s", text)
	}
	if metrics.SemanticAnchorCount != 2 {
		t.Fatalf("semantic anchor count=%d want 2", metrics.SemanticAnchorCount)
	}
}

func TestBuildSymbolEmbeddingText_StripsCommentsFromSourceButKeepsDoc(t *testing.T) {
	info := SymbolInfo{
		Name:      "CreateUser",
		Kind:      "function",
		FilePath:  "src/users.ts",
		Signature: "export function CreateUser(input: Input)",
		Doc:       "CreateUser validates and stores users.",
		Code: `// noisy chunk label that should not embed
export function CreateUser(input: Input) {
  const label = "Chunk 6/15 of file";
  // implementation detail comment
  return save(input); /* trailing implementation note */
}`,
	}

	text, metrics := BuildSymbolEmbeddingTextWithMetrics(info, DefaultSymbolTextOptionsDocEnriched())
	if strings.Contains(text, "noisy chunk label") || strings.Contains(text, "implementation detail comment") || strings.Contains(text, "trailing implementation note") {
		t.Fatalf("expected comments to be stripped from source, got: %s", text)
	}
	if !strings.Contains(text, "Documentation: CreateUser validates and stores users.") {
		t.Fatalf("expected extracted documentation metadata to remain, got: %s", text)
	}
	if !strings.Contains(text, `const label = "Chunk 6/15 of file";`) {
		t.Fatalf("expected string literals to be preserved, got: %s", text)
	}
	if metrics.StrippedSourceChars >= metrics.SourceChars {
		t.Fatalf("expected stripped source to be smaller: %+v", metrics)
	}
	if metrics.EmbeddingTextChars != len(text) {
		t.Fatalf("embedding text chars mismatch: %+v text=%q", metrics, text)
	}
}

func TestBuildSymbolEmbeddingText_IncludesClassMembers(t *testing.T) {
	info := SymbolInfo{
		Name:     "UserCard",
		Kind:     "class",
		FilePath: "src/user_card.ts",
		Code: `export class UserCard {
  // display state
  private displayName: string;
  readonly avatarUrl?: string;
  constructor(user: User) {}
  render() {
    return this.displayName;
  }
}`,
	}

	text, metrics := BuildSymbolEmbeddingTextWithMetrics(info, DefaultSymbolTextOptionsDocEnriched())
	if !strings.Contains(text, "Members:") {
		t.Fatalf("expected member hints, got: %s", text)
	}
	memberLine := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Members:") {
			memberLine = line
			break
		}
	}
	for _, want := range []string{"avatarUrl", "displayName", "render"} {
		if !strings.Contains(memberLine, want) {
			t.Fatalf("expected member %q in line: %s", want, memberLine)
		}
	}
	if strings.Contains(memberLine, "constructor") {
		t.Fatalf("did not expect constructor as member hint: %s", memberLine)
	}
	if metrics.ExtractedFieldCount < 3 {
		t.Fatalf("expected extracted field metrics, got: %+v", metrics)
	}
}

func TestBuildSymbolAliases(t *testing.T) {
	info := SymbolInfo{
		Name:     "Jido.AgentServer.SignalRouter",
		FilePath: "lib/jido/agent_server/signal_router.ex",
		Package:  "lib/jido/agent_server",
	}

	aliases := BuildSymbolAliases(info)
	joined := strings.Join(aliases, "\n")
	for _, want := range []string{"jido agent server signal router", "signal_router", "agent_server signal_router"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected alias %q in %v", want, aliases)
		}
	}
}

func TestProfileSymbolSource_GoTSPythonUseSameAPI(t *testing.T) {
	tests := []struct {
		name         string
		info         SymbolInfo
		wantMembers  []string
		dropMembers  []string
		dropFragment string
	}{
		{
			name: "go struct",
			info: SymbolInfo{
				Kind:     "struct",
				FilePath: "config.go",
				Code: `type Config struct {
  // endpoint comment
  Endpoint string
  RetryCount, TimeoutSeconds int
}`,
			},
			wantMembers:  []string{"Endpoint", "RetryCount", "TimeoutSeconds"},
			dropMembers:  []string{"type"},
			dropFragment: "endpoint comment",
		},
		{
			name: "typescript class",
			info: SymbolInfo{
				Kind:     "class",
				FilePath: "user_card.ts",
				Code: `class UserCard {
  // display state
  private displayName: string;
  readonly avatarUrl?: string;
}`,
			},
			wantMembers:  []string{"avatarUrl", "displayName"},
			dropMembers:  []string{"class"},
			dropFragment: "display state",
		},
		{
			name: "python class",
			info: SymbolInfo{
				Kind:     "class",
				FilePath: "user_card.py",
				Code: `class UserCard:
    """rendering state"""
    def __init__(self, user):
        self.display_name = user.name
        self.avatar_url = user.avatar_url  # optional
`,
			},
			wantMembers:  []string{"avatar_url", "display_name"},
			dropFragment: "rendering state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profiled := profileSymbolSource(tt.info)
			if strings.Contains(profiled.Code, tt.dropFragment) {
				t.Fatalf("expected cleaned code to drop %q: %s", tt.dropFragment, profiled.Code)
			}
			joined := strings.Join(profiled.Members, "\n")
			for _, want := range tt.wantMembers {
				if !strings.Contains(joined, want) {
					t.Fatalf("expected member %q in %v", want, profiled.Members)
				}
			}
			for _, drop := range tt.dropMembers {
				if strings.Contains(joined, drop) {
					t.Fatalf("did not expect member %q in %v", drop, profiled.Members)
				}
			}
		})
	}
}
