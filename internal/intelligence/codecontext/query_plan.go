package codecontext

import "github.com/joshka0/foxctl/internal/intelligence/searchquery"

type IdentifierKind = searchquery.IdentifierKind

const (
	IdentifierKindSymbol    = searchquery.IdentifierKindSymbol
	IdentifierKindQualified = searchquery.IdentifierKindQualified
	IdentifierKindSnake     = searchquery.IdentifierKindSnake
	IdentifierKindCamel     = searchquery.IdentifierKindCamel
	IdentifierKindPath      = searchquery.IdentifierKindPath
)

type Identifier = searchquery.Identifier

type Phrase = searchquery.Phrase

type PathHint = searchquery.PathHint

type QueryPlan = searchquery.QueryPlan

type MatchScore = searchquery.MatchScore

func ParseQuery(raw string) QueryPlan {
	return searchquery.ParseQuery(raw)
}

func ScoreText(plan QueryPlan, text string) MatchScore {
	return searchquery.ScoreText(plan, text)
}
