package contextbuilder

import "testing"

func TestResolveLayerBudget_DefaultNoSemantic(t *testing.T) {
	t.Parallel()

	got := resolveLayerBudget(LayeredRequest{MaxChars: 100}, false)
	if got.TotalChars != 100 {
		t.Fatalf("TotalChars=%d want 100", got.TotalChars)
	}
	if got.L2Chars != 20 || got.L1Chars != 25 || got.L0Chars != 55 || got.SemanticChars != 0 {
		t.Fatalf("unexpected budget: %+v", got)
	}
}

func TestResolveLayerBudget_DefaultWithSemantic(t *testing.T) {
	t.Parallel()

	got := resolveLayerBudget(LayeredRequest{MaxChars: 100}, true)
	if got.TotalChars != 100 {
		t.Fatalf("TotalChars=%d want 100", got.TotalChars)
	}
	if got.L2Chars != 20 || got.L1Chars != 25 || got.L0Chars != 45 || got.SemanticChars != 10 {
		t.Fatalf("unexpected budget: %+v", got)
	}
}

func TestResolveLayerBudget_OverridesAndRemainderToL0(t *testing.T) {
	t.Parallel()

	got := resolveLayerBudget(LayeredRequest{
		MaxChars: 100,
		Budget: &LayerBudget{
			L2Chars:       10,
			L1Chars:       15,
			L0Chars:       20,
			SemanticChars: 30,
		},
	}, true)
	if got.TotalChars != 100 {
		t.Fatalf("TotalChars=%d want 100", got.TotalChars)
	}
	if got.L2Chars != 10 || got.L1Chars != 15 || got.L0Chars != 45 || got.SemanticChars != 30 {
		t.Fatalf("unexpected budget: %+v", got)
	}
	if got.L2Chars+got.L1Chars+got.L0Chars+got.SemanticChars != got.TotalChars {
		t.Fatalf("budget sum mismatch: %+v", got)
	}
}

func TestResolveLayerBudget_ClampsToTotal(t *testing.T) {
	t.Parallel()

	got := resolveLayerBudget(LayeredRequest{
		MaxChars: 60,
		Budget: &LayerBudget{
			L2Chars:       50,
			L1Chars:       50,
			L0Chars:       50,
			SemanticChars: 50,
		},
	}, true)
	if got.TotalChars != 60 {
		t.Fatalf("TotalChars=%d want 60", got.TotalChars)
	}
	if got.L2Chars != 50 || got.L1Chars != 10 || got.L0Chars != 0 || got.SemanticChars != 0 {
		t.Fatalf("unexpected clamped budget: %+v", got)
	}
	if got.L2Chars+got.L1Chars+got.L0Chars+got.SemanticChars != got.TotalChars {
		t.Fatalf("budget sum mismatch: %+v", got)
	}
}

func TestResolveLayerBudget_UsesBudgetTotalOverride(t *testing.T) {
	t.Parallel()

	got := resolveLayerBudget(LayeredRequest{
		MaxChars: 200,
		Budget: &LayerBudget{
			TotalChars: 80,
		},
	}, false)
	if got.TotalChars != 80 {
		t.Fatalf("TotalChars=%d want 80", got.TotalChars)
	}
	if got.L2Chars != 16 || got.L1Chars != 20 || got.L0Chars != 44 || got.SemanticChars != 0 {
		t.Fatalf("unexpected budget: %+v", got)
	}
}
