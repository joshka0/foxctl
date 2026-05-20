package scoring

import (
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// ParseScoringTable
// ---------------------------------------------------------------------------

func TestParseScoringTable_StandardFormat(t *testing.T) {
	input := `## Scoring Table

| # | Orig | Resist | Thesis | Concrete | Cognitive | Score |
|---|------|--------|--------|----------|-----------|-------|
| 1 | 4 | 5 | 3 | 4 | 5 | 4.25 |
| 2 | 5 | 4 | 4 | 3 | 4 | 4.00 |
| 3 | 3 | 3 | 5 | 5 | 3 | 3.80 |
`

	got := ParseScoringTable(input)
	if len(got) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(got))
	}

	assertAxisScores(t, "row 1", got[0], AxisScores{
		IdeaNum: 1, Originality: 4, Resistance: 5, ThesisDensity: 3,
		ConcreteGrounding: 4, CognitiveLoad: 5, ScoreAggregate: 4.25,
	})
	assertAxisScores(t, "row 2", got[1], AxisScores{
		IdeaNum: 2, Originality: 5, Resistance: 4, ThesisDensity: 4,
		ConcreteGrounding: 3, CognitiveLoad: 4, ScoreAggregate: 4.0,
	})
	assertAxisScores(t, "row 3", got[2], AxisScores{
		IdeaNum: 3, Originality: 3, Resistance: 3, ThesisDensity: 5,
		ConcreteGrounding: 5, CognitiveLoad: 3, ScoreAggregate: 3.8,
	})
}

