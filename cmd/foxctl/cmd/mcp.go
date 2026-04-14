package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/buildinfo"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
	obsidiantool "github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
)

const (
	// maxInlineResponseBytes is the maximum size for inline responses (2KB)
	maxInlineResponseBytes = 2048

	// defaultPIDFile is the default location for the MCP daemon PID file
	defaultPIDFile = "~/.foxctl/mcp-daemon.pid"

	// shutdownTimeout is the maximum time to wait for graceful shutdown
	shutdownTimeout = 10 * time.Second
)

// daemonState tracks the running daemon state
type daemonState struct {
	pidFile   string
	startTime time.Time
	addr      string
}

// skillGroups defines logical groupings of foxctl skills exposed as first-class MCP tools.
// Use --groups flag to enable specific groups (e.g., --groups code-intel,project).
var skillGroups = map[string][]string{
	// code-intel: Code analysis and search tools
	"code-intel": {
		"code/semantic_search",
		"code/smart_search",
		"code/symbols",
		"code/snippet_extract",
		"code/context_grep",
		"code/dag_grep",
		"codemap/get",
	},
	// optimized-retrieval: Only the compact retrieval tools we optimized for agent use.
	// Keep repo_index_* and structured_shell in curated MCP registration to avoid name conflicts.
	"optimized-retrieval": {
		"code/semantic_search",
		"code/smart_search",
		"code/symbols",
		"code/snippet_extract",
		"code/context_grep",
		"code/dag_grep",
		"codemap/get",
		"code/refactor_scout",
	},
	// mobile: simulator/device automation surfaces.
	"mobile": {
		"mobile/android",
		"mobile/ios",
		"mobile/expo",
	},
	// godot: Godot editor/build automation.
	"godot": {
		"build/godot",
		"editor/godot",
	},
	// api: narrow HTTP/OpenAPI planning surface.
	"api": {
		"http/openapi",
	},
	// jira: stable Jira Cloud board and issue automation.
	"jira": {
		"jira/board",
		"jira/issue",
	},
	// refactor: refactor discovery/planning without the broader retrieval set.
	"refactor": {
		"code/refactor_scout",
		"code/refactor_advisor",
		"code/symbols",
		"code/context_grep",
		"code/snippet_extract",
		"code/semantic_search",
	},
	// context: session/context continuity tools.
	"context": {
		"session/recall",
		"session/timeline",
		"session/query",
		"session/summarize",
	},
	// room: command-backed durable room coordination surface.
	"room": {},
	// mux: command-backed terminal collaboration surface.
	"mux": {},
	// collab: combined room + mux collaboration surface.
	"collab": {},
	// code-write: Code modification tools
	"code-write": {
		"fs/apply_edit",
		"code/smart_write",
	},
	// project: Project management tools
	"project": {
		"fs/read",
		"todo/manage",
		"memory/query",
		"session/recall",
		"session/timeline",
	},
	// foxctl-ci: CI/CD integration tools
	"foxctl-ci": {
		"ci/checks",
		"ci/prcomments",
	},
}

// storeToCAS stores content in the CAS and returns the digest.
// Returns the digest in "sha256:hex" format.
func storeToCAS(ctx context.Context, content []byte, contentType string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Compute digest
	h := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(h[:])

	// Get CAS root from config or default
	home := os.Getenv("AGENTCTL_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get user home dir: %w", err)
		}
		home = filepath.Join(userHome, ".foxctl")
	}
	casRoot := filepath.Join(home, "cas", "sha256")

	// Ensure directory exists
	if err := os.MkdirAll(casRoot, 0o755); err != nil {
		return "", fmt.Errorf("create CAS dir: %w", err)
	}

	// Write content (skip if already exists - content-addressed)
	hexDigest := hex.EncodeToString(h[:])
	objPath := filepath.Join(casRoot, hexDigest)
	if _, err := os.Stat(objPath); os.IsNotExist(err) {
		if err := os.WriteFile(objPath, content, 0o644); err != nil {
			return "", fmt.Errorf("write CAS object: %w", err)
		}
	}

	return digest, nil
}

// truncateLargeResponse checks if a tool result exceeds the size limit.
// If so, stores the full content in CAS and returns a truncated result with reference.
// This specifically targets web search and extract tools (exa, tavily).
func truncateLargeResponse(ctx context.Context, result *mcp.CallToolResult, toolName string) *mcp.CallToolResult {
	if result == nil || len(result.Content) == 0 {
		return result
	}

	// Only apply to web/search tools
	webTools := map[string]bool{
		"web_search":         true,
		"web_search_general": true,
		"web_extract":        true,
		"web_crawl":          true,
		"web_map":            true,
		"ask":                true,
		"code_search":        true,
	}
	if !webTools[toolName] {
		return result
	}

	// Calculate total content size
	var totalSize int
	var textContents []mcp.TextContent
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			totalSize += len(tc.Text)
			textContents = append(textContents, tc)
		}
	}

	// If under limit, return as-is
	if totalSize <= maxInlineResponseBytes {
		return result
	}

	// Combine all text content
	var fullText strings.Builder
	for _, tc := range textContents {
		fullText.WriteString(tc.Text)
		fullText.WriteString("\n")
	}
	fullContent := fullText.String()

	// Store in CAS
	digest, err := storeToCAS(ctx, []byte(fullContent), "text/plain")
	if err != nil {
		// On error, just return original (don't fail the tool call)
		return result
	}

	// Create truncated response
	truncatedText := fullContent
	if len(truncatedText) > maxInlineResponseBytes {
		truncatedText = truncatedText[:maxInlineResponseBytes]
		for len(truncatedText) > 0 && !utf8.ValidString(truncatedText) {
			truncatedText = truncatedText[:len(truncatedText)-1]
		}
	}

	// Calculate total pages for the note
	totalPages := (totalSize + maxInlineResponseBytes - 1) / maxInlineResponseBytes

	note := fmt.Sprintf("\n\n---\n⚠️ Response truncated (%d bytes total, %d pages). Full output stored in CAS: %s\nRead page 2: foxctl cas read %s --page 2",
		totalSize, totalPages, digest, digest)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: truncatedText + note,
			},
		},
		IsError: result.IsError,
	}
}

// mcpServerConfig holds configuration for backend MCP servers.
type mcpServerConfig struct {
	// Stdio servers
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// HTTP servers
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// backendClients caches MCP client connections to avoid reconnecting on each call.
type backendClients struct {
	mu      sync.Mutex
	clients map[string]*client.Client
	configs map[string]mcpServerConfig
}

func skillGroupNames() []string {
	names := make([]string, 0, len(skillGroups)+1)
	for name := range skillGroups {
		names = append(names, name)
	}
	names = append(names, "all")
	sort.Strings(names)
	return names
}

var backends = &backendClients{
	clients: make(map[string]*client.Client),
	configs: make(map[string]mcpServerConfig),
}

func newMCPCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server facade for Claude Code",
		Long: `Run foxctl as an MCP server that provides a curated set of tools,
proxying requests to backend MCP servers (tavily, exa, context7, perplexity,
expo, supabase, playwright) and exposing local foxctl tools (repo_index_*, html_edit).

This reduces token overhead by exposing simplified tool schemas.`,
	}

	cmd.AddCommand(newMCPServeCommand())
	cmd.AddCommand(newMCPStopCommand())
	cmd.AddCommand(newMCPStatusCommand())
	return cmd
}

const (
	// defaultDaemonPort is the default port for daemon mode
	defaultDaemonPort = ":8091"

	// defaultLogFile is the log file for daemon mode
	defaultLogFile = "~/.foxctl/logs/mcp-daemon.log"
)

func newMCPServeCommand() *cobra.Command {
	var (
		configFile         string
		httpAddr           string
		enableSkills       bool
		daemonMode         bool
		skillGroupsF       []string
		optimizedRetrieval bool
		groupOnly          bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server",
		Long: `Start foxctl as an MCP server using stdio or HTTP/SSE transport.

By default, uses stdio transport for Claude Code integration.
Use --http to run as an HTTP daemon with SSE transport (foreground).
Use --daemon to run as an HTTP daemon in background mode.
Use --skills to expose all foxctl skills via generic agentctl_run/agentctl_skills tools.
Use --groups to expose specific skill groups as first-class MCP tools.
Use --group-only to expose only the specified groups (no default curated MCP tools).

Available skill groups:
  all         - All installed foxctl skills as first-class MCP tools
  code-intel  - Code analysis: semantic_search, smart_search, symbols, snippet_extract, context_grep, codemap_get, codemap_generate
  optimized-retrieval - Only the compact retrieval tools optimized for agent use
  mobile      - mobile/android, mobile/ios, mobile/expo
  godot       - build/godot, editor/godot
  api         - http/openapi
  refactor    - refactor_scout, refactor_advisor, symbols, snippet tools
  context     - session recall/timeline/query/summarize
  room        - command-backed room coordination tools
  mux         - command-backed terminal collaboration tools
  collab      - room + mux collaboration tools
  code-write  - Code modification: smart_write
  project     - Project management: todo/manage, memory/query, session/recall
  foxctl-ci - CI/CD integration: checks, prcomments

Configure backend MCP servers via --config or environment variables:
  TAVILY_API_KEY     - Tavily API key (web search, extract, crawl, map)
  EXA_API_KEY        - Exa API key (code search, web search)
  PERPLEXITY_API_KEY - Perplexity API key (ask)
  EXPO_TOKEN         - Expo access token (EAS builds, updates, submit)
  SUPABASE_URL       - Supabase project URL (required for supabase tools)
  SUPABASE_KEY       - Supabase service role key (required for supabase tools)

Example usage in Claude's mcp.json (stdio):
  {
    "mcpServers": {
      "foxctl": {
        "command": "/path/to/foxctl",
        "args": ["mcp", "serve", "--groups", "code-intel,project,foxctl-ci"]
      }
    }
  }

Example usage with HTTP/SSE daemon (foreground):
  foxctl mcp serve --http :8091 --groups code-intel,project

  # Start a narrow retrieval-focused MCP bridge
  foxctl mcp serve --optimized-retrieval

  # Start a mobile-only MCP bridge
  foxctl mcp serve --groups mobile --group-only

  # Start a Godot-only MCP bridge
  foxctl mcp serve --groups godot --group-only

  # Start a room-only MCP bridge
  foxctl mcp serve --groups room --group-only

  # Start a mux-only MCP bridge
  foxctl mcp serve --groups mux --group-only

  Example usage with daemon mode (background):
  # Start daemon in background
  foxctl mcp serve --daemon --groups code-intel,project,foxctl-ci

  # Check status
  foxctl mcp status

  # Stop daemon
  foxctl mcp stop

  # Configure in mcp.json
  {
    "mcpServers": {
      "foxctl": {
        "url": "http://localhost:8091/sse"
      }
    }
  }`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !optimizedRetrieval {
				for _, group := range skillGroupsF {
					if strings.EqualFold(strings.TrimSpace(group), "optimized-retrieval") {
						optimizedRetrieval = true
						break
					}
				}
			}
			if optimizedRetrieval && len(skillGroupsF) == 0 {
				skillGroupsF = []string{"optimized-retrieval"}
			}
			// Daemon mode implies HTTP mode with default port
			if daemonMode {
				if httpAddr == "" {
					httpAddr = defaultDaemonPort
				}
				return runDaemonMode(cmd.Context(), mcpServerOptions{
					configFile:         configFile,
					httpAddr:           httpAddr,
					enableSkills:       enableSkills,
					groups:             skillGroupsF,
					optimizedRetrieval: optimizedRetrieval,
					groupOnly:          groupOnly,
				})
			}
			return runMCPServer(cmd.Context(), mcpServerOptions{
				configFile:         configFile,
				httpAddr:           httpAddr,
				enableSkills:       enableSkills,
				groups:             skillGroupsF,
				optimizedRetrieval: optimizedRetrieval,
				groupOnly:          groupOnly,
			})
		},
	}

	cmd.Flags().StringVar(&configFile, "config", "", "Path to backend MCP servers config file")
	cmd.Flags().StringVar(&httpAddr, "http", "", "HTTP address for SSE daemon mode (e.g., :8091)")
	cmd.Flags().BoolVar(&enableSkills, "skills", false, "Expose all foxctl skills via agentctl_run/agentctl_skills tools")
	cmd.Flags().BoolVar(&daemonMode, "daemon", false, "Run as background daemon (implies --http :8091)")
	cmd.Flags().StringSliceVar(&skillGroupsF, "groups", nil, "Skill groups to expose as first-class tools (code-intel,optimized-retrieval,mobile,godot,api,refactor,context,room,mux,collab,code-write,project,foxctl-ci)")
	cmd.Flags().BoolVar(&groupOnly, "group-only", false, "Expose only the specified skill groups (skip the default curated MCP tools)")
	cmd.Flags().BoolVar(&optimizedRetrieval, "optimized-retrieval", false, "Expose only the optimized retrieval MCP surface: structured_shell, repo index retrieval, and the optimized-retrieval skill group")
	return cmd
}

// runDaemonMode spawns a background process running the MCP server
func runDaemonMode(ctx context.Context, opts mcpServerOptions) error {
	pidFile := expandPath(defaultPIDFile)

	// Check if daemon already running
	if existingPID, _, running := isDaemonRunning(pidFile); running {
		fmt.Printf("MCP daemon already running (PID %d)\n", existingPID)
		fmt.Printf("Use 'foxctl mcp stop' to stop it first\n")
		return nil
	}

	// Prepare log file
	logFile := expandPath(defaultLogFile)
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	// Get the current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Build command args for the child process
	args := []string{"mcp", "serve", "--http", opts.httpAddr}
	if opts.enableSkills {
		args = append(args, "--skills")
	}
	if opts.optimizedRetrieval {
		args = append(args, "--optimized-retrieval")
	}
	if opts.configFile != "" {
		args = append(args, "--config", opts.configFile)
	}
	if len(opts.groups) > 0 {
		args = append(args, "--groups", strings.Join(opts.groups, ","))
	}

	// Open log file for output
	logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	// Spawn background process
	cmd := &exec.Cmd{
		Path:   execPath,
		Args:   append([]string{execPath}, args...),
		Env:    os.Environ(),
		Stdout: logF,
		Stderr: logF,
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		logF.Close()
		return fmt.Errorf("start daemon: %w", err)
	}

	// Capture PID before releasing
	pid := cmd.Process.Pid

	// Close log file (child has inherited it)
	logF.Close()

	// Detach from the child process
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release process: %w", err)
	}

	// Helper to cleanup failed daemon process
	cleanupDaemon := func() {
		if process, err := os.FindProcess(pid); err == nil {
			_ = process.Signal(syscall.SIGTERM)
			// Wait briefly for graceful shutdown
			done := make(chan struct{})
			go func() {
				_, _ = process.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = process.Kill()
				_, _ = process.Wait()
			}
		}
		// Remove PID file if it was created
		pidFile := expandPath(defaultPIDFile)
		_ = os.Remove(pidFile)
	}

	healthURL := fmt.Sprintf("http://localhost%s/health", opts.httpAddr)
	healthClient := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if ctx.Err() != nil {
			cleanupDaemon()
			return fmt.Errorf("daemon health check canceled: %w", ctx.Err())
		}
		if time.Now().After(deadline) {
			cleanupDaemon()
			return fmt.Errorf("daemon did not become healthy within 3s (check logs: %s)", logFile)
		}
		resp, err := healthClient.Get(healthURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Printf("MCP daemon started (PID %d)\n", pid)
	fmt.Printf("Listening on http://localhost%s\n", opts.httpAddr)
	fmt.Printf("Logs: %s\n", logFile)
	fmt.Printf("\nUse 'foxctl mcp status' to check status\n")
	fmt.Printf("Use 'foxctl mcp stop' to stop\n")
	return nil
}

func newMCPStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the MCP daemon",
		Long:  "Stop a running MCP daemon process.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pidFile := expandPath(defaultPIDFile)

			pid, _, running := isDaemonRunning(pidFile)
			if !running {
				fmt.Println("MCP daemon is not running")
				return nil
			}

			// Find and signal the process
			process, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("find process: %w", err)
			}

			// Send SIGTERM for graceful shutdown
			if err := process.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("send SIGTERM: %w", err)
			}

			// Wait for process to exit (with timeout)
			fmt.Printf("Stopping MCP daemon (PID %d)...\n", pid)

			// Poll for process exit
			for i := 0; i < 50; i++ { // 5 second timeout
				time.Sleep(100 * time.Millisecond)
				if err := process.Signal(syscall.Signal(0)); err != nil {
					// Process has exited
					os.Remove(pidFile)
					fmt.Println("MCP daemon stopped")
					return nil
				}
			}

			// Force kill if still running
			fmt.Println("Daemon not responding, sending SIGKILL...")
			if err := process.Kill(); err != nil {
				return fmt.Errorf("kill process: %w", err)
			}
			os.Remove(pidFile)
			fmt.Println("MCP daemon killed")
			return nil
		},
	}
}

func newMCPStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check MCP daemon status",
		Long:  "Check if the MCP daemon is running and display status information.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pidFile := expandPath(defaultPIDFile)

			pid, storedAddr, running := isDaemonRunning(pidFile)
			if !running {
				fmt.Println("MCP daemon: not running")
				return nil
			}

			fmt.Printf("MCP daemon: running (PID %d)\n", pid)

			// Use stored address or fallback to default
			port := defaultDaemonPort
			if storedAddr != "" {
				port = extractPort(storedAddr)
			}

			// Try to get health info
			healthURL := fmt.Sprintf("http://localhost%s/health", port)
			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Get(healthURL)
			if err != nil {
				fmt.Printf("Health check failed: %v\n", err)
				return nil
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				var health map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&health); err == nil {
					fmt.Printf("Status: %s\n", health["status"])
					if uptime, ok := health["uptime_sec"].(float64); ok {
						fmt.Printf("Uptime: %d seconds\n", int(uptime))
					}
					if addr, ok := health["addr"].(string); ok {
						fmt.Printf("Address: %s\n", addr)
					}
				}
			}
			return nil
		},
	}
}

// mcpServerOptions holds configuration for the MCP server.
type mcpServerOptions struct {
	configFile         string
	httpAddr           string
	enableSkills       bool
	groups             []string
	optimizedRetrieval bool
	groupOnly          bool
}

func runMCPServer(ctx context.Context, opts mcpServerOptions) error {
	// Load config (includes .env loading)
	config.LoadDotEnv()
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Initialize backend configurations from config
	initBackendConfigs(cfg.Search)

	// Load config file if provided
	if opts.configFile != "" {
		if err := loadBackendConfig(opts.configFile); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	}

	info := buildinfo.Current()
	s := server.NewMCPServer("foxctl", info.Version,
		server.WithToolCapabilities(true),
	)

	// Register curated tools with simplified schemas unless this is a groups-only surface.
	if opts.groupOnly {
		// Intentionally skip default curated tool registration.
	} else if opts.optimizedRetrieval {
		registerOptimizedRetrievalTools(s)
	} else {
		registerTools(s)
	}

	// Register focused command-backed group tools regardless of skill-group mode.
	registerFocusedGroupTools(s, opts.groups)

	// Register skill groups as first-class MCP tools
	if len(opts.groups) > 0 {
		if err := registerSkillGroups(ctx, s, opts.groups); err != nil {
			observability.Emit(ctx, observability.NewEvent("mcp.skill_groups_registration_warning").
				WithComponent(observability.ComponentCLI).
				WithData("reason", "continuing without skill groups").
				Error(err, 0))
		}
	}

	// Register generic foxctl tools (run + discovery) instead of individual skill tools
	// This reduces token overhead from ~83k to ~1k
	if opts.enableSkills {
		registerAgentctlTools(s)
	}

	// Cleanup on exit
	defer backends.closeAll()

	// Start server (SSE or stdio)
	if opts.httpAddr != "" {
		return runSSEDaemon(ctx, s, opts.httpAddr)
	}

	// Default: stdio transport (blocks until client disconnects)
	return server.ServeStdio(s)
}

