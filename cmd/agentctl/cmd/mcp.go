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

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/config"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
	obsidiantool "github.com/jkatigb/agentctl/internal/tools/obsidian"
)

const (
	// maxInlineResponseBytes is the maximum size for inline responses (2KB)
	maxInlineResponseBytes = 2048

	// defaultPIDFile is the default location for the MCP daemon PID file
	defaultPIDFile = "~/.agentctl/mcp-daemon.pid"

	// shutdownTimeout is the maximum time to wait for graceful shutdown
	shutdownTimeout = 10 * time.Second
)

// daemonState tracks the running daemon state
type daemonState struct {
	pidFile   string
	startTime time.Time
	addr      string
}

// skillGroups defines logical groupings of agentctl skills exposed as first-class MCP tools.
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
	// agentctl-ci: CI/CD integration tools
	"agentctl-ci": {
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
		home = filepath.Join(userHome, ".agentctl")
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

	note := fmt.Sprintf("\n\n---\n⚠️ Response truncated (%d bytes total, %d pages). Full output stored in CAS: %s\nRead page 2: agentctl cas read %s --page 2",
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
		Long: `Run agentctl as an MCP server that provides a curated set of tools,
proxying requests to backend MCP servers (tavily, exa, context7, perplexity,
expo, supabase, playwright) and exposing local agentctl tools (repo_index_*, html_edit).

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
	defaultLogFile = "~/.agentctl/logs/mcp-daemon.log"
)

func newMCPServeCommand() *cobra.Command {
	var (
		configFile   string
		httpAddr     string
		enableSkills bool
		daemonMode   bool
		skillGroupsF []string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server",
		Long: `Start agentctl as an MCP server using stdio or HTTP/SSE transport.

By default, uses stdio transport for Claude Code integration.
Use --http to run as an HTTP daemon with SSE transport (foreground).
Use --daemon to run as an HTTP daemon in background mode.
Use --skills to expose all agentctl skills via generic agentctl_run/agentctl_skills tools.
Use --groups to expose specific skill groups as first-class MCP tools.

Available skill groups:
  all         - All installed agentctl skills as first-class MCP tools
  code-intel  - Code analysis: semantic_search, smart_search, symbols, snippet_extract, context_grep, codemap_get, codemap_generate
  code-write  - Code modification: smart_write
  project     - Project management: todo/manage, memory/query, session/recall
  agentctl-ci - CI/CD integration: checks, prcomments

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
      "agentctl": {
        "command": "/path/to/agentctl",
        "args": ["mcp", "serve", "--groups", "code-intel,project,agentctl-ci"]
      }
    }
  }

Example usage with HTTP/SSE daemon (foreground):
  agentctl mcp serve --http :8091 --groups code-intel,project

