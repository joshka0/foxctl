// Package cmd provides the mcp command for running agentctl as an MCP server.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
)

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
expo, supabase, playwright).

This reduces token overhead by exposing simplified tool schemas.`,
	}

	cmd.AddCommand(newMCPServeCommand())
	return cmd
}

func newMCPServeCommand() *cobra.Command {
	var configFile string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio transport)",
		Long: `Start agentctl as an MCP server using stdio transport.

Configure backend MCP servers via --config or environment variables:
  TAVILY_API_KEY     - Tavily API key (web search, extract, crawl, map)
  EXA_API_KEY        - Exa API key (code search, web search)
  PERPLEXITY_API_KEY - Perplexity API key (ask)
  EXPO_TOKEN         - Expo access token (EAS builds, updates, submit)
  SUPABASE_URL       - Supabase project URL (required for supabase tools)
  SUPABASE_KEY       - Supabase service role key (required for supabase tools)

Example usage in Claude's mcp.json:
  {
    "mcpServers": {
      "agentctl": {
        "command": "/path/to/agentctl",
        "args": ["mcp", "serve"]
      }
    }
  }`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCPServer(cmd.Context(), configFile)
		},
	}

	cmd.Flags().StringVar(&configFile, "config", "", "Path to backend MCP servers config file")
	return cmd
}

func runMCPServer(ctx context.Context, configFile string) error {
	// Initialize backend configurations from environment
	initBackendConfigs()

	// Load config file if provided
	if configFile != "" {
		if err := loadBackendConfig(configFile); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	}

	info := buildinfo.Current()
	s := server.NewMCPServer("agentctl", info.Version,
		server.WithToolCapabilities(true),
	)

	// Register curated tools with simplified schemas
	registerTools(s)

	// Cleanup on exit
	defer backends.closeAll()

	// Start stdio server (blocks until client disconnects)
	return server.ServeStdio(s)
}

func initBackendConfigs() {
	// Tavily (stdio via npx)
	if key := os.Getenv("TAVILY_API_KEY"); key != "" {
		backends.configs["tavily"] = mcpServerConfig{
			Command: "npx",
			Args:    []string{"-y", "tavily-mcp@latest"},
			Env:     map[string]string{"TAVILY_API_KEY": key},
		}
	}

	// Exa (HTTP)
	if key := os.Getenv("EXA_API_KEY"); key != "" {
		backends.configs["exa"] = mcpServerConfig{
			URL: "https://mcp.exa.ai/mcp",
			Headers: map[string]string{
				"Accept":        "application/json, text/event-stream",
				"Authorization": "Bearer " + key,
			},
		}
	}

	// Perplexity (stdio via npx)
	if key := os.Getenv("PERPLEXITY_API_KEY"); key != "" {
		backends.configs["perplexity"] = mcpServerConfig{
			Command: "npx",
			Args:    []string{"-y", "@perplexity-ai/mcp-server"},
			Env: map[string]string{
				"PERPLEXITY_API_KEY":    key,
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
	// web_search - Simplified tavily-search (drops huge country enum)
	s.AddTool(
		mcp.NewTool("web_search",
			mcp.WithDescription("Search the web for current information. Returns relevant results with snippets."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
			mcp.WithNumber("max_results", mcp.Description("Max results to return (default: 10, max: 20)")),
			mcp.WithString("topic", mcp.Description("Topic: 'general' or 'news'")),
		),
		handleWebSearch,
	)

	// web_extract - Simplified tavily-extract
	s.AddTool(
		mcp.NewTool("web_extract",
			mcp.WithDescription("Extract content from URLs. Returns markdown-formatted page content."),
			mcp.WithArray("urls", mcp.Required(), mcp.Description("URLs to extract content from")),
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
}

// Tool handlers

// getArgs safely extracts arguments from the request as a map.
func getArgs(req mcp.CallToolRequest) map[string]any {
	if args, ok := req.Params.Arguments.(map[string]any); ok {
		return args
	}
	return make(map[string]any)
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

	return callBackend(ctx, "tavily", "tavily-search", map[string]any{
		"query":       query,
		"max_results": int(maxResults),
		"topic":       topic,
	})
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

func init() {
	rootCmd.AddCommand(newMCPCommand())
}