// runSSEDaemon starts the MCP server as an HTTP/SSE daemon with:
// - PID file to prevent duplicate instances
// - Health check endpoint
// - Graceful shutdown on SIGTERM/SIGINT
func runSSEDaemon(ctx context.Context, s *server.MCPServer, addr string) error {
	// Resolve PID file path
	pidFile := expandPath(defaultPIDFile)

	// Check if daemon already running
	if existingPID, _, running := isDaemonRunning(pidFile); running {
		// Try to connect to existing daemon
		healthURL := fmt.Sprintf("http://localhost%s/health", extractPort(addr))
		if resp, err := http.Get(healthURL); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Fprintf(os.Stderr, "MCP daemon already running (PID %d) at %s\n", existingPID, addr) //nolint:forbidigo // CLI user-facing status output
				fmt.Fprintf(os.Stderr, "Use existing daemon or kill PID %d to restart\n", existingPID)   //nolint:forbidigo // CLI user-facing status output
				return nil
			}
		}
		// Stale PID file - remove it
		os.Remove(pidFile)
	}

	// Write PID file with address for status command to use
	if err := writePIDFile(pidFile, addr); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}
	defer os.Remove(pidFile)

	// Create SSE server
	sseServer := server.NewSSEServer(s)

	// Create HTTP mux with health endpoint
	mux := http.NewServeMux()

	// Health check endpoint
	state := &daemonState{
		pidFile:   pidFile,
		startTime: time.Now(),
		addr:      addr,
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		healthResponse := map[string]any{
			"status":     "ok",
			"pid":        os.Getpid(),
			"uptime_sec": int(time.Since(state.startTime).Seconds()),
			"addr":       state.addr,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(healthResponse); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
		}
	})

	// Mount SSE server routes
	mux.Handle("/sse", sseServer.SSEHandler())
	mux.Handle("/message", sseServer.MessageHandler())

	// Create HTTP server
	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Set up graceful shutdown
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGTERM, syscall.SIGINT)

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "Starting MCP SSE daemon on %s (PID %d)\n", addr, os.Getpid())
		fmt.Fprintf(os.Stderr, "Health: http://%s/health\n", addr)
		fmt.Fprintf(os.Stderr, "SSE: http://%s/sse\n", addr)
		fmt.Fprintf(os.Stderr, "Message: http://%s/message\n", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-shutdownCh:
		fmt.Fprintf(os.Stderr, "\nReceived %s, shutting down gracefully...\n", sig)
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		fmt.Fprintf(os.Stderr, "\nContext cancelled, shutting down...\n")
	}

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
		return err
	}

	fmt.Fprintf(os.Stderr, "MCP daemon stopped\n")
	return nil
}

// expandPath expands ~ to home directory
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// extractPort extracts the port from an address like ":8091" or "0.0.0.0:8091"
func extractPort(addr string) string {
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		return addr[idx:]
	}
	return addr
}

// writePIDFile writes the current process ID and address to the PID file.
// Format: "PID\nADDR" (e.g., "12345\n:8091")
func writePIDFile(path string, addr string) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("%d\n%s", os.Getpid(), addr)
	return os.WriteFile(path, []byte(content), 0o644)
}

// isDaemonRunning checks if a daemon is already running based on PID file.
// Returns (pid, addr, running). Addr may be empty for legacy PID files.
func isDaemonRunning(pidFile string) (int, string, bool) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, "", false
	}

	// Parse PID and optional address (format: "PID\nADDR" or just "PID")
	lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, "", false
	}

	var addr string
	if len(lines) > 1 {
		addr = strings.TrimSpace(lines[1])
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return pid, addr, false
	}

	// On Unix, FindProcess always succeeds - need to send signal 0 to check
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return pid, addr, false
	}

	return pid, addr, true
}

func initBackendConfigs(search config.SearchSettings) {
	// Tavily (stdio via npx)
	if search.TavilyAPIKey != "" {
		backends.configs["tavily"] = mcpServerConfig{
			Command: "npx",
			Args:    []string{"-y", "tavily-mcp@latest"},
			Env:     map[string]string{"TAVILY_API_KEY": search.TavilyAPIKey},
		}
	}

	// Exa (HTTP)
	if search.ExaAPIKey != "" {
		backends.configs["exa"] = mcpServerConfig{
			URL: "https://mcp.exa.ai/mcp",
			Headers: map[string]string{
				"Accept":        "application/json, text/event-stream",
				"Authorization": "Bearer " + search.ExaAPIKey,
			},
		}
	}

	// Perplexity (stdio via npx)
	if search.PerplexityAPIKey != "" {
		backends.configs["perplexity"] = mcpServerConfig{
			Command: "npx",
			Args:    []string{"-y", "@perplexity-ai/mcp-server"},
			Env: map[string]string{
				"PERPLEXITY_API_KEY":    search.PerplexityAPIKey,
				"PERPLEXITY_TIMEOUT_MS": "600000",
			},
		}
	}

	// Context7 (stdio via npx, no API key needed)
	backends.configs["context7"] = mcpServerConfig{
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp"},
	}

	// Expo (HTTP - official Expo MCP server)
	// Note: Expo MCP works without token for basic features, token needed for EAS
	backends.configs["expo"] = mcpServerConfig{
		URL: "https://mcp.expo.dev/mcp",
		Headers: map[string]string{
			"Accept": "application/json, text/event-stream",
		},
	}
	// Add auth header if token provided
	if token := os.Getenv("EXPO_TOKEN"); token != "" {
		backends.configs["expo"] = mcpServerConfig{
			URL: "https://mcp.expo.dev/mcp",
			Headers: map[string]string{
				"Accept":        "application/json, text/event-stream",
				"Authorization": "Bearer " + token,
			},
		}
	}

	// Supabase (HTTP - official Supabase MCP server)
	if url := os.Getenv("SUPABASE_URL"); url != "" {
		// Use the remote MCP server with project ref
		backends.configs["supabase"] = mcpServerConfig{
			URL: "https://mcp.supabase.com/mcp",
			Headers: map[string]string{
				"Accept": "application/json, text/event-stream",
			},
		}
	}

	// Playwright (stdio via npx)
	backends.configs["playwright"] = mcpServerConfig{
		Command: "npx",
		Args:    []string{"-y", "@anthropic-ai/mcp-playwright"},
	}
}

func loadBackendConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg map[string]mcpServerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	for name, c := range cfg {
		backends.configs[name] = c
	}
	return nil
}

func registerTools(s *server.MCPServer) {
	// web_search - Unified search across providers (exa, tavily, perplexity)
	s.AddTool(
		mcp.NewTool("web_search",
			mcp.WithDescription("Search the web for current information. Returns relevant results with snippets."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
			mcp.WithNumber("max_results", mcp.Description("Max results to return (default: 10, max: 20)")),
			mcp.WithString("topic", mcp.Description("Topic: 'general' or 'news'")),
			mcp.WithString("provider", mcp.Description("Search provider: 'exa' (default), 'tavily', or 'perplexity'. Override with AGENTCTL_SEARCH_PROVIDER env var.")),
		),
		handleWebSearch,
	)

	// web_extract - Simplified tavily-extract
	s.AddTool(
		mcp.NewTool("web_extract",
			mcp.WithDescription("Extract content from URLs. Returns markdown-formatted page content."),
			mcp.WithArray("urls", mcp.Required(), mcp.WithStringItems(), mcp.Description("URLs to extract content from")),
		),
		handleWebExtract,
	)

	// docs_lookup - Simplified context7 (resolve + get-library-docs combined)
	s.AddTool(
		mcp.NewTool("docs_lookup",
			mcp.WithDescription("Look up documentation for a library or framework. Automatically resolves library ID."),
			mcp.WithString("library", mcp.Required(), mcp.Description("Library name (e.g., 'react', 'nextjs', 'express')")),
			mcp.WithString("topic", mcp.Description("Topic to focus on (e.g., 'hooks', 'routing')")),
		),
		handleDocsLookup,
	)

	// code_search - Simplified exa code search
	s.AddTool(
		mcp.NewTool("code_search",
			mcp.WithDescription("Search for code examples and programming context. Best for API usage, patterns, and implementations."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Code search query (e.g., 'React useState hook examples')")),
		),
		handleCodeSearch,
	)

	// ask - Simplified perplexity
	s.AddTool(
		mcp.NewTool("ask",
			mcp.WithDescription("Ask a question and get a comprehensive answer with citations. Good for research and explanations."),
			mcp.WithString("question", mcp.Required(), mcp.Description("Question to ask")),
		),
		handleAsk,
	)

	// web_crawl - Tavily crawl for recursive website crawling
	s.AddTool(
		mcp.NewTool("web_crawl",
			mcp.WithDescription("Crawl a website recursively starting from a URL. Good for exploring site structure."),
			mcp.WithString("url", mcp.Required(), mcp.Description("Starting URL to crawl")),
			mcp.WithNumber("max_depth", mcp.Description("Max depth to crawl (default: 2)")),
			mcp.WithNumber("limit", mcp.Description("Max pages to crawl (default: 10)")),
		),
		handleWebCrawl,
	)

	// web_map - Tavily map for URL structure discovery
	s.AddTool(
		mcp.NewTool("web_map",
			mcp.WithDescription("Map the URL structure of a website. Returns a list of discovered URLs."),
			mcp.WithString("url", mcp.Required(), mcp.Description("Website URL to map")),
			mcp.WithNumber("limit", mcp.Description("Max URLs to discover (default: 50)")),
		),
		handleWebMap,
	)

	// web_search_general - Exa general web search (not code-specific)
	s.AddTool(
		mcp.NewTool("web_search_general",
			mcp.WithDescription("General web search using Exa AI. Better for finding articles, blogs, and discussions."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
			mcp.WithNumber("num_results", mcp.Description("Number of results (default: 8)")),
		),
		handleWebSearchGeneral,
	)

	// === Expo Tools ===

	// expo_build - Trigger EAS build
	s.AddTool(
		mcp.NewTool("expo_build",
			mcp.WithDescription("Trigger an EAS cloud build for iOS or Android."),
			mcp.WithString("platform", mcp.Required(), mcp.Description("Platform: 'ios', 'android', or 'all'")),
			mcp.WithString("profile", mcp.Description("Build profile (default: 'development')")),
		),
		handleExpoBuild,
	)

	// expo_update - Publish OTA update
	s.AddTool(
		mcp.NewTool("expo_update",
			mcp.WithDescription("Publish an over-the-air update to a channel."),
			mcp.WithString("channel", mcp.Required(), mcp.Description("Update channel (e.g., 'production', 'preview')")),
			mcp.WithString("message", mcp.Description("Update message")),
		),
		handleExpoUpdate,
	)

	// expo_screenshot - Take simulator screenshot
	s.AddTool(
		mcp.NewTool("expo_screenshot",
			mcp.WithDescription("Take a screenshot from iOS Simulator or Android Emulator."),
			mcp.WithString("platform", mcp.Description("Platform: 'ios' or 'android' (default: 'ios')")),
		),
		handleExpoScreenshot,
	)

	// === Supabase Tools ===

	// supabase_query - Execute SQL query
	s.AddTool(
		mcp.NewTool("supabase_query",
			mcp.WithDescription("Execute a SQL query against the Supabase database."),
			mcp.WithString("sql", mcp.Required(), mcp.Description("SQL query to execute")),
		),
		handleSupabaseQuery,
	)

	// supabase_tables - List database tables
	s.AddTool(
		mcp.NewTool("supabase_tables",
			mcp.WithDescription("List all tables in the Supabase database with their schemas."),
		),
		handleSupabaseTables,
	)

	// supabase_logs - Get service logs
	s.AddTool(
		mcp.NewTool("supabase_logs",
			mcp.WithDescription("Get logs from Supabase services (postgres, auth, storage, etc)."),
			mcp.WithString("service", mcp.Description("Service: 'postgres', 'auth', 'storage', 'realtime', 'edge_functions'")),
			mcp.WithNumber("limit", mcp.Description("Number of log entries (default: 100)")),
		),
		handleSupabaseLogs,
	)

	// === Playwright/Browser Tools ===

	// browser_navigate - Navigate to URL
	s.AddTool(
		mcp.NewTool("browser_navigate",
			mcp.WithDescription("Navigate the browser to a URL."),
			mcp.WithString("url", mcp.Required(), mcp.Description("URL to navigate to")),
		),
		handleBrowserNavigate,
	)

	// browser_screenshot - Take screenshot
	s.AddTool(
		mcp.NewTool("browser_screenshot",
			mcp.WithDescription("Take a screenshot of the current browser page."),
			mcp.WithString("name", mcp.Description("Screenshot filename (optional)")),
			mcp.WithBoolean("full_page", mcp.Description("Capture full page (default: false)")),
		),
		handleBrowserScreenshot,
	)

	// browser_click - Click element
	s.AddTool(
		mcp.NewTool("browser_click",
			mcp.WithDescription("Click an element on the page."),
			mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector or text to click")),
		),
		handleBrowserClick,
	)

	// browser_fill - Fill form input
	s.AddTool(
		mcp.NewTool("browser_fill",
			mcp.WithDescription("Fill a form input with text."),
			mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector for input")),
			mcp.WithString("value", mcp.Required(), mcp.Description("Value to fill")),
		),
		handleBrowserFill,
	)

	// browser_content - Get page content
	s.AddTool(
		mcp.NewTool("browser_content",
			mcp.WithDescription("Get the text content of the current page or a specific element."),
			mcp.WithString("selector", mcp.Description("CSS selector (optional, defaults to full page)")),
		),
		handleBrowserContent,
	)

	// === Repo index tools ===

	// repo_index_build - Build repo graph index
	s.AddTool(
		mcp.NewTool("repo_index_build",
			mcp.WithDescription("Build the repo graph index for a workspace."),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
			mcp.WithArray("go_pattern", mcp.Description("Go package patterns to index (default: ./...)"), mcp.WithStringItems()),
			mcp.WithBoolean("include_go", mcp.Description("Include Go sources (default: true)")),
			mcp.WithBoolean("include_typescript", mcp.Description("Include TypeScript sources (default: true)")),
			mcp.WithBoolean("include_elixir", mcp.Description("Include Elixir sources (default: false)")),
			mcp.WithBoolean("include_terraform", mcp.Description("Include Terraform sources (default: false)")),
			mcp.WithBoolean("include_kubernetes", mcp.Description("Include Kubernetes/Helm manifests (default: false)")),
			mcp.WithBoolean("include_shell", mcp.Description("Include shell scripts (default: false)")),
			mcp.WithBoolean("include_tests", mcp.Description("Include test files (default: false)")),
			mcp.WithBoolean("dry_run", mcp.Description("Build without writing to the index (default: false)")),
		),
		handleRepoIndexBuild,
	)

	// repo_index_status - Index status
	s.AddTool(
		mcp.NewTool("repo_index_status",
			mcp.WithDescription("Show repo graph index status."),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
		),
		handleRepoIndexStatus,
	)

	// repo_index_search - Search nodes
	s.AddTool(
		mcp.NewTool("repo_index_search",
			mcp.WithDescription("Search repo index nodes by text."),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
			mcp.WithString("query", mcp.Required(), mcp.Description("FTS query string")),
			mcp.WithNumber("limit", mcp.Description("Maximum results (default: 20)")),
		),
		handleRepoIndexSearch,
	)

	// repo_index_expand - Expand graph
	s.AddTool(
		mcp.NewTool("repo_index_expand",
			mcp.WithDescription("Expand the graph from seed nodes."),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
			mcp.WithArray("seed", mcp.Required(), mcp.Description("Seed node IDs (repeatable)"), mcp.WithStringItems()),
			mcp.WithArray("edge", mcp.Description("Edge types to traverse (repeatable)"), mcp.WithStringItems()),
			mcp.WithNumber("depth", mcp.Description("Traversal depth (default: 1)")),
			mcp.WithNumber("budget", mcp.Description("Max nodes to return (default: 50)")),
			mcp.WithNumber("per_node", mcp.Description("Max edges per node per hop (default: 50)")),
			mcp.WithString("direction", mcp.Description("Traversal direction: out or in (default: out)")),
		),
		handleRepoIndexExpand,
	)

	// repo_index_open - Open node
	s.AddTool(
		mcp.NewTool("repo_index_open",
			mcp.WithDescription("Open a node by ID."),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
			mcp.WithString("id", mcp.Required(), mcp.Description("Node ID")),
		),
		handleRepoIndexOpen,
	)

	// repo_index_ask - Ask questions using repo index
	s.AddTool(
		mcp.NewTool("repo_index_ask",
			mcp.WithDescription("Ask a question using the repo index."),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
			mcp.WithString("question", mcp.Required(), mcp.Description("Question to ask")),
			mcp.WithString("provider", mcp.Description("LLM provider (cerebras|openrouter|groq|openai|gemini|anthropic)")),
			mcp.WithString("model", mcp.Description("LLM model")),
			mcp.WithString("api_key", mcp.Description("LLM API key override")),
			mcp.WithNumber("max_iterations", mcp.Description("Maximum tool-call iterations (default: 12)")),
			mcp.WithNumber("timeout_sec", mcp.Description("LLM request timeout in seconds (default: 60)")),
		),
		handleRepoIndexAsk,
	)

	// === Local Skill Tools (HTML Editing) ===

	// html_select - Query HTML elements
	s.AddTool(
		mcp.NewTool("html_select",
			mcp.WithDescription("Query HTML elements using CSS selectors. Returns matched element info without modifying the file."),
			mcp.WithString("path", mcp.Required(), mcp.Description("HTML file path")),
			mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector (e.g., '#header', '.nav-item', 'div.content > p')")),
		),
		handleHTMLSelect,
	)

	// html_insert - Insert HTML content
	s.AddTool(
		mcp.NewTool("html_insert",
			mcp.WithDescription("Insert HTML content relative to matched elements."),
			mcp.WithString("path", mcp.Required(), mcp.Description("HTML file path")),
			mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector for target elements")),
			mcp.WithString("position", mcp.Required(), mcp.Description("Insert position: 'before', 'after', 'prepend', or 'append'")),
			mcp.WithString("html", mcp.Required(), mcp.Description("HTML content to insert")),
			mcp.WithBoolean("dry_run", mcp.Description("Preview changes without modifying file")),
		),
		handleHTMLInsert,
	)

	// html_replace - Replace HTML content
	s.AddTool(
		mcp.NewTool("html_replace",
			mcp.WithDescription("Replace matched elements or their inner content."),
			mcp.WithString("path", mcp.Required(), mcp.Description("HTML file path")),
			mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector for elements to replace")),
			mcp.WithString("html", mcp.Required(), mcp.Description("Replacement HTML content")),
			mcp.WithBoolean("inner", mcp.Description("Replace only inner HTML, not entire element (default: false)")),
			mcp.WithBoolean("dry_run", mcp.Description("Preview changes without modifying file")),
		),
		handleHTMLReplace,
	)

	// html_delete - Delete HTML elements
	s.AddTool(
		mcp.NewTool("html_delete",
			mcp.WithDescription("Delete matched elements from the DOM."),
			mcp.WithString("path", mcp.Required(), mcp.Description("HTML file path")),
			mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector for elements to delete")),
			mcp.WithBoolean("dry_run", mcp.Description("Preview changes without modifying file")),
		),
		handleHTMLDelete,
	)

	// html_set_attr - Update HTML attributes
	s.AddTool(
		mcp.NewTool("html_set_attr",
			mcp.WithDescription("Set or remove attributes on matched elements. Use empty string to remove an attribute."),
			mcp.WithString("path", mcp.Required(), mcp.Description("HTML file path")),
			mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector for target elements")),
			mcp.WithString("attr", mcp.Required(), mcp.Description("Attribute name to set")),
			mcp.WithString("value", mcp.Description("Attribute value (omit or empty string to remove)")),
			mcp.WithBoolean("dry_run", mcp.Description("Preview changes without modifying file")),
		),
		handleHTMLSetAttr,
	)
}

func registerOptimizedRetrievalTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("repo_index_build",
			mcp.WithDescription("Build or refresh the repo graph index."),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
			mcp.WithBoolean("include_go", mcp.Description("Include Go sources (default: true)")),
			mcp.WithBoolean("include_typescript", mcp.Description("Include TypeScript sources (default: true)")),
			mcp.WithBoolean("include_elixir", mcp.Description("Include Elixir sources (default: false)")),
			mcp.WithBoolean("include_terraform", mcp.Description("Include Terraform sources (default: false)")),
			mcp.WithBoolean("include_kubernetes", mcp.Description("Include Kubernetes/Helm manifests (default: false)")),
			mcp.WithBoolean("include_shell", mcp.Description("Include shell scripts (default: false)")),
			mcp.WithBoolean("include_tests", mcp.Description("Include test files (default: false)")),
			mcp.WithBoolean("dry_run", mcp.Description("Build without writing to the index (default: false)")),
		),
		handleRepoIndexBuild,
	)

	s.AddTool(
		mcp.NewTool("repo_index_status",
			mcp.WithDescription("Show repo graph index status."),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
		),
		handleRepoIndexStatus,
	)

	s.AddTool(
		mcp.NewTool("structured_shell",
			mcp.WithDescription("Structured shell router for supported read-only repo inspection commands. This is not an arbitrary shell executor."),
			mcp.WithString("command", mcp.Description("Shell command string to route, for example `git log --stat -5` or `rg -n 'spawn' internal/agent | head -n 10`")),
			mcp.WithArray("argv", mcp.Description("Optional argv form instead of command string"), mcp.WithStringItems()),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
			mcp.WithBoolean("measure_raw", mcp.Description("Measure raw output bytes and token estimates against the reduced summary")),
			mcp.WithString("token_model", mcp.Description("Tokenizer model or encoding for measurement (default: cl100k_base)")),
		),
		handleStructuredShell,
	)

	s.AddTool(
		mcp.NewTool("context_show",
			mcp.WithDescription("Show current context-plane workspace state."),
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
		),
		handleContextShow,
	)

	s.AddTool(
		mcp.NewTool("repo_index_search",
			mcp.WithDescription("Search the repo index with compact preview-first output."),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
			mcp.WithString("query", mcp.Required(), mcp.Description("FTS query string")),
			mcp.WithNumber("limit", mcp.Description("Maximum results (default: 20)")),
			mcp.WithString("inline_mode", mcp.Description("Inline mode: auto, full, preview, or artifact_only")),
		),
		handleRepoIndexSearch,
	)

	s.AddTool(
		mcp.NewTool("repo_index_expand",
			mcp.WithDescription("Expand the repo index graph from seed nodes with compact preview-first output."),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
			mcp.WithArray("seed", mcp.Required(), mcp.Description("Seed node IDs (repeatable)"), mcp.WithStringItems()),
			mcp.WithArray("edge", mcp.Description("Edge types to traverse (repeatable)"), mcp.WithStringItems()),
			mcp.WithNumber("depth", mcp.Description("Traversal depth (default: 1)")),
			mcp.WithNumber("budget", mcp.Description("Max nodes to return (default: 50)")),
			mcp.WithNumber("per_node", mcp.Description("Max edges per node per hop (default: 50)")),
			mcp.WithString("direction", mcp.Description("Traversal direction: out or in (default: out)")),
			mcp.WithString("inline_mode", mcp.Description("Inline mode: auto, full, preview, or artifact_only")),
		),
		handleRepoIndexExpand,
	)

	s.AddTool(
		mcp.NewTool("repo_index_dag_grep",
			mcp.WithDescription("Search and expand the repo index into a compact explanation subgraph with preview-first output."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
			mcp.WithString("workspace", mcp.Description("Workspace root (default: .)")),
			mcp.WithString("mode", mcp.Description("Search mode: fts, semantic, or hybrid")),
			mcp.WithNumber("k", mcp.Description("Number of seed nodes (default: 10)")),
			mcp.WithArray("node_kinds", mcp.Description("Node kinds to include"), mcp.WithStringItems()),
			mcp.WithArray("edge_sets", mcp.Description("Edge sets to include"), mcp.WithStringItems()),
			mcp.WithArray("edge_types", mcp.Description("Explicit edge types to traverse"), mcp.WithStringItems()),
			mcp.WithString("direction", mcp.Description("Traversal direction: out or in")),
			mcp.WithNumber("depth", mcp.Description("Traversal depth")),
			mcp.WithNumber("budget", mcp.Description("Max nodes to return")),
			mcp.WithNumber("per_node_cap", mcp.Description("Max edges per node")),
			mcp.WithBoolean("include_anchors", mcp.Description("Include file/package anchors")),
			mcp.WithString("render", mcp.Description("Optional render format: tree or mermaid")),
			mcp.WithString("inline_mode", mcp.Description("Inline mode: auto, full, preview, or artifact_only")),
		),
		handleRepoIndexDAGGrep,
	)
}

func registerFocusedGroupTools(s *server.MCPServer, groups []string) {
	groupSet := make(map[string]bool, len(groups))
	for _, group := range groups {
		group = strings.ToLower(strings.TrimSpace(group))
		if group != "" {
			groupSet[group] = true
		}
	}
	if groupSet["collab"] || groupSet["room"] {
		registerRoomTools(s)
	}
	if groupSet["collab"] || groupSet["mux"] {
		registerMuxTools(s)
	}
	if groupSet["mobile"] {
		registerMobileTools(s)
	}
}

func registerMobileTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("mobile_expo",
			mcp.WithDescription("Expo/React Native mobile debugging and dev-menu surface for iOS and Android simulators/devices."),
			mcp.WithString("operation", mcp.Required(), mcp.Description("Operation to perform: debug_status, debug_snapshot, shake, reload, deep_link, dev_menu, toggle_inspector, toggle_performance, toggle_remote_debug, build, update, build_status, logs")),
			mcp.WithString("device_id", mcp.Description("Device ID (UDID for iOS, serial for Android). Auto-detects if omitted.")),
			mcp.WithString("platform", mcp.Description("Target platform. ios, android, or auto.")),
			mcp.WithString("url", mcp.Description("Deep link URL for deep_link operation")),
			mcp.WithString("build_platform", mcp.Description("Platform for EAS build: ios, android, or all")),
			mcp.WithString("profile", mcp.Description("EAS build profile: development, preview, or production")),
			mcp.WithString("channel", mcp.Description("Update channel for EAS update")),
			mcp.WithString("message", mcp.Description("Update message for EAS update")),
			mcp.WithString("filter", mcp.Description("Filter pattern for logs operation")),
			mcp.WithNumber("count", mcp.Description("Number of log lines to retrieve")),
		),
		handleMobileExpo,
	)
}

func registerRoomTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("room",
			mcp.WithDescription("Command-backed durable room coordination tool. Actions: create, list, show, status, inbox, send, ack, resolve, clear, join, leave, subscribe, relay, coordinator_set."),
			mcp.WithString("action", mcp.Required(), mcp.Description("Room action to run")),
			mcp.WithString("workspace", mcp.Description("Workspace root override (default: .)")),
			mcp.WithString("room_id", mcp.Description("Room id for actions that target a room")),
			mcp.WithString("title", mcp.Description("Room title for create")),
			mcp.WithString("description", mcp.Description("Room description for create")),
			mcp.WithArray("members", mcp.Description("Room members for create"), mcp.WithStringItems()),
			mcp.WithString("actor", mcp.Description("Actor or participant id override")),
			mcp.WithString("role", mcp.Description("Room member role for join")),
			mcp.WithString("backend", mcp.Description("Backend override for join or relay (tmux|zellij|auto)")),
			mcp.WithString("session", mcp.Description("Mux/Zellij session override")),
			mcp.WithString("pane_id", mcp.Description("Pane id for join")),
			mcp.WithBoolean("unbound", mcp.Description("Mark room member transport unbound")),
			mcp.WithBoolean("create", mcp.Description("Create the room if missing for join")),
			mcp.WithBoolean("current", mcp.Description("Join current tmux/zellij participant when actor is omitted")),
			mcp.WithNumber("limit", mcp.Description("Limit for list/show/status/inbox/subscribe")),
			mcp.WithString("recipient", mcp.Description("Recipient actor/participant id for send")),
			mcp.WithString("subject", mcp.Description("Optional room message subject")),
			mcp.WithString("hint", mcp.Description("Optional explicit hint for how the recipient should respond")),
			mcp.WithString("text", mcp.Description("Message body for send")),
			mcp.WithString("kind", mcp.Description("Message kind for send")),
			mcp.WithString("task_id", mcp.Description("Optional task id for send")),
			mcp.WithNumber("priority", mcp.Description("Priority from 1 to 5 for send")),
			mcp.WithBoolean("ack_required", mcp.Description("Require explicit ack for send")),
			mcp.WithBoolean("reply_expected", mcp.Description("Mark send as expecting a reply")),
			mcp.WithBoolean("interrupt", mcp.Description("Interrupt target pane for direct send")),
			mcp.WithBoolean("auto_create", mcp.Description("Create room if missing for send")),
			mcp.WithArray("message_ids", mcp.Description("Message ids for ack/resolve"), mcp.WithStringItems()),
			mcp.WithString("mode", mcp.Description("Mode for resolve or clear")),
			mcp.WithBoolean("all", mcp.Description("Resolve all current entries matching filters")),
			mcp.WithArray("only", mcp.Description("Filter list for status/resolve"), mcp.WithStringItems()),
			mcp.WithString("filter", mcp.Description("Inbox filter")),
			mcp.WithBoolean("grouped", mcp.Description("Group inbox entries")),
			mcp.WithBoolean("ids_only", mcp.Description("Return only matching inbox ids")),
			mcp.WithBoolean("include_broadcasts", mcp.Description("Include broadcasts in inbox all-filter")),
			mcp.WithNumber("history", mcp.Description("History count for subscribe/relay")),
			mcp.WithBoolean("follow", mcp.Description("Follow room subscribe stream")),
			mcp.WithString("poll", mcp.Description("Poll duration string, e.g. 2s")),
			mcp.WithString("plugin_path", mcp.Description("Zellij plugin path for relay")),
			mcp.WithString("participant_id", mcp.Description("Participant id for leave or coordinator_set")),
			mcp.WithString("preset", mcp.Description("Clear preset")),
		),
		handleRoomTool,
	)

	s.AddTool(
		mcp.NewTool("room_pulse",
			mcp.WithDescription("Read-only room-wide epic coordinator pulse. Summarizes all epics in one room using existing epic resume/health/next/checkpoint helpers."),
			mcp.WithString("workspace", mcp.Description("Workspace root override (default: .)")),
			mcp.WithString("room_id", mcp.Required(), mcp.Description("Room id")),
			mcp.WithString("actor", mcp.Description("Actor id used for actor-specific next-action derivation")),
			mcp.WithNumber("limit", mcp.Description("Maximum room-wide top items to return")),
			mcp.WithArray("only", mcp.Description("Filter epic pulse lanes (all, blocked, intake, review, stale, ready)"), mcp.WithStringItems()),
		),
		handleRoomPulseTool,
	)

	s.AddTool(
		mcp.NewTool("room_task",
			mcp.WithDescription("Command-backed room task management. Actions: add, list, assign, reassign, claim, touch, block, reclaim, unblock, abandon, complete."),
			mcp.WithString("action", mcp.Required(), mcp.Description("Room task action to run")),
			mcp.WithString("workspace", mcp.Description("Workspace root override (default: .)")),
			mcp.WithString("room_id", mcp.Description("Room id")),
			mcp.WithString("sender", mcp.Description("Sender actor or participant id override")),
			mcp.WithString("task_id", mcp.Description("Task id")),
			mcp.WithString("title", mcp.Description("Task title for add")),
			mcp.WithString("description", mcp.Description("Task description for add")),
			mcp.WithString("scope", mcp.Description("Task scope path for add")),
			mcp.WithString("parent", mcp.Description("Parent task id for add")),
			mcp.WithArray("depends_on", mcp.Description("Dependency task ids"), mcp.WithStringItems()),
			mcp.WithBoolean("create_room", mcp.Description("Create room if missing for add")),
			mcp.WithString("status", mcp.Description("Status filter for list")),
			mcp.WithString("recipient", mcp.Description("Assignee participant id")),
			mcp.WithString("notes", mcp.Description("Assignment or completion notes")),
			mcp.WithString("reason", mcp.Description("Block/reassign/reclaim/abandon reason")),
			mcp.WithString("gotchas", mcp.Description("Completion gotchas")),
		),
		handleRoomTaskTool,
	)

	s.AddTool(
		mcp.NewTool("room_interview",
			mcp.WithDescription("Command-backed durable room interview protocol. Actions: start, ask, answer, verify, next, show."),
			mcp.WithString("action", mcp.Required(), mcp.Description("Interview action to run")),
			mcp.WithString("workspace", mcp.Description("Workspace root override (default: .)")),
			mcp.WithString("room_id", mcp.Description("Room id")),
			mcp.WithString("sender", mcp.Description("Sender actor or participant id override")),
			mcp.WithString("topic", mcp.Description("Interview topic for start")),
			mcp.WithString("session_id", mcp.Description("Interview session id")),
			mcp.WithString("question_id", mcp.Description("Interview question id")),
			mcp.WithString("answer_id", mcp.Description("Interview answer id")),
			mcp.WithString("spec", mcp.Description("Inline spec or request summary for start")),
			mcp.WithString("spec_ref", mcp.Description("Doc path, plan id, or message id that anchors the interview")),
			mcp.WithString("submitter", mcp.Description("Actor who submitted the plan or spec")),
			mcp.WithString("questioner", mcp.Description("Actor responsible for drafting interview questions")),
			mcp.WithString("respondent", mcp.Description("Actor expected to answer the questions")),
			mcp.WithString("verifier", mcp.Description("Actor who decides whether answers match the original intent")),
			mcp.WithArray("constraint", mcp.Description("Constraint or guardrail (repeatable)"), mcp.WithStringItems()),
			mcp.WithString("to", mcp.Description("Respondent actor id override for ask")),
			mcp.WithString("question", mcp.Description("Interview question text")),
			mcp.WithString("answer", mcp.Description("Interview answer text")),
			mcp.WithString("verdict", mcp.Description("Interview verdict: accept, clarify, or reject")),
			mcp.WithString("notes", mcp.Description("Verifier notes")),
			mcp.WithString("actor", mcp.Description("Actor id for interview next")),
			mcp.WithNumber("limit", mcp.Description("Maximum room messages to inspect for next/show")),
		),
		handleRoomInterviewTool,
	)

	s.AddTool(
		mcp.NewTool("room_agile",
			mcp.WithDescription("Command-backed agile room protocol. Actions: epic_start, epic_ask, epic_answer, epic_finalize, epic_close, epic_shape, epic_checkpoint, epic_show, epic_resume, epic_health, epic_next, milestone_start, milestone_contract, milestone_criteria, milestone_review, milestone_summary, milestone_show, story_propose, story_accept, story_add, story_state, story_validate, story_show, log_append, log_show, retro_add, retro_show, aca_promote, workpack_show, workpack_sync."),
			mcp.WithString("action", mcp.Required(), mcp.Description("Agile room action to run")),
			mcp.WithString("workspace", mcp.Description("Workspace root override (default: .)")),
			mcp.WithString("room_id", mcp.Description("Room id")),
			mcp.WithString("sender", mcp.Description("Sender actor or participant id override")),
			mcp.WithString("actor", mcp.Description("Actor id override for actor-specific read-model actions")),
			mcp.WithString("epic_id", mcp.Description("Epic id")),
			mcp.WithString("milestone_id", mcp.Description("Milestone id")),
			mcp.WithString("proposal_id", mcp.Description("Milestone proposal id")),
			mcp.WithString("story_proposal_id", mcp.Description("Story proposal id")),
			mcp.WithString("story_id", mcp.Description("Story id")),
			mcp.WithString("source_id", mcp.Description("ACA promotion source id")),
			mcp.WithString("target_kind", mcp.Description("ACA promotion target kind: epic, milestone, retro, validation")),
			mcp.WithString("validator_type", mcp.Description("Story validation type: review, test, integration, user_test, manual_check, audit")),
			mcp.WithString("validation_status", mcp.Description("Story validation status: pass, fail, blocked, waived")),
			mcp.WithString("artifact_path", mcp.Description("Optional validation artifact path")),
			mcp.WithString("artifact_digest", mcp.Description("Optional CAS digest for the validation artifact")),
			mcp.WithString("command", mcp.Description("Optional command or check run for story validation")),
			mcp.WithString("validation_notes", mcp.Description("Optional extra notes for story validation")),
			mcp.WithString("kind", mcp.Description("Retro kind: process, tooling, coordination, quality, delivery")),
			mcp.WithString("summary", mcp.Description("Structured summary or retro summary text")),
			mcp.WithString("impact", mcp.Description("Retro impact statement")),
			mcp.WithString("change", mcp.Description("Retro recommended change")),
			mcp.WithArray("related_story_ids", mcp.Description("Related story ids for cross-story validation"), mcp.WithStringItems()),
			mcp.WithString("question_id", mcp.Description("Epic intake question id")),
			mcp.WithString("title", mcp.Description("Epic, milestone, or story title")),
			mcp.WithString("goal", mcp.Description("Goal text for epic or milestone start")),
			mcp.WithString("owner", mcp.Description("Owner actor id")),
			mcp.WithString("outcome", mcp.Description("Expected outcome for epic start")),
			mcp.WithString("horizon", mcp.Description("Delivery horizon for epic start")),
			mcp.WithArray("scope", mcp.Description("Scope item (repeatable)"), mcp.WithStringItems()),
			mcp.WithArray("success", mcp.Description("Epic success signal (repeatable)"), mcp.WithStringItems()),
			mcp.WithString("to", mcp.Description("Directed respondent actor id for epic intake")),
			mcp.WithString("question_kind", mcp.Description("Epic intake question kind: product, technical, constraint, success")),
			mcp.WithString("question", mcp.Description("Epic intake question text")),
			mcp.WithString("answer", mcp.Description("Epic intake answer text")),
			mcp.WithString("close_reason", mcp.Description("Epic close reason: completed, wont_do, superseded, cancelled")),
			mcp.WithString("label", mcp.Description("Checkpoint label")),
			mcp.WithString("note", mcp.Description("Checkpoint coordinator note")),
			mcp.WithString("criterion", mcp.Description("Acceptance criterion text")),
			mcp.WithString("verdict", mcp.Description("Milestone review verdict: pass or block")),
			mcp.WithString("notes", mcp.Description("Notes or description body")),
			mcp.WithString("rationale", mcp.Description("Why a proposed story belongs in the milestone")),
			mcp.WithArray("completed", mcp.Description("Delivery log completed items"), mcp.WithStringItems()),
			mcp.WithArray("in_flight", mcp.Description("Delivery log in-flight items"), mcp.WithStringItems()),
			mcp.WithArray("blocker", mcp.Description("Delivery log blocker items"), mcp.WithStringItems()),
			mcp.WithArray("next", mcp.Description("Delivery log next-focus items"), mcp.WithStringItems()),
			mcp.WithArray("follow_up", mcp.Description("Retro follow-up items"), mcp.WithStringItems()),
			mcp.WithNumber("limit", mcp.Description("Maximum room messages to inspect for show actions")),
			mcp.WithNumber("count", mcp.Description("Maximum proposal count for epic shaping")),
		),
		handleRoomAgileTool,
	)

	s.AddTool(
		mcp.NewTool("room_remind",
			mcp.WithDescription("Command-backed durable room follow-up scheduler. Actions: add, list, cancel."),
			mcp.WithString("action", mcp.Required(), mcp.Description("Reminder action to run")),
			mcp.WithString("workspace", mcp.Description("Workspace root override (default: .)")),
			mcp.WithString("room_id", mcp.Description("Room id")),
			mcp.WithString("sender", mcp.Description("Sender actor or participant id override")),
			mcp.WithString("actor", mcp.Description("Coordinator actor id for cancel")),
			mcp.WithString("recipient", mcp.Description("Direct reminder recipient")),
			mcp.WithString("subject", mcp.Description("Optional root message subject")),
			mcp.WithString("text", mcp.Description("Reminder request text")),
			mcp.WithString("every", mcp.Description("Reminder interval duration")),
			mcp.WithNumber("max_iterations", mcp.Description("Maximum reminder follow-ups after the initial request")),
			mcp.WithBoolean("ack_required", mcp.Description("Require ack to stop reminders")),
			mcp.WithBoolean("reply_expected", mcp.Description("Require reply to stop reminders")),
			mcp.WithBoolean("interrupt", mcp.Description("Interrupt the target pane for reminder follow-ups")),
			mcp.WithString("reminder_id", mcp.Description("Reminder id for cancel")),
			mcp.WithBoolean("all", mcp.Description("Include inactive reminders when listing")),
		),
		handleRoomRemindTool,
	)

	s.AddTool(
		mcp.NewTool("agent_room",
			mcp.WithDescription("Command-backed agent control-room tool. Actions: info, policy."),
			mcp.WithString("action", mcp.Required(), mcp.Description("Agent room action to run")),
			mcp.WithString("agent_ref", mcp.Description("Agent reference for info/policy")),
			mcp.WithString("workspace", mcp.Description("Workspace path override")),
			mcp.WithString("room_id", mcp.Description("Override control room id")),
			mcp.WithString("dispatch_policy", mcp.Description("Dispatch policy for policy action")),
			mcp.WithArray("dispatch_agents", mcp.Description("Dispatch agent ids for policy action"), mcp.WithStringItems()),
		),
		handleAgentRoomTool,
	)
}

func registerMuxTools(s *server.MCPServer) {
	s.AddTool(
		mcp.NewTool("mux",
			mcp.WithDescription("Command-backed tmux/zellij collaboration tool. Actions: list, read, send, submit, send_parent, observe, doctor, create."),
			mcp.WithString("action", mcp.Required(), mcp.Description("Mux action to run")),
			mcp.WithString("backend", mcp.Description("Mux backend (tmux|zellij|auto)")),
			mcp.WithString("session", mcp.Description("Mux session override")),
			mcp.WithNumber("limit", mcp.Description("Limit when listing zellij-bound panes")),
			mcp.WithString("target", mcp.Description("Pane target for read/send/observe")),
			mcp.WithNumber("lines", mcp.Description("Scrollback lines to read/observe")),
			mcp.WithString("sender", mcp.Description("Sender pane label or pane id override")),
			mcp.WithString("text", mcp.Description("Message text for send/send_parent")),
			mcp.WithString("statement", mcp.Description("Observation statement override")),
			mcp.WithString("workspace", mcp.Description("Workspace path for observe")),
			mcp.WithNumber("confidence", mcp.Description("Observation confidence")),
			mcp.WithNumber("count", mcp.Description("Observation count")),
			mcp.WithString("project", mcp.Description("Observation project")),
			mcp.WithString("area", mcp.Description("Observation area")),
			mcp.WithBoolean("dry_run", mcp.Description("Preview observe without persisting")),
			mcp.WithNumber("panes", mcp.Description("Number of panes for create")),
			mcp.WithString("pane_command", mcp.Description("Command to launch in each pane")),
			mcp.WithString("agent", mcp.Description("Agent CLI to launch in each pane")),
			mcp.WithString("mode", mcp.Description("Agent launch mode for create")),
			mcp.WithArray("agent_args", mcp.Description("Agent CLI arguments"), mcp.WithStringItems()),
			mcp.WithString("agent_session_id", mcp.Description("Resume agent session id for create")),
			mcp.WithString("cwd", mcp.Description("Working directory for create")),
			mcp.WithString("label_prefix", mcp.Description("Label prefix for create")),
			mcp.WithString("parent_participant", mcp.Description("Parent participant id for child panes")),
			mcp.WithString("parent_agent_id", mcp.Description("Parent agent id for child panes")),
			mcp.WithString("room_id", mcp.Description("Room id exported into created panes")),
			mcp.WithString("room_access", mcp.Description("Room access policy for created panes")),
			mcp.WithBoolean("attach", mcp.Description("Attach or switch after create")),
		),
		handleMuxTool,
	)
}

