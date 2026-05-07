package symbolutil

import platformsymbol "github.com/joshka0/foxctl/internal/platform/symbolutil"

// EntryName returns the legacy file/name memory entry name for a symbol.
func EntryName(workspaceID, filePath, symbolName string) string {
	return platformsymbol.EntryName(workspaceID, filePath, symbolName)
}

// KeyEntryName returns the package-scoped canonical memory entry name for a symbol.
func KeyEntryName(workspaceID, packageID, symbolKey string) string {
	return platformsymbol.KeyEntryName(workspaceID, packageID, symbolKey)
}

// ScopedSymbolID returns the package-scoped stable symbol identifier.
func ScopedSymbolID(packageID, symbolKey string) string {
	return platformsymbol.ScopedSymbolID(packageID, symbolKey)
}
