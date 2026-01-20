package retrieval

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
)

// TreeNode represents a node in the semantic search tree.
// Can be either a file (leaf) or directory (branch).
type TreeNode struct {
	// Path is the relative path for this node (file or directory).
	Path string `json:"path"`

	// Score is the relevance score (0-1 range).
	// For files: normalized vector similarity.
	// For directories: aggregated from children.
	Score float64 `json:"score"`

	// Summary is a short description of the file or directory.
	// Optional for directories, recommended for files.
	Summary string `json:"summary,omitempty"`

	// IsDir indicates whether this is a directory node.
	IsDir bool `json:"is_dir,omitempty"`

	// Children contains nested nodes (empty for files).
	Children []*TreeNode `json:"children,omitempty"`

	// FileCount is the total number of files under this node (for directories).
	FileCount int `json:"file_count,omitempty"`
}

// TreeOutput is the result of tree construction.
type TreeOutput struct {
	// Nodes is the list of top-level nodes (usually just the root).
	Nodes []*TreeNode `json:"nodes"`

	// Stats provides metadata about the tree.
	Stats TreeStats `json:"stats"`
}

// TreeStats tracks tree construction metrics.
type TreeStats struct {
	TotalFiles        int `json:"total_files"`
	TotalDirectories  int `json:"total_directories"`
	MaxDepth          int `json:"max_depth"`
	SummariesIncluded int `json:"summaries_included"`
	SummariesMissing  int `json:"summaries_missing"`
}

// TreeOptions controls tree construction behavior.
type TreeOptions struct {
	// Depth is the maximum directory depth to include (default: 2).
	// 0 means unlimited depth.
	Depth int

	// MaxChildren is the maximum nodes per level (default: 10).
	// Nodes are sorted by score and truncated.
	MaxChildren int

	// IncludeSummaries enables file summaries in output (default: true).
	IncludeSummaries bool

	// TopK is the number of top-scoring children used for directory scoring.
	// Default: 3.
	TopK int

	// ASCIIIcons uses ASCII characters instead of emoji for tree rendering.
	// When true, uses [D] for directories and [F] for files.
	// Default: false (uses 📁 and 📄).
	ASCIIIcons bool
}

// DefaultTreeOptions returns sensible defaults for tree construction.
func DefaultTreeOptions() TreeOptions {
	return TreeOptions{
		Depth:            2,
		MaxChildren:      10,
		IncludeSummaries: true,
		TopK:             3,
	}
}

// FileEntry represents a file candidate for tree construction.
type FileEntry struct {
	// Path is the relative file path.
	Path string

	// Score is the relevance score (0-1).
	Score float64

	// Summary is the file summary (optional).
	Summary string
}

// TreeBuilder constructs hierarchical trees from flat file candidates.
type TreeBuilder struct {
	opts TreeOptions
}

// NewTreeBuilder creates a new tree builder with the given options.
// Note: Depth=0 means unlimited depth (no truncation).
func NewTreeBuilder(opts TreeOptions) *TreeBuilder {
	// Don't override Depth=0 - it means unlimited
	if opts.MaxChildren == 0 {
		opts.MaxChildren = 10
	}
	if opts.TopK == 0 {
		opts.TopK = 3
	}
	return &TreeBuilder{opts: opts}
}

// Build constructs a tree from flat file entries.
// The rootSummary parameter is used for the root node summary (can be empty).
func (b *TreeBuilder) Build(entries []FileEntry, rootSummary string) *TreeOutput {
	if len(entries) == 0 {
		return &TreeOutput{
			Nodes: []*TreeNode{{
				Path:    ".",
				IsDir:   true,
				Summary: rootSummary,
			}},
		}
	}

	// Build internal tree structure
	root := &treeBuilderNode{
		path:     ".",
		isDir:    true,
		children: make(map[string]*treeBuilderNode),
	}

	stats := TreeStats{}

	// Insert each file into the tree
	for _, entry := range entries {
		b.insertFile(root, entry)
		stats.TotalFiles++
	}

	// Compute directory scores bottom-up
	b.computeScores(root)

	// Convert to output format with depth limiting
	rootNode := b.toTreeNode(root, 0, &stats)
	rootNode.Summary = rootSummary

	// Sort and truncate children at each level
	b.pruneTree(rootNode)

	stats.MaxDepth = b.computeDepth(rootNode)

	return &TreeOutput{
		Nodes: []*TreeNode{rootNode},
		Stats: stats,
	}
}

