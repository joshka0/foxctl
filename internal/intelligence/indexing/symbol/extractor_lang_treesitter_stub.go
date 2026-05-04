//go:build !cgo

package symbol

import "context"

func extractTypeScriptSymbolsWithTreeSitter(_ context.Context, _ string, _ []byte) ([]Symbol, bool, error) {
	return nil, false, nil
}

func extractTypeScriptCallsWithTreeSitter(_ context.Context, _ Symbol, _ []byte) ([]string, bool, error) {
	return nil, false, nil
}

func extractTypeScriptRefsWithTreeSitter(_ context.Context, _ Symbol, _ []byte) ([]string, bool, error) {
	return nil, false, nil
}

func extractPythonSymbolsWithTreeSitter(_ context.Context, _ string, _ []byte) ([]Symbol, bool, error) {
	return nil, false, nil
}

func extractPythonCallsWithTreeSitter(_ context.Context, _ Symbol, _ []byte) ([]string, bool, error) {
	return nil, false, nil
}

func extractRustSymbolsWithTreeSitter(_ context.Context, _ string, _ []byte) ([]Symbol, bool, error) {
	return nil, false, nil
}

func extractRustCallsWithTreeSitter(_ context.Context, _ Symbol, _ []byte) ([]string, bool, error) {
	return nil, false, nil
}

func extractCSharpSymbolsWithTreeSitter(_ context.Context, _ string, _ []byte) ([]Symbol, bool, error) {
	return nil, false, nil
}

func extractCSharpCallsWithTreeSitter(_ context.Context, _ Symbol, _ []byte) ([]string, bool, error) {
	return nil, false, nil
}