// Tool handlers

// getArgs safely extracts arguments from the request as a map.
func getArgs(req mcp.CallToolRequest) map[string]any {
	if args, ok := req.Params.Arguments.(map[string]any); ok {
		return args
	}
	return make(map[string]any)
}

func getStringArg(args map[string]any, key, fallback string) string {
	if value, ok := args[key].(string); ok {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

func getBoolArg(args map[string]any, key string, fallback bool) bool {
	if value, ok := args[key].(bool); ok {
		return value
	}
	return fallback
}

func getIntArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case float64:
		if int(value) > 0 {
			return int(value)
		}
	case int:
		if value > 0 {
			return value
		}
	}
	return fallback
}

func getStringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		return value
	case []any:
		var result []string
		for _, item := range value {
			if str, ok := item.(string); ok {
				trimmed := strings.TrimSpace(str)
				if trimmed != "" {
					result = append(result, trimmed)
				}
			}
		}
		return result
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return []string{trimmed}
		}
	}
	return nil
}

func getFloatArg(args map[string]any, key string, fallback float64) float64 {
	switch value := args[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	}
	return fallback
}

func appendStringFlagArgs(argv []string, flagName, value string) []string {
	if strings.TrimSpace(value) == "" {
		return argv
	}
	return append(argv, flagName, strings.TrimSpace(value))
}

func appendStringSliceFlagArgs(argv []string, flagName string, values []string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		argv = append(argv, flagName, value)
	}
	return argv
}

func appendBoolFlagArgs(argv []string, flagName string, enabled bool) []string {
	if enabled {
		return append(argv, flagName)
	}
	return argv
}

func appendIntFlagArgs(argv []string, flagName string, value int) []string {
	if value > 0 {
		return append(argv, flagName, strconv.Itoa(value))
	}
	return argv
}

func appendFloatFlagArgs(argv []string, flagName string, value float64) []string {
	if value > 0 {
		return append(argv, flagName, strconv.FormatFloat(value, 'f', -1, 64))
	}
	return argv
}

func appendDurationFlagArgs(argv []string, flagName string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return argv
	}
	if _, err := time.ParseDuration(strings.TrimSpace(value)); err != nil {
		return argv
	}
	return append(argv, flagName, strings.TrimSpace(value))
}

func runCLICommandAsMCP(ctx context.Context, toolLabel string, newCmd func() *cobra.Command, argv []string) (*mcp.CallToolResult, error) {
	return runEnvelopeCommand(ctx, toolLabel, func(cmd *cobra.Command) error {
		root := newCmd()
		var out bytes.Buffer
		var errBuf bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&errBuf)
		runCtx := ctx
		if _, ok := config.FromContext(runCtx); !ok {
			if cfg, err := loadConfig(ctx); err == nil {
				runCtx = config.WithContext(runCtx, cfg)
			}
		}
		root.SetContext(runCtx)
		root.SetArgs(argv)
		if err := root.Execute(); err != nil {
			return err
		}
		if out.Len() > 0 {
			_, _ = cmd.OutOrStdout().Write(out.Bytes())
		}
		if errBuf.Len() > 0 {
			_, _ = cmd.ErrOrStderr().Write(errBuf.Bytes())
		}
		return nil
	})
}

func runRepoIndexCommand(ctx context.Context, run func(cmd *cobra.Command) error) (*mcp.CallToolResult, error) {
	return runEnvelopeCommand(ctx, "repo_index", run)
}

func runEnvelopeCommand(ctx context.Context, toolLabel string, run func(cmd *cobra.Command) error) (*mcp.CallToolResult, error) {
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	runCtx := ctx
	if _, ok := config.FromContext(runCtx); !ok {
		if cfg, err := loadConfig(ctx); err == nil {
			runCtx = config.WithContext(runCtx, cfg)
		}
	}
	cmd.SetContext(runCtx)

	if err := run(cmd); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	raw := strings.TrimSpace(out.String())
	if raw == "" {
		return mcp.NewToolResultText("{}"), nil
	}

	var envelope struct {
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return truncateSkillOutput(ctx, raw, toolLabel)
	}
	if envelope.Status == "error" {
		if envelope.Error.Message != "" {
			return mcp.NewToolResultError(envelope.Error.Message), nil
		}
		return mcp.NewToolResultError(toolLabel + " command failed"), nil
	}

	result, err := json.MarshalIndent(envelope.Data, "", "  ")
	if err != nil {
		return truncateSkillOutput(ctx, raw, toolLabel)
	}

	return truncateSkillOutput(ctx, string(result), toolLabel)
}

func handleWebSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	query, _ := args["query"].(string)
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	maxResults := 10.0
	if mr, ok := args["max_results"].(float64); ok && mr > 0 {
		maxResults = mr
	}

	topic := "general"
	if t, ok := args["topic"].(string); ok && t != "" {
		topic = t
	}

	// Check for provider override (env var or arg)
	provider := os.Getenv("AGENTCTL_SEARCH_PROVIDER")
	if p, ok := args["provider"].(string); ok && p != "" {
		provider = p
	}
	if provider == "" {
		provider = "exa" // Default to exa
	}

	switch provider {
	case "tavily":
		return callBackend(ctx, "tavily", "tavily-search", map[string]any{
			"query":       query,
			"max_results": int(maxResults),
			"topic":       topic,
		})
	case "perplexity":
		return callBackend(ctx, "perplexity", "perplexity_ask", map[string]any{
			"messages": []map[string]string{
				{"role": "user", "content": query},
			},
		})
	default: // exa
		return callBackend(ctx, "exa", "web_search_exa", map[string]any{
			"query":      query,
			"numResults": int(maxResults),
		})
	}
}

func handleWebExtract(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	urls, ok := args["urls"].([]any)
	if !ok || len(urls) == 0 {
		return mcp.NewToolResultError("urls is required"), nil
	}

	// Convert to string slice
	urlStrings := make([]string, 0, len(urls))
	for _, u := range urls {
		if s, ok := u.(string); ok {
			urlStrings = append(urlStrings, s)
		}
	}

	return callBackend(ctx, "tavily", "tavily-extract", map[string]any{
		"urls": urlStrings,
	})
}

func handleDocsLookup(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	library, _ := args["library"].(string)
	if library == "" {
		return mcp.NewToolResultError("library is required"), nil
	}

	topic, _ := args["topic"].(string)

	// First resolve the library ID
	resolveResult, err := callBackend(ctx, "context7", "resolve-library-id", map[string]any{
		"libraryName": library,
	})
	if err != nil {
		return mcp.NewToolResultErrorFromErr("resolve library", err), nil
	}
	if resolveResult.IsError {
		return resolveResult, nil
	}

	// Parse the resolved library ID from the result
	libraryID := extractLibraryID(resolveResult)
	if libraryID == "" {
		return mcp.NewToolResultError("could not resolve library ID for: " + library), nil
	}

	// Now get the docs
	docsArgs := map[string]any{
		"context7CompatibleLibraryID": libraryID,
	}
	if topic != "" {
		docsArgs["topic"] = topic
	}

	return callBackend(ctx, "context7", "get-library-docs", docsArgs)
}

func handleCodeSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	query, _ := args["query"].(string)
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	return callBackend(ctx, "exa", "get_code_context_exa", map[string]any{
		"query": query,
	})
}

func handleAsk(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	question, _ := args["question"].(string)
	if question == "" {
		return mcp.NewToolResultError("question is required"), nil
	}

	return callBackend(ctx, "perplexity", "perplexity_ask", map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": question},
		},
	})
}

func handleWebCrawl(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	url, _ := args["url"].(string)
	if url == "" {
		return mcp.NewToolResultError("url is required"), nil
	}

	maxDepth := 2.0
	if md, ok := args["max_depth"].(float64); ok && md > 0 {
		maxDepth = md
	}

	limit := 10.0
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = l
	}

	return callBackend(ctx, "tavily", "tavily-crawl", map[string]any{
		"url":       url,
		"max_depth": int(maxDepth),
		"limit":     int(limit),
	})
}

func handleWebMap(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	url, _ := args["url"].(string)
	if url == "" {
		return mcp.NewToolResultError("url is required"), nil
	}

	limit := 50.0
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = l
	}

	return callBackend(ctx, "tavily", "tavily-map", map[string]any{
		"url":   url,
		"limit": int(limit),
	})
}

func handleWebSearchGeneral(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	query, _ := args["query"].(string)
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}

	numResults := 8.0
	if nr, ok := args["num_results"].(float64); ok && nr > 0 {
		numResults = nr
	}

	return callBackend(ctx, "exa", "web_search_exa", map[string]any{
		"query":      query,
		"numResults": int(numResults),
	})
}

// Expo tool handlers

func handleExpoBuild(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	platform, _ := args["platform"].(string)
	if platform == "" {
		return mcp.NewToolResultError("platform is required"), nil
	}

	buildArgs := map[string]any{
		"platform": platform,
	}
	if profile, ok := args["profile"].(string); ok && profile != "" {
		buildArgs["profile"] = profile
	}

	return callBackend(ctx, "expo", "eas_build", buildArgs)
}

func handleExpoUpdate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	channel, _ := args["channel"].(string)
	if channel == "" {
		return mcp.NewToolResultError("channel is required"), nil
	}

	updateArgs := map[string]any{
		"channel": channel,
	}
	if message, ok := args["message"].(string); ok && message != "" {
		updateArgs["message"] = message
	}

	return callBackend(ctx, "expo", "eas_update", updateArgs)
}

func handleExpoScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)

	platform := "ios"
	if p, ok := args["platform"].(string); ok && p != "" {
		platform = p
	}

	return callBackend(ctx, "expo", "take_screenshot", map[string]any{
		"platform": platform,
	})
}

// Supabase tool handlers

func handleSupabaseQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	sql, _ := args["sql"].(string)
	if sql == "" {
		return mcp.NewToolResultError("sql is required"), nil
	}

	return callBackend(ctx, "supabase", "execute_sql", map[string]any{
		"query": sql,
	})
}

func handleSupabaseTables(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return callBackend(ctx, "supabase", "list_tables", map[string]any{})
}

func handleSupabaseLogs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)

	logArgs := map[string]any{}
	if service, ok := args["service"].(string); ok && service != "" {
		logArgs["service"] = service
	}
	if limit, ok := args["limit"].(float64); ok && limit > 0 {
		logArgs["limit"] = int(limit)
	}

	return callBackend(ctx, "supabase", "get_logs", logArgs)
}

// Playwright/Browser tool handlers

func handleBrowserNavigate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	url, _ := args["url"].(string)
	if url == "" {
		return mcp.NewToolResultError("url is required"), nil
	}

	return callBackend(ctx, "playwright", "browser_navigate", map[string]any{
		"url": url,
	})
}

func handleBrowserScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)

	screenshotArgs := map[string]any{}
	if name, ok := args["name"].(string); ok && name != "" {
		screenshotArgs["name"] = name
	}
	if fullPage, ok := args["full_page"].(bool); ok {
		screenshotArgs["fullPage"] = fullPage
	}

	return callBackend(ctx, "playwright", "browser_screenshot", screenshotArgs)
}

func handleBrowserClick(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	selector, _ := args["selector"].(string)
	if selector == "" {
		return mcp.NewToolResultError("selector is required"), nil
	}

	return callBackend(ctx, "playwright", "browser_click", map[string]any{
		"selector": selector,
	})
}

func handleBrowserFill(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	selector, _ := args["selector"].(string)
	if selector == "" {
		return mcp.NewToolResultError("selector is required"), nil
	}
	value, _ := args["value"].(string)
	if value == "" {
		return mcp.NewToolResultError("value is required"), nil
	}

	return callBackend(ctx, "playwright", "browser_fill", map[string]any{
		"selector": selector,
		"value":    value,
	})
}

func handleBrowserContent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)

	contentArgs := map[string]any{}
	if selector, ok := args["selector"].(string); ok && selector != "" {
		contentArgs["selector"] = selector
	}

	return callBackend(ctx, "playwright", "browser_get_content", contentArgs)
}

// ============================================================================
// Generic Agentctl Tools (run + discovery)
// ============================================================================

// registerAgentctlTools adds the generic agentctl_run and agentctl_skills tools.
// This replaces individual skill registration, reducing token overhead from ~83k to ~1k.
func registerAgentctlTools(s *server.MCPServer) {
	// agentctl_run - Generic skill execution
	s.AddTool(
		mcp.NewTool("agentctl_run",
			mcp.WithDescription("Run an foxctl skill. Use agentctl_skills to discover available skills."),
			mcp.WithString("skill", mcp.Required(), mcp.Description("Skill name (e.g., 'code/complexity', 'todo/manage', 'test/run')")),
			mcp.WithObject("input", mcp.Description("Input arguments for the skill as a JSON object. Check skill signature with agentctl_skills for required parameters.")),
		),
		handleAgentctlRun,
	)

	// agentctl_skills - Skill discovery
	s.AddTool(
		mcp.NewTool("agentctl_skills",
			mcp.WithDescription("List available foxctl skills with their descriptions and parameters."),
			mcp.WithString("filter", mcp.Description("Filter skills by category prefix (e.g., 'code/', 'test/', 'session/')")),
			mcp.WithBoolean("verbose", mcp.Description("Include full parameter signatures (default: false)")),
		),
		handleAgentctlSkills,
	)

	s.AddTool(
		mcp.NewTool("context_show",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
		),
		handleContextShow,
	)

	s.AddTool(
		mcp.NewTool("context_report",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
		),
		handleContextReport,
	)

	s.AddTool(
		mcp.NewTool("context_retrieve",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("vault_path", mcp.Required(), mcp.Description("Vault path")),
			mcp.WithString("query", mcp.Required(), mcp.Description("Retrieval query")),
			mcp.WithNumber("limit", mcp.Description("Maximum ranked vault hits")),
		),
		handleContextRetrieve,
	)

	s.AddTool(
		mcp.NewTool("context_next",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
		),
		handleContextNext,
	)

	s.AddTool(
		mcp.NewTool("context_next_proposal_merge",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("vault_path", mcp.Description("Optional vault path for health refresh before selection")),
			mcp.WithNumber("limit", mcp.Description("Maintenance-task scan limit (default: 50)")),
			mcp.WithBoolean("claim", mcp.Description("Claim the selected proposal-merge task so it is not re-offered")),
		),
		handleContextNextProposalMerge,
	)

	s.AddTool(
		mcp.NewTool("context_dispatch",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("task_id", mcp.Description("Explicit task ID (optional)")),
		),
		handleContextDispatch,
	)

	s.AddTool(
		mcp.NewTool("context_contradictions",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("vault_path", mcp.Required(), mcp.Description("Vault path")),
			mcp.WithNumber("limit", mcp.Description("Maximum findings")),
		),
		handleContextContradictions,
	)

	s.AddTool(
		mcp.NewTool("context_handoffs",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("path", mcp.Description("Specific handoff path or filename (optional)")),
			mcp.WithNumber("limit", mcp.Description("Maximum handoffs to list (default: 20)")),
		),
		handleContextHandoffs,
	)

	s.AddTool(
		mcp.NewTool("context_observations",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithNumber("limit", mcp.Description("Maximum observations to list (default: 20)")),
		),
		handleContextObservations,
	)

	s.AddTool(
		mcp.NewTool("context_tensions",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithNumber("limit", mcp.Description("Maximum tensions to list (default: 20)")),
		),
		handleContextTensions,
	)

	s.AddTool(
		mcp.NewTool("context_proposals",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("id", mcp.Description("Specific proposal ID (optional)")),
			mcp.WithNumber("limit", mcp.Description("Maximum proposals to list (default: 20)")),
		),
		handleContextProposals,
	)

	s.AddTool(
		mcp.NewTool("context_proposal_apply",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("id", mcp.Required(), mcp.Description("Proposal ID")),
		),
		handleContextProposalApply,
	)

	s.AddTool(
		mcp.NewTool("context_proposal_reject",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("id", mcp.Required(), mcp.Description("Proposal ID")),
		),
		handleContextProposalReject,
	)

	s.AddTool(
		mcp.NewTool("context_proposal_release_merge",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("id", mcp.Required(), mcp.Description("Proposal ID")),
		),
		handleContextProposalReleaseMerge,
	)

	s.AddTool(
		mcp.NewTool("context_proposal_merge",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("id", mcp.Required(), mcp.Description("Proposal ID")),
			mcp.WithString("vault_name", mcp.Description("Vault name")),
			mcp.WithString("vault_path", mcp.Required(), mcp.Description("Vault path")),
			mcp.WithString("draft_path", mcp.Description("Optional draft path override")),
			mcp.WithString("target_path", mcp.Description("Optional canonical target note path override")),
			mcp.WithString("heading", mcp.Description("Optional bounded review heading override")),
		),
		handleContextProposalMerge,
	)

	s.AddTool(
		mcp.NewTool("context_rethink",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("vault_path", mcp.Description("Vault path for health-derived maintenance tasks")),
			mcp.WithNumber("limit", mcp.Description("Maximum maintenance tasks to emit (default: 20)")),
		),
		handleContextRethink,
	)

	s.AddTool(
		mcp.NewTool("context_promote",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("source", mcp.Description("Promotion source: handoff or observation")),
			mcp.WithString("path", mcp.Description("Handoff path or filename when source=handoff")),
			mcp.WithString("id", mcp.Description("Observation ID when source=observation")),
			mcp.WithString("type", mcp.Description("Draft note type")),
			mcp.WithString("title", mcp.Description("Draft note title")),
		),
		handleContextPromote,
	)

	s.AddTool(
		mcp.NewTool("context_merge_promotion",
			mcp.WithString("workspace", mcp.Description("Workspace path (optional; defaults to current workspace context)")),
			mcp.WithString("vault_name", mcp.Description("Vault name")),
			mcp.WithString("vault_path", mcp.Required(), mcp.Description("Vault path")),
			mcp.WithString("draft_path", mcp.Description("Promotion draft path (defaults to latest drafted job)")),
			mcp.WithString("target_path", mcp.Required(), mcp.Description("Canonical target note path inside the vault")),
			mcp.WithString("heading", mcp.Description("Bounded review heading when appending into an existing note")),
		),
		handleContextMergePromotion,
	)

	s.AddTool(
		mcp.NewTool("obsidian_read",
			mcp.WithString("vault_name", mcp.Description("Vault name")),
			mcp.WithString("vault_path", mcp.Description("Vault path")),
			mcp.WithString("path", mcp.Required(), mcp.Description("Note path")),
		),
		handleObsidianRead,
	)

	s.AddTool(
		mcp.NewTool("obsidian_search",
			mcp.WithString("vault_name", mcp.Description("Vault name")),
			mcp.WithString("vault_path", mcp.Description("Vault path")),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
			mcp.WithString("scope_path", mcp.Description("Vault subpath filter")),
			mcp.WithNumber("limit", mcp.Description("Maximum result count")),
		),
		handleObsidianSearch,
	)

	s.AddTool(
		mcp.NewTool("obsidian_related",
			mcp.WithString("vault_path", mcp.Required(), mcp.Description("Vault path")),
			mcp.WithString("path", mcp.Required(), mcp.Description("Note path")),
			mcp.WithNumber("limit", mcp.Description("Maximum result count")),
		),
		handleObsidianRelated,
	)

	s.AddTool(
		mcp.NewTool("obsidian_create_note",
			mcp.WithString("vault_name", mcp.Description("Vault name")),
			mcp.WithString("vault_path", mcp.Description("Vault path")),
			mcp.WithString("path", mcp.Required(), mcp.Description("Note path")),
			mcp.WithString("type", mcp.Description("Note type")),
			mcp.WithString("project", mcp.Description("Project name")),
			mcp.WithString("status", mcp.Description("Status")),
			mcp.WithString("trust", mcp.Description("Trust level")),
			mcp.WithString("body", mcp.Description("Markdown body")),
		),
		handleObsidianCreateNote,
	)

	s.AddTool(
		mcp.NewTool("obsidian_append_under_heading",
			mcp.WithString("vault_name", mcp.Description("Vault name")),
			mcp.WithString("vault_path", mcp.Description("Vault path")),
			mcp.WithString("path", mcp.Required(), mcp.Description("Note path")),
			mcp.WithString("heading", mcp.Required(), mcp.Description("Heading to append under")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Markdown content")),
		),
		handleObsidianAppendUnderHeading,
	)

	s.AddTool(
		mcp.NewTool("obsidian_capture_session",
			mcp.WithString("vault_name", mcp.Description("Vault name")),
			mcp.WithString("vault_path", mcp.Description("Vault path")),
			mcp.WithString("slug", mcp.Description("Session note slug")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Markdown content")),
		),
		handleObsidianCaptureSession,
	)

	s.AddTool(
		mcp.NewTool("obsidian_promote_to_evergreen",
			mcp.WithString("vault_name", mcp.Description("Vault name")),
			mcp.WithString("vault_path", mcp.Description("Vault path")),
			mcp.WithString("slug", mcp.Description("Draft slug")),
			mcp.WithString("content", mcp.Required(), mcp.Description("Markdown content")),
		),
		handleObsidianPromoteToEvergreen,
	)

	s.AddTool(
		mcp.NewTool("obsidian_merge_reviewed_draft",
			mcp.WithString("vault_name", mcp.Description("Vault name")),
			mcp.WithString("vault_path", mcp.Required(), mcp.Description("Vault path")),
			mcp.WithString("draft_path", mcp.Required(), mcp.Description("Local reviewed draft path")),
			mcp.WithString("target_path", mcp.Required(), mcp.Description("Canonical target note path inside the vault")),
			mcp.WithString("heading", mcp.Description("Bounded heading for appending into an existing note")),
		),
		handleObsidianMergeReviewedDraft,
	)
}

// registerSkillGroups registers skills from specified groups as first-class MCP tools.
// This provides better UX than agentctl_run by exposing proper parameter schemas.
func registerSkillGroups(ctx context.Context, s *server.MCPServer, groups []string) error {
	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Collect skills to register from specified groups
	skillsToRegister := make(map[string]bool)
	includeAll := false
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group), "all") {
			includeAll = true
			break
		}
	}

	if includeAll {
		manifests, err := discoverSkills(cfg)
		if err != nil {
			return fmt.Errorf("discover skills: %w", err)
		}
		for _, manifest := range manifests {
			skillsToRegister[manifest.Metadata.Name] = true
		}
	} else {
		for _, group := range groups {
			group = strings.ToLower(strings.TrimSpace(group))
			if group == "" {
				continue
			}
			skills, ok := skillGroups[group]
			if !ok {
				available := strings.Join(skillGroupNames(), ", ")
				fmt.Fprintf(os.Stderr, "Warning: unknown skill group %q (available: %s)\n", group, available)
				continue
			}
			for _, skillName := range skills {
				skillsToRegister[skillName] = true
			}
		}
	}

	// Load and register each skill
	for skillName := range skillsToRegister {
		handle, err := findSkill(cfg, skillName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skill %q not found: %v\n", skillName, err)
			continue
		}
		registerSkillAsTool(s, handle.Manifest, handle.ArtifactPath)
	}

	return nil
}

