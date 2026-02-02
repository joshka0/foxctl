package symbolutil

import platformsymbol "github.com/jkatigb/agentctl/internal/platform/symbolutil"

// EntryName returns the canonical memory entry name for a symbol.
func EntryName(workspaceID, filePath, symbolName string) string {
	return platformsymbol.EntryName(workspaceID, filePath, symbolName)
}
