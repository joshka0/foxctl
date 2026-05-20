// Package scoring provides scoring-table parsing, aggregate recalculation,
// and threshold-with-drift for the open-collider bisociation engine.
//
// This is a direct port of open-collider/src/open_collider/scoring/score_parser.py
// and the parse/threshold logic from idea_scorer.py.
//
// Plan semantics preserved:
//   - ParseScoringResponse does NOT set Retained.
//   - ApplyThreshold is a separate function that drifts once across the full merged set.
package scoring

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Axis name constants — fixed 5-axis layout matching judge.md.
const (
	AxisOriginality       = "originality"
	AxisResistance        = "resistance"
	AxisThesisDensity     = "thesis_density"
	AxisConcreteGrounding = "concrete_grounding"
	AxisCognitiveLoad     = "cognitive_load"
)

// DefaultWeights is the default scoring-axis weight map.
// Can be overridden per project via judge_axes in project_config.yaml.
var DefaultWeights = map[string]float64{
	AxisOriginality:       0.25,
	AxisResistance:        0.20,
	AxisThesisDensity:     0.20,
	AxisConcreteGrounding: 0.20,
	AxisCognitiveLoad:     0.15,
}

// Default thresholds, matching open-collider defaults.
const (
	DefaultScoreThreshold      = 4.2
	DefaultDriftThreshold      = 4.0
	DefaultMinIdeasBeforeDrift = 3
	DefaultDriftStep           = 0.1
	BatchSize                  = 25
)

// AxisScores holds per-axis scores for one idea extracted from the judge
// scoring table (Step 1 of the judge response).
type AxisScores struct {
	IdeaNum           int
	Originality       float64
	Resistance        float64
	ThesisDensity     float64
	ConcreteGrounding float64
	CognitiveLoad     float64
	ScoreAggregate    float64 // as reported by the LLM; RecalculateAggregate overrides this
}

// Idea is an input idea from the generation phase.
// GlobalNum and OrigIdeaNum support the renumbering that happens
// when ideas from multiple combos are merged into scoring batches.
type Idea struct {
	GlobalNum   int // 1-based global renumbering across all ideas in the batch
	OrigIdeaNum int // original idea number within its combo (0 = use IdeaNum)
	IdeaNum     int // fallback when GlobalNum/OrigIdeaNum are zero
	Text        string
	Combo       string
	TextID      string
	SetID       string
	CollisionID string
	IdeaID      string
	GenModel    string
	Strategy    string
	Iteration   int
}

// ScoredIdea is an idea with per-axis scores and a recalculated aggregate.
// Retained is NOT set by ParseScoringResponse; use ApplyThreshold for that.
type ScoredIdea struct {
	IdeaNum        int
	Text           string
	Combo          string
	TextID         string
	SetID          string
	CollisionID    string
	IdeaID         string
	GenModel       string
	Strategy       string
	Iteration      int
	Scores         map[string]float64
	ScoreAggregate float64
	JudgeNote      string
	Retained       bool
	ThresholdUsed  float64
}

// ThresholdConfig holds the parameters for retention threshold with drift.
type ThresholdConfig struct {
	ScoreThreshold      float64
	DriftThreshold      float64 // floor — threshold never drifts below this
	MinIdeasBeforeDrift int     // minimum passing ideas before we accept the threshold
	DriftStep           float64 // step-down increment
}

// DefaultThresholdConfig returns the default threshold configuration.
func DefaultThresholdConfig() ThresholdConfig {
	return ThresholdConfig{
		ScoreThreshold:      DefaultScoreThreshold,
		DriftThreshold:      DefaultDriftThreshold,
		MinIdeasBeforeDrift: DefaultMinIdeasBeforeDrift,
		DriftStep:           DefaultDriftStep,
	}
}

// WithDefaults returns a copy of cfg with zero values replaced by defaults.
func (cfg ThresholdConfig) WithDefaults() ThresholdConfig {
	d := DefaultThresholdConfig()
	if cfg.ScoreThreshold == 0 {
		cfg.ScoreThreshold = d.ScoreThreshold
	}
	if cfg.DriftThreshold == 0 {
		cfg.DriftThreshold = d.DriftThreshold
	}
	if cfg.MinIdeasBeforeDrift == 0 {
		cfg.MinIdeasBeforeDrift = d.MinIdeasBeforeDrift
	}
	if cfg.DriftStep == 0 {
		cfg.DriftStep = d.DriftStep
	}
	return cfg
}

// scoreCell matches one scoring-table cell.
// Supported formats: 4 | **4** | 4/5 | **4**/5 | 4.25 | **4.25** | 4,5 | **4,5**
// Comma is accepted as a decimal separator (European locale LLMs).
const scoreCell = `\s*\*{0,2}([\d.,]+)\*{0,2}(?:/5)?\s*\|`

// scoringRowRe matches a full row of the 7-column scoring table.
var scoringRowRe = regexp.MustCompile(
	`\|\s*(\d+)\s*\|` + // idea number
		scoreCell + // originality
		scoreCell + // resistance
		scoreCell + // thesis_density
		scoreCell + // concrete_grounding
		scoreCell + // cognitive_load
		scoreCell, // score_aggregate
)

// judgeNoteRe extracts the per-idea "main strength" from the final checklist.
// Supports EN (Idea) and FR (Idée) headers, and ✓ ✔ ☑ prefixes.
var judgeNoteRe = regexp.MustCompile(
	`[✓✔☑]\s*(?:Id[eé]e|Idea)\s*#?(\d+)\s*[—\-–]+\s*Score\s*([\d.,]+)\s*[—\-–]+\s*(.*)`)

