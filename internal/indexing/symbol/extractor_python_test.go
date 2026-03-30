package symbol

import (
	"context"
	"testing"
)

func TestPythonExtractorExtract(t *testing.T) {
	source := `class Greeter:
    '''Greets users.'''

    def greet(self):
        '''Return greeting.'''
        return "hi"

async def fetch():
    '''Fetch data.'''
    return 1

def add(a, b):
    '''Add numbers.'''
    return a + b
`

	extractor := NewPythonExtractor()
	syms, err := extractor.Extract(context.Background(), "app.py", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	expected := map[string]Kind{
		"Greeter": KindClass,
		"fetch":   KindFunction,
		"add":     KindFunction,
	}

	docs := map[string]string{}
	for _, sym := range syms {
		docs[sym.Name] = sym.Documentation
		if kind, ok := expected[sym.Name]; ok {
			if sym.Kind != kind {
				t.Errorf("symbol %s kind: expected %s, got %s", sym.Name, kind, sym.Kind)
			}
		}
	}

	if len(syms) != len(expected) {
		t.Errorf("expected %d symbols, got %d", len(expected), len(syms))
	}
	for name := range expected {
		if _, ok := docs[name]; !ok {
			t.Errorf("missing symbol: %s", name)
		}
	}

	if docs["Greeter"] != "Greets users." {
		t.Errorf("unexpected doc for Greeter: %q", docs["Greeter"])
	}
	if docs["fetch"] != "Fetch data." {
		t.Errorf("unexpected doc for fetch: %q", docs["fetch"])
	}
	if docs["add"] != "Add numbers." {
		t.Errorf("unexpected doc for add: %q", docs["add"])
	}
}

func TestPythonExtractorExtractCalls(t *testing.T) {
	source := `def helper():
    return None

def add(a, b):
    helper()
    client.run()
    return a + b
`

	extractor := NewPythonExtractor()
	syms, err := extractor.Extract(context.Background(), "app.py", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var add Symbol
	for _, sym := range syms {
		if sym.Name == "add" {
			add = sym
			break
		}
	}
	if add.Name == "" {
		t.Fatalf("missing add symbol")
	}

	calls, err := extractor.ExtractCalls(context.Background(), add, []byte(source))
	if err != nil {
		t.Fatalf("extract calls: %v", err)
	}

	want := map[string]bool{
		"helper": true,
		"run":    true,
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
			t.Errorf("expected call %q in %v", name, calls)
		}
	}
}
