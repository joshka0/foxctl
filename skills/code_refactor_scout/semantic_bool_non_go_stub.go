//go:build !cgo

package main

import symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"

func analyzeTypeScriptSemanticSimplifications(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}

func analyzePythonSemanticSimplifications(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}

func analyzeElixirSemanticSimplifications(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}
