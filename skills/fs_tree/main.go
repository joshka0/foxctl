// Package main implements the fs/tree skill.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	fshelpers "github.com/joshka0/foxctl/internal/adapters/skillslib/fs"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

// Input defines the input parameters for fs/tree operations.
type Input struct {
	Path          string `json:"path"`
	MaxDepth      int    `json:"max_depth" validate:"gte=0"`
	IncludeHidden bool   `json:"include_hidden"`
	IncludeSize   bool   `json:"include_size"`
	DirsOnly      bool   `json:"dirs_only"`
	Pattern       string `json:"pattern"`
	Format        string `json:"format" validate:"omitempty,oneof=tree list json"`
}

// treeNode represents a node in the directory tree.
type treeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"is_dir"`
	Size     int64       `json:"size,omitempty"`
	Children []*treeNode `json:"children,omitempty"`
	Level    int         `json:"level"`
}

// treeStats contains statistics about the directory tree.
type treeStats struct {
	TotalDirs  int   `json:"total_dirs"`
	TotalFiles int   `json:"total_files"`
	TotalSize  int64 `json:"total_size"`
	MaxDepth   int   `json:"max_depth"`
}

// treeOutput contains the complete tree representation.
type treeOutput struct {
	Root     *treeNode `json:"root"`
	Stats    treeStats `json:"stats"`
	TreeText string    `json:"tree_text,omitempty"`
	ListText []string  `json:"list_text,omitempty"`
}

// main is the skill entry point for fs/tree.
func main() {
	skillmain.Main("fs/tree", run)
}

// run orchestrates directory tree generation with multiple output formats and statistics.
//
// Index:
//   Purpose: Generate directory trees with filtering, statistics, and multiple output formats
//   Flow: validate input → resolve path → build tree → generate output format → emit results
//   SideEffects: directory traversal; file system access; CAS storage for large trees
//   FailureModes: invalid paths, permission errors, directory read errors
//   Observability: emits tree structure, statistics, and artifact hints for large trees
//   Related: buildTree, renderTree, renderList, formatSize
//   Keywords: fs/tree, directory_tree, tree_generation, statistics, file_system
//
// [[domain:directory-tree-generation]]
// [[protocol:recursive-tree-rendering]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if in.MaxDepth <= 0 {
		in.MaxDepth = 3
	}
	if in.Format == "" {
		in.Format = "tree"
	}

	// Resolve workspace and search path
	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	// Build tree
	root, stats, err := buildTree(searchPath, workspace, in, 0)
	if err != nil {
		return err
	}

	// Generate output based on format
	output := treeOutput{
		Root:  root,
		Stats: stats,
	}

	switch in.Format {
	case "tree":
		output.TreeText = renderTree(root, "", true, in.IncludeSize)
	case "list":
		output.ListText = renderList(root, in.IncludeSize)
	case "json":
		// JSON format already has the tree structure
	}

	// Prepare artifact for tree text (can be large)
	var artifact *skillmain.Artifact
	if in.Format == "tree" && len(output.TreeText) > 1024 {
		buf := bytes.NewBufferString(output.TreeText)
		persisted, err := skillout.PersistBuffer(ctx, rc, buf, "text/plain", "fs_tree")
		if err != nil {
			return err
		}
		artifact = &persisted
		// Keep a preview of the tree
		lines := strings.Split(output.TreeText, "\n")
		if len(lines) > 50 {
			output.TreeText = strings.Join(lines[:50], "\n") + "\n... (truncated)"
		}
	}

	// Build response
	data := map[string]any{
		"tree":   output,
		"format": in.Format,
	}
	skillout.AddArtifact(data, artifact)

	return skillout.Emit(rc, "fs/tree", data)
}

// buildTree recursively builds the directory tree structure.
func buildTree(path, workspace string, in Input, level int) (*treeNode, treeStats, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, treeStats{}, skillerr.WrapIO("stat "+path, err)
	}

	relPath := pathutil.RelTo(workspace, path)
	node := &treeNode{
		Name:  filepath.Base(path),
		Path:  relPath,
		IsDir: info.IsDir(),
		Size:  info.Size(),
		Level: level,
	}

	stats := treeStats{
		MaxDepth: level,
	}

	if !info.IsDir() {
		stats.TotalFiles = 1
		stats.TotalSize = info.Size()
		return node, stats, nil
	}

	stats.TotalDirs = 1

	// Don't descend further if we've reached max depth
	if level >= in.MaxDepth {
		return node, stats, nil
	}

	// Read directory entries
	entries, err := os.ReadDir(path)
	if err != nil {
		return node, stats, nil // Skip directories we can't read
	}

	// Sort entries: directories first, then alphabetically
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if fsutil.IsSymlinkMode(entry.Type()) {
			continue
		}

		// Skip hidden files/directories
		if fshelpers.ShouldSkipHidden(entry.Name(), in.IncludeHidden) {
			continue
		}

		// Skip common excludes
		if fsutil.IsCommonExclude(entry.Name()) {
			continue
		}

		// Skip files if dirs_only
		if in.DirsOnly && !entry.IsDir() {
			continue
		}

		// Pattern filtering (only for files)
		if in.Pattern != "" && !entry.IsDir() {
			matched, err := filepath.Match(in.Pattern, entry.Name())
			if err != nil || !matched {
				continue
			}
		}

		childPath := filepath.Join(path, entry.Name())
		childNode, childStats, err := buildTree(childPath, workspace, in, level+1)
		if err != nil {
			continue // Skip problematic entries
		}

		node.Children = append(node.Children, childNode)

		// Aggregate stats
		stats.TotalDirs += childStats.TotalDirs
		stats.TotalFiles += childStats.TotalFiles
		stats.TotalSize += childStats.TotalSize
		if childStats.MaxDepth > stats.MaxDepth {
			stats.MaxDepth = childStats.MaxDepth
		}
	}

	return node, stats, nil
}

// renderTree generates ASCII tree representation with optional size information.
func renderTree(node *treeNode, prefix string, isLast bool, includeSize bool) string {
	if node == nil {
		return ""
	}

	var builder strings.Builder

	// Build current line
	if node.Level == 0 {
		builder.WriteString(node.Name)
	} else {
		var connector string
		if isLast {
			connector = "└── "
		} else {
			connector = "├── "
		}

		builder.WriteString(prefix)
		builder.WriteString(connector)
		builder.WriteString(node.Name)
	}

	// Add size if requested and it's a file
	if includeSize && !node.IsDir && node.Size > 0 {
		builder.WriteString(fmt.Sprintf(" (%s)", formatSize(node.Size)))
	}

	// Add directory indicator
	if node.IsDir && len(node.Children) > 0 {
		builder.WriteString("/")
	}

	builder.WriteString("\n")

	// Render children
	if node.IsDir && len(node.Children) > 0 {
		var newPrefix string
		if node.Level == 0 {
			newPrefix = ""
		} else if isLast {
			newPrefix = prefix + "    "
		} else {
			newPrefix = prefix + "│   "
		}

		for i, child := range node.Children {
			isChildLast := i == len(node.Children)-1
			builder.WriteString(renderTree(child, newPrefix, isChildLast, includeSize))
		}
	}

	return builder.String()
}

// renderList generates flat list representation with indentation.
func renderList(node *treeNode, includeSize bool) []string {
	var lines []string

	var traverse func(*treeNode)
	traverse = func(n *treeNode) {
		if n == nil {
			return
		}

		indent := strings.Repeat("  ", n.Level)
		line := indent + n.Path

		if includeSize && !n.IsDir && n.Size > 0 {
			line += fmt.Sprintf(" (%s)", formatSize(n.Size))
		}

		if n.IsDir {
			line += "/"
		}

		lines = append(lines, line)

		for _, child := range n.Children {
			traverse(child)
		}
	}

	traverse(node)
	return lines
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
