package symbol

import (
	"context"
	"testing"
)

func TestRustExtractorExtract(t *testing.T) {
	source := `/// Greets users.
pub struct Greeter;

impl Greeter {
    /// Return greeting status.
    pub fn greet(&self) -> bool {
        helper();
        self.render();
        true
    }

    fn render(&self) {}
}

fn helper() {}
`

	extractor := NewRustExtractor()
	syms, err := extractor.Extract(context.Background(), "src/lib.rs", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	expected := map[string]Kind{
		"Greeter":        KindStruct,
		"Greeter.greet":  KindMethod,
		"Greeter.render": KindMethod,
		"helper":         KindFunction,
	}

	got := map[string]Kind{}
	docs := map[string]string{}
	for _, sym := range syms {
		got[sym.Name] = sym.Kind
		docs[sym.Name] = sym.Documentation
	}
	if len(got) != len(expected) {
		t.Fatalf("len(symbols)=%d want %d (%#v)", len(got), len(expected), got)
	}
	for name, kind := range expected {
		if got[name] != kind {
			t.Fatalf("symbol %s kind=%s want %s (all=%#v)", name, got[name], kind, got)
		}
	}
	if docs["Greeter"] != "Greets users." {
		t.Fatalf("Greeter doc=%q", docs["Greeter"])
	}
	if docs["Greeter.greet"] != "Return greeting status." {
		t.Fatalf("Greeter.greet doc=%q", docs["Greeter.greet"])
	}
}

func TestRustExtractorExtractCalls(t *testing.T) {
	source := `impl Greeter {
    pub fn greet(&self) -> bool {
        helper();
        self.render();
        true
    }

    fn render(&self) {}
}

fn helper() {}
`

	extractor := NewRustExtractor()
	syms, err := extractor.Extract(context.Background(), "src/lib.rs", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var greet Symbol
	for _, sym := range syms {
		if sym.Name == "Greeter.greet" {
			greet = sym
			break
		}
	}
	if greet.Name == "" {
		t.Fatalf("missing Greeter.greet symbol")
	}

	calls, err := extractor.ExtractCalls(context.Background(), greet, []byte(source))
	if err != nil {
		t.Fatalf("extract calls: %v", err)
	}
	want := map[string]bool{
		"helper": true,
		"render": true,
	}
	for name := range want {
		found := false
		for _, call := range calls {
			if call == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected call %q in %v", name, calls)
		}
	}
}
