package symbol

import (
	"context"
	"strings"
	"testing"
)

func TestCSharpExtractorExtractsSymbolsAndCalls(t *testing.T) {
	source := []byte(`namespace Overcharge.Client.Core;

public interface IActionSink
{
    void Emit(ActionFrame frame);
}

public class ControllerDriver
{
    private readonly ControllerPolicy policy;
    public int Count { get; set; }

    public ControllerDriver(ControllerPolicy policy)
    {
        this.policy = policy;
    }

    public FrameData Tick(InputState input)
    {
        return policy.Evaluate(input);
    }
}

public record struct FrameData(int Tick, float X);
`)
	extractor := NewCSharpExtractor()
	syms, err := extractor.Extract(context.Background(), "client/Scripts/Core/ControllerDriver.cs", source)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	names := map[string]Symbol{}
	for _, sym := range syms {
		names[sym.Name] = sym
		if sym.Language != "csharp" {
			t.Fatalf("symbol %s language=%q want csharp", sym.Name, sym.Language)
		}
	}
	for _, want := range []string{"IActionSink", "ControllerDriver", "ControllerDriver.Count", "ControllerDriver.Tick", "FrameData"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("missing C# symbol %q in %#v", want, symbolNames(syms))
		}
	}

	calls, err := extractor.ExtractCalls(context.Background(), names["ControllerDriver.Tick"], source)
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}
	if !containsExactString(calls, "Evaluate") {
		t.Fatalf("calls=%v want Evaluate", calls)
	}
}

func TestCSharpSymbolKeyDisambiguatesPrivateSymbols(t *testing.T) {
	if got := CSharpSymbolKey("ControllerDriver.Tick", true, "ControllerDriver.cs"); got != "ControllerDriver.Tick" {
		t.Fatalf("exported key=%q", got)
	}
	if got := CSharpSymbolKey("ControllerDriver.policy", false, "ControllerDriver.cs"); got != "ControllerDriver.cs/ControllerDriver.policy" {
		t.Fatalf("private key=%q", got)
	}
}

func symbolNames(symbols []Symbol) []string {
	out := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		out = append(out, sym.Name)
	}
	return out
}

func containsExactString(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}
