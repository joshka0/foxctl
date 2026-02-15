package symbol

import (
	"fmt"
	"strings"
)

// SymbolKey is a stable, file-path-independent identifier for a code symbol.
// It encodes only the qualified symbol name within its package, making it
// resilient to file moves and renames.
//
// Formats by language:
//   - Go: "QualifiedName" (e.g., "Builder.Build") or "init@filename.go" for init functions
//   - TS exported: "Name" (e.g., "ConversationsList")
//   - TS non-exported: "fileBasename/Name" (e.g., "utils.tsx/helperFunc")
//   - Elixir: "QualifiedName" (e.g., "MyApp.Server.handle_call")
type SymbolKey string

// String returns the SymbolKey as a string, trimming whitespace.
func (k SymbolKey) String() string { return strings.TrimSpace(string(k)) }

// Name extracts the human-readable name from the key.
// For keys with a "/" separator (non-exported TS), returns the part after the last "/".
func (k SymbolKey) Name() string {
	v := k.String()
	if idx := strings.LastIndex(v, "/"); idx != -1 {
		return v[idx+1:]
	}
	return v
}

// GoSymbolKey creates a SymbolKey for a Go symbol.
// For regular symbols, name is the qualified name (e.g., "Builder.Build").
// For init functions, use GoInitSymbolKey instead.
func GoSymbolKey(name string) SymbolKey {
	return SymbolKey(strings.TrimSpace(name))
}

// GoInitSymbolKey creates a SymbolKey for a Go init function.
// Since multiple init functions can exist per package, the filename is used for disambiguation.
// Format: "init@<filename>" (e.g., "init@store.go")
func GoInitSymbolKey(filename string) SymbolKey {
	return SymbolKey(fmt.Sprintf("init@%s", strings.TrimSpace(filename)))
}

// GoNonExportedSymbolKey creates a SymbolKey for an unexported Go symbol.
// Non-exported symbols are disambiguated by basename because they are package-local.
func GoNonExportedSymbolKey(name, fileBasename string) SymbolKey {
	name = strings.TrimSpace(name)
	fileBasename = strings.TrimSpace(fileBasename)
	if name == "" {
		return ""
	}
	if fileBasename == "" {
		return SymbolKey(name)
	}
	return SymbolKey(fmt.Sprintf("%s/%s", fileBasename, name))
}

// TSSymbolKey creates a SymbolKey for a TypeScript/JavaScript symbol.
// Exported symbols use just the name; non-exported symbols are prefixed with the file basename
// for disambiguation (e.g., "utils.tsx/helperFunc").
func TSSymbolKey(name string, exported bool, fileBasename string) SymbolKey {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if exported {
		return SymbolKey(name)
	}
	fileBasename = strings.TrimSpace(fileBasename)
	if fileBasename == "" {
		return SymbolKey(name)
	}
	return SymbolKey(fmt.Sprintf("%s/%s", fileBasename, name))
}

// ElixirSymbolKey creates a SymbolKey for an Elixir symbol.
// Uses the qualified name (e.g., "MyApp.Server.handle_call").
func ElixirSymbolKey(name string) SymbolKey {
	return SymbolKey(strings.TrimSpace(name))
}

// PythonSymbolKey creates a SymbolKey for a Python symbol.
// Uses the trimmed symbol name directly.
func PythonSymbolKey(name string) SymbolKey {
	return SymbolKey(strings.TrimSpace(name))
}