// treeBuilderNode is the internal mutable tree structure.
type treeBuilderNode struct {
	path      string
	score     float64
	summary   string
	isDir     bool
	children  map[string]*treeBuilderNode
	fileCount int
}

// insertFile inserts a file entry into the tree, creating intermediate directories.
func (b *TreeBuilder) insertFile(root *treeBuilderNode, entry FileEntry) {
	// Split path into components
	parts := strings.Split(filepath.ToSlash(entry.Path), "/")
	current := root

	// Create intermediate directory nodes
	for i := 0; i < len(parts)-1; i++ {
		dirName := parts[i]
		if dirName == "" {
			continue
		}

		child, ok := current.children[dirName]
		if !ok {
			child = &treeBuilderNode{
				path:     filepath.Join(current.path, dirName),
				isDir:    true,
				children: make(map[string]*treeBuilderNode),
			}
			if current.path == "." {
				child.path = dirName
			}
			current.children[dirName] = child
		}
		current = child
	}

	// Create file node
	fileName := parts[len(parts)-1]
	fileNode := &treeBuilderNode{
		path:    entry.Path,
		score:   entry.Score,
		summary: entry.Summary,
		isDir:   false,
	}
	current.children[fileName] = fileNode
}

// computeScores computes directory scores bottom-up.
// Formula: dir_score = sum(top_k(file_scores)) / (1 + log(1 + total_files))
func (b *TreeBuilder) computeScores(node *treeBuilderNode) (score float64, fileCount int) {
	if !node.isDir {
		return node.score, 1
	}

	// Collect scores from all children
	var childScores []float64
	totalFiles := 0

	for _, child := range node.children {
		childScore, childFiles := b.computeScores(child)
		childScores = append(childScores, childScore)
		totalFiles += childFiles
	}

	node.fileCount = totalFiles

	if len(childScores) == 0 {
		return 0, 0
	}

	// Sort scores descending
	sort.Float64s(childScores)
	for i, j := 0, len(childScores)-1; i < j; i, j = i+1, j-1 {
		childScores[i], childScores[j] = childScores[j], childScores[i]
	}

	// Take top-k scores
	k := b.opts.TopK
	if k > len(childScores) {
		k = len(childScores)
	}

	sumTopK := 0.0
	for i := 0; i < k; i++ {
		sumTopK += childScores[i]
	}

	// Apply size penalty: score / (1 + log(1 + total_files))
	penalty := 1.0 + math.Log(1.0+float64(totalFiles))
	node.score = sumTopK / penalty

	return node.score, totalFiles
}

// toTreeNode converts the internal tree to the output format.
func (b *TreeBuilder) toTreeNode(node *treeBuilderNode, depth int, stats *TreeStats) *TreeNode {
	result := &TreeNode{
		Path:      node.path,
		Score:     node.score,
		IsDir:     node.isDir,
		FileCount: node.fileCount,
	}

	if b.opts.IncludeSummaries && node.summary != "" {
		result.Summary = node.summary
		stats.SummariesIncluded++
	} else if !node.isDir && node.summary == "" {
		stats.SummariesMissing++
	}

	if node.isDir {
		stats.TotalDirectories++

		// Check depth limit (0 means unlimited)
		if b.opts.Depth > 0 && depth >= b.opts.Depth {
			// At max depth, flatten remaining files
			result.Children = b.flattenChildren(node, stats)
		} else {
			// Recurse into children
			result.Children = make([]*TreeNode, 0, len(node.children))
			for _, child := range node.children {
				childNode := b.toTreeNode(child, depth+1, stats)
				result.Children = append(result.Children, childNode)
			}
		}
	}

	return result
}

// flattenChildren collects all file descendants into a flat list.
func (b *TreeBuilder) flattenChildren(node *treeBuilderNode, stats *TreeStats) []*TreeNode {
	var files []*TreeNode
	b.collectFiles(node, &files, stats)
	return files
}

// collectFiles recursively collects all file nodes.
func (b *TreeBuilder) collectFiles(node *treeBuilderNode, files *[]*TreeNode, stats *TreeStats) {
	for _, child := range node.children {
		if child.isDir {
			b.collectFiles(child, files, stats)
		} else {
			fileNode := &TreeNode{
				Path:  child.path,
				Score: child.score,
				IsDir: false,
			}
			if b.opts.IncludeSummaries && child.summary != "" {
				fileNode.Summary = child.summary
				stats.SummariesIncluded++
			} else if child.summary == "" {
				stats.SummariesMissing++
			}
			*files = append(*files, fileNode)
		}
	}
}

