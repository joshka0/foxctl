//go:build !cgo

package main

import symindex "github.com/jkatigb/agentctl/internal/intelligence/indexing/symbol"

func analyzeElixirDuplicateRecoveryBlocks(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}

func analyzeElixirDuplicatedErrorRemaps(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}

func analyzeElixirRepeatedGuardLadders(_ string, _ string, _ string, _ []byte, _ []symindex.Symbol) []finding {
	return nil
}
