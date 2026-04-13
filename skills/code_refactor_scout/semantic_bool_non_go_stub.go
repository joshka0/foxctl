//go:build !cgo

package main

import symindex "github.com/jkatigb/agentctl/internal/intelligence/indexing/symbol"

func analyzeTypeScriptSemanticSimplifications(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}

func analyzePythonSemanticSimplifications(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}

func analyzeElixirSemanticSimplifications(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}
