//go:build !cgo

package main

import symindex "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"

func analyzeTypeScriptDuplicateRecoveryBlocks(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}

func analyzeTypeScriptDuplicatedErrorRemaps(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}

func analyzeTypeScriptRepeatedGuardLadders(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}