// registerSkillAsTool registers a single skill as a first-class MCP tool with proper schema.
func registerSkillAsTool(s *server.MCPServer, manifest skill.Manifest, artifactPath string) {
	// Convert skill name to MCP tool name: code/semantic_search -> code_semantic_search
	toolName := strings.ReplaceAll(manifest.Metadata.Name, "/", "_")
	if _, exists := s.ListTools()[toolName]; exists {
		return
	}

	// Build MCP tool options from manifest
	toolOpts := []mcp.ToolOption{
		mcp.WithDescription(manifest.Metadata.Description),
	}

	// Add parameters from skill signature
	for _, param := range manifest.Signature.Parameters {
		switch param.Type {
		case "string":
			if param.Required {
				toolOpts = append(toolOpts, mcp.WithString(param.Name, mcp.Required(), mcp.Description(param.Description)))
			} else {
				toolOpts = append(toolOpts, mcp.WithString(param.Name, mcp.Description(param.Description)))
			}
		case "number", "integer", "float":
			if param.Required {
				toolOpts = append(toolOpts, mcp.WithNumber(param.Name, mcp.Required(), mcp.Description(param.Description)))
			} else {
				toolOpts = append(toolOpts, mcp.WithNumber(param.Name, mcp.Description(param.Description)))
			}
		case "boolean", "bool":
			if param.Required {
				toolOpts = append(toolOpts, mcp.WithBoolean(param.Name, mcp.Required(), mcp.Description(param.Description)))
			} else {
				toolOpts = append(toolOpts, mcp.WithBoolean(param.Name, mcp.Description(param.Description)))
			}
		case "array":
			// Arrays need items schema - determine item type from param.Items or default to string
			var itemsOpt mcp.PropertyOption
			if param.Items != nil && param.Items.Type != "" {
				switch param.Items.Type {
				case "number", "integer", "float":
					itemsOpt = mcp.WithNumberItems()
				case "boolean", "bool":
					itemsOpt = mcp.WithBooleanItems()
				case "object":
					itemsOpt = mcp.Items(map[string]any{"type": "object"})
				default:
					itemsOpt = mcp.WithStringItems()
				}
			} else {
				itemsOpt = mcp.WithStringItems() // default to string items
			}
			if param.Required {
				toolOpts = append(toolOpts, mcp.WithArray(param.Name, mcp.Required(), mcp.Description(param.Description), itemsOpt))
			} else {
				toolOpts = append(toolOpts, mcp.WithArray(param.Name, mcp.Description(param.Description), itemsOpt))
			}
		case "object":
			if param.Required {
				toolOpts = append(toolOpts, mcp.WithObject(param.Name, mcp.Required(), mcp.Description(param.Description)))
			} else {
				toolOpts = append(toolOpts, mcp.WithObject(param.Name, mcp.Description(param.Description)))
			}
		default:
			// Default to string for unknown types
			if param.Required {
				toolOpts = append(toolOpts, mcp.WithString(param.Name, mcp.Required(), mcp.Description(param.Description)))
			} else {
				toolOpts = append(toolOpts, mcp.WithString(param.Name, mcp.Description(param.Description)))
			}
		}
	}

	// Create the MCP tool
	tool := mcp.NewTool(toolName, toolOpts...)
	ensureArrayItems(tool.InputSchema.Properties)

	// Create handler that captures manifest and artifactPath
	handler := makeSkillHandler(manifest, artifactPath)

	s.AddTool(tool, handler)
}

func ensureArrayItems(properties map[string]any) {
	for _, prop := range properties {
		propSchema, ok := prop.(map[string]any)
		if !ok {
			continue
		}
		propType, _ := propSchema["type"].(string)
		switch propType {
		case "array":
			if items, ok := propSchema["items"]; !ok || items == nil {
				propSchema["items"] = map[string]any{"type": "string"}
			}
		case "object":
			nested, ok := propSchema["properties"].(map[string]any)
			if ok {
				ensureArrayItems(nested)
			}
		}
	}
}

// makeSkillHandler creates a tool handler for a specific skill.
func makeSkillHandler(manifest skill.Manifest, artifactPath string) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := getArgs(req)

		// Marshal arguments to JSON for skill input
		inputBytes, err := json.Marshal(args)
		if err != nil {
			return mcp.NewToolResultError("marshal input: " + err.Error()), nil
		}

		// Resolve workspace context
		runCtx := resolveWorkspaceContext(ctx, "")

		// Execute the skill
		stdout, stderr, err := executeSkill(runCtx, manifest, artifactPath, inputBytes)
		return renderSkillExecutionResult(ctx, manifest.Metadata.Name, stdout, stderr, err)
	}
}

func handleAgentctlRun(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	skillName, _ := args["skill"].(string)
	if skillName == "" {
		return mcp.NewToolResultError("skill name is required"), nil
	}

	// Extract input - can be map or nil
	var input map[string]any
	if inputArg, ok := args["input"].(map[string]any); ok {
		input = inputArg
	} else {
		input = make(map[string]any)
	}

	// Load config
	cfg, err := loadConfig(ctx)
	if err != nil {
		return mcp.NewToolResultError("load config: " + err.Error()), nil
	}

	// Find the skill
	handle, err := findSkill(cfg, skillName)
	if err != nil {
		return mcp.NewToolResultError("skill not found: " + skillName + " - " + err.Error()), nil
	}

	// Marshal input to JSON
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return mcp.NewToolResultError("marshal input: " + err.Error()), nil
	}

	// Resolve workspace context
	runCtx := resolveWorkspaceContext(ctx, "")

	// Execute the skill
	stdout, stderr, err := executeSkill(runCtx, handle.Manifest, handle.ArtifactPath, inputBytes)
	return renderSkillExecutionResult(ctx, skillName, stdout, stderr, err)
}

func runSkillAsMCP(ctx context.Context, skillName string, input map[string]any) (*mcp.CallToolResult, error) {
	if strings.TrimSpace(skillName) == "" {
		return mcp.NewToolResultError("skill name is required"), nil
	}
	if input == nil {
		input = make(map[string]any)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		return mcp.NewToolResultError("load config: " + err.Error()), nil
	}

	handle, err := findSkill(cfg, skillName)
	if err != nil {
		return mcp.NewToolResultError("skill not found: " + skillName + " - " + err.Error()), nil
	}

	inputBytes, err := json.Marshal(input)
	if err != nil {
		return mcp.NewToolResultError("marshal input: " + err.Error()), nil
	}

	runCtx := resolveWorkspaceContext(ctx, "")
	stdout, stderr, err := executeSkill(runCtx, handle.Manifest, handle.ArtifactPath, inputBytes)
	return renderSkillExecutionResult(ctx, skillName, stdout, stderr, err)
}

