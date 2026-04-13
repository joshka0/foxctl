package codecontext

import "github.com/jkatigb/agentctl/internal/intelligence/searchquery"

type IdentifierKind string

const (
	IdentifierKindSymbol    IdentifierKind = "symbol"
	IdentifierKindQualified IdentifierKind = "qualified"
	IdentifierKindSnake     IdentifierKind = "snake"
	IdentifierKindCamel     IdentifierKind = "camel"
	IdentifierKindPath      IdentifierKind = "path"
)

type Identifier struct {
	Value string
	Kind  IdentifierKind
}

type Phrase struct {
	Text string
}

type PathHint struct {
	Path string
}

type QueryPlan struct {
	Raw         string
	Terms       []string
	Identifiers []Identifier
	Phrases     []Phrase
	PathHints   []PathHint
}

type MatchScore struct {
	Score             float64
	TermMatches       int
	IdentifierMatches int
	PhraseMatches     int
	PathHintMatches   int
	MaxPossible       float64
	Actual            float64
}

func ParseQuery(raw string) QueryPlan {
	return fromSearchQueryPlan(searchquery.ParseQuery(raw))
}

func ScoreText(plan QueryPlan, text string) MatchScore {
	score := searchquery.ScoreText(toSearchQueryPlan(plan), text)
	return MatchScore{
		Score:             score.Score,
		TermMatches:       score.TermMatches,
		IdentifierMatches: score.IdentifierMatches,
		PhraseMatches:     score.PhraseMatches,
		PathHintMatches:   score.PathHintMatches,
		MaxPossible:       score.MaxPossible,
		Actual:            score.Actual,
	}
}

func fromSearchQueryPlan(plan searchquery.QueryPlan) QueryPlan {
	out := QueryPlan{
		Raw:         plan.Raw,
		Terms:       append([]string(nil), plan.Terms...),
		Identifiers: make([]Identifier, 0, len(plan.Identifiers)),
		Phrases:     make([]Phrase, 0, len(plan.Phrases)),
		PathHints:   make([]PathHint, 0, len(plan.PathHints)),
	}
	for _, item := range plan.Identifiers {
		out.Identifiers = append(out.Identifiers, Identifier{
			Value: item.Value,
			Kind:  IdentifierKind(item.Kind),
		})
	}
	for _, item := range plan.Phrases {
		out.Phrases = append(out.Phrases, Phrase{Text: item.Text})
	}
	for _, item := range plan.PathHints {
		out.PathHints = append(out.PathHints, PathHint{Path: item.Path})
	}
	return out
}

func toSearchQueryPlan(plan QueryPlan) searchquery.QueryPlan {
	out := searchquery.QueryPlan{
		Raw:         plan.Raw,
		Terms:       append([]string(nil), plan.Terms...),
		Identifiers: make([]searchquery.Identifier, 0, len(plan.Identifiers)),
		Phrases:     make([]searchquery.Phrase, 0, len(plan.Phrases)),
		PathHints:   make([]searchquery.PathHint, 0, len(plan.PathHints)),
	}
	for _, item := range plan.Identifiers {
		out.Identifiers = append(out.Identifiers, searchquery.Identifier{
			Value: item.Value,
			Kind:  searchquery.IdentifierKind(item.Kind),
		})
	}
	for _, item := range plan.Phrases {
		out.Phrases = append(out.Phrases, searchquery.Phrase{Text: item.Text})
	}
	for _, item := range plan.PathHints {
		out.PathHints = append(out.PathHints, searchquery.PathHint{Path: item.Path})
	}
	return out
}
