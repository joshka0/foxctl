package symbolutil

import platformsymbol "github.com/joshka0/foxctl/internal/platform/symbolutil"

// EntryName returns the canonical memory entry name for a symbol.
func EntryName(workspaceID, filePath, symbolName string) string {
	return platformsymbol.EntryName(workspaceID, filePath, symbolName)
}
