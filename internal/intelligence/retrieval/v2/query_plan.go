package retrievalv2

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

func ParseQuery(raw string) QueryPlan {
	return searchquery.ParseQuery(raw)
}

func ComposeLexicalQuery(plan QueryPlan) string {
	return searchquery.ComposeLexicalQuery(plan)
}
