package searchquery

import "testing"

func TestParseQuery(t *testing.T) {
	t.Run("phrases and path hints", func(t *testing.T) {
		q := ParseQuery(`find user session for "login flow" path:internal/auth/login.go getUserSession`)
		if q.Raw == "" {
			t.Fatal("expected raw query")
		}

		if len(q.Phrases) != 1 {
			t.Fatalf("phrases = %d, want 1", len(q.Phrases))
		}
		if q.Phrases[0].Text != "login flow" {
			t.Fatalf("phrase %q, want %q", q.Phrases[0].Text, "login flow")
		}

		if len(q.PathHints) != 1 {
			t.Fatalf("path hints = %d, want 1", len(q.PathHints))
		}
		if q.PathHints[0].Path != "internal/auth/login.go" {
			t.Fatalf("path hint %q, want %q", q.PathHints[0].Path, "internal/auth/login.go")
		}

		if !containsTerm(q.Terms, "find") || !containsTerm(q.Terms, "user") || !containsTerm(q.Terms, "session") {
			t.Fatalf("terms missing expected values: %#v", q.Terms)
		}

		if !containsIdentifier(q.Identifiers, "getusersession", IdentifierKindCamel) {
			t.Fatalf("expected normalized camel identifier, got %#v", q.Identifiers)
		}
	})

	t.Run("path hint with quoted value", func(t *testing.T) {
		q := ParseQuery(`path:"internal/auth/handler.go" session`)
		if len(q.PathHints) != 1 || q.PathHints[0].Path != "internal/auth/handler.go" {
			t.Fatalf("path hint parse mismatch: %#v", q.PathHints)
		}
		if !containsTerm(q.Terms, "session") {
			t.Fatalf("expected term session, got %#v", q.Terms)
		}
	})
}

func TestComposeLexicalQuery(t *testing.T) {
	plan := QueryPlan{
		Raw:         "legacy raw query",
		Terms:       []string{"login", "session"},
		Identifiers: []Identifier{{Value: "getUserSession", Kind: IdentifierKindCamel}},
		Phrases:     []Phrase{{Text: "login flow"}},
		PathHints:   []PathHint{{Path: "internal/auth"}},
	}

	got := ComposeLexicalQuery(plan)
	expected := `path:internal/auth "login flow" getUserSession login session`
	if got != expected {
		t.Fatalf("compose query = %q, want %q", got, expected)
	}
}

func TestScoreText(t *testing.T) {
	plan := ParseQuery(`auth user "login flow" path:internal/auth/login.go authenticateUser`)
	text := `The login flow for user and authenticateUser is defined in internal/auth/login.go and is easy to search.`
	s := ScoreText(plan, text)
	if s.Score < 0.95 {
		t.Fatalf("score = %v, want >= 0.95", s.Score)
	}

	if s.TermMatches == 0 {
		t.Fatalf("expected at least one term match, got %#v", s)
	}
	if s.PhraseMatches != 1 {
		t.Fatalf("phrase matches = %d, want 1", s.PhraseMatches)
	}
	if s.PathHintMatches != 1 {
		t.Fatalf("path hint matches = %d, want 1", s.PathHintMatches)
	}
}

func TestScoreTextZeroWhenNoMatch(t *testing.T) {
	plan := ParseQuery(`auth login`)
	s := ScoreText(plan, `No relevant content in this file`)
	if s.Score != 0 {
		t.Fatalf("score = %v, want 0", s.Score)
	}
}

func TestStableHelpers(t *testing.T) {
	plan := ParseQuery(`auth "login flow" path:internal/auth path:internal/auth Login`)

	gotTerms := plan.StableTerms()
	if len(gotTerms) < 2 || gotTerms[0] != "auth" || gotTerms[1] != "login" {
		t.Fatalf("stable terms mismatch: %#v", gotTerms)
	}
	if got := plan.StablePathHints(); len(got) != 1 {
		t.Fatalf("stable path hints = %#v", got)
	}
	if got := plan.StablePhrases(); len(got) != 1 || got[0].Text != "login flow" {
		t.Fatalf("stable phrases = %#v", got)
	}
}

func TestExtractionHelpers(t *testing.T) {
	query := `find "login flow" path:src/service/user.go getUserSession`

	if !containsTerm(ExtractTerms(query), "find") || !containsTerm(ExtractTerms(query), "getusersession") {
		t.Fatalf("extract terms mismatch: %#v", ExtractTerms(query))
	}

	ids := ExtractIdentifiers(query)
	if len(ids) == 0 || !containsIdentifier(ids, "getusersession", IdentifierKindCamel) {
		t.Fatalf("extract identifiers mismatch: %#v", ids)
	}

	phrases := ExtractPhrases(query)
	if len(phrases) != 1 || phrases[0].Text != "login flow" {
		t.Fatalf("extract phrases mismatch: %#v", phrases)
	}

	hints := ExtractPathHints(query)
	if len(hints) != 1 || hints[0].Path != "src/service/user.go" {
		t.Fatalf("extract path hints mismatch: %#v", hints)
	}
}

func containsTerm(terms []string, value string) bool {
	for _, term := range terms {
		if term == value {
			return true
		}
	}
	return false
}

func containsIdentifier(ids []Identifier, value string, kind IdentifierKind) bool {
	for _, id := range ids {
		if id.Value == value && id.Kind == kind {
			return true
		}
	}
	return false
}
