//go:build !cgo

package main

import symindex "github.com/jkatigb/agentctl/internal/indexing/symbol"

func analyzeElixirPreloadAfterGetChains(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}

func analyzeElixirPostTransactionPreloads(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}

func analyzeElixirTransactionScriptHotspots(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}