type skillExecutionEnvelope struct {
	Status  string         `json:"status"`
	Command string         `json:"command"`
	Data    map[string]any `json:"data"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func renderSkillExecutionResult(ctx context.Context, skillName string, stdout, stderr []byte, execErr error) (*mcp.CallToolResult, error) {
	if envelope, ok := decodeSkillExecutionEnvelope(stdout); ok {
		if envelope.Status == "error" {
			msg := strings.TrimSpace(envelope.Error.Message)
			if msg == "" {
				msg = skillName + " failed"
			}
			return mcp.NewToolResultError(msg), nil
		}
		result, err := json.MarshalIndent(envelope.Data, "", "  ")
		if err != nil {
			return truncateSkillOutput(ctx, strings.TrimSpace(string(stdout)), skillName)
		}
		return truncateSkillOutput(ctx, string(result), skillName)
	}

	if execErr != nil {
		errMsg := execErr.Error()
		if len(stderr) > 0 {
			errMsg += "\nstderr: " + string(stderr)
		}
		return mcp.NewToolResultError("execute skill: " + errMsg), nil
	}

	return truncateSkillOutput(ctx, strings.TrimSpace(string(stdout)), skillName)
}

func decodeSkillExecutionEnvelope(stdout []byte) (skillExecutionEnvelope, bool) {
	var envelope skillExecutionEnvelope
	if len(bytes.TrimSpace(stdout)) == 0 {
		return envelope, false
	}
	if err := json.Unmarshal(stdout, &envelope); err != nil {
		return envelope, false
	}
	if strings.TrimSpace(envelope.Status) == "" {
		return envelope, false
	}
	return envelope, true
}

func handleAgentctlSkills(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	filter, _ := args["filter"].(string)
	verbose, _ := args["verbose"].(bool)

	// Load config
	cfg, err := loadConfig(ctx)
	if err != nil {
		return mcp.NewToolResultError("load config: " + err.Error()), nil
	}

	// Discover all skills
	manifests, err := discoverSkills(cfg)
	if err != nil {
		return mcp.NewToolResultError("discover skills: " + err.Error()), nil
	}

	// Build response
	type skillInfo struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Parameters  []skill.Parameter `json:"parameters,omitempty"`
		InputSchema map[string]any    `json:"input_schema,omitempty"`
	}

	var skills []skillInfo
	for _, m := range manifests {
		// Apply filter
		if filter != "" && !strings.HasPrefix(m.Metadata.Name, filter) {
			continue
		}

		info := skillInfo{
			Name:        m.Metadata.Name,
			Description: m.Metadata.Description,
		}
		if verbose {
			info.Parameters = m.Signature.Parameters
			info.InputSchema = buildSkillInputSchema(m.Signature.Parameters)
		}
		skills = append(skills, info)
	}

	// Sort by name
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})

	result := map[string]any{
		"count":  len(skills),
		"skills": skills,
	}
	if filter != "" {
		result["filter"] = filter
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return mcp.NewToolResultText(string(output)), nil
}

func handleContextShow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	top, err := store.LoadTopOfMind()
	if err != nil {
		return mcp.NewToolResultError("load top_of_mind: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"top_of_mind":    top,
	})), nil
}

func handleContextHandoffs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if path, _ := args["path"].(string); strings.TrimSpace(path) != "" {
		handoff, err := store.LoadHandoff(path)
		if err != nil {
			return mcp.NewToolResultError("load handoff: " + err.Error()), nil
		}
		return mcp.NewToolResultText(mustJSON(map[string]any{
			"workspace_path": target,
			"handoff":        handoff,
			"path":           path,
		})), nil
	}
	limit := 20
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	items, err := store.ListHandoffs(limit)
	if err != nil {
		return mcp.NewToolResultError("list handoffs: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"handoffs":       items,
		"count":          len(items),
	})), nil
}

func handleContextReport(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	report, err := store.BuildReport()
	if err != nil {
		return mcp.NewToolResultError("build report: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"report":         report,
	})), nil
}

func handleContextRetrieve(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	vaultPath := getStringArg(args, "vault_path", "")
	query := getStringArg(args, "query", "")
	if strings.TrimSpace(vaultPath) == "" || strings.TrimSpace(query) == "" {
		return mcp.NewToolResultError("vault_path and query are required"), nil
	}
	cfg, err := loadConfig(ctx)
	if err != nil {
		return mcp.NewToolResultError("load config: " + err.Error()), nil
	}
	index, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
	if err != nil {
		return mcp.NewToolResultError("open obsidian index: " + err.Error()), nil
	}
	defer func() { _ = index.Close() }()
	repo, err := repoindex.Open(ctx, cfg.Storage.Root, target)
	if err != nil {
		return mcp.NewToolResultError("open repo index: " + err.Error()), nil
	}
	defer func() { _ = repo.Close() }()
	limit := 5
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	result, err := store.Retrieve(ctx, index, repo, openObsidianSemanticProvider(cfg), query, limit)
	if err != nil {
		return mcp.NewToolResultError("retrieve context: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"vault_path":     vaultPath,
		"result":         result,
	})), nil
}

func handleContextNext(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	target := resolveContextWorkspace(getStringArg(args, "workspace", ""))
	if target == "" {
		return mcp.NewToolResultError("workspace path required"), nil
	}
	cfg, err := loadConfig(ctx)
	if err != nil {
		return mcp.NewToolResultError("load config: " + err.Error()), nil
	}
	taskDB, err := taskstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return mcp.NewToolResultError("open task store: " + err.Error()), nil
	}
	defer func() { _ = taskDB.Close() }()
	task, ok, err := contextplane.SelectNextTask(ctx, taskDB, ws.CanonicalID(target))
	if err != nil {
		return mcp.NewToolResultError("select next task: " + err.Error()), nil
	}
	if !ok {
		return mcp.NewToolResultError("no eligible task found"), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"task":           task,
	})), nil
}

func handleContextNextProposalMerge(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := 50
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	vaultPath := getStringArg(args, "vault_path", "")
	if strings.TrimSpace(vaultPath) != "" {
		cfg, err := loadConfig(ctx)
		if err != nil {
			return mcp.NewToolResultError("load config: " + err.Error()), nil
		}
		index, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
		if err != nil {
			return mcp.NewToolResultError("open obsidian index: " + err.Error()), nil
		}
		defer func() { _ = index.Close() }()
		health, err := index.Health(ctx)
		if err != nil {
			return mcp.NewToolResultError("health report: " + err.Error()), nil
		}
		if _, err := store.GenerateMaintenanceTasksWithHealth(ctx, limit, &health); err != nil {
			return mcp.NewToolResultError("generate maintenance tasks: " + err.Error()), nil
		}
	} else {
		if _, err := store.GenerateMaintenanceTasks(ctx, limit); err != nil {
			return mcp.NewToolResultError("generate maintenance tasks: " + err.Error()), nil
		}
	}
	claim := false
	if raw, ok := args["claim"].(bool); ok {
		claim = raw
	}
	var task *contextplane.MaintenanceTask
	if claim {
		task, err = store.ClaimNextProposalMergeTask(ctx, limit)
	} else {
		task, err = store.NextProposalMergeTask(ctx, limit)
	}
	if err != nil {
		return mcp.NewToolResultError("select next proposal merge: " + err.Error()), nil
	}
	found := task != nil
	var packet *contextplane.ProposalWorkPacket
	if task != nil {
		packet = task.WorkPacket
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"vault_path":     vaultPath,
		"found":          found,
		"task":           task,
		"work_packet":    packet,
	})), nil
}

func handleContextDispatch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	cfg, err := loadConfig(ctx)
	if err != nil {
		return mcp.NewToolResultError("load config: " + err.Error()), nil
	}
	taskDB, err := taskstore.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return mcp.NewToolResultError("open task store: " + err.Error()), nil
	}
	defer func() { _ = taskDB.Close() }()
	packet, err := store.BuildTaskPacket(ctx, taskDB, ws.CanonicalID(target), getStringArg(args, "task_id", ""))
	if err != nil {
		return mcp.NewToolResultError("build task packet: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"packet":         packet,
	})), nil
}

func handleContextContradictions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	vaultPath := getStringArg(args, "vault_path", "")
	if strings.TrimSpace(vaultPath) == "" {
		return mcp.NewToolResultError("vault_path is required"), nil
	}
	cfg, err := loadConfig(ctx)
	if err != nil {
		return mcp.NewToolResultError("load config: " + err.Error()), nil
	}
	index, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
	if err != nil {
		return mcp.NewToolResultError("open obsidian index: " + err.Error()), nil
	}
	defer func() { _ = index.Close() }()
	repo, err := repoindex.Open(ctx, cfg.Storage.Root, target)
	if err != nil {
		return mcp.NewToolResultError("open repo index: " + err.Error()), nil
	}
	defer func() { _ = repo.Close() }()
	limit := 10
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	findings, err := store.DetectContradictions(ctx, index, repo, openObsidianSemanticProvider(cfg), limit)
	if err != nil {
		return mcp.NewToolResultError("detect contradictions: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"vault_path":     vaultPath,
		"findings":       findings,
		"count":          len(findings),
	})), nil
}

func handleContextObservations(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := 20
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	items, err := store.ListObservations(limit)
	if err != nil {
		return mcp.NewToolResultError("list observations: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"observations":   items,
		"count":          len(items),
	})), nil
}

func handleContextTensions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := 20
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	items, err := store.ListTensions(limit)
	if err != nil {
		return mcp.NewToolResultError("list tensions: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"tensions":       items,
		"count":          len(items),
	})), nil
}

func handleContextProposals(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if id := getStringArg(args, "id", ""); strings.TrimSpace(id) != "" {
		item, err := store.GetMemoryProposal(ctx, id)
		if err != nil {
			return mcp.NewToolResultError("get proposal: " + err.Error()), nil
		}
		if item == nil {
			return mcp.NewToolResultError("no proposal found for " + strings.TrimSpace(id)), nil
		}
		return mcp.NewToolResultText(mustJSON(map[string]any{
			"workspace_path": target,
			"proposal":       item,
		})), nil
	}
	limit := 20
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	items, err := store.ListMemoryProposals(ctx, limit)
	if err != nil {
		return mcp.NewToolResultError("list proposals: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"proposals":      items,
		"count":          len(items),
	})), nil
}

func handleContextProposalApply(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id := getStringArg(args, "id", "")
	if strings.TrimSpace(id) == "" {
		return mcp.NewToolResultError("id is required"), nil
	}
	proposal, result, packet, err := store.ApplyMemoryProposal(ctx, id)
	if err != nil {
		return mcp.NewToolResultError("apply proposal: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"proposal":       proposal,
		"result":         result,
		"work_packet":    packet,
	})), nil
}

func handleContextProposalReject(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id := getStringArg(args, "id", "")
	if strings.TrimSpace(id) == "" {
		return mcp.NewToolResultError("id is required"), nil
	}
	proposal, err := store.RejectMemoryProposal(ctx, id)
	if err != nil {
		return mcp.NewToolResultError("reject proposal: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"proposal":       proposal,
	})), nil
}

func handleContextProposalReleaseMerge(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id := getStringArg(args, "id", "")
	if strings.TrimSpace(id) == "" {
		return mcp.NewToolResultError("id is required"), nil
	}
	proposal, err := store.ReleaseProposalMergeClaim(ctx, id)
	if err != nil {
		return mcp.NewToolResultError("release proposal merge: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"proposal":       proposal,
	})), nil
}

func handleContextProposalMerge(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id := getStringArg(args, "id", "")
	vaultPath := getStringArg(args, "vault_path", "")
	if strings.TrimSpace(id) == "" || strings.TrimSpace(vaultPath) == "" {
		return mcp.NewToolResultError("id and vault_path are required"), nil
	}
	proposal, merge, packet, err := store.MergeMemoryProposal(
		ctx,
		getStringArg(args, "vault_name", ""),
		vaultPath,
		id,
		getStringArg(args, "draft_path", ""),
		getStringArg(args, "target_path", ""),
		getStringArg(args, "heading", ""),
	)
	if err != nil {
		return mcp.NewToolResultError("merge proposal: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"vault_path":     vaultPath,
		"proposal":       proposal,
		"merge":          merge,
		"work_packet":    packet,
	})), nil
}

func handleContextRethink(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	limit := 20
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	vaultPath := getStringArg(args, "vault_path", "")
	var items []contextplane.MaintenanceTask
	if strings.TrimSpace(vaultPath) != "" {
		cfg, err := loadConfig(ctx)
		if err != nil {
			return mcp.NewToolResultError("load config: " + err.Error()), nil
		}
		index, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
		if err != nil {
			return mcp.NewToolResultError("open obsidian index: " + err.Error()), nil
		}
		defer func() { _ = index.Close() }()
		health, err := index.Health(ctx)
		if err != nil {
			return mcp.NewToolResultError("health report: " + err.Error()), nil
		}
		items, err = store.GenerateMaintenanceTasksWithHealth(ctx, limit, &health)
		if err != nil {
			return mcp.NewToolResultError("generate maintenance tasks: " + err.Error()), nil
		}
	} else {
		items, err = store.GenerateMaintenanceTasks(ctx, limit)
		if err != nil {
			return mcp.NewToolResultError("generate maintenance tasks: " + err.Error()), nil
		}
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path":    target,
		"vault_path":        vaultPath,
		"maintenance_tasks": items,
		"count":             len(items),
	})), nil
}

func handleContextPromote(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	sourceKind := getStringArg(args, "source", "handoff")
	noteType := getStringArg(args, "type", "investigation")
	title := getStringArg(args, "title", "")
	switch sourceKind {
	case "handoff":
		path := getStringArg(args, "path", "")
		result, err := store.DraftPromotionFromHandoff(path, noteType, title)
		if err != nil {
			return mcp.NewToolResultError("draft promotion: " + err.Error()), nil
		}
		return mcp.NewToolResultText(mustJSON(map[string]any{
			"workspace_path": target,
			"draft":          result,
		})), nil
	case "observation":
		id := getStringArg(args, "id", "")
		result, err := store.DraftPromotionFromObservation(id, noteType, title)
		if err != nil {
			return mcp.NewToolResultError("draft promotion: " + err.Error()), nil
		}
		return mcp.NewToolResultText(mustJSON(map[string]any{
			"workspace_path": target,
			"draft":          result,
		})), nil
	default:
		return mcp.NewToolResultError("unsupported source: " + sourceKind), nil
	}
}

func handleContextMergePromotion(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	store, target, err := openContextWorkspaceStore(ctx, args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	vaultPath := getStringArg(args, "vault_path", "")
	targetPath := getStringArg(args, "target_path", "")
	if strings.TrimSpace(vaultPath) == "" || strings.TrimSpace(targetPath) == "" {
		return mcp.NewToolResultError("vault_path and target_path are required"), nil
	}
	result, err := store.MergePromotionDraft(
		ctx,
		getStringArg(args, "vault_name", ""),
		vaultPath,
		getStringArg(args, "draft_path", ""),
		targetPath,
		getStringArg(args, "heading", ""),
	)
	if err != nil {
		return mcp.NewToolResultError("merge promotion: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"workspace_path": target,
		"vault_path":     vaultPath,
		"merge":          result,
	})), nil
}

func handleObsidianRead(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	res, err := obsidiantool.Read(ctx, obsidiantool.ReadOptions{
		VaultName: getStringArg(args, "vault_name", ""),
		VaultPath: getStringArg(args, "vault_path", ""),
		NotePath:  getStringArg(args, "path", ""),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{"result": res})), nil
}

func handleObsidianSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	limit := 20
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	res, err := obsidiantool.Search(ctx, obsidiantool.SearchOptions{
		VaultName: getStringArg(args, "vault_name", ""),
		VaultPath: getStringArg(args, "vault_path", ""),
		Query:     getStringArg(args, "query", ""),
		ScopePath: getStringArg(args, "scope_path", ""),
		Limit:     limit,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{"result": res})), nil
}

func handleObsidianRelated(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	limit := 20
	if raw, ok := args["limit"].(float64); ok && raw > 0 {
		limit = int(raw)
	}
	res, err := obsidiantool.RelatedNotes(
		getStringArg(args, "vault_path", ""),
		getStringArg(args, "path", ""),
		obsidiantool.LinkQueryOptions{Depth: 1, IncludeDirect: true, IncludeBack: true, IncludeAlias: true, Limit: limit},
	)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{"results": res})), nil
}

func handleObsidianCreateNote(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	writer := obsidiantool.NewWriter("", getStringArg(args, "vault_name", ""), obsidiantool.DefaultPolicy())
	writer.VaultPath = getStringArg(args, "vault_path", "")
	path := getStringArg(args, "path", "")
	body := buildVaultDraftContent(
		filepath.Base(path),
		getStringArg(args, "type", "investigation"),
		getStringArg(args, "project", ""),
		getStringArg(args, "status", "draft"),
		getStringArg(args, "trust", "raw"),
		getStringArg(args, "body", ""),
	)
	if err := writer.CreateNote(ctx, path, body, true); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{"path": path})), nil
}

func handleObsidianAppendUnderHeading(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	writer := obsidiantool.NewWriter("", getStringArg(args, "vault_name", ""), obsidiantool.DefaultPolicy())
	writer.VaultPath = getStringArg(args, "vault_path", "")
	path := getStringArg(args, "path", "")
	if err := writer.AppendUnderHeading(ctx, path, getStringArg(args, "heading", ""), getStringArg(args, "content", "")); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{"path": path})), nil
}

func handleObsidianCaptureSession(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	writer := obsidiantool.NewWriter("", getStringArg(args, "vault_name", ""), obsidiantool.DefaultPolicy())
	writer.VaultPath = getStringArg(args, "vault_path", "")
	path, err := writer.CaptureSession(ctx, getStringArg(args, "slug", ""), getStringArg(args, "content", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{"path": path})), nil
}

func handleObsidianPromoteToEvergreen(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	writer := obsidiantool.NewWriter("", getStringArg(args, "vault_name", ""), obsidiantool.DefaultPolicy())
	writer.VaultPath = getStringArg(args, "vault_path", "")
	path, err := writer.PromoteToEvergreen(ctx, getStringArg(args, "slug", ""), getStringArg(args, "content", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{"path": path})), nil
}

func handleObsidianMergeReviewedDraft(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	draftPath := getStringArg(args, "draft_path", "")
	body, err := os.ReadFile(draftPath)
	if err != nil {
		return mcp.NewToolResultError("read draft: " + err.Error()), nil
	}
	writer := obsidiantool.NewWriter("", getStringArg(args, "vault_name", ""), obsidiantool.DefaultPolicy())
	writer.VaultPath = getStringArg(args, "vault_path", "")
	result, err := writer.MergeReviewedDraftContent(
		ctx,
		getStringArg(args, "target_path", ""),
		getStringArg(args, "heading", ""),
		string(body),
		filepath.Base(draftPath),
	)
	if err != nil {
		return mcp.NewToolResultError("merge reviewed draft: " + err.Error()), nil
	}
	return mcp.NewToolResultText(mustJSON(map[string]any{
		"draft_path":  draftPath,
		"target_path": getStringArg(args, "target_path", ""),
		"merge":       result,
	})), nil
}

func openContextWorkspaceStore(ctx context.Context, args map[string]any) (*contextplane.WorkspaceStore, string, error) {
	target, _ := args["workspace"].(string)
	runCtx := resolveWorkspaceContext(ctx, target)
	target, ok := ws.FromContext(runCtx)
	if !ok || strings.TrimSpace(target) == "" {
		target = ws.Detect("")
	}
	target = ws.Normalize(target)
	if target == "" {
		return nil, "", fmt.Errorf("detect workspace")
	}
	return contextplane.NewWorkspaceStore(target), target, nil
}

func mustJSON(v any) string {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(body)
}

func buildSkillInputSchema(params []skill.Parameter) map[string]any {
	properties := make(map[string]any, len(params))
	var required []string
	for _, param := range params {
		properties[param.Name] = buildSkillParamSchema(param)
		if param.Required {
			required = append(required, param.Name)
		}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func buildSkillParamSchema(param skill.Parameter) map[string]any {
	schema := map[string]any{
		"type": normalizeSchemaType(param.Type),
	}
	if param.Description != "" {
		schema["description"] = param.Description
	}
	if param.Default != nil {
		schema["default"] = param.Default
	}
	if len(param.Enum) > 0 {
		schema["enum"] = param.Enum
	}

	switch schema["type"] {
	case "array":
		if param.Items != nil {
			schema["items"] = buildSkillParamSchema(*param.Items)
		} else {
			schema["items"] = map[string]any{"type": "string"}
		}
	case "object":
		if len(param.Properties) > 0 {
			props := make(map[string]any, len(param.Properties))
			var required []string
			for name, prop := range param.Properties {
				props[name] = buildSkillParamSchema(prop)
				if prop.Required {
					required = append(required, name)
				}
			}
			schema["properties"] = props
			if len(required) > 0 {
				schema["required"] = required
			}
		}
	}

	return schema
}

func normalizeSchemaType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "string":
		return "string"
	case "number", "float":
		return "number"
	case "integer":
		return "integer"
	case "boolean", "bool":
		return "boolean"
	case "array":
		return "array"
	case "object":
		return "object"
	default:
		return "string"
	}
}

// Local tool handlers

func handleRepoIndexBuild(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	workspace := getStringArg(args, "workspace", ".")
	patterns := getStringSliceArg(args, "go_pattern")
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	includeGo := getBoolArg(args, "include_go", true)
	includePython := getBoolArg(args, "include_python", false)
	includeRust := getBoolArg(args, "include_rust", false)
	includeTS := getBoolArg(args, "include_typescript", true)
	includeElixir := getBoolArg(args, "include_elixir", false)
	includeTerraform := getBoolArg(args, "include_terraform", false)
	includeKubernetes := getBoolArg(args, "include_kubernetes", false)
	includeShell := getBoolArg(args, "include_shell", false)
	includeTests := getBoolArg(args, "include_tests", false)
	dryRun := getBoolArg(args, "dry_run", false)

	return runRepoIndexCommand(ctx, func(cmd *cobra.Command) error {
		return runIndexRepoBuild(cmd, workspace, patterns, includeGo, includePython, includeRust, includeTS, includeElixir, includeTerraform, includeKubernetes, includeShell, includeTests, dryRun)
	})
}

func handleRepoIndexStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	workspace := getStringArg(args, "workspace", ".")
	return runRepoIndexCommand(ctx, func(cmd *cobra.Command) error {
		return runIndexRepoStatus(cmd, workspace)
	})
}

func handleRepoIndexSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	workspace := getStringArg(args, "workspace", ".")
	query := getStringArg(args, "query", "")
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}
	limit := getIntArg(args, "limit", 20)
	input := map[string]any{
		"workspace": workspace,
		"query":     query,
		"limit":     limit,
	}
	if inlineMode := getStringArg(args, "inline_mode", ""); inlineMode != "" {
		input["inline_mode"] = inlineMode
	}
	return runSkillAsMCP(ctx, "repo/index_search", input)
}

func handleRepoIndexExpand(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	workspace := getStringArg(args, "workspace", ".")
	seeds := getStringSliceArg(args, "seed")
	if len(seeds) == 0 {
		return mcp.NewToolResultError("seed is required"), nil
	}
	edgeTypes := getStringSliceArg(args, "edge")
	depth := getIntArg(args, "depth", 1)
	budget := getIntArg(args, "budget", 50)
	perNodeCap := getIntArg(args, "per_node", 50)
	direction := getStringArg(args, "direction", "out")
	input := map[string]any{
		"workspace":    workspace,
		"seeds":        seeds,
		"edge_types":   edgeTypes,
		"depth":        depth,
		"budget":       budget,
		"per_node_cap": perNodeCap,
		"direction":    direction,
	}
	if inlineMode := getStringArg(args, "inline_mode", ""); inlineMode != "" {
		input["inline_mode"] = inlineMode
	}
	return runSkillAsMCP(ctx, "repo/index_expand", input)
}

func handleRepoIndexDAGGrep(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	query := getStringArg(args, "query", "")
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}
	input := map[string]any{
		"query":     query,
		"workspace": getStringArg(args, "workspace", "."),
	}
	for _, key := range []string{"mode", "direction", "render", "inline_mode"} {
		if value := getStringArg(args, key, ""); value != "" {
			input[key] = value
		}
	}
	for _, key := range []string{"k", "depth", "budget", "per_node_cap"} {
		if value := getIntArg(args, key, 0); value > 0 {
			input[key] = value
		}
	}
	if values := getStringSliceArg(args, "node_kinds"); len(values) > 0 {
		input["node_kinds"] = values
	}
	if values := getStringSliceArg(args, "edge_sets"); len(values) > 0 {
		input["edge_sets"] = values
	}
	if values := getStringSliceArg(args, "edge_types"); len(values) > 0 {
		input["edge_types"] = values
	}
	if includeAnchors, ok := args["include_anchors"].(bool); ok {
		input["include_anchors"] = includeAnchors
	}
	return runSkillAsMCP(ctx, "code/dag_grep", input)
}

func handleStructuredShell(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	command := getStringArg(args, "command", "")
	argv := getStringSliceArg(args, "argv")
	if command == "" && len(argv) == 0 {
		return mcp.NewToolResultError("command or argv is required"), nil
	}
	workspace := getStringArg(args, "workspace", ".")
	measureRaw := getBoolArg(args, "measure_raw", false)
	tokenModel := getStringArg(args, "token_model", "cl100k_base")
	return runEnvelopeCommand(ctx, "structured_shell", func(cmd *cobra.Command) error {
		return runShellCommand(cmd, workspace, command, measureRaw, tokenModel, argv)
	})
}

func handleMobileExpo(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	operation := getStringArg(args, "operation", "")
	if operation == "" {
		return mcp.NewToolResultError("operation is required"), nil
	}
	input := map[string]any{
		"operation": operation,
	}
	for _, key := range []string{"device_id", "platform", "url", "build_platform", "profile", "channel", "message", "filter"} {
		if value := getStringArg(args, key, ""); value != "" {
			input[key] = value
		}
	}
	if value := getIntArg(args, "count", 0); value > 0 {
		input["count"] = value
	}
	return runSkillAsMCP(ctx, "mobile/expo", input)
}

func handleRoomTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	action := getStringArg(args, "action", "")
	workspace := getStringArg(args, "workspace", "")
	roomID := getStringArg(args, "room_id", "")
	argv := make([]string, 0, 32)

	switch action {
	case "create":
		if roomID == "" {
			return mcp.NewToolResultError("room_id is required for create"), nil
		}
		argv = append(argv, "create", roomID)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--title", getStringArg(args, "title", ""))
		argv = appendStringFlagArgs(argv, "--description", getStringArg(args, "description", ""))
		argv = appendStringSliceFlagArgs(argv, "--member", getStringSliceArg(args, "members"))
	case "list":
		argv = append(argv, "list")
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "show":
		if roomID == "" {
			return mcp.NewToolResultError("room_id is required for show"), nil
		}
		argv = append(argv, "show", roomID)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "status":
		if roomID == "" {
			return mcp.NewToolResultError("room_id is required for status"), nil
		}
		argv = append(argv, "status", roomID)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
		argv = appendStringSliceFlagArgs(argv, "--only", getStringSliceArg(args, "only"))
	case "inbox":
		if roomID == "" {
			return mcp.NewToolResultError("room_id is required for inbox"), nil
		}
		argv = append(argv, "inbox", roomID)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
		argv = appendStringFlagArgs(argv, "--filter", getStringArg(args, "filter", ""))
		argv = appendBoolFlagArgs(argv, "--grouped", getBoolArg(args, "grouped", false))
		argv = appendBoolFlagArgs(argv, "--ids-only", getBoolArg(args, "ids_only", false))
		argv = appendBoolFlagArgs(argv, "--include-broadcasts", getBoolArg(args, "include_broadcasts", false))
	case "send":
		text := getStringArg(args, "text", "")
		if roomID == "" || text == "" {
			return mcp.NewToolResultError("room_id and text are required for send"), nil
		}
		argv = append(argv, "send", roomID, text)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "actor", ""))
		argv = appendStringFlagArgs(argv, "--to", getStringArg(args, "recipient", ""))
		argv = appendStringFlagArgs(argv, "--subject", getStringArg(args, "subject", ""))
		argv = appendStringFlagArgs(argv, "--hint", getStringArg(args, "hint", ""))
		argv = appendStringFlagArgs(argv, "--kind", getStringArg(args, "kind", ""))
		argv = appendStringFlagArgs(argv, "--task-id", getStringArg(args, "task_id", ""))
		argv = appendIntFlagArgs(argv, "--priority", getIntArg(args, "priority", 0))
		argv = appendBoolFlagArgs(argv, "--ack-required", getBoolArg(args, "ack_required", false))
		argv = appendBoolFlagArgs(argv, "--reply-expected", getBoolArg(args, "reply_expected", false))
		argv = appendBoolFlagArgs(argv, "--interrupt", getBoolArg(args, "interrupt", false))
		if _, ok := args["auto_create"]; ok {
			if getBoolArg(args, "auto_create", false) {
				argv = append(argv, "--auto-create")
			} else {
				argv = append(argv, "--auto-create=false")
			}
		}
	case "ack":
		messageIDs := getStringSliceArg(args, "message_ids")
		if roomID == "" || len(messageIDs) == 0 {
			return mcp.NewToolResultError("room_id and message_ids are required for ack"), nil
		}
		argv = append(argv, "ack", roomID)
		argv = append(argv, messageIDs...)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
	case "resolve":
		if roomID == "" {
			return mcp.NewToolResultError("room_id is required for resolve"), nil
		}
		argv = append(argv, "resolve", roomID)
		argv = append(argv, getStringSliceArg(args, "message_ids")...)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
		argv = appendStringFlagArgs(argv, "--mode", getStringArg(args, "mode", ""))
		argv = appendBoolFlagArgs(argv, "--all", getBoolArg(args, "all", false))
		argv = appendStringSliceFlagArgs(argv, "--only", getStringSliceArg(args, "only"))
	case "clear":
		if roomID == "" {
			return mcp.NewToolResultError("room_id is required for clear"), nil
		}
		argv = append(argv, "clear", roomID)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
		argv = appendStringFlagArgs(argv, "--mode", getStringArg(args, "mode", ""))
		argv = appendStringFlagArgs(argv, "--preset", getStringArg(args, "preset", ""))
	case "join":
		if roomID == "" {
			return mcp.NewToolResultError("room_id is required for join"), nil
		}
		argv = append(argv, "join", roomID)
		if actor := getStringArg(args, "actor", ""); actor != "" {
			argv = append(argv, actor)
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--role", getStringArg(args, "role", ""))
		argv = appendStringFlagArgs(argv, "--backend", getStringArg(args, "backend", ""))
		argv = appendStringFlagArgs(argv, "--session", getStringArg(args, "session", ""))
		argv = appendStringFlagArgs(argv, "--pane-id", getStringArg(args, "pane_id", ""))
		argv = appendBoolFlagArgs(argv, "--unbound", getBoolArg(args, "unbound", false))
		if _, ok := args["create"]; ok {
			if getBoolArg(args, "create", false) {
				argv = append(argv, "--create")
			} else {
				argv = append(argv, "--create=false")
			}
		}
		argv = appendBoolFlagArgs(argv, "--current", getBoolArg(args, "current", false))
	case "leave":
		participantID := getStringArg(args, "participant_id", "")
		if roomID == "" || participantID == "" {
			return mcp.NewToolResultError("room_id and participant_id are required for leave"), nil
		}
		argv = append(argv, "leave", roomID, participantID)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
	case "subscribe":
		if roomID == "" {
			return mcp.NewToolResultError("room_id is required for subscribe"), nil
		}
		argv = append(argv, "subscribe", roomID)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
		argv = appendBoolFlagArgs(argv, "--follow", getBoolArg(args, "follow", false))
		argv = appendDurationFlagArgs(argv, "--poll", getStringArg(args, "poll", ""))
		argv = appendIntFlagArgs(argv, "--history", getIntArg(args, "history", 0))
	case "relay":
		if roomID == "" {
			return mcp.NewToolResultError("room_id is required for relay"), nil
		}
		argv = append(argv, "relay", roomID)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--backend", getStringArg(args, "backend", ""))
		argv = appendStringFlagArgs(argv, "--session", getStringArg(args, "session", ""))
		argv = appendStringFlagArgs(argv, "--plugin-path", getStringArg(args, "plugin_path", ""))
		argv = appendDurationFlagArgs(argv, "--poll", getStringArg(args, "poll", ""))
		argv = appendIntFlagArgs(argv, "--history", getIntArg(args, "history", 0))
	case "coordinator_set":
		participantID := getStringArg(args, "participant_id", "")
		if roomID == "" || participantID == "" {
			return mcp.NewToolResultError("room_id and participant_id are required for coordinator_set"), nil
		}
		argv = append(argv, "coordinator", "set", roomID, participantID)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
	default:
		return mcp.NewToolResultError("unsupported room action: " + action), nil
	}
	return runCLICommandAsMCP(ctx, "room", newRoomCommand, argv)
}

func handleRoomPulseTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	roomID := getStringArg(args, "room_id", "")
	if roomID == "" {
		return mcp.NewToolResultError("room_id is required"), nil
	}
	workspace := getStringArg(args, "workspace", "")
	argv := []string{"pulse", roomID}
	argv = appendStringFlagArgs(argv, "--workspace", workspace)
	argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
	argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	argv = appendStringSliceFlagArgs(argv, "--only", getStringSliceArg(args, "only"))
	return runCLICommandAsMCP(ctx, "room_pulse", newRoomCommand, argv)
}

func handleRoomTaskTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	action := getStringArg(args, "action", "")
	workspace := getStringArg(args, "workspace", "")
	roomID := getStringArg(args, "room_id", "")
	if roomID == "" {
		return mcp.NewToolResultError("room_id is required"), nil
	}
	argv := []string{"task", action, roomID}
	switch action {
	case "add":
		title := getStringArg(args, "title", "")
		if title == "" {
			return mcp.NewToolResultError("title is required for room_task add"), nil
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--title", title)
		argv = appendStringFlagArgs(argv, "--description", getStringArg(args, "description", ""))
		argv = appendStringFlagArgs(argv, "--scope", getStringArg(args, "scope", ""))
		argv = appendStringFlagArgs(argv, "--parent", getStringArg(args, "parent", ""))
		argv = appendStringSliceFlagArgs(argv, "--depends-on", getStringSliceArg(args, "depends_on"))
		if _, ok := args["create_room"]; ok {
			if getBoolArg(args, "create_room", false) {
				argv = append(argv, "--create-room")
			} else {
				argv = append(argv, "--create-room=false")
			}
		}
	case "list":
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--status", getStringArg(args, "status", ""))
	case "assign", "reassign":
		taskID := getStringArg(args, "task_id", "")
		recipient := getStringArg(args, "recipient", "")
		if taskID == "" || recipient == "" {
			return mcp.NewToolResultError("task_id and recipient are required"), nil
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--id", taskID)
		argv = appendStringFlagArgs(argv, "--to", recipient)
		if action == "assign" {
			argv = appendStringFlagArgs(argv, "--notes", getStringArg(args, "notes", ""))
		} else {
			argv = appendStringFlagArgs(argv, "--reason", getStringArg(args, "reason", ""))
		}
	case "claim", "touch", "unblock":
		taskID := getStringArg(args, "task_id", "")
		if taskID == "" {
			return mcp.NewToolResultError("task_id is required"), nil
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--id", taskID)
	case "block", "reclaim", "abandon":
		taskID := getStringArg(args, "task_id", "")
		reason := getStringArg(args, "reason", "")
		if taskID == "" {
			return mcp.NewToolResultError("task_id is required"), nil
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--id", taskID)
		argv = appendStringFlagArgs(argv, "--reason", reason)
	case "complete":
		taskID := getStringArg(args, "task_id", "")
		if taskID == "" {
			return mcp.NewToolResultError("task_id is required"), nil
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--id", taskID)
		argv = appendStringFlagArgs(argv, "--notes", getStringArg(args, "notes", ""))
		argv = appendStringFlagArgs(argv, "--gotchas", getStringArg(args, "gotchas", ""))
	default:
		return mcp.NewToolResultError("unsupported room_task action: " + action), nil
	}
	return runCLICommandAsMCP(ctx, "room_task", newRoomCommand, argv)
}

func handleRoomInterviewTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	action := getStringArg(args, "action", "")
	workspace := getStringArg(args, "workspace", "")
	roomID := getStringArg(args, "room_id", "")
	if roomID == "" {
		return mcp.NewToolResultError("room_id is required"), nil
	}
	argv := []string{"interview", action, roomID}
	switch action {
	case "start":
		topic := getStringArg(args, "topic", "")
		if topic == "" {
			return mcp.NewToolResultError("topic is required for room_interview start"), nil
		}
		argv = append(argv, topic)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--spec", getStringArg(args, "spec", ""))
		argv = appendStringFlagArgs(argv, "--spec-ref", getStringArg(args, "spec_ref", ""))
		argv = appendStringFlagArgs(argv, "--submitter", getStringArg(args, "submitter", ""))
		argv = appendStringFlagArgs(argv, "--questioner", getStringArg(args, "questioner", ""))
		argv = appendStringFlagArgs(argv, "--respondent", getStringArg(args, "respondent", ""))
		argv = appendStringFlagArgs(argv, "--verifier", getStringArg(args, "verifier", ""))
		argv = appendStringSliceFlagArgs(argv, "--constraint", getStringSliceArg(args, "constraint"))
	case "ask":
		sessionID := getStringArg(args, "session_id", "")
		question := getStringArg(args, "question", "")
		if sessionID == "" || question == "" {
			return mcp.NewToolResultError("session_id and question are required for room_interview ask"), nil
		}
		argv = append(argv, sessionID, question)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--to", getStringArg(args, "to", ""))
	case "answer":
		questionID := getStringArg(args, "question_id", "")
		answer := getStringArg(args, "answer", "")
		if questionID == "" || answer == "" {
			return mcp.NewToolResultError("question_id and answer are required for room_interview answer"), nil
		}
		argv = append(argv, questionID, answer)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
	case "verify":
		answerID := getStringArg(args, "answer_id", "")
		verdict := getStringArg(args, "verdict", "")
		notes := getStringArg(args, "notes", "")
		if answerID == "" || verdict == "" || notes == "" {
			return mcp.NewToolResultError("answer_id, verdict, and notes are required for room_interview verify"), nil
		}
		argv = append(argv, answerID, verdict, notes)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
	case "next":
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "show":
		if sessionID := getStringArg(args, "session_id", ""); sessionID != "" {
			argv = append(argv, sessionID)
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	default:
		return mcp.NewToolResultError("unsupported room_interview action: " + action), nil
	}
	return runCLICommandAsMCP(ctx, "room_interview", newRoomCommand, argv)
}

func handleRoomAgileTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	action := getStringArg(args, "action", "")
	roomID := getStringArg(args, "room_id", "")
	if roomID == "" {
		return mcp.NewToolResultError("room_id is required"), nil
	}
	workspace := getStringArg(args, "workspace", "")
	var argv []string
	switch action {
	case "epic_start":
		title := getStringArg(args, "title", "")
		if title == "" {
			return mcp.NewToolResultError("title is required for room_agile epic_start"), nil
		}
		argv = []string{"epic", "start", roomID, title}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--goal", getStringArg(args, "goal", ""))
		argv = appendStringFlagArgs(argv, "--owner", getStringArg(args, "owner", ""))
		argv = appendStringFlagArgs(argv, "--outcome", getStringArg(args, "outcome", ""))
		argv = appendStringFlagArgs(argv, "--horizon", getStringArg(args, "horizon", ""))
		argv = appendStringSliceFlagArgs(argv, "--scope", getStringSliceArg(args, "scope"))
		argv = appendStringSliceFlagArgs(argv, "--success", getStringSliceArg(args, "success"))
	case "epic_ask":
		epicID := getStringArg(args, "epic_id", "")
		question := getStringArg(args, "question", "")
		if epicID == "" || question == "" {
			return mcp.NewToolResultError("epic_id and question are required for room_agile epic_ask"), nil
		}
		argv = []string{"epic", "ask", roomID, epicID, question}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--to", getStringArg(args, "to", ""))
		argv = appendStringFlagArgs(argv, "--kind", getStringArg(args, "question_kind", ""))
	case "epic_answer":
		questionID := getStringArg(args, "question_id", "")
		answer := getStringArg(args, "answer", "")
		if questionID == "" || answer == "" {
			return mcp.NewToolResultError("question_id and answer are required for room_agile epic_answer"), nil
		}
		argv = []string{"epic", "answer", roomID, questionID, answer}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
	case "epic_finalize":
		epicID := getStringArg(args, "epic_id", "")
		notes := getStringArg(args, "notes", "")
		if epicID == "" || notes == "" {
			return mcp.NewToolResultError("epic_id and notes are required for room_agile epic_finalize"), nil
		}
		argv = []string{"epic", "finalize", roomID, epicID, notes}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
	case "epic_close":
		epicID := getStringArg(args, "epic_id", "")
		reason := getStringArg(args, "close_reason", "")
		notes := getStringArg(args, "notes", "")
		if epicID == "" || reason == "" || notes == "" {
			return mcp.NewToolResultError("epic_id, close_reason, and notes are required for room_agile epic_close"), nil
		}
		argv = []string{"epic", "close", roomID, epicID, reason, notes}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
	case "epic_shape":
		epicID := getStringArg(args, "epic_id", "")
		if epicID == "" {
			return mcp.NewToolResultError("epic_id is required for room_agile epic_shape"), nil
		}
		argv = []string{"epic", "shape", roomID, epicID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendIntFlagArgs(argv, "--count", getIntArg(args, "count", 0))
	case "epic_show":
		argv = []string{"epic", "show", roomID}
		if epicID := getStringArg(args, "epic_id", ""); epicID != "" {
			argv = append(argv, epicID)
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "epic_checkpoint":
		epicID := getStringArg(args, "epic_id", "")
		if epicID == "" {
			return mcp.NewToolResultError("epic_id is required for room_agile epic_checkpoint"), nil
		}
		argv = []string{"epic", "checkpoint", roomID, epicID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
		argv = appendStringFlagArgs(argv, "--label", getStringArg(args, "label", ""))
		argv = appendStringFlagArgs(argv, "--note", getStringArg(args, "note", ""))
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "epic_resume":
		epicID := getStringArg(args, "epic_id", "")
		if epicID == "" {
			return mcp.NewToolResultError("epic_id is required for room_agile epic_resume"), nil
		}
		argv = []string{"epic", "resume", roomID, epicID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
	case "epic_health":
		epicID := getStringArg(args, "epic_id", "")
		if epicID == "" {
			return mcp.NewToolResultError("epic_id is required for room_agile epic_health"), nil
		}
		argv = []string{"epic", "health", roomID, epicID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "epic_next":
		epicID := getStringArg(args, "epic_id", "")
		if epicID == "" {
			return mcp.NewToolResultError("epic_id is required for room_agile epic_next"), nil
		}
		argv = []string{"epic", "next", roomID, epicID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
	case "milestone_start":
		epicID := getStringArg(args, "epic_id", "")
		title := getStringArg(args, "title", "")
		proposalID := getStringArg(args, "proposal_id", "")
		if epicID == "" || (title == "" && proposalID == "") {
			return mcp.NewToolResultError("epic_id and either title or proposal_id are required for room_agile milestone_start"), nil
		}
		if getBoolArg(args, "enforce_exit_policy", false) && getBoolArg(args, "no_enforce_exit_policy", false) {
			return mcp.NewToolResultError("enforce_exit_policy and no_enforce_exit_policy cannot both be true for room_agile milestone_start"), nil
		}
		argv = []string{"milestone", "start", roomID, epicID}
		if title != "" {
			argv = append(argv, title)
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--goal", getStringArg(args, "goal", ""))
		argv = appendStringFlagArgs(argv, "--objective", getStringArg(args, "objective", ""))
		argv = appendStringFlagArgs(argv, "--owner", getStringArg(args, "owner", ""))
		argv = appendStringSliceFlagArgs(argv, "--scope", getStringSliceArg(args, "scope"))
		argv = appendStringSliceFlagArgs(argv, "--risk", getStringSliceArg(args, "risk"))
		argv = appendStringSliceFlagArgs(argv, "--exclude", getStringSliceArg(args, "exclude"))
		argv = appendStringSliceFlagArgs(argv, "--dependency", getStringSliceArg(args, "dependency"))
		argv = appendStringSliceFlagArgs(argv, "--validator", getStringSliceArg(args, "validator"))
		argv = appendStringSliceFlagArgs(argv, "--required-lane", getStringSliceArg(args, "required_lane"))
		argv = appendStringSliceFlagArgs(argv, "--optional-lane", getStringSliceArg(args, "optional_lane"))
		argv = appendBoolFlagArgs(argv, "--enforce-exit-policy", getBoolArg(args, "enforce_exit_policy", false))
		argv = appendBoolFlagArgs(argv, "--no-enforce-exit-policy", getBoolArg(args, "no_enforce_exit_policy", false))
		argv = appendStringSliceFlagArgs(argv, "--exit", getStringSliceArg(args, "exit"))
		argv = appendStringFlagArgs(argv, "--proposal", proposalID)
	case "milestone_contract":
		milestoneID := getStringArg(args, "milestone_id", "")
		if milestoneID == "" {
			return mcp.NewToolResultError("milestone_id is required for room_agile milestone_contract"), nil
		}
		if getBoolArg(args, "enforce_exit_policy", false) && getBoolArg(args, "no_enforce_exit_policy", false) {
			return mcp.NewToolResultError("enforce_exit_policy and no_enforce_exit_policy cannot both be true for room_agile milestone_contract"), nil
		}
		argv = []string{"milestone", "contract", roomID, milestoneID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--objective", getStringArg(args, "objective", ""))
		argv = appendStringSliceFlagArgs(argv, "--risk", getStringSliceArg(args, "risk"))
		argv = appendStringSliceFlagArgs(argv, "--exclude", getStringSliceArg(args, "exclude"))
		argv = appendStringSliceFlagArgs(argv, "--dependency", getStringSliceArg(args, "dependency"))
		argv = appendStringSliceFlagArgs(argv, "--validator", getStringSliceArg(args, "validator"))
		argv = appendStringSliceFlagArgs(argv, "--required-lane", getStringSliceArg(args, "required_lane"))
		argv = appendStringSliceFlagArgs(argv, "--optional-lane", getStringSliceArg(args, "optional_lane"))
		argv = appendBoolFlagArgs(argv, "--enforce-exit-policy", getBoolArg(args, "enforce_exit_policy", false))
		argv = appendBoolFlagArgs(argv, "--no-enforce-exit-policy", getBoolArg(args, "no_enforce_exit_policy", false))
		argv = appendStringSliceFlagArgs(argv, "--exit", getStringSliceArg(args, "exit"))
	case "milestone_criteria":
		milestoneID := getStringArg(args, "milestone_id", "")
		criterion := getStringArg(args, "criterion", "")
		if milestoneID == "" || criterion == "" {
			return mcp.NewToolResultError("milestone_id and criterion are required for room_agile milestone_criteria"), nil
		}
		argv = []string{"milestone", "criteria", roomID, milestoneID, criterion}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
	case "milestone_review":
		milestoneID := getStringArg(args, "milestone_id", "")
		verdict := getStringArg(args, "verdict", "")
		notes := getStringArg(args, "notes", "")
		if milestoneID == "" || verdict == "" || notes == "" {
			return mcp.NewToolResultError("milestone_id, verdict, and notes are required for room_agile milestone_review"), nil
		}
		argv = []string{"milestone", "review", roomID, milestoneID, verdict, notes}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
	case "milestone_summary":
		milestoneID := getStringArg(args, "milestone_id", "")
		notes := getStringArg(args, "notes", "")
		summaryText := getStringArg(args, "summary", "")
		if milestoneID == "" || (notes == "" && summaryText == "" && len(getStringSliceArg(args, "passed_criterion")) == 0 && len(getStringSliceArg(args, "failed_criterion")) == 0 && len(getStringSliceArg(args, "waived_validation")) == 0 && len(getStringSliceArg(args, "blocking_validation")) == 0 && len(getStringSliceArg(args, "decision")) == 0 && len(getStringSliceArg(args, "finding")) == 0 && len(getStringSliceArg(args, "next")) == 0 && len(getStringSliceArg(args, "guidance")) == 0) {
			return mcp.NewToolResultError("milestone_id and either notes, summary, or structured synthesis fields are required for room_agile milestone_summary"), nil
		}
		argv = []string{"milestone", "summary", roomID, milestoneID}
		if notes != "" {
			argv = append(argv, notes)
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--summary", summaryText)
		argv = appendStringSliceFlagArgs(argv, "--passed-criterion", getStringSliceArg(args, "passed_criterion"))
		argv = appendStringSliceFlagArgs(argv, "--failed-criterion", getStringSliceArg(args, "failed_criterion"))
		argv = appendStringSliceFlagArgs(argv, "--waived-validation", getStringSliceArg(args, "waived_validation"))
		argv = appendStringSliceFlagArgs(argv, "--blocking-validation", getStringSliceArg(args, "blocking_validation"))
		argv = appendStringSliceFlagArgs(argv, "--decision", getStringSliceArg(args, "decision"))
		argv = appendStringSliceFlagArgs(argv, "--finding", getStringSliceArg(args, "finding"))
		argv = appendStringSliceFlagArgs(argv, "--next", getStringSliceArg(args, "next"))
		argv = appendStringSliceFlagArgs(argv, "--guidance", getStringSliceArg(args, "guidance"))
	case "milestone_show":
		argv = []string{"milestone", "show", roomID}
		if milestoneID := getStringArg(args, "milestone_id", ""); milestoneID != "" {
			argv = append(argv, milestoneID)
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "story_propose":
		milestoneID := getStringArg(args, "milestone_id", "")
		title := getStringArg(args, "title", "")
		notes := getStringArg(args, "notes", "")
		if milestoneID == "" || title == "" || notes == "" {
			return mcp.NewToolResultError("milestone_id, title, and notes are required for room_agile story_propose"), nil
		}
		argv = []string{"story", "propose", roomID, milestoneID, title, notes}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--owner", getStringArg(args, "owner", ""))
		argv = appendStringFlagArgs(argv, "--rationale", getStringArg(args, "rationale", ""))
	case "story_accept":
		milestoneID := getStringArg(args, "milestone_id", "")
		proposalID := getStringArg(args, "story_proposal_id", "")
		if milestoneID == "" || proposalID == "" {
			return mcp.NewToolResultError("milestone_id and story_proposal_id are required for room_agile story_accept"), nil
		}
		argv = []string{"story", "accept", roomID, milestoneID, proposalID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--owner", getStringArg(args, "owner", ""))
	case "story_add":
		milestoneID := getStringArg(args, "milestone_id", "")
		title := getStringArg(args, "title", "")
		notes := getStringArg(args, "notes", "")
		if milestoneID == "" || title == "" || notes == "" {
			return mcp.NewToolResultError("milestone_id, title, and notes are required for room_agile story_add"), nil
		}
		argv = []string{"story", "add", roomID, milestoneID, title, notes}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--owner", getStringArg(args, "owner", ""))
	case "story_state":
		storyID := getStringArg(args, "story_id", "")
		state := getStringArg(args, "state", "")
		if storyID == "" || state == "" {
			return mcp.NewToolResultError("story_id and state are required for room_agile story_state"), nil
		}
		argv = []string{"story", "state", roomID, storyID, state}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--reason", getStringArg(args, "reason", ""))
		argv = appendStringFlagArgs(argv, "--blocked-by", getStringArg(args, "blocked_by", ""))
		argv = appendStringFlagArgs(argv, "--reviewer", getStringArg(args, "reviewer", ""))
	case "story_validate":
		storyID := getStringArg(args, "story_id", "")
		validatorType := getStringArg(args, "validator_type", "")
		validationStatus := getStringArg(args, "validation_status", "")
		notes := getStringArg(args, "notes", "")
		if storyID == "" || validatorType == "" || validationStatus == "" || notes == "" {
			return mcp.NewToolResultError("story_id, validator_type, validation_status, and notes are required for room_agile story_validate"), nil
		}
		argv = []string{"story", "validate", roomID, storyID, validatorType, validationStatus, notes}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--artifact-path", getStringArg(args, "artifact_path", ""))
		argv = appendStringFlagArgs(argv, "--artifact-digest", getStringArg(args, "artifact_digest", ""))
		argv = appendStringFlagArgs(argv, "--command", getStringArg(args, "command", ""))
		argv = appendStringFlagArgs(argv, "--notes", getStringArg(args, "validation_notes", ""))
		argv = appendStringSliceFlagArgs(argv, "--related-story", getStringSliceArg(args, "related_story_ids"))
	case "story_show":
		argv = []string{"story", "show", roomID}
		if storyID := getStringArg(args, "story_id", ""); storyID != "" {
			argv = append(argv, storyID)
		}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "log_append":
		epicID := getStringArg(args, "epic_id", "")
		title := getStringArg(args, "title", "")
		if epicID == "" || title == "" {
			return mcp.NewToolResultError("epic_id and title are required for room_agile log_append"), nil
		}
		argv = []string{"log", "append", roomID, epicID, title}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringSliceFlagArgs(argv, "--completed", getStringSliceArg(args, "completed"))
		argv = appendStringSliceFlagArgs(argv, "--in-flight", getStringSliceArg(args, "in_flight"))
		argv = appendStringSliceFlagArgs(argv, "--blocker", getStringSliceArg(args, "blocker"))
		argv = appendStringSliceFlagArgs(argv, "--next", getStringSliceArg(args, "next"))
		argv = appendStringFlagArgs(argv, "--notes", getStringArg(args, "notes", ""))
	case "log_show":
		epicID := getStringArg(args, "epic_id", "")
		if epicID == "" {
			return mcp.NewToolResultError("epic_id is required for room_agile log_show"), nil
		}
		argv = []string{"log", "show", roomID, epicID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "retro_add":
		epicID := getStringArg(args, "epic_id", "")
		kind := getStringArg(args, "kind", "")
		summaryText := getStringArg(args, "summary", "")
		impact := getStringArg(args, "impact", "")
		change := getStringArg(args, "change", "")
		if epicID == "" || kind == "" || summaryText == "" || impact == "" || change == "" {
			return mcp.NewToolResultError("epic_id, kind, summary, impact, and change are required for room_agile retro_add"), nil
		}
		argv = []string{"retro", "add", roomID, epicID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--milestone", getStringArg(args, "milestone_id", ""))
		argv = appendStringFlagArgs(argv, "--kind", kind)
		argv = appendStringFlagArgs(argv, "--summary", summaryText)
		argv = appendStringFlagArgs(argv, "--impact", impact)
		argv = appendStringFlagArgs(argv, "--change", change)
		argv = appendStringSliceFlagArgs(argv, "--scope", getStringSliceArg(args, "scope"))
		argv = appendStringSliceFlagArgs(argv, "--follow-up", getStringSliceArg(args, "follow_up"))
	case "retro_show":
		epicID := getStringArg(args, "epic_id", "")
		if epicID == "" {
			return mcp.NewToolResultError("epic_id is required for room_agile retro_show"), nil
		}
		argv = []string{"retro", "show", roomID, epicID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--milestone", getStringArg(args, "milestone_id", ""))
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "aca_promote":
		targetKind := getStringArg(args, "target_kind", "")
		sourceID := firstNonEmpty(getStringArg(args, "source_id", ""), getStringArg(args, "epic_id", ""), getStringArg(args, "milestone_id", ""), getStringArg(args, "story_id", ""))
		if targetKind == "" || sourceID == "" {
			return mcp.NewToolResultError("target_kind and source_id are required for room_agile aca_promote"), nil
		}
		argv = []string{"aca", "promote", targetKind, roomID, sourceID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
	case "workpack_show":
		epicID := getStringArg(args, "epic_id", "")
		if epicID == "" {
			return mcp.NewToolResultError("epic_id is required for room_agile workpack_show"), nil
		}
		argv = []string{"workpack", "show", roomID, epicID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
	case "workpack_sync":
		epicID := getStringArg(args, "epic_id", "")
		if epicID == "" {
			return mcp.NewToolResultError("epic_id is required for room_agile workpack_sync"), nil
		}
		argv = []string{"workpack", "sync", roomID, epicID}
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
	default:
		return mcp.NewToolResultError("unsupported room_agile action: " + action), nil
	}
	return runCLICommandAsMCP(ctx, "room_agile", newRoomCommand, argv)
}

func handleRoomRemindTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	action := getStringArg(args, "action", "")
	roomID := getStringArg(args, "room_id", "")
	if roomID == "" {
		return mcp.NewToolResultError("room_id is required"), nil
	}
	workspace := getStringArg(args, "workspace", "")
	argv := []string{"remind", action, roomID}
	switch action {
	case "add":
		recipient := getStringArg(args, "recipient", "")
		text := getStringArg(args, "text", "")
		if recipient == "" || text == "" {
			return mcp.NewToolResultError("recipient and text are required for room_remind add"), nil
		}
		argv = append(argv, recipient, text)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
		argv = appendStringFlagArgs(argv, "--subject", getStringArg(args, "subject", ""))
		argv = appendDurationFlagArgs(argv, "--every", getStringArg(args, "every", ""))
		argv = appendIntFlagArgs(argv, "--max-iterations", getIntArg(args, "max_iterations", 0))
		argv = appendBoolFlagArgs(argv, "--ack-required", getBoolArg(args, "ack_required", false))
		if _, ok := args["reply_expected"]; ok {
			if getBoolArg(args, "reply_expected", false) {
				argv = append(argv, "--reply-expected")
			} else {
				argv = append(argv, "--reply-expected=false")
			}
		}
		argv = appendBoolFlagArgs(argv, "--interrupt", getBoolArg(args, "interrupt", false))
		argv = appendBoolFlagArgs(argv, "--allow-passive", getBoolArg(args, "allow_passive", false))
	case "list":
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendBoolFlagArgs(argv, "--all", getBoolArg(args, "all", false))
	case "cancel":
		reminderID := getStringArg(args, "reminder_id", "")
		if reminderID == "" {
			return mcp.NewToolResultError("reminder_id is required for room_remind cancel"), nil
		}
		argv = append(argv, reminderID)
		argv = appendStringFlagArgs(argv, "--workspace", workspace)
		argv = appendStringFlagArgs(argv, "--actor", getStringArg(args, "actor", ""))
	default:
		return mcp.NewToolResultError("unsupported room_remind action: " + action), nil
	}
	return runCLICommandAsMCP(ctx, "room_remind", newRoomCommand, argv)
}

func handleAgentRoomTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	action := getStringArg(args, "action", "")
	agentRef := getStringArg(args, "agent_ref", "")
	if agentRef == "" {
		return mcp.NewToolResultError("agent_ref is required"), nil
	}
	argv := []string{action, agentRef}
	argv = appendStringFlagArgs(argv, "--workspace", getStringArg(args, "workspace", ""))
	argv = appendStringFlagArgs(argv, "--room-id", getStringArg(args, "room_id", ""))
	switch action {
	case "info":
	case "policy":
		argv = appendStringFlagArgs(argv, "--dispatch-policy", getStringArg(args, "dispatch_policy", ""))
		argv = appendStringSliceFlagArgs(argv, "--dispatch-agent", getStringSliceArg(args, "dispatch_agents"))
	default:
		return mcp.NewToolResultError("unsupported agent_room action: " + action), nil
	}
	return runCLICommandAsMCP(ctx, "agent_room", newAgentRoomCommand, argv)
}

func handleMuxTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	action := getStringArg(args, "action", "")
	argv := make([]string, 0, 32)
	switch action {
	case "list":
		argv = append(argv, "list")
		argv = appendStringFlagArgs(argv, "--backend", getStringArg(args, "backend", ""))
		argv = appendStringFlagArgs(argv, "--session", getStringArg(args, "session", ""))
		argv = appendIntFlagArgs(argv, "--limit", getIntArg(args, "limit", 0))
	case "read":
		target := getStringArg(args, "target", "")
		if target == "" {
			return mcp.NewToolResultError("target is required for mux read"), nil
		}
		argv = append(argv, "read", target)
		argv = appendIntFlagArgs(argv, "--lines", getIntArg(args, "lines", 0))
	case "send":
		target := getStringArg(args, "target", "")
		text := getStringArg(args, "text", "")
		if target == "" || text == "" {
			return mcp.NewToolResultError("target and text are required for mux send"), nil
		}
		argv = append(argv, "send", target, text)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
	case "submit":
		argv = append(argv, "submit")
		target := getStringArg(args, "target", "")
		if target != "" {
			argv = append(argv, target)
		}
		argv = appendStringFlagArgs(argv, "--backend", getStringArg(args, "backend", ""))
		argv = appendStringFlagArgs(argv, "--session", getStringArg(args, "session", ""))
	case "send_parent":
		text := getStringArg(args, "text", "")
		if text == "" {
			return mcp.NewToolResultError("text is required for mux send_parent"), nil
		}
		argv = append(argv, "send-parent", text)
		argv = appendStringFlagArgs(argv, "--sender", getStringArg(args, "sender", ""))
	case "observe":
		target := getStringArg(args, "target", "")
		if target == "" {
			return mcp.NewToolResultError("target is required for mux observe"), nil
		}
		argv = append(argv, "observe", target)
		argv = appendIntFlagArgs(argv, "--lines", getIntArg(args, "lines", 0))
		argv = appendStringFlagArgs(argv, "--statement", getStringArg(args, "statement", ""))
		argv = appendStringFlagArgs(argv, "--workspace", getStringArg(args, "workspace", ""))
		argv = appendFloatFlagArgs(argv, "--confidence", getFloatArg(args, "confidence", 0))
		argv = appendIntFlagArgs(argv, "--count", getIntArg(args, "count", 0))
		argv = appendStringFlagArgs(argv, "--project", getStringArg(args, "project", ""))
		argv = appendStringFlagArgs(argv, "--area", getStringArg(args, "area", ""))
		argv = appendBoolFlagArgs(argv, "--dry-run", getBoolArg(args, "dry_run", false))
	case "doctor":
		argv = append(argv, "doctor")
	case "create":
		argv = append(argv, "create")
		argv = appendStringFlagArgs(argv, "--backend", getStringArg(args, "backend", ""))
		argv = appendStringFlagArgs(argv, "--session", getStringArg(args, "session", ""))
		argv = appendIntFlagArgs(argv, "--panes", getIntArg(args, "panes", 0))
		argv = appendStringFlagArgs(argv, "--pane-command", getStringArg(args, "pane_command", ""))
		argv = appendStringFlagArgs(argv, "--agent", getStringArg(args, "agent", ""))
		argv = appendStringFlagArgs(argv, "--mode", getStringArg(args, "mode", ""))
		argv = appendStringSliceFlagArgs(argv, "--agent-arg", getStringSliceArg(args, "agent_args"))
		argv = appendStringFlagArgs(argv, "--agent-session-id", getStringArg(args, "agent_session_id", ""))
		argv = appendStringFlagArgs(argv, "--cwd", getStringArg(args, "cwd", ""))
		argv = appendStringFlagArgs(argv, "--label-prefix", getStringArg(args, "label_prefix", ""))
		argv = appendStringFlagArgs(argv, "--parent-participant", getStringArg(args, "parent_participant", ""))
		argv = appendStringFlagArgs(argv, "--parent-agent-id", getStringArg(args, "parent_agent_id", ""))
		argv = appendStringFlagArgs(argv, "--room-id", getStringArg(args, "room_id", ""))
		argv = appendStringFlagArgs(argv, "--room-access", getStringArg(args, "room_access", ""))
		argv = appendBoolFlagArgs(argv, "--attach", getBoolArg(args, "attach", false))
	default:
		return mcp.NewToolResultError("unsupported mux action: " + action), nil
	}
	return runCLICommandAsMCP(ctx, "mux", newTmuxCommand, argv)
}

func handleRepoIndexOpen(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	workspace := getStringArg(args, "workspace", ".")
	id := getStringArg(args, "id", "")
	if id == "" {
		return mcp.NewToolResultError("id is required"), nil
	}
	return runRepoIndexCommand(ctx, func(cmd *cobra.Command) error {
		return runIndexRepoOpen(cmd, workspace, id)
	})
}

func handleRepoIndexAsk(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	workspace := getStringArg(args, "workspace", ".")
	question := getStringArg(args, "question", "")
	if question == "" {
		return mcp.NewToolResultError("question is required"), nil
	}
	provider := getStringArg(args, "provider", "")
	model := getStringArg(args, "model", "")
	apiKey := getStringArg(args, "api_key", "")
	maxIterations := getIntArg(args, "max_iterations", 12)
	timeoutSec := getIntArg(args, "timeout_sec", 60)
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	timeout := time.Duration(timeoutSec) * time.Second

	return runRepoIndexCommand(ctx, func(cmd *cobra.Command) error {
		return runIndexRepoAsk(cmd, workspace, question, provider, model, apiKey, maxIterations, timeout)
	})
}

func handleHTMLSelect(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	path, _ := args["path"].(string)
	selector, _ := args["selector"].(string)

	if path == "" || selector == "" {
		return mcp.NewToolResultError("path and selector are required"), nil
	}

	return callLocalSkill(ctx, "html/edit", map[string]any{
		"path": path,
		"operations": []map[string]any{
			{"type": "select", "selector": selector},
		},
	})
}

func handleHTMLInsert(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	path, _ := args["path"].(string)
	selector, _ := args["selector"].(string)
	position, _ := args["position"].(string)
	html, _ := args["html"].(string)
	dryRun, _ := args["dry_run"].(bool)

	if path == "" || selector == "" || position == "" || html == "" {
		return mcp.NewToolResultError("path, selector, position, and html are required"), nil
	}

	return callLocalSkill(ctx, "html/edit", map[string]any{
		"path":    path,
		"dry_run": dryRun,
		"operations": []map[string]any{
			{"type": "insert", "selector": selector, "position": position, "html": html},
		},
	})
}

func handleHTMLReplace(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	path, _ := args["path"].(string)
	selector, _ := args["selector"].(string)
	html, _ := args["html"].(string)
	inner, _ := args["inner"].(bool)
	dryRun, _ := args["dry_run"].(bool)

	if path == "" || selector == "" || html == "" {
		return mcp.NewToolResultError("path, selector, and html are required"), nil
	}

	return callLocalSkill(ctx, "html/edit", map[string]any{
		"path":    path,
		"dry_run": dryRun,
		"operations": []map[string]any{
			{"type": "replace", "selector": selector, "html": html, "inner": inner},
		},
	})
}

func handleHTMLDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	path, _ := args["path"].(string)
	selector, _ := args["selector"].(string)
	dryRun, _ := args["dry_run"].(bool)

	if path == "" || selector == "" {
		return mcp.NewToolResultError("path and selector are required"), nil
	}

	return callLocalSkill(ctx, "html/edit", map[string]any{
		"path":    path,
		"dry_run": dryRun,
		"operations": []map[string]any{
			{"type": "delete", "selector": selector},
		},
	})
}

func handleHTMLSetAttr(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := getArgs(req)
	path, _ := args["path"].(string)
	selector, _ := args["selector"].(string)
	attr, _ := args["attr"].(string)
	value, hasValue := args["value"].(string)
	dryRun, _ := args["dry_run"].(bool)

	if path == "" || selector == "" || attr == "" {
		return mcp.NewToolResultError("path, selector, and attr are required"), nil
	}

	// Build attributes map - nil value means remove
	attributes := map[string]any{}
	if hasValue && value != "" {
		attributes[attr] = value
	} else {
		attributes[attr] = nil // Remove attribute
	}

	return callLocalSkill(ctx, "html/edit", map[string]any{
		"path":    path,
		"dry_run": dryRun,
		"operations": []map[string]any{
			{"type": "update_attr", "selector": selector, "attributes": attributes},
		},
	})
}

// callLocalSkill executes a local foxctl skill and returns the result.
func callLocalSkill(ctx context.Context, skillName string, input map[string]any) (*mcp.CallToolResult, error) {
	// Load config
	cfg, err := loadConfig(ctx)
	if err != nil {
		return mcp.NewToolResultError("load config: " + err.Error()), nil
	}

	// Find the skill
	handle, err := findSkill(cfg, skillName)
	if err != nil {
		return mcp.NewToolResultError("find skill " + skillName + ": " + err.Error()), nil
	}

	// Marshal input to JSON
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return mcp.NewToolResultError("marshal input: " + err.Error()), nil
	}

	// Resolve workspace context for skill execution
	runCtx := resolveWorkspaceContext(ctx, "")

	// Execute the skill
	stdout, stderr, err := executeSkill(runCtx, handle.Manifest, handle.ArtifactPath, inputBytes)
	return renderSkillExecutionResult(ctx, skillName, stdout, stderr, err)
}

// Backend communication

func callBackend(ctx context.Context, backendName, toolName string, args map[string]any) (*mcp.CallToolResult, error) {
	c, err := backends.getOrCreate(ctx, backendName)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("connect to "+backendName, err), nil
	}

	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	})
	if err != nil {
		return mcp.NewToolResultErrorFromErr("call "+toolName, err), nil
	}

	// Apply truncation for large web/search responses
	result = truncateLargeResponse(ctx, result, toolName)

	return result, nil
}

func (b *backendClients) getOrCreate(ctx context.Context, name string) (*client.Client, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if c, ok := b.clients[name]; ok {
		return c, nil
	}

	cfg, ok := b.configs[name]
	if !ok {
		return nil, fmt.Errorf("backend %q not configured", name)
	}

	var c *client.Client
	var err error

	if cfg.URL != "" {
		// HTTP transport
		c, err = client.NewStreamableHttpClient(cfg.URL, transport.WithHTTPHeaders(cfg.Headers))
		if err != nil {
			return nil, fmt.Errorf("create HTTP client: %w", err)
		}
		if err := c.Start(ctx); err != nil {
			return nil, fmt.Errorf("start HTTP client: %w", err)
		}
	} else if cfg.Command != "" {
		// Stdio transport
		env := os.Environ()
		for k, v := range cfg.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		c, err = client.NewStdioMCPClient(cfg.Command, env, cfg.Args...)
		if err != nil {
			return nil, fmt.Errorf("create stdio client: %w", err)
		}
	} else {
		return nil, fmt.Errorf("backend %q has no command or URL", name)
	}

	// Initialize MCP session
	_, err = c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "foxctl-mcp-facade",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	})
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("initialize MCP session: %w", err)
	}

	b.clients[name] = c
	return c, nil
}

func (b *backendClients) closeAll() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for name, c := range b.clients {
		if err := c.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close %s client: %v\n", name, err)
		}
	}
	b.clients = make(map[string]*client.Client)
}

// extractLibraryID parses the library ID from context7's resolve-library-id result.
func extractLibraryID(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}

	// The result is typically TextContent with the library ID
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			text := tc.Text
			// Context7 returns text like "Library ID: /vercel/next.js"
			// or structured output with the ID
			if strings.Contains(text, "/") {
				// Find the first path-like string
				parts := strings.Fields(text)
				for _, p := range parts {
					if strings.HasPrefix(p, "/") {
						return strings.TrimSpace(p)
					}
				}
			}
			return text
		}
	}
	return ""
}

// ============================================================================
// Skill Discovery (used by agentctl_skills tool)
// ============================================================================

// discoverSkills finds all skill manifests in the configured paths.
func discoverSkills(cfg config.Config) ([]skill.Manifest, error) {
	var manifests []skill.Manifest
	seen := make(map[string]bool)

	// Search paths: AGENTCTL_SKILLS_PATH, ~/.foxctl/skills, ./skills
	searchPaths := []string{cfg.Paths.Skills}
	if env := os.Getenv("AGENTCTL_SKILLS_PATH"); env != "" {
		searchPaths = append(filepath.SplitList(env), searchPaths...)
	}
	if pwd, err := os.Getwd(); err == nil {
		searchPaths = append(searchPaths, filepath.Join(pwd, "skills"))
	}

	for _, root := range searchPaths {
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}

		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // Skip inaccessible directories
			}
			if d.IsDir() || filepath.Base(path) != "skill.yaml" {
				return nil
			}

			manifest, err := skill.LoadManifest(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to load %s: %v\n", path, err)
				return nil
			}

			// Dedupe by skill name
			if seen[manifest.Metadata.Name] {
				return nil
			}
			seen[manifest.Metadata.Name] = true
			manifests = append(manifests, manifest)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return manifests, nil
}

// truncateSkillOutput checks if skill output exceeds size limit.
// If so, stores full content in CAS and returns structured result with pagination.
func truncateSkillOutput(ctx context.Context, content string, skillName string) (*mcp.CallToolResult, error) {
	totalSize := len(content)

	// If under limit, return as-is
	if totalSize <= maxInlineResponseBytes {
		return mcp.NewToolResultText(content), nil
	}

	// Store full content in CAS
	digest, err := storeToCAS(ctx, []byte(content), "application/json")
	if err != nil {
		// On CAS error, still truncate but without digest
		truncated := content[:maxInlineResponseBytes]
		for len(truncated) > 0 && !utf8.ValidString(truncated) {
			truncated = truncated[:len(truncated)-1]
		}
		return mcp.NewToolResultText(truncated + "\n\n---\n⚠️ Output truncated (CAS storage failed)"), nil
	}

	// Calculate pagination info
	totalPages := (totalSize + maxInlineResponseBytes - 1) / maxInlineResponseBytes

	// Truncate content for inline display
	truncated := content[:maxInlineResponseBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}

	// Return structured result with pagination metadata
	structuredData := map[string]any{
		"content":   truncated,
		"truncated": true,
		"pagination": map[string]any{
			"total_bytes":  totalSize,
			"total_pages":  totalPages,
			"current_page": 1,
			"page_size":    maxInlineResponseBytes,
			"has_more":     true,
		},
		"artifact": map[string]any{
			"digest":       digest,
			"content_type": "application/json",
			"retrieval":    fmt.Sprintf("foxctl cas read %s --page 2", digest),
		},
	}

	// Create human-readable fallback text
	fallbackText := fmt.Sprintf("%s\n\n---\n⚠️ Output truncated (%d bytes, %d pages)\nFull output: %s\nRead more: foxctl cas read %s --page 2",
		truncated, totalSize, totalPages, digest, digest)

	// Use structured content for better client handling
	return mcp.NewToolResultStructured(structuredData, fallbackText), nil
}

func init() {
	rootCmd.AddCommand(newMCPCommand())
}
