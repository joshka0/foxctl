# agentctl MCP Facade

The MCP facade allows Claude Code to access web search, documentation lookup, browser automation, and other tools through a single agentctl MCP server, reducing token overhead significantly.

## Token Savings

| Metric | Before (multiple MCP servers) | After (agentctl facade) |
|--------|------------------------------|-------------------------|
| Tool schemas in context | ~3,500+ tokens | ~600 tokens |
| Startup processes | 6+ | 1 |
| Tool count | 20+ tools | 16 curated tools |

## Available Tools

### Web & Search Tools

| Tool | Description | Backend |
|------|-------------|---------|
| `web_search` | Search the web for current information | tavily-search |
| `web_extract` | Extract content from URLs as markdown | tavily-extract |
| `web_crawl` | Recursively crawl a website | tavily-crawl |
| `web_map` | Map URL structure of a website | tavily-map |
| `web_search_general` | General web search (articles, blogs) | exa |
| `code_search` | Search for code examples and patterns | exa |
| `docs_lookup` | Look up library documentation | context7 |
| `ask` | Get comprehensive answers with citations | perplexity |

### Expo Tools (Mobile Development)

| Tool | Description | Backend |
|------|-------------|---------|
| `expo_build` | Trigger EAS cloud build | expo-mcp |
| `expo_update` | Publish OTA update | expo-mcp |
| `expo_screenshot` | Take simulator screenshot | expo-mcp |

### Supabase Tools (Database)

| Tool | Description | Backend |
|------|-------------|---------|
| `supabase_query` | Execute SQL query | supabase-mcp |
| `supabase_tables` | List database tables | supabase-mcp |
| `supabase_logs` | Get service logs | supabase-mcp |

### Browser Automation Tools

| Tool | Description | Backend |
|------|-------------|---------|
| `browser_navigate` | Navigate to a URL | playwright |
| `browser_screenshot` | Take page screenshot | playwright |
| `browser_click` | Click an element | playwright |
| `browser_fill` | Fill form input | playwright |
| `browser_content` | Get page content | playwright |

## Configuration

### 1. Set Environment Variables

Add these to your shell profile (`~/.zshrc` or `~/.bashrc`):

```bash
# Required for web search tools
export TAVILY_API_KEY="your-tavily-key"
export EXA_API_KEY="your-exa-key"
export PERPLEXITY_API_KEY="your-perplexity-key"

# Optional - for Expo EAS builds/updates
export EXPO_TOKEN="your-expo-token"

# Optional - for Supabase database access
export SUPABASE_URL="your-project-url"
export SUPABASE_KEY="your-service-role-key"

# context7 and playwright don't need API keys
```

### 2. Update Claude's MCP Config

Replace your `~/.cursor/mcp.json` or `~/.claude.json` with:

```json
{
  "mcpServers": {
    "agentctl": {
      "command": "/path/to/agentctl",
      "args": ["mcp", "serve"],
      "env": {
        "TAVILY_API_KEY": "your-tavily-key",
        "EXA_API_KEY": "your-exa-key",
        "PERPLEXITY_API_KEY": "your-perplexity-key",
        "EXPO_TOKEN": "your-expo-token",
        "SUPABASE_URL": "your-supabase-url"
      }
    }
  }
}
```

Or keep API keys in your shell and use:

```json
{
  "mcpServers": {
    "agentctl": {
      "command": "/path/to/agentctl",
      "args": ["mcp", "serve"]
    }
  }
}
```

### 3. Build agentctl

```bash
cd /path/to/agentctl
CGO_ENABLED=0 go build -o bin/agentctl ./cmd/agentctl
```

## Tool Usage Examples

### Web Search Tools

```json
// web_search - Basic web search
{"query": "React 19 new features", "max_results": 5, "topic": "general"}

// web_extract - Extract page content
{"urls": ["https://react.dev/blog/2024/04/25/react-19"]}

// web_crawl - Crawl a website
{"url": "https://docs.expo.dev", "max_depth": 2, "limit": 10}

// web_map - Map URL structure
{"url": "https://nextjs.org/docs", "limit": 50}

// web_search_general - Exa general search
{"query": "best state management libraries 2024", "num_results": 10}
```

### Documentation & Q&A

```json
// docs_lookup - Library documentation
{"library": "nextjs", "topic": "app router"}

// code_search - Code examples
{"query": "React Server Components data fetching patterns"}

// ask - Comprehensive answers
{"question": "What are the best practices for error handling in Go?"}
```

### Expo Tools

```json
// expo_build - Trigger build
{"platform": "ios", "profile": "production"}

// expo_update - Publish update
{"channel": "production", "message": "Bug fixes"}

// expo_screenshot - Take screenshot
{"platform": "ios"}
```

### Supabase Tools

```json
// supabase_query - Execute SQL
{"sql": "SELECT * FROM users LIMIT 10"}

// supabase_tables - List tables
{}

// supabase_logs - Get logs
{"service": "postgres", "limit": 100}
```

### Browser Automation

```json
// browser_navigate
{"url": "https://example.com"}

// browser_screenshot
{"name": "homepage", "full_page": true}

// browser_click
{"selector": "button.submit"}

// browser_fill
{"selector": "#email", "value": "test@example.com"}

// browser_content
{"selector": ".main-content"}
```

## Architecture

```
Claude Code
    |
    v
agentctl mcp serve (stdio)
    |
    |-- tavily (npx stdio) --> web_search, web_extract, web_crawl, web_map
    |-- context7 (npx stdio) --> docs_lookup
    |-- exa (HTTP) --> code_search, web_search_general
    |-- perplexity (npx stdio) --> ask
    |-- expo (HTTP) --> expo_build, expo_update, expo_screenshot
    |-- supabase (HTTP) --> supabase_query, supabase_tables, supabase_logs
    +-- playwright (npx stdio) --> browser_*
```

The facade:
1. Exposes 16 curated tools with minimal schemas
2. Lazily connects to backend MCP servers on first use
3. Caches connections for subsequent calls
4. Cleans up on exit

## Backend Connection Types

| Backend | Transport | Startup |
|---------|-----------|---------|
| tavily | npx stdio | On first use |
| context7 | npx stdio | On first use |
| exa | HTTP | Instant |
| perplexity | npx stdio | On first use |
| expo | HTTP | Instant |
| supabase | HTTP | Instant |
| playwright | npx stdio | On first use |

## Comparison: Before and After

### Before (Multiple MCP servers)
- tavily-search with huge country enum (~800 tokens)
- tavily-extract, tavily-crawl, tavily-map
- context7 resolve-library-id + get-library-docs
- exa web_search + code_search with complex options
- perplexity_ask with message array
- supabase with full SQL tools
- expo-mcp with EAS integration
- playwright with full browser API

### After (agentctl facade)
- 16 tools with minimal schemas
- Single process startup
- Unified error handling
- Same functionality, ~80% fewer schema tokens
- Lazy backend initialization
