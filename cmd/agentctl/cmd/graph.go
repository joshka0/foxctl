package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/spf13/cobra"
)

func newGraphCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Manage the dependency graph",
	}
	cmd.AddCommand(
		newGraphStatsCommand(),
		newGraphTopCommand(),
		newGraphRepairCommand(),
		newGraphEdgesCommand(),
	)
	return cmd
}

func newGraphStatsCommand() *cobra.Command {
	var workspace string
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show graph statistics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}

			ws := resolveGraphWorkspace(workspace)

			store, err := graph.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return fmt.Errorf("open graph store: %w", err)
			}
			defer store.Close()

			stats, err := store.Stats(ctx, ws)
			if err != nil {
				return fmt.Errorf("get stats: %w", err)
			}

			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(stats)
			}

			// Human-readable output
			fmt.Fprintf(cmd.OutOrStdout(), "Graph Statistics for %s\n", ws)
			fmt.Fprintf(cmd.OutOrStdout(), "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
			fmt.Fprintf(cmd.OutOrStdout(), "\nNodes: %d\n", stats.Nodes.TotalNodes)
			fmt.Fprintf(cmd.OutOrStdout(), "  Avg PageRank: %.6f\n", stats.Nodes.AvgPageRank)
			fmt.Fprintf(cmd.OutOrStdout(), "  Max PageRank: %.6f\n", stats.Nodes.MaxPageRank)
			fmt.Fprintf(cmd.OutOrStdout(), "  Avg In-Degree: %.2f\n", stats.Nodes.AvgInDegree)
			fmt.Fprintf(cmd.OutOrStdout(), "  Avg Out-Degree: %.2f\n", stats.Nodes.AvgOutDegree)

			if len(stats.Nodes.ByType) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  By Type:\n")
				for nodeType, count := range stats.Nodes.ByType {
					fmt.Fprintf(cmd.OutOrStdout(), "    %s: %d\n", nodeType, count)
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\nEdges: %d\n", stats.Edges.TotalEdges)
			fmt.Fprintf(cmd.OutOrStdout(), "  Avg Weight: %.2f\n", stats.Edges.AvgWeight)

			if len(stats.Edges.ByType) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  By Type:\n")
				for edgeType, count := range stats.Edges.ByType {
					fmt.Fprintf(cmd.OutOrStdout(), "    %s: %d\n", edgeType, count)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: current directory)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newGraphTopCommand() *cobra.Command {
	var workspace string
	var nodeType string
	var limit int
	var minRank float64
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "top",
		Short: "Show top nodes by PageRank",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}

			ws := resolveGraphWorkspace(workspace)

			store, err := graph.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return fmt.Errorf("open graph store: %w", err)
			}
			defer store.Close()

			opts := graph.TopNodesOptions{
				Workspace: ws,
				Limit:     limit,
				MinRank:   minRank,
			}
			if nodeType != "" {
				nt := graph.NodeType(nodeType)
				opts.NodeType = &nt
			}

			nodes, err := store.TopNodes(ctx, opts)
			if err != nil {
				return fmt.Errorf("get top nodes: %w", err)
			}

			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(nodes)
			}

			// Human-readable output
			fmt.Fprintf(cmd.OutOrStdout(), "Top %d Nodes by PageRank\n", len(nodes))
			fmt.Fprintf(cmd.OutOrStdout(), "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

			for i, node := range nodes {
				title := node.Title
				if title == "" {
					title = node.NodeID
				}
				// Truncate long titles
				if len(title) > 50 {
					title = title[:47] + "..."
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%2d. [%s] %s\n", i+1, node.NodeType, title)
				fmt.Fprintf(cmd.OutOrStdout(), "    PageRank: %.6f | In: %d | Out: %d\n", node.PageRank, node.InDegree, node.OutDegree)
				if node.CurrentPath != "" {
					path := node.CurrentPath
					if len(path) > 60 {
						path = "..." + path[len(path)-57:]
					}
					fmt.Fprintf(cmd.OutOrStdout(), "    Path: %s\n", path)
				}
			}

			if len(nodes) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No nodes found.\n")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: current directory)")
	cmd.Flags().StringVar(&nodeType, "type", "", "Filter by node type (session, task, symbol, memory, file)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum number of nodes to show")
	cmd.Flags().Float64Var(&minRank, "min-rank", 0.0, "Minimum PageRank threshold")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newGraphRepairCommand() *cobra.Command {
	var workspace string
	var dryRun bool
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Repair graph by cleaning expired/dangling edges and recalculating degrees",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}

			ws := resolveGraphWorkspace(workspace)

			store, err := graph.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return fmt.Errorf("open graph store: %w", err)
			}
			defer store.Close()

			result := make(map[string]any)
			result["workspace"] = ws

			if dryRun {
				// Just show stats about what would be cleaned
				stats, err := store.Stats(ctx, ws)
				if err != nil {
					return fmt.Errorf("get stats: %w", err)
				}
				result["dry_run"] = true
				result["current_nodes"] = stats.Nodes.TotalNodes
				result["current_edges"] = stats.Edges.TotalEdges

				if outputJSON {
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(result)
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Dry run - would repair graph for %s\n", ws)
				fmt.Fprintf(cmd.OutOrStdout(), "Current: %d nodes, %d edges\n", stats.Nodes.TotalNodes, stats.Edges.TotalEdges)
				fmt.Fprintf(cmd.OutOrStdout(), "Run without --dry-run to perform cleanup.\n")
				return nil
			}

			// Step 1: Clean expired edges
			expiredCount, err := store.CleanupExpiredEdges(ctx)
			if err != nil {
				return fmt.Errorf("cleanup expired edges: %w", err)
			}
			result["expired_edges_removed"] = expiredCount

			// Step 2: Clean dangling edges
			danglingCount, err := store.CleanupDanglingEdges(ctx, ws)
			if err != nil {
				return fmt.Errorf("cleanup dangling edges: %w", err)
			}
			result["dangling_edges_removed"] = danglingCount

			// Step 3: Recalculate degrees
			if err := store.RecalculateDegrees(ctx, ws); err != nil {
				return fmt.Errorf("recalculate degrees: %w", err)
			}
			result["degrees_recalculated"] = true

			// Get final stats
			stats, err := store.Stats(ctx, ws)
			if err == nil {
				result["final_nodes"] = stats.Nodes.TotalNodes
				result["final_edges"] = stats.Edges.TotalEdges
			}

			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Graph repair complete for %s\n", ws)
			fmt.Fprintf(cmd.OutOrStdout(), "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Expired edges removed: %d\n", expiredCount)
			fmt.Fprintf(cmd.OutOrStdout(), "Dangling edges removed: %d\n", danglingCount)
			fmt.Fprintf(cmd.OutOrStdout(), "Degrees recalculated: ✓\n")
			if stats.Nodes.TotalNodes > 0 || stats.Edges.TotalEdges > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Final: %d nodes, %d edges\n", stats.Nodes.TotalNodes, stats.Edges.TotalEdges)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: current directory)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be cleaned without making changes")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newGraphEdgesCommand() *cobra.Command {
	var workspace string
	var nodeID string
	var direction string
	var edgeTypes []string
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "edges",
		Short: "Show edges for a node",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if nodeID == "" {
				return fmt.Errorf("--node is required")
			}

			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}

			ws := resolveGraphWorkspace(workspace)

			store, err := graph.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return fmt.Errorf("open graph store: %w", err)
			}
			defer store.Close()

			// Convert edge type strings to EdgeType
			var edgeTypeFilters []graph.EdgeType
			for _, et := range edgeTypes {
				edgeTypeFilters = append(edgeTypeFilters, graph.EdgeType(et))
			}

			var edges []graph.Edge
			dir := strings.ToLower(direction)

			switch dir {
			case "out", "outgoing":
				edges, err = store.GetEdgesFrom(ctx, ws, nodeID, edgeTypeFilters)
			case "in", "incoming":
				edges, err = store.GetEdgesTo(ctx, ws, nodeID, edgeTypeFilters)
			case "both", "":
				outEdges, outErr := store.GetEdgesFrom(ctx, ws, nodeID, edgeTypeFilters)
				if outErr != nil {
					return fmt.Errorf("get outgoing edges: %w", outErr)
				}
				inEdges, inErr := store.GetEdgesTo(ctx, ws, nodeID, edgeTypeFilters)
				if inErr != nil {
					return fmt.Errorf("get incoming edges: %w", inErr)
				}
				edges = append(outEdges, inEdges...)
			default:
				return fmt.Errorf("invalid direction: %s (expected: out, in, both)", direction)
			}

			if err != nil {
				return fmt.Errorf("get edges: %w", err)
			}

			if outputJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(edges)
			}

			// Human-readable output
			fmt.Fprintf(cmd.OutOrStdout(), "Edges for node: %s\n", nodeID)
			fmt.Fprintf(cmd.OutOrStdout(), "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

			for _, edge := range edges {
				dirIndicator := "→"
				if edge.ToID == nodeID {
					dirIndicator = "←"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s [%s] %s %s %s\n",
					truncateID(edge.FromID), edge.EdgeType, dirIndicator, truncateID(edge.ToID), formatWeight(edge.Weight))
			}

			if len(edges) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No edges found.\n")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "\nTotal: %d edges\n", len(edges))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: current directory)")
	cmd.Flags().StringVar(&nodeID, "node", "", "Node ID to query (required)")
	cmd.Flags().StringVar(&direction, "direction", "both", "Edge direction: out, in, both")
	cmd.Flags().StringSliceVar(&edgeTypes, "type", nil, "Filter by edge type(s)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func resolveGraphWorkspace(workspace string) string {
	if workspace != "" {
		return workspace
	}
	// Default to current working directory
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func truncateID(id string) string {
	if len(id) > 40 {
		return id[:37] + "..."
	}
	return id
}

func formatWeight(w float64) string {
	if w == 1.0 {
		return ""
	}
	return fmt.Sprintf("(w=%.2f)", w)
}

func init() {
	rootCmd.AddCommand(newGraphCommand())
}