func TestParseScoringTable_BoldScores(t *testing.T) {
	// Matches patterns like **4**/5 and **4.25**
	input := `| 1 | **4**/5 | **5**/5 | **3**/5 | **4**/5 | **5**/5 | **4.25** |
| 2 | **5** | **4** | **4** | **3** | **4** | **4.00** |
`

	got := ParseScoringTable(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	assertAxisScores(t, "bold row 1", got[0], AxisScores{
		IdeaNum: 1, Originality: 4, Resistance: 5, ThesisDensity: 3,
		ConcreteGrounding: 4, CognitiveLoad: 5, ScoreAggregate: 4.25,
	})
	assertAxisScores(t, "bold row 2", got[1], AxisScores{
		IdeaNum: 2, Originality: 5, Resistance: 4, ThesisDensity: 4,
		ConcreteGrounding: 3, CognitiveLoad: 4, ScoreAggregate: 4.0,
	})
}

func TestParseScoringTable_SlashFive(t *testing.T) {
	input := `| 1 | 4/5 | 5/5 | 3/5 | 4/5 | 5/5 | 4.25 |
`

	got := ParseScoringTable(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].Originality != 4 {
		t.Errorf("originality: got %f, want 4", got[0].Originality)
	}
	if got[0].Resistance != 5 {
		t.Errorf("resistance: got %f, want 5", got[0].Resistance)
	}
}

func TestParseScoringTable_CommaDecimals(t *testing.T) {
	// European locale: 4,5 instead of 4.5
	input := `| 1 | 4,5 | 3,5 | 4,0 | 5,0 | 4,5 | 4,30 |
`

	got := ParseScoringTable(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	assertAxisScores(t, "comma row", got[0], AxisScores{
		IdeaNum: 1, Originality: 4.5, Resistance: 3.5, ThesisDensity: 4.0,
		ConcreteGrounding: 5.0, CognitiveLoad: 4.5, ScoreAggregate: 4.3,
	})
}

func TestParseScoringTable_MixedFormats(t *testing.T) {
	// Bold + /5 + comma decimals in one table
	input := `| 1 | **4**/5 | 3,5 | 4 | **5** | 4/5 | 4.10 |
`

	got := ParseScoringTable(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	assertAxisScores(t, "mixed row", got[0], AxisScores{
		IdeaNum: 1, Originality: 4.0, Resistance: 3.5, ThesisDensity: 4.0,
		ConcreteGrounding: 5.0, CognitiveLoad: 4.0, ScoreAggregate: 4.1,
	})
}

func TestParseScoringTable_EmptyInput(t *testing.T) {
	got := ParseScoringTable("")
	if len(got) != 0 {
		t.Fatalf("expected 0 rows for empty input, got %d", len(got))
	}
}

func TestParseScoringTable_NoTable(t *testing.T) {
	input := `This is just text with numbers 4 and 5 but no table format.`
	got := ParseScoringTable(input)
	if len(got) != 0 {
		t.Fatalf("expected 0 rows for non-table input, got %d", len(got))
	}
}

func TestParseScoringTable_MalformedRow(t *testing.T) {
	// Only 5 columns instead of 7 — should not match
	input := `| 1 | 4 | 5 | 3 | 4 | 5 |
`
	got := ParseScoringTable(input)
	if len(got) != 0 {
		t.Fatalf("expected 0 rows for malformed table, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// ExtractJudgeNotes
// ---------------------------------------------------------------------------

func TestExtractJudgeNotes_StandardEnglish(t *testing.T) {
	input := `✓ Idea #1 — Score 4.60 — Strong structural transfer from mycology to governance
✓ Idea #2 — Score 4.25 — Good resistance but weak concrete grounding
✓ Idea #3 — Score 3.90 — Solid but lacks bisociation depth
`
	notes := ExtractJudgeNotes(input)
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(notes))
	}
	if notes[1] != "Strong structural transfer from mycology to governance" {
		t.Errorf("note 1: got %q", notes[1])
	}
	if notes[2] != "Good resistance but weak concrete grounding" {
		t.Errorf("note 2: got %q", notes[2])
	}
	if notes[3] != "Solid but lacks bisociation depth" {
		t.Errorf("note 3: got %q", notes[3])
	}
}

func TestExtractJudgeNotes_FrenchHeaders(t *testing.T) {
	input := `✔ Idée #5 — Score 4,50 — Bon transfert structurel
`
	notes := ExtractJudgeNotes(input)
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
	if notes[5] != "Bon transfert structurel" {
		t.Errorf("note 5: got %q", notes[5])
	}
}

func TestExtractJudgeNotes_VariousCheckmarks(t *testing.T) {
	input := `✓ Idea #1 — Score 4.0 — check mark
✔ Idea #2 — Score 4.0 — heavy check
☑ Idea #3 — Score 4.0 — ballot check
`
	notes := ExtractJudgeNotes(input)
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(notes))
	}
}

func TestExtractJudgeNotes_EmDash(t *testing.T) {
	// Various dash styles: — (em dash), - (hyphen), – (en dash)
	input := `✓ Idea #1 — Score 4.0 — em dash
✓ Idea #2 - Score 4.0 - hyphen
✓ Idea #3 – Score 4.0 – en dash
`
	notes := ExtractJudgeNotes(input)
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes for different dash styles, got %d", len(notes))
	}
}

func TestExtractJudgeNotes_NoMatches(t *testing.T) {
	input := `Some regular text without any judge notes.`
	notes := ExtractJudgeNotes(input)
	if len(notes) != 0 {
		t.Fatalf("expected 0 notes, got %d", len(notes))
	}
}

// ---------------------------------------------------------------------------
// RecalculateAggregate
// ---------------------------------------------------------------------------

func TestRecalculateAggregate_DefaultWeights(t *testing.T) {
	scores := map[string]float64{
		AxisOriginality:       4,
		AxisResistance:        5,
		AxisThesisDensity:     3,
		AxisConcreteGrounding: 4,
		AxisCognitiveLoad:     5,
	}
	// 4*0.25 + 5*0.20 + 3*0.20 + 4*0.20 + 5*0.15
	// = 1.00 + 1.00 + 0.60 + 0.80 + 0.75
	// = 4.15
	got := RecalculateAggregate(scores, DefaultWeights)
	assertFloat(t, "aggregate", got, 4.15)
}

func TestRecalculateAggregate_PerfectScore(t *testing.T) {
	scores := map[string]float64{
		AxisOriginality:       5,
		AxisResistance:        5,
		AxisThesisDensity:     5,
		AxisConcreteGrounding: 5,
		AxisCognitiveLoad:     5,
	}
	// All 5s → 5.0
	got := RecalculateAggregate(scores, DefaultWeights)
	assertFloat(t, "aggregate", got, 5.0)
}

func TestRecalculateAggregate_CustomWeights(t *testing.T) {
	scores := map[string]float64{
		AxisOriginality:       4,
		AxisResistance:        4,
		AxisThesisDensity:     4,
		AxisConcreteGrounding: 4,
		AxisCognitiveLoad:     4,
	}
	// Equal weights, all 4s → 4.0
	weights := map[string]float64{
		AxisOriginality:       0.20,
		AxisResistance:        0.20,
		AxisThesisDensity:     0.20,
		AxisConcreteGrounding: 0.20,
		AxisCognitiveLoad:     0.20,
	}
	got := RecalculateAggregate(scores, weights)
	assertFloat(t, "aggregate", got, 4.0)
}

func TestRecalculateAggregate_MissingAxes(t *testing.T) {
	// Only 2 axes present — missing ones contribute 0
	scores := map[string]float64{
		AxisOriginality: 5,
		AxisResistance:  5,
	}
	// 5*0.25 + 5*0.20 + 0 + 0 + 0 = 1.25 + 1.00 = 2.25
	got := RecalculateAggregate(scores, DefaultWeights)
	assertFloat(t, "aggregate", got, 2.25)
}

func TestRecalculateAggregate_Rounding(t *testing.T) {
	// Produce a value that needs rounding to 2 decimal places
	scores := map[string]float64{
		AxisOriginality:       3,
		AxisResistance:        4,
		AxisThesisDensity:     3,
		AxisConcreteGrounding: 4,
		AxisCognitiveLoad:     3,
	}
	// 3*0.25 + 4*0.20 + 3*0.20 + 4*0.20 + 3*0.15
	// = 0.75 + 0.80 + 0.60 + 0.80 + 0.45
	// = 3.40
	got := RecalculateAggregate(scores, DefaultWeights)
	assertFloat(t, "aggregate", got, 3.40)
}

// ---------------------------------------------------------------------------
// ParseScoringResponse
// ---------------------------------------------------------------------------

func TestParseScoringResponse_BasicMapping(t *testing.T) {
	response := `## Scoring Table
| 1 | 4 | 5 | 3 | 4 | 5 | 4.25 |
| 2 | 5 | 4 | 4 | 3 | 4 | 4.00 |

✓ Idea #1 — Score 4.25 — Strong transfer
✓ Idea #2 — Score 4.00 — Good but common
`
	ideas := []Idea{
		{GlobalNum: 1, OrigIdeaNum: 3, IdeaNum: 3, Text: "idea about mycology", Combo: "T01_fresh_DS1"},
		{GlobalNum: 2, OrigIdeaNum: 5, IdeaNum: 5, Text: "idea about jazz", Combo: "T02_fresh_DS2"},
	}

	scored := ParseScoringResponse(response, ideas, DefaultWeights)

	if len(scored) != 2 {
		t.Fatalf("expected 2 scored ideas, got %d", len(scored))
	}

	// First idea: OrigIdeaNum=3 should be used as IdeaNum
	if scored[0].IdeaNum != 3 {
		t.Errorf("idea 0 IdeaNum: got %d, want 3", scored[0].IdeaNum)
	}
	if scored[0].Text != "idea about mycology" {
		t.Errorf("idea 0 Text: got %q", scored[0].Text)
	}
	if scored[0].JudgeNote != "Strong transfer" {
		t.Errorf("idea 0 JudgeNote: got %q", scored[0].JudgeNote)
	}
	if scored[0].Retained {
		t.Error("idea 0 should NOT be retained — ParseScoringResponse never sets retained")
	}

	// Verify aggregate is recalculated, not just copied from table
	// 4*0.25 + 5*0.20 + 3*0.20 + 4*0.20 + 5*0.15 = 4.15
	assertFloat(t, "idea 0 aggregate", scored[0].ScoreAggregate, 4.15)

	// Second idea
	if scored[1].IdeaNum != 5 {
		t.Errorf("idea 1 IdeaNum: got %d, want 5", scored[1].IdeaNum)
	}
	if scored[1].JudgeNote != "Good but common" {
		t.Errorf("idea 1 JudgeNote: got %q", scored[1].JudgeNote)
	}
	if scored[1].Retained {
		t.Error("idea 1 should NOT be retained — ParseScoringResponse never sets retained")
	}
}

func TestParseScoringResponse_GlobalNumFallback(t *testing.T) {
	// When GlobalNum is 0, should fall back to IdeaNum for lookup
	response := `| 42 | 4 | 4 | 4 | 4 | 4 | 4.0 |
`
	ideas := []Idea{
		{IdeaNum: 42, Text: "fallback idea"},
	}
	scored := ParseScoringResponse(response, ideas, DefaultWeights)
	if len(scored) != 1 {
		t.Fatalf("expected 1 scored idea, got %d", len(scored))
	}
	if scored[0].Text != "fallback idea" {
		t.Errorf("text: got %q", scored[0].Text)
	}
}

func TestParseScoringResponse_MissingIdea(t *testing.T) {
	// Axis score for idea #99 — not in ideas list, should be skipped
	response := `| 99 | 5 | 5 | 5 | 5 | 5 | 5.0 |
| 1  | 4 | 4 | 4 | 4 | 4 | 4.0 |
`
	ideas := []Idea{
		{GlobalNum: 1, IdeaNum: 1, Text: "exists"},
	}
	scored := ParseScoringResponse(response, ideas, DefaultWeights)
	if len(scored) != 1 {
		t.Fatalf("expected 1 scored idea (99 skipped), got %d", len(scored))
	}
	if scored[0].Text != "exists" {
		t.Errorf("text: got %q", scored[0].Text)
	}
}

func TestParseScoringResponse_ScoresMapKeys(t *testing.T) {
	response := `| 1 | 4 | 5 | 3 | 4 | 5 | 4.25 |
`
	ideas := []Idea{{GlobalNum: 1, IdeaNum: 1, Text: "test"}}
	scored := ParseScoringResponse(response, ideas, DefaultWeights)
	if len(scored) != 1 {
		t.Fatalf("expected 1, got %d", len(scored))
	}
	s := scored[0].Scores
	expected := map[string]float64{
		AxisOriginality: 4, AxisResistance: 5, AxisThesisDensity: 3,
		AxisConcreteGrounding: 4, AxisCognitiveLoad: 5,
	}
	for k, v := range expected {
		if math.Abs(s[k]-v) > 0.001 {
			t.Errorf("scores[%q]: got %f, want %f", k, s[k], v)
		}
	}
}

// ---------------------------------------------------------------------------
// ApplyThreshold
// ---------------------------------------------------------------------------

func TestApplyThreshold_AllPass(t *testing.T) {
	scored := []ScoredIdea{
		{ScoreAggregate: 4.5},
		{ScoreAggregate: 4.3},
		{ScoreAggregate: 4.8},
	}
	result := ApplyThreshold(scored, ThresholdConfig{
		ScoreThreshold:      4.2,
		DriftThreshold:      4.0,
		MinIdeasBeforeDrift: 3,
		DriftStep:           0.1,
	})
	for i, idea := range result {
		if !idea.Retained {
			t.Errorf("idea %d (score %.2f) should be retained", i, idea.ScoreAggregate)
		}
		if idea.ThresholdUsed != 4.2 {
			t.Errorf("idea %d threshold: got %.2f, want 4.2", i, idea.ThresholdUsed)
		}
	}
}

func TestApplyThreshold_NonePass(t *testing.T) {
	scored := []ScoredIdea{
		{ScoreAggregate: 2.0},
		{ScoreAggregate: 2.5},
		{ScoreAggregate: 3.0},
	}
	result := ApplyThreshold(scored, ThresholdConfig{
		ScoreThreshold:      4.2,
		DriftThreshold:      4.0,
		MinIdeasBeforeDrift: 3,
		DriftStep:           0.1,
	})
	// Drifts all the way to floor (4.0), none pass → all not retained
	for i, idea := range result {
		if idea.Retained {
			t.Errorf("idea %d (score %.2f) should NOT be retained at floor 4.0", i, idea.ScoreAggregate)
		}
		if idea.ThresholdUsed != 4.0 {
			t.Errorf("idea %d threshold: got %.2f, want 4.0 (floor)", i, idea.ThresholdUsed)
		}
	}
}

func TestApplyThreshold_Drift(t *testing.T) {
	// 2 ideas above 4.2, 1 at 4.1, 1 at 3.5. MinIdeas=3 → drift to 4.1
	scored := []ScoredIdea{
		{ScoreAggregate: 4.5},
		{ScoreAggregate: 4.3},
		{ScoreAggregate: 4.1},
		{ScoreAggregate: 3.5},
	}
	result := ApplyThreshold(scored, ThresholdConfig{
		ScoreThreshold:      4.2,
		DriftThreshold:      4.0,
		MinIdeasBeforeDrift: 3,
		DriftStep:           0.1,
	})
	// At 4.2: 2 pass (< 3) → drift to 4.1
	// At 4.1: 3 pass (≥ 3) → accept
	assertFloat(t, "threshold", result[0].ThresholdUsed, 4.1)

	if !result[0].Retained { // 4.5 >= 4.1
		t.Error("idea 0 (4.5) should be retained")
	}
	if !result[1].Retained { // 4.3 >= 4.1
		t.Error("idea 1 (4.3) should be retained")
	}
	if !result[2].Retained { // 4.1 >= 4.1
		t.Error("idea 2 (4.1) should be retained at drift threshold")
	}
	if result[3].Retained { // 3.5 < 4.1
		t.Error("idea 3 (3.5) should NOT be retained")
	}
}

func TestApplyThreshold_DriftToFloor(t *testing.T) {
	// Only 1 idea above 4.0, need 3. Drift all the way to floor.
	scored := []ScoredIdea{
		{ScoreAggregate: 4.5},
		{ScoreAggregate: 3.9},
		{ScoreAggregate: 3.8},
		{ScoreAggregate: 3.5},
	}
	result := ApplyThreshold(scored, ThresholdConfig{
		ScoreThreshold:      4.2,
		DriftThreshold:      4.0,
		MinIdeasBeforeDrift: 3,
		DriftStep:           0.1,
	})
	// Drifts: 4.2→4.1→4.0, at each step < 3 pass
	// Loop exits at 4.0 (4.0 > 4.0 is false), floor clamp keeps 4.0
	assertFloat(t, "threshold", result[0].ThresholdUsed, 4.0)

	if !result[0].Retained {
		t.Error("idea 0 (4.5) should be retained at floor")
	}
	if result[1].Retained {
		t.Error("idea 1 (3.9) should NOT be retained at floor 4.0")
	}
}

func TestApplyThreshold_MinIdeasBeforeDrift(t *testing.T) {
	// 4 ideas above 4.2, min=5 → drift to 4.1, 4 ideas at 4.1+, still < 5 → drift to 4.0
	scored := []ScoredIdea{
		{ScoreAggregate: 4.5},
		{ScoreAggregate: 4.3},
		{ScoreAggregate: 4.2},
		{ScoreAggregate: 4.2},
		{ScoreAggregate: 3.5},
	}
	result := ApplyThreshold(scored, ThresholdConfig{
		ScoreThreshold:      4.2,
		DriftThreshold:      4.0,
		MinIdeasBeforeDrift: 5,
		DriftStep:           0.1,
	})
	// At 4.2: 4 pass (< 5). At 4.1: still 4. At 4.0: 4 (still < 5).
	// Loop exits at floor. Threshold = 4.0.
	assertFloat(t, "threshold", result[0].ThresholdUsed, 4.0)
}

func TestApplyThreshold_DefaultConfig(t *testing.T) {
	// Zero-value config should use defaults
	scored := []ScoredIdea{
		{ScoreAggregate: 4.5},
		{ScoreAggregate: 4.3},
		{ScoreAggregate: 4.1},
	}
	result := ApplyThreshold(scored, ThresholdConfig{})
	assertFloat(t, "threshold", result[0].ThresholdUsed, 4.1)
	// All 3 >= 4.1 → retained
	for i, idea := range result {
		if !idea.Retained {
			t.Errorf("idea %d (score %.2f) should be retained at drift 4.1", i, idea.ScoreAggregate)
		}
	}
}

func TestApplyThreshold_ExactBoundary(t *testing.T) {
	// Score exactly at threshold should be retained
	scored := []ScoredIdea{
		{ScoreAggregate: 4.2},
		{ScoreAggregate: 4.2},
		{ScoreAggregate: 4.2},
	}
	result := ApplyThreshold(scored, ThresholdConfig{
		ScoreThreshold:      4.2,
		DriftThreshold:      4.0,
		MinIdeasBeforeDrift: 3,
		DriftStep:           0.1,
	})
	for i, idea := range result {
		if !idea.Retained {
			t.Errorf("idea %d (score %.2f == threshold) should be retained", i, idea.ScoreAggregate)
		}
	}
}

func TestApplyThreshold_DoesNotDriftWhenEnoughPass(t *testing.T) {
	// 3 ideas pass at 4.2, min=3 → no drift needed
	scored := []ScoredIdea{
		{ScoreAggregate: 4.2},
		{ScoreAggregate: 4.5},
		{ScoreAggregate: 4.8},
		{ScoreAggregate: 3.0},
	}
	result := ApplyThreshold(scored, ThresholdConfig{
		ScoreThreshold:      4.2,
		DriftThreshold:      4.0,
		MinIdeasBeforeDrift: 3,
		DriftStep:           0.1,
	})
	assertFloat(t, "threshold", result[0].ThresholdUsed, 4.2)
	if !result[0].Retained {
		t.Error("idea 0 should be retained")
	}
	if result[3].Retained {
		t.Error("idea 3 (3.0) should NOT be retained at 4.2")
	}
}

// ---------------------------------------------------------------------------
// Integration: ParseScoringResponse → ApplyThreshold
// ---------------------------------------------------------------------------

func TestIntegration_ParseThenThreshold(t *testing.T) {
	// Full pipeline: parse response, then apply threshold
	response := `## Step 1: Scoring Table

| # | O | R | T | C | CL | Agg |
|---|---|---|---|---|----|-----|
| 1 | 5 | 5 | 5 | 5 | 5 | **5.00** |
| 2 | 4 | 4 | 3 | 4 | 4 | 3.80 |
| 3 | 3 | 3 | 2 | 3 | 3 | 2.80 |
| 4 | 5 | 4 | 4 | 4 | 4 | 4.20 |

✓ Idea #1 — Score 5.00 — Perfect structural transfer
✓ Idea #2 — Score 3.80 — Decent but derivative
✓ Idea #3 — Score 2.80 — Weak collision
✓ Idea #4 — Score 4.20 — Strong but narrow
`
	ideas := []Idea{
		{GlobalNum: 1, IdeaNum: 1, Text: "perfect idea"},
		{GlobalNum: 2, IdeaNum: 2, Text: "decent idea"},
		{GlobalNum: 3, IdeaNum: 3, Text: "weak idea"},
		{GlobalNum: 4, IdeaNum: 4, Text: "strong idea"},
	}

	// Parse — should NOT set retained
	scored := ParseScoringResponse(response, ideas, DefaultWeights)
	if len(scored) != 4 {
		t.Fatalf("expected 4 scored, got %d", len(scored))
	}
	for i, s := range scored {
		if s.Retained {
			t.Errorf("scored[%d].Retained should be false after ParseScoringResponse", i)
		}
	}

	// Apply threshold
	result := ApplyThreshold(scored, ThresholdConfig{
		ScoreThreshold:      4.2,
		DriftThreshold:      4.0,
		MinIdeasBeforeDrift: 3,
		DriftStep:           0.1,
	})

	// At 4.2: ideas 1 and 4 pass (score 5.0 and ~4.2). That's 2 < 3 → drift.
	// Idea 2 aggregate: 4*0.25 + 4*0.20 + 3*0.20 + 4*0.20 + 4*0.15 = 1+0.8+0.6+0.8+0.6 = 3.8
	// At 4.1: ideas 1 and 4 still pass, idea 2 is 3.8 < 4.1. Still 2 < 3 → drift.
	// At 4.0: same. Loop exits at floor 4.0.
	// Threshold = 4.0
	retained := 0
	for _, s := range result {
		if s.Retained {
			retained++
		}
	}
	if retained < 2 {
		t.Errorf("expected at least 2 retained, got %d", retained)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertAxisScores(t *testing.T, label string, got, want AxisScores) {
	t.Helper()
	if got.IdeaNum != want.IdeaNum {
		t.Errorf("%s IdeaNum: got %d, want %d", label, got.IdeaNum, want.IdeaNum)
	}
	if math.Abs(got.Originality-want.Originality) > 0.001 {
		t.Errorf("%s Originality: got %f, want %f", label, got.Originality, want.Originality)
	}
	if math.Abs(got.Resistance-want.Resistance) > 0.001 {
		t.Errorf("%s Resistance: got %f, want %f", label, got.Resistance, want.Resistance)
	}
	if math.Abs(got.ThesisDensity-want.ThesisDensity) > 0.001 {
		t.Errorf("%s ThesisDensity: got %f, want %f", label, got.ThesisDensity, want.ThesisDensity)
	}
	if math.Abs(got.ConcreteGrounding-want.ConcreteGrounding) > 0.001 {
		t.Errorf("%s ConcreteGrounding: got %f, want %f", label, got.ConcreteGrounding, want.ConcreteGrounding)
	}
	if math.Abs(got.CognitiveLoad-want.CognitiveLoad) > 0.001 {
		t.Errorf("%s CognitiveLoad: got %f, want %f", label, got.CognitiveLoad, want.CognitiveLoad)
	}
	if math.Abs(got.ScoreAggregate-want.ScoreAggregate) > 0.001 {
		t.Errorf("%s ScoreAggregate: got %f, want %f", label, got.ScoreAggregate, want.ScoreAggregate)
	}
}

func assertFloat(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.001 {
		t.Errorf("%s: got %f, want %f", label, got, want)
	}
}