// ParseScoringTable extracts AxisScores from the judge-response markdown table.
// Returns an empty (non-nil) slice if the table is missing or malformed.
func ParseScoringTable(content string) []AxisScores {
	var results []AxisScores
	for _, m := range scoringRowRe.FindAllStringSubmatch(content, -1) {
		num, err := atoi(m[1])
		if err != nil {
			continue
		}
		orig, err := parseScore(m[2])
		if err != nil {
			continue
		}
		resist, err := parseScore(m[3])
		if err != nil {
			continue
		}
		thesis, err := parseScore(m[4])
		if err != nil {
			continue
		}
		concrete, err := parseScore(m[5])
		if err != nil {
			continue
		}
		cognitive, err := parseScore(m[6])
		if err != nil {
			continue
		}
		aggregate, err := parseScore(m[7])
		if err != nil {
			continue
		}
		results = append(results, AxisScores{
			IdeaNum:           num,
			Originality:       orig,
			Resistance:        resist,
			ThesisDensity:     thesis,
			ConcreteGrounding: concrete,
			CognitiveLoad:     cognitive,
			ScoreAggregate:    aggregate,
		})
	}
	return results
}

// ExtractJudgeNotes extracts per-idea judge notes from the final list section
// of a judge response (the ✓ lines).
// Returns a map of idea number → main-strength text.
func ExtractJudgeNotes(content string) map[int]string {
	notes := make(map[int]string)
	for _, m := range judgeNoteRe.FindAllStringSubmatch(content, -1) {
		num, err := atoi(m[1])
		if err != nil {
			continue
		}
		notes[num] = strings.TrimSpace(m[3])
	}
	return notes
}

// RecalculateAggregate computes a weighted sum of per-axis scores.
// weights is typically DefaultWeights or a project override from judge_axes.
// Missing axes contribute 0.
func RecalculateAggregate(scores map[string]float64, weights map[string]float64) float64 {
	total := 0.0
	for axis, weight := range weights {
		total += scores[axis] * weight
	}
	return round2(total)
}

// ParseScoringResponse parses a judge response into scored ideas.
// It maps axis scores back to the input ideas using GlobalNum (falling back
// to IdeaNum when GlobalNum is zero).
//
// It recalculates score_aggregate from the provided weights but does NOT
// set Retained — use ApplyThreshold for that.
func ParseScoringResponse(response string, ideas []Idea, weights map[string]float64) []ScoredIdea {
	axisScores := ParseScoringTable(response)
	judgeNotes := ExtractJudgeNotes(response)

	// Build lookup by global number.
	numToIdea := make(map[int]Idea, len(ideas))
	for _, idea := range ideas {
		num := idea.GlobalNum
		if num == 0 {
			num = idea.IdeaNum
		}
		numToIdea[num] = idea
	}

	var scored []ScoredIdea
	for _, ax := range axisScores {
		idea, ok := numToIdea[ax.IdeaNum]
		if !ok {
			continue
		}

		scores := map[string]float64{
			AxisOriginality:       ax.Originality,
			AxisResistance:        ax.Resistance,
			AxisThesisDensity:     ax.ThesisDensity,
			AxisConcreteGrounding: ax.ConcreteGrounding,
			AxisCognitiveLoad:     ax.CognitiveLoad,
		}
		aggregate := RecalculateAggregate(scores, weights)

		outNum := idea.OrigIdeaNum
		if outNum == 0 {
			outNum = idea.IdeaNum
		}

		scored = append(scored, ScoredIdea{
			IdeaNum:        outNum,
			Text:           idea.Text,
			Combo:          idea.Combo,
			TextID:         idea.TextID,
			SetID:          idea.SetID,
			CollisionID:    idea.CollisionID,
			IdeaID:         idea.IdeaID,
			GenModel:       idea.GenModel,
			Strategy:       idea.Strategy,
			Iteration:      idea.Iteration,
			Scores:         scores,
			ScoreAggregate: aggregate,
			JudgeNote:      judgeNotes[ax.IdeaNum],
		})
	}
	return scored
}

// ApplyThreshold sets Retained and ThresholdUsed on each scored idea.
// It drifts the threshold down by DriftStep until at least MinIdeasBeforeDrift
// ideas pass, or until the threshold reaches DriftThreshold (the floor).
//
// The function modifies the slice in place and returns it for convenience.
// This is the finalize-time operation — score parsing never calls this.
func ApplyThreshold(scored []ScoredIdea, cfg ThresholdConfig) []ScoredIdea {
	cfg = cfg.WithDefaults()

	current := cfg.ScoreThreshold
	for current > cfg.DriftThreshold {
		passing := 0
		for i := range scored {
			if scored[i].ScoreAggregate >= current {
				passing++
			}
		}
		if passing >= cfg.MinIdeasBeforeDrift {
			break
		}
		current = round2(current - cfg.DriftStep)
	}
	// Floor clamp — matches Python's max(current_threshold, drift_floor).
	if current < cfg.DriftThreshold {
		current = cfg.DriftThreshold
	}

	for i := range scored {
		scored[i].Retained = scored[i].ScoreAggregate >= current
		scored[i].ThresholdUsed = current
	}
	return scored
}

// parseScore parses a score cell value, accepting both . and , as decimal
// separators (European-locale LLMs may output 4,5 instead of 4.5).
func parseScore(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", ".")
	return strconv.ParseFloat(s, 64)
}

// atoi is a thin wrapper around strconv.Atoi on trimmed input.
func atoi(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}

// round2 rounds to 2 decimal places, matching Python's round(x, 2).
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