// pruneTree sorts children by score and truncates to MaxChildren.
func (b *TreeBuilder) pruneTree(node *TreeNode) {
	if node.Children == nil {
		return
	}

	// Sort children by score descending
	sort.Slice(node.Children, func(i, j int) bool {
		return node.Children[i].Score > node.Children[j].Score
	})

	// Truncate to max children
	if len(node.Children) > b.opts.MaxChildren {
		node.Children = node.Children[:b.opts.MaxChildren]
	}

	// Recurse
	for _, child := range node.Children {
		b.pruneTree(child)
	}
}

// computeDepth returns the maximum depth of the tree.
func (b *TreeBuilder) computeDepth(node *TreeNode) int {
	if len(node.Children) == 0 {
		return 0
	}
	maxChildDepth := 0
	for _, child := range node.Children {
		childDepth := b.computeDepth(child)
		if childDepth > maxChildDepth {
			maxChildDepth = childDepth
		}
	}
	return maxChildDepth + 1
}

// RenderText renders the tree as indented text suitable for CLI output.
func (b *TreeBuilder) RenderText(output *TreeOutput) string {
	if output == nil || len(output.Nodes) == 0 {
		return "(empty tree)"
	}

	var sb strings.Builder
	for _, node := range output.Nodes {
		b.renderNode(&sb, node, 0)
	}
	return sb.String()
}

// renderNode recursively renders a node and its children.
func (b *TreeBuilder) renderNode(sb *strings.Builder, node *TreeNode, indent int) {
	prefix := strings.Repeat("  ", indent)

	// Select icons based on ASCIIIcons option
	dirIcon := "📁"
	fileIcon := "📄"
	if b.opts.ASCIIIcons {
		dirIcon = "[D]"
		fileIcon = "[F]"
	}

	// Format score as percentage
	scoreStr := fmt.Sprintf("%.0f%%", node.Score*100)

	if node.IsDir {
		// Directory format
		if node.Path == "." {
			fmt.Fprintf(sb, "%s%s . [%s]", prefix, dirIcon, scoreStr)
		} else {
			fmt.Fprintf(sb, "%s%s %s/ [%s]", prefix, dirIcon, filepath.Base(node.Path), scoreStr)
		}
		if node.FileCount > 0 {
			fmt.Fprintf(sb, " (%d files)", node.FileCount)
		}
	} else {
		// File format
		fmt.Fprintf(sb, "%s%s %s [%s]", prefix, fileIcon, filepath.Base(node.Path), scoreStr)
	}

	// Add summary if present
	if node.Summary != "" {
		fmt.Fprintf(sb, "\n%s   %s", prefix, node.Summary)
	}

	sb.WriteString("\n")

	// Render children
	for _, child := range node.Children {
		b.renderNode(sb, child, indent+1)
	}
}

// CandidatesToFileEntries converts retrieval candidates to file entries.
// This is a bridge function for integration with the existing retrieval system.
func CandidatesToFileEntries(candidates []Candidate, summaries map[string]string) []FileEntry {
	entries := make([]FileEntry, 0, len(candidates))
	for _, c := range candidates {
		entry := FileEntry{
			Path:  c.Path,
			Score: c.Score,
		}
		if summaries != nil {
			entry.Summary = summaries[c.Path]
		}
		entries = append(entries, entry)
	}
	return entries
}

// MergeFileEntries merges two sets of file entries, keeping the best score per path.
// Summaries from the primary set are preferred unless missing.
func MergeFileEntries(primary, secondary []FileEntry) []FileEntry {
	if len(primary) == 0 && len(secondary) == 0 {
		return nil
	}

	merged := make(map[string]FileEntry, len(primary)+len(secondary))
	for _, entry := range primary {
		if entry.Path == "" {
			continue
		}
		merged[entry.Path] = entry
	}

	for _, entry := range secondary {
		if entry.Path == "" {
			continue
		}
		if existing, ok := merged[entry.Path]; ok {
			if entry.Score > existing.Score {
				existing.Score = entry.Score
			}
			if existing.Summary == "" && entry.Summary != "" {
				existing.Summary = entry.Summary
			}
			merged[entry.Path] = existing
			continue
		}
		merged[entry.Path] = entry
	}

	entries := make([]FileEntry, 0, len(merged))
	for _, entry := range merged {
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		return entries[i].Path < entries[j].Path
	})

	return entries
}
