//go:build !cgo

package repoindex

func extractTSImportsWithTreeSitter(_ string, _ []byte) ([]string, bool) {
	return nil, false
}

func extractTSImportBindingsWithTreeSitter(_ string, _ []byte) ([]tsImportBinding, bool) {
	return nil, false
}
