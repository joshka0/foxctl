//go:build !cgo

package repoindex

func extractElixirFileRelationsWithTreeSitter(_ []byte) ([]elixirFileRelation, bool) {
	return nil, false
}
