// Package main implements the fs/tree skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	Path          string `json:"path"`
	MaxDepth      int    `json:"max_depth"`
	IncludeHidden bool   `json:"include_hidden"`
	IncludeSize   bool   `json:"include_size"`
	DirsOnly      bool   `json:"dirs_only"`
	Pattern       string `json:"pattern"`
	Format        string `json:"format"`
}

type treeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"is_dir"`
	Size     int64       `json:"size,omitempty"`
	Children []*treeNode `json:"children,omitempty"`
	Level    int         `json:"level"`
}

type treeStats struct {
	TotalDirs  int   `json:"total_dirs"`
	TotalFiles int   `json:"total_files"`
	TotalSize  int64 `json:"total_size"`
	MaxDepth   int   `json:"max_depth"`
}

type treeOutput struct {
	Root     *treeNode `json:"root"`
	Stats    treeStats `json:"stats"`
	TreeText string    `json:"tree_text,omitempty"`
	ListText []string  `json:"list_text,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("fs/tree", "ECONFIG", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("fs/tree", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("fs/tree", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("fs/tree", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Resolve workspace and search path
	workspace := rc.PathValidator.Workspace()
	searchPath := workspace
	if in.Path != "" {
		validated, err := rc.PathValidator.ValidatePath(in.Path)
		if err != nil {
			return fmt.Errorf("path validation failed: %w", err)
		}
		searchPath = validated
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
	var artifact runner.Artifact
	if in.Format == "tree" && len(output.TreeText) > 1024 {
		buf := bytes.NewBufferString(output.TreeText)
		artifact, err = runner.PersistBuffer(ctx, rc, buf, "text/plain", "fs_tree")
		if err != nil {
			return err
		}
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
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
		data["artifact_kind"] = artifact.Kind
		data["artifact_size_bytes"] = artifact.Size
	}

	return rc.Emit("fs/tree", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if in.MaxDepth <= 0 {
		in.MaxDepth = 3
	}
	if in.Format == "" {
		in.Format = "tree"
	}
	return in, nil
}

func buildTree(path, workspace string, in input, level int) (*treeNode, treeStats, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, treeStats{}, fmt.Errorf("stat path: %w", err)
	}

	relPath := relativeTo(workspace, path)
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
		fmt.Printf("Debug: buildTree processing %s (dir=%v)\n", entry.Name(), entry.IsDir())
		// Skip hidden files/directories
		if !in.IncludeHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		// Skip common excludes
		if isCommonExclude(entry.Name()) {
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

func renderTree(node *treeNode, prefix string, isLast bool, includeSize bool) string {
	if node == nil {
		return ""
	}

	var builder strings.Builder

	// Build current line
	if node.Level == 0 {
		_, _ = builder.WriteString(node.Name)
	} else {
		var connector string
		if isLast {
			connector = "└── "
		} else {
			connector = "├── "
		}

		_, _ = builder.WriteString(prefix)
		_, _ = builder.WriteString(connector)
		_, _ = builder.WriteString(node.Name)
	}

	// Add size if requested and it's a file
	if includeSize && !node.IsDir && node.Size > 0 {
		_, _ = builder.WriteString(fmt.Sprintf(" (%s)", formatSize(node.Size)))
	}

	// Add directory indicator
	if node.IsDir && len(node.Children) > 0 {
		_, _ = builder.WriteString("/")
	}

	_, _ = builder.WriteString("\n")

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
			_, _ = builder.WriteString(renderTree(child, newPrefix, isChildLast, includeSize))
		}
	}

	return builder.String()
}

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

func isCommonExclude(name string) bool {
	excludes := []string{
		".git", ".svn", ".hg",
		"node_modules", "vendor", "__pycache__",
		".venv", "venv", ".tox",
		"dist", "build", "target",
	}
	for _, exclude := range excludes {
		if name == exclude {
			return true
		}
	}
	return false
}

func relativeTo(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	if strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit fs/tree failure")
	os.Exit(1)
}
