package symbolutil

import "fmt"

// EntryName returns the canonical name for a symbol memory entry.
// Format: symbol://<workspace>/<file_path>:<symbol_name>
func EntryName(workspace, filePath, symbolName string) string {
	return fmt.Sprintf("symbol://%s/%s:%s", workspace, filePath, symbolName)
}

// FileMetaEntryName returns the canonical name for a file meta memory entry.
// Format: symbol-meta://<workspace>/<file_path>
func FileMetaEntryName(workspace, filePath string) string {
	return fmt.Sprintf("symbol-meta://%s/%s", workspace, filePath)
}