Example usage with daemon mode (background):
  # Start daemon in background
  agentctl mcp serve --daemon --groups code-intel,project,agentctl-ci

  # Check status
  agentctl mcp status

  # Stop daemon
  agentctl mcp stop

  # Configure in mcp.json
  {
    "mcpServers": {
      "agentctl": {
        "url": "http://localhost:8091/sse"
      }
    }
  }`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Daemon mode implies HTTP mode with default port
			if daemonMode {
				if httpAddr == "" {
					httpAddr = defaultDaemonPort
				}
				return runDaemonMode(cmd.Context(), mcpServerOptions{
					configFile:   configFile,
					httpAddr:     httpAddr,
					enableSkills: enableSkills,
					groups:       skillGroupsF,
				})
			}
			return runMCPServer(cmd.Context(), mcpServerOptions{
				configFile:   configFile,
				httpAddr:     httpAddr,
				enableSkills: enableSkills,
				groups:       skillGroupsF,
			})
		},
	}

	cmd.Flags().StringVar(&configFile, "config", "", "Path to backend MCP servers config file")
	cmd.Flags().StringVar(&httpAddr, "http", "", "HTTP address for SSE daemon mode (e.g., :8091)")
	cmd.Flags().BoolVar(&enableSkills, "skills", false, "Expose all agentctl skills via agentctl_run/agentctl_skills tools")
	cmd.Flags().BoolVar(&daemonMode, "daemon", false, "Run as background daemon (implies --http :8091)")
	cmd.Flags().StringSliceVar(&skillGroupsF, "groups", nil, "Skill groups to expose as first-class tools (code-intel,code-write,project,agentctl-ci)")
	return cmd
}

// runDaemonMode spawns a background process running the MCP server
func runDaemonMode(ctx context.Context, opts mcpServerOptions) error {
	pidFile := expandPath(defaultPIDFile)

	// Check if daemon already running
	if existingPID, _, running := isDaemonRunning(pidFile); running {
		fmt.Printf("MCP daemon already running (PID %d)\n", existingPID)
		fmt.Printf("Use 'agentctl mcp stop' to stop it first\n")
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
	fmt.Printf("\nUse 'agentctl mcp status' to check status\n")
	fmt.Printf("Use 'agentctl mcp stop' to stop\n")
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
	configFile   string
	httpAddr     string
	enableSkills bool
	groups       []string
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
	s := server.NewMCPServer("agentctl", info.Version,
		server.WithToolCapabilities(true),
	)

	// Register curated tools with simplified schemas (external MCP proxies + html)
	registerTools(s)

	// Register skill groups as first-class MCP tools
	if len(opts.groups) > 0 {
		if err := registerSkillGroups(ctx, s, opts.groups); err != nil {
			observability.Emit(ctx, observability.NewEvent("mcp.skill_groups_registration_warning").
				WithComponent(observability.ComponentCLI).
				WithData("reason", "continuing without skill groups").
				Error(err, 0))
		}
	}

	// Register generic agentctl tools (run + discovery) instead of individual skill tools
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

func runRepoIndexCommand(ctx context.Context, run func(cmd *cobra.Command) error) (*mcp.CallToolResult, error) {
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetContext(ctx)

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
		return truncateSkillOutput(ctx, raw, "repo_index")
	}
	if envelope.Status == "error" {
		if envelope.Error.Message != "" {
			return mcp.NewToolResultError(envelope.Error.Message), nil
		}
		return mcp.NewToolResultError("repo index command failed"), nil
	}

	result, err := json.MarshalIndent(envelope.Data, "", "  ")
	if err != nil {
		return truncateSkillOutput(ctx, raw, "repo_index")
	}

	return truncateSkillOutput(ctx, string(result), "repo_index")
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
			mcp.WithDescription("Run an agentctl skill. Use agentctl_skills to discover available skills."),
			mcp.WithString("skill", mcp.Required(), mcp.Description("Skill name (e.g., 'code/complexity', 'todo/manage', 'test/run')")),
			mcp.WithObject("input", mcp.Description("Input arguments for the skill as a JSON object. Check skill signature with agentctl_skills for required parameters.")),
		),
		handleAgentctlRun,
	)

	// agentctl_skills - Skill discovery
	s.AddTool(
		mcp.NewTool("agentctl_skills",
			mcp.WithDescription("List available agentctl skills with their descriptions and parameters."),
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
		if err != nil {
			errMsg := err.Error()
			if len(stderr) > 0 {
				errMsg += "\nstderr: " + string(stderr)
			}
			return mcp.NewToolResultError("execute skill: " + errMsg), nil
		}

		// Parse the envelope response
		var envelope struct {
			Status  string         `json:"status"`
			Command string         `json:"command"`
			Data    map[string]any `json:"data"`
			Error   struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}

		if err := json.Unmarshal(stdout, &envelope); err != nil {
			// Return raw output if not a valid envelope
			return truncateSkillOutput(ctx, string(stdout), manifest.Metadata.Name)
		}

		if envelope.Status == "error" {
			return mcp.NewToolResultError(envelope.Error.Message), nil
		}

		// Format the data as readable output
		result, err := json.MarshalIndent(envelope.Data, "", "  ")
		if err != nil {
			return truncateSkillOutput(ctx, string(stdout), manifest.Metadata.Name)
		}

		return truncateSkillOutput(ctx, string(result), manifest.Metadata.Name)
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
	if err != nil {
		errMsg := err.Error()
		if len(stderr) > 0 {
			errMsg += "\nstderr: " + string(stderr)
		}
		return mcp.NewToolResultError("execute skill: " + errMsg), nil
	}

	// Parse the envelope response
	var envelope struct {
		Status  string         `json:"status"`
		Command string         `json:"command"`
		Data    map[string]any `json:"data"`
		Meta    map[string]any `json:"meta"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(stdout, &envelope); err != nil {
		// Return raw output if not a valid envelope
		return truncateSkillOutput(ctx, string(stdout), skillName)
	}

	if envelope.Status == "error" {
		return mcp.NewToolResultError(envelope.Error.Message), nil
	}

	// Format the data as readable output
	result, err := json.MarshalIndent(envelope.Data, "", "  ")
	if err != nil {
		return truncateSkillOutput(ctx, string(stdout), skillName)
	}

	return truncateSkillOutput(ctx, string(result), skillName)
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
	includeTS := getBoolArg(args, "include_typescript", true)
	includeElixir := getBoolArg(args, "include_elixir", false)
	includeTerraform := getBoolArg(args, "include_terraform", false)
	includeKubernetes := getBoolArg(args, "include_kubernetes", false)
	includeShell := getBoolArg(args, "include_shell", false)
	includeTests := getBoolArg(args, "include_tests", false)
	dryRun := getBoolArg(args, "dry_run", false)

	return runRepoIndexCommand(ctx, func(cmd *cobra.Command) error {
		return runIndexRepoBuild(cmd, workspace, patterns, includeGo, includePython, includeTS, includeElixir, includeTerraform, includeKubernetes, includeShell, includeTests, dryRun)
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
	return runRepoIndexCommand(ctx, func(cmd *cobra.Command) error {
		return runIndexRepoSearch(cmd, workspace, query, limit)
	})
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

	return runRepoIndexCommand(ctx, func(cmd *cobra.Command) error {
		return runIndexRepoExpand(cmd, workspace, seeds, edgeTypes, depth, budget, perNodeCap, direction)
	})
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

// callLocalSkill executes a local agentctl skill and returns the result.
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
	if err != nil {
		errMsg := err.Error()
		if len(stderr) > 0 {
			errMsg += "\nstderr: " + string(stderr)
		}
		return mcp.NewToolResultError("execute skill: " + errMsg), nil
	}

	// Parse the envelope response
	var envelope struct {
		Status  string         `json:"status"`
		Command string         `json:"command"`
		Data    map[string]any `json:"data"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(stdout, &envelope); err != nil {
		// Return raw output if not a valid envelope
		return mcp.NewToolResultText(string(stdout)), nil
	}

	if envelope.Status == "error" {
		return mcp.NewToolResultError(envelope.Error.Message), nil
	}

	// Format the data as readable output
	result, err := json.MarshalIndent(envelope.Data, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(string(stdout)), nil
	}

	return mcp.NewToolResultText(string(result)), nil
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
				Name:    "agentctl-mcp-facade",
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

	// Search paths: AGENTCTL_SKILLS_PATH, ~/.agentctl/skills, ./skills
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
			"retrieval":    fmt.Sprintf("agentctl cas read %s --page 2", digest),
		},
	}

	// Create human-readable fallback text
	fallbackText := fmt.Sprintf("%s\n\n---\n⚠️ Output truncated (%d bytes, %d pages)\nFull output: %s\nRead more: agentctl cas read %s --page 2",
		truncated, totalSize, totalPages, digest, digest)

	// Use structured content for better client handling
	return mcp.NewToolResultStructured(structuredData, fallbackText), nil
}

func init() {
	rootCmd.AddCommand(newMCPCommand())
}
