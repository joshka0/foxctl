package symbolutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

// DeriveSymbolPackage derives a package identifier for stable symbol-key names.
func DeriveSymbolPackage(filePath, lang string) string {
	dir := filepath.ToSlash(filepath.Dir(filePath))
	if dir == "." || dir == "" {
		dir = "root"
	}
	switch lang {
	case "go":
		return "go:" + dir
	case "typescript", "javascript":
		return "ts:local:" + dir
	case "python":
		return "py:" + dir
	case "elixir":
		return "ex:" + dir
	default:
		return "file:" + dir
	}
}

// EntryName returns the legacy file/name symbol memory entry name.
// Format: symbol://<workspace>/<file_path>:<symbol_name>
func EntryName(workspace, filePath, symbolName string) string {
	return fmt.Sprintf("symbol://%s/%s:%s", workspace, filePath, symbolName)
}

// FileMetaEntryName returns the canonical name for a file meta memory entry.
// Format: symbol-meta://<workspace>/<file_path>
func FileMetaEntryName(workspace, filePath string) string {
	return fmt.Sprintf("symbol-meta://%s/%s", workspace, filePath)
}

// KeyEntryName returns the canonical name for a symbol memory entry using a stable SymbolKey.
// Format: symbol://<workspace>/<pkg>::<symbolKey>
//
// Unlike EntryName which embeds the file path, KeyEntryName uses a file-path-independent
// key that remains stable across file moves. Both formats coexist during migration.
func KeyEntryName(workspace, pkg, symbolKey string) string {
	return fmt.Sprintf("symbol://%s/%s", workspace, ScopedSymbolID(pkg, symbolKey))
}

// ScopedSymbolID returns the package-scoped stable symbol identifier used by
// symbol embedding storage and key-based memory entry names.
func ScopedSymbolID(pkg, symbolKey string) string {
	pkg = strings.TrimSpace(pkg)
	symbolKey = strings.TrimSpace(symbolKey)
	if pkg == "" {
		return symbolKey
	}
	if symbolKey == "" {
		return pkg
	}
	return pkg + "::" + symbolKey
}

// ScopedSymbolIDFromKeyEntryName extracts "<pkg>::<symbolKey>" from a
// key-based symbol memory entry name.
func ScopedSymbolIDFromKeyEntryName(workspace, name string) (string, bool) {
	prefix := fmt.Sprintf("symbol://%s/", strings.TrimSpace(workspace))
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	scoped := strings.TrimPrefix(name, prefix)
	if !strings.Contains(scoped, "::") {
		return "", false
	}
	return scoped, true
}
