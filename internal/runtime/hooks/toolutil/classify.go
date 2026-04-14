package toolutil

import "strings"

// Kind represents a tool category.
type Kind string

const (
	KindRead   Kind = "read"   // File/content reading operations
	KindWrite  Kind = "write"  // File/content modification operations
	KindExec   Kind = "exec"   // Command/process execution
	KindSearch Kind = "search" // Search/query operations
	KindPlan   Kind = "plan"   // Planning/task management
	KindAny    Kind = "any"    // Matches any kind
)

// CCWriteTools are Claude Code tool names that perform write operations.
var CCWriteTools = []string{
	"Edit",
	"Write",
	"MultiEdit",
	"NotebookEdit",
}

// CCReadTools are Claude Code tool names that perform read operations.
var CCReadTools = []string{
	"Read",
}

// CCSearchTools are Claude Code tool names that perform search operations.
var CCSearchTools = []string{
	"Grep",
	"Glob",
}

// CCExecTools are Claude Code tool names that perform exec operations.
var CCExecTools = []string{
	"Bash",
	"Task",
}

// CCPlanTools are Claude Code tool names for planning/task management.
var CCPlanTools = []string{
	"TodoWrite",
}

// IsWriteOperation returns true if the tool performs a write operation.
// This is the cross-platform version that checks:
// - CC tool names (Edit, Write, MultiEdit, NotebookEdit)
// - Canonical tool names (edit.*, fs.write_*, fs.create_*)
// - Tool kind ("write")
func IsWriteOperation(toolName, toolCanonical, toolKind string) bool {
	// Check explicit kind
	if toolKind == string(KindWrite) {
		return true
	}

	// Check CC tool names
	for _, t := range CCWriteTools {
		if toolName == t {
			return true
		}
	}

	// Check CC plan tools (treat as write for task guard purposes)
	for _, t := range CCPlanTools {
		if toolName == t {
			return true
		}
	}

	// Check canonical tool names
	if toolCanonical != "" {
		if strings.HasPrefix(toolCanonical, "edit.") {
			return true
		}
		if strings.HasPrefix(toolCanonical, "fs.write") || strings.HasPrefix(toolCanonical, "fs.create") {
			return true
		}
		if strings.HasPrefix(toolCanonical, "todo.") {
			return true
		}
	}

	return false
}

// IsReadOperation returns true if the tool performs a read operation.
func IsReadOperation(toolName, toolCanonical, toolKind string) bool {
	// Check explicit kind
	if toolKind == string(KindRead) {
		return true
	}

	// Check CC tool names
	for _, t := range CCReadTools {
		if toolName == t {
			return true
		}
	}

	// Check canonical tool names
	if toolCanonical != "" {
		if strings.HasPrefix(toolCanonical, "fs.read") {
			return true
		}
		// fs.* without write/create is probably read
		if strings.HasPrefix(toolCanonical, "fs.") &&
			!strings.HasPrefix(toolCanonical, "fs.write") &&
			!strings.HasPrefix(toolCanonical, "fs.create") {
			return true
		}
	}

	return false
}

// IsSearchOperation returns true if the tool performs a search operation.
func IsSearchOperation(toolName, toolCanonical, toolKind string) bool {
	// Check explicit kind
	if toolKind == string(KindSearch) {
		return true
	}

	// Check CC tool names
	for _, t := range CCSearchTools {
		if toolName == t {
			return true
		}
	}

	// Check canonical tool names
	if toolCanonical != "" {
		if strings.HasPrefix(toolCanonical, "code.search") ||
			strings.HasPrefix(toolCanonical, "code.semantic") ||
			strings.HasPrefix(toolCanonical, "text.grep") ||
			strings.HasPrefix(toolCanonical, "text.") {
			return true
		}
	}

	return false
}

// IsExecOperation returns true if the tool performs command execution.
func IsExecOperation(toolName, toolCanonical, toolKind string) bool {
	// Check explicit kind
	if toolKind == string(KindExec) {
		return true
	}

	// Check CC tool names
	for _, t := range CCExecTools {
		if toolName == t {
			return true
		}
	}

	// Check canonical tool names
	if toolCanonical != "" {
		if strings.HasPrefix(toolCanonical, "tests.") ||
			strings.HasPrefix(toolCanonical, "bash.") ||
			strings.HasPrefix(toolCanonical, "shell.") {
			return true
		}
	}

	return false
}

// ClassifyTool determines the Kind of a tool based on its names.
func ClassifyTool(toolName, toolCanonical, toolKind string) Kind {
	// If kind is explicitly provided, use it
	if toolKind != "" && toolKind != string(KindAny) {
		return Kind(toolKind)
	}

	// Check each category
	if IsWriteOperation(toolName, toolCanonical, "") {
		return KindWrite
	}
	if IsReadOperation(toolName, toolCanonical, "") {
		return KindRead
	}
	if IsSearchOperation(toolName, toolCanonical, "") {
		return KindSearch
	}
	if IsExecOperation(toolName, toolCanonical, "") {
		return KindExec
	}

	return KindAny
}

// NormalizeToolName returns a normalized tool name for consistent matching.
// It handles case variations and common aliases.
func NormalizeToolName(toolName string) string {
	// For now, just return as-is
	// Can add case normalization or alias mapping later
	return toolName
}

// CanonicalToCC maps canonical tool names to CC equivalents where applicable.
var CanonicalToCC = map[string]string{
	"edit.apply_patch": "Edit",
	"fs.read_file":     "Read",
	"fs.write_file":    "Write",
}

// CCToCanonical maps CC tool names to canonical equivalents.
var CCToCanonical = map[string]string{
	"Edit":         "edit.apply_patch",
	"Write":        "fs.write_file",
	"Read":         "fs.read_file",
	"MultiEdit":    "edit.multi_patch",
	"NotebookEdit": "edit.notebook_patch",
	"Grep":         "text.grep",
	"Glob":         "text.glob",
	"Bash":         "shell.execute",
	"Task":         "agent.spawn",
	"TodoWrite":    "todo.write",
}

// ToCanonical converts a tool name to its canonical form.
// If already canonical (contains a dot) or unknown, returns as-is.
func ToCanonical(toolName string) string {
	if strings.Contains(toolName, ".") {
		return toolName // Already canonical
	}
	if canonical, ok := CCToCanonical[toolName]; ok {
		return canonical
	}
	return toolName
}

// ToCC converts a canonical tool name to its CC equivalent.
// If not mappable, returns empty string.
func ToCC(toolCanonical string) string {
	if cc, ok := CanonicalToCC[toolCanonical]; ok {
		return cc
	}
	return ""
}
