#!/usr/bin/env bash
# scripts/init.sh - Initialize foxctl system setup
#
# This script:
# 1. Creates required directories
# 2. Symlinks binaries to ~/.local/bin/
# 3. Sets up Claude Code integration (hooks, skills)
# 4. Validates .env configuration
#
# Usage: make init (preferred) or ./scripts/init.sh

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Directories
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
BIN_DIR="$REPO_ROOT/bin"
LOCAL_BIN="${HOME}/.local/bin"
FOXCTL_HOME="${FOXCTL_HOME:-$HOME/.foxctl}"
CLAUDE_DIR="$HOME/.claude"
CODEX_DIR="$HOME/.codex"
GEMINI_DIR="$HOME/.gemini"

echo -e "${BLUE}=== foxctl System Initialization ===${NC}"
echo ""

# Track status
WARNINGS=0
ERRORS=0

# Helper functions
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; ((WARNINGS++)); }
error() { echo -e "${RED}[ERROR]${NC} $1"; ((ERRORS++)); }

SKILLS_CONFIG="$REPO_ROOT/configs/providers/skills.json"

expand_home_path() {
    local p="$1"
    if [[ "$p" == ~/* ]]; then
        echo "$HOME/${p#\~/}"
        return
    fi
    echo "$p"
}

provider_cfg() {
    local provider="$1"
    local key="$2"
    local default_value="$3"

    if command -v jq &>/dev/null && [[ -f "$SKILLS_CONFIG" ]]; then
        local v
        v="$(jq -r --arg p "$provider" --arg k "$key" '.providers[$p][$k] // empty' "$SKILLS_CONFIG" 2>/dev/null || true)"
        if [[ -n "$v" && "$v" != "null" ]]; then
            expand_home_path "$v"
            return
        fi
    fi

    expand_home_path "$default_value"
}

resolve_repo_path() {
    local p="$1"
    p="$(expand_home_path "$p")"
    if [[ "$p" == /* ]]; then
        echo "$p"
        return
    fi
    echo "$REPO_ROOT/$p"
}

provider_cfg_list() {
    local provider="$1"
    local key="$2"
    local default_value="$3"

    if command -v jq &>/dev/null && [[ -f "$SKILLS_CONFIG" ]]; then
        local v
        v="$(jq -r --arg p "$provider" --arg k "$key" '
            .providers[$p][$k] // empty
            | if type == "array" then .[] else . end
        ' "$SKILLS_CONFIG" 2>/dev/null || true)"
        if [[ -n "$v" && "$v" != "null" ]]; then
            printf '%s\n' "$v"
            return
        fi
    fi

    printf '%s\n' "$default_value"
}

# 1. Check binaries exist
echo -e "${BLUE}1. Checking binaries...${NC}"
if [[ ! -f "$BIN_DIR/foxctl" ]]; then
    error "bin/foxctl not found. Run 'make build' first."
else
    success "bin/foxctl exists"
fi

echo ""

# 2. Create directories
echo -e "${BLUE}2. Creating directories...${NC}"
mkdir -p "$LOCAL_BIN"
success "Created $LOCAL_BIN"

mkdir -p "$FOXCTL_HOME"/{storage,cache,cas,skills,jobs,observability/events,backups}
success "Created $FOXCTL_HOME structure"

info "Ensuring foxctl share configs link"
FOXCTL_SHARE="$FOXCTL_HOME/share"
mkdir -p "$FOXCTL_SHARE"

if [[ -L "$FOXCTL_SHARE/configs" ]]; then
    rm "$FOXCTL_SHARE/configs"
elif [[ -e "$FOXCTL_SHARE/configs" ]]; then
    warn "foxctl share path exists (skipping): $FOXCTL_SHARE/configs"
else
    REPO_CONFIGS_ABS="$(cd "$REPO_ROOT/configs" && pwd)"
    ln -s "$REPO_CONFIGS_ABS" "$FOXCTL_SHARE/configs"
    success "Symlinked foxctl configs -> $FOXCTL_SHARE/configs"
fi

mkdir -p "$CLAUDE_DIR"
success "Created $CLAUDE_DIR"
echo ""

# 3. Symlink binaries
echo -e "${BLUE}3. Setting up binary symlinks...${NC}"

symlink_binary() {
    local src="$1"
    local dst="$2"
    local name="$3"

    if [[ ! -f "$src" ]]; then
        warn "Skipping $name (source not found)"
        return
    fi

    if [[ -L "$dst" ]]; then
        # Remove existing symlink
        rm "$dst"
    elif [[ -f "$dst" ]]; then
        # Backup existing file
        mv "$dst" "${dst}.bak"
        warn "Backed up existing $dst to ${dst}.bak"
    fi

    ln -s "$src" "$dst"
    success "Symlinked $name -> $dst"
}

symlink_binary "$BIN_DIR/foxctl" "$LOCAL_BIN/foxctl" "foxctl"
echo ""

# 4. Check PATH
echo -e "${BLUE}4. Checking PATH...${NC}"
if [[ ":$PATH:" == *":$LOCAL_BIN:"* ]]; then
    success "$LOCAL_BIN is in PATH"
else
    warn "$LOCAL_BIN is not in PATH"
    echo "    Add to your shell config:"
    echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
fi
echo ""

# 5. Setup Claude Code integration
echo -e "${BLUE}5. Setting up Claude Code integration...${NC}"

# Create hooks directory
mkdir -p "$CLAUDE_DIR/hooks"

# Symlink foxctl hooks folder
HOOKS_SOURCE="$REPO_ROOT/configs/hooks"
HOOKS_TARGET="$CLAUDE_DIR/hooks/foxctl"

if [[ -d "$HOOKS_SOURCE" ]]; then
    # Remove existing symlink (including broken ones) or directory
    # Note: -L checks for symlink regardless of whether target exists
    if [[ -L "$HOOKS_TARGET" ]]; then
        rm "$HOOKS_TARGET"
    elif [[ -e "$HOOKS_TARGET" ]]; then
        # Only warn and remove if it's a real directory (not a symlink)
        warn "Removing existing foxctl hooks directory"
        rm -rf "$HOOKS_TARGET"
    fi

    # Resolve source to absolute path for reliable symlinks
    # This ensures the symlink works even if cwd changes
    HOOKS_SOURCE_ABS="$(cd "$HOOKS_SOURCE" && pwd)"

    # Create symlink to hooks folder using absolute path
    ln -s "$HOOKS_SOURCE_ABS" "$HOOKS_TARGET"
    success "Symlinked foxctl hooks -> $HOOKS_TARGET"
else
    warn "No hooks to symlink (configs/hooks not found)"
fi

# Configure Claude Code settings.json with foxctl hooks
echo -e "${BLUE}5a1. Configuring Claude Code settings.json...${NC}"

CLAUDE_SETTINGS_TEMPLATE="$REPO_ROOT/configs/claude-settings.json"
CLAUDE_SETTINGS_TARGET="$CLAUDE_DIR/settings.json"

if [[ -f "$CLAUDE_SETTINGS_TEMPLATE" ]]; then
    if command -v jq &>/dev/null; then
        if [[ -f "$CLAUDE_SETTINGS_TARGET" ]]; then
            # Merge: preserve user's existing settings, add/update hooks from template
            # .[0] * .[1] merges all fields, then we ensure hooks come from template
            MERGED=$(jq -s '.[0] * .[1] * {hooks: .[1].hooks}' \
                "$CLAUDE_SETTINGS_TARGET" "$CLAUDE_SETTINGS_TEMPLATE" 2>/dev/null)

            if [[ -n "$MERGED" ]]; then
                # Backup existing settings
                cp "$CLAUDE_SETTINGS_TARGET" "$CLAUDE_SETTINGS_TARGET.bak"
                echo "$MERGED" > "$CLAUDE_SETTINGS_TARGET"
                success "Merged foxctl hooks into $CLAUDE_SETTINGS_TARGET (backup: .bak)"
            else
                warn "Failed to merge settings.json, keeping existing"
            fi
        else
            # No existing settings, use template directly
            cp "$CLAUDE_SETTINGS_TEMPLATE" "$CLAUDE_SETTINGS_TARGET"
            success "Created $CLAUDE_SETTINGS_TARGET with foxctl hooks"
        fi
    else
        warn "jq not found. Manually configure $CLAUDE_SETTINGS_TARGET with hooks from:"
        echo "    $CLAUDE_SETTINGS_TEMPLATE"
    fi
else
    warn "No Claude settings template found (configs/claude-settings.json)"
fi

# Copy skills manifest reference
if [[ -d "$REPO_ROOT/.claude/skills" ]]; then
    mkdir -p "$CLAUDE_DIR/skills"
    success "Claude skills directory ready at $CLAUDE_DIR/skills"
fi

echo -e "${BLUE}5a2. Setting up Claude skills...${NC}"

CLAUDE_SKILLS_DIR="$(provider_cfg claude target_dir "$CLAUDE_DIR/skills")"
CLAUDE_SKILLS_LEGACY="$(provider_cfg claude legacy_dir "$CLAUDE_DIR/skills-legacy")"

mkdir -p "$CLAUDE_SKILLS_DIR" "$CLAUDE_SKILLS_LEGACY"

while IFS= read -r prune_root; do
    prune_root="$(resolve_repo_path "$prune_root")"
    [[ -d "$prune_root" ]] || continue

    for skill_dir in "$prune_root"/*; do
        [[ -d "$skill_dir" ]] || continue

        skill_name="$(basename "$skill_dir")"
        target="$CLAUDE_SKILLS_DIR/$skill_name"

        if [[ -L "$target" ]]; then
            legacy="$CLAUDE_SKILLS_LEGACY/$skill_name"
            if [[ ! -e "$legacy" ]]; then
                mv "$target" "$legacy"
            else
                rm "$target"
            fi
        fi
    done
done < <(provider_cfg_list claude prune_sources "$REPO_ROOT/configs/skills-condensed")

claude_installed_any=0
while IFS= read -r source_root; do
    source_root="$(resolve_repo_path "$source_root")"
    [[ -d "$source_root" ]] || continue
    claude_installed_any=1

    info "Linking Claude skills from $source_root"

    for skill_dir in "$source_root"/*; do
        [[ -d "$skill_dir" ]] || continue

        skill_name="$(basename "$skill_dir")"
        target="$CLAUDE_SKILLS_DIR/$skill_name"

        if [[ -L "$target" ]]; then
            rm "$target"
        elif [[ -e "$target" ]]; then
            warn "Claude skill already exists (skipping): $target"
            continue
        fi

        skill_dir_abs="$(cd "$skill_dir" && pwd)"
        ln -s "$skill_dir_abs" "$target"
    done
done < <(provider_cfg_list claude sources "$REPO_ROOT/configs/skills-pack")

if [[ "$claude_installed_any" == "1" ]]; then
    success "Installed foxctl Claude skills"
else
    warn "No Claude skills to install (sources not found)"
fi

echo ""

# 5b. Setup OpenCode plugin integration
echo -e "${BLUE}5b. Setting up OpenCode plugin integration...${NC}"

OPENCODE_CONFIG_DIR="${HOME}/.config/opencode"
OPENCODE_HOOKS_SOURCE="$REPO_ROOT/configs/opencode-hooks"

if [[ -d "$OPENCODE_HOOKS_SOURCE" ]]; then
    # Create OpenCode config directory
    mkdir -p "$OPENCODE_CONFIG_DIR"

    # Resolve source to absolute path
    OPENCODE_HOOKS_SOURCE_ABS="$(cd "$OPENCODE_HOOKS_SOURCE" && pwd)"

    # Create or update package.json with file:// dependency
    OPENCODE_PKG="$OPENCODE_CONFIG_DIR/package.json"
    if [[ -f "$OPENCODE_PKG" ]]; then
        # Check if foxctl-opencode-hooks is already in package.json
        if grep -q '"foxctl-opencode-hooks"' "$OPENCODE_PKG"; then
            success "OpenCode package.json already has foxctl-opencode-hooks"
        else
            # Add the dependency using jq if available, otherwise warn
            if command -v jq &>/dev/null; then
                jq --arg path "file://$OPENCODE_HOOKS_SOURCE_ABS" \
                   '.dependencies["foxctl-opencode-hooks"] = $path' \
                   "$OPENCODE_PKG" > "${OPENCODE_PKG}.tmp" && mv "${OPENCODE_PKG}.tmp" "$OPENCODE_PKG"
                success "Added foxctl-opencode-hooks to OpenCode package.json"
            else
                warn "jq not found. Manually add to $OPENCODE_PKG:"
                echo "    \"foxctl-opencode-hooks\": \"file://$OPENCODE_HOOKS_SOURCE_ABS\""
            fi
        fi
    else
        # Create new package.json
        cat > "$OPENCODE_PKG" <<EOF
{
  "dependencies": {
    "foxctl-opencode-hooks": "file://$OPENCODE_HOOKS_SOURCE_ABS"
  }
}
EOF
        success "Created OpenCode package.json with foxctl-opencode-hooks"
    fi

    # Run bun install to link the plugin
    if command -v bun &>/dev/null; then
        (cd "$OPENCODE_CONFIG_DIR" && bun install --silent 2>/dev/null)
        success "Installed OpenCode plugin dependencies"
    else
        warn "bun not found. Run: cd $OPENCODE_CONFIG_DIR && bun install"
    fi

    # Check if plugin is in opencode.json plugin array
    OPENCODE_JSON="$OPENCODE_CONFIG_DIR/opencode.json"
    if [[ -f "$OPENCODE_JSON" ]]; then
        if grep -q '"foxctl-opencode-hooks"' "$OPENCODE_JSON"; then
            success "OpenCode config has foxctl-opencode-hooks in plugin array"
        else
            warn "Add 'foxctl-opencode-hooks' to plugin array in $OPENCODE_JSON"
        fi
    else
        info "OpenCode config not found at $OPENCODE_JSON"
    fi
else
    warn "No OpenCode hooks to install (configs/opencode-hooks not found)"
fi

echo -e "${BLUE}5b2. Setting up OpenCode skills...${NC}"

OPENCODE_SKILLS_DIR="$(provider_cfg opencode target_dir "$OPENCODE_CONFIG_DIR/skill")"
OPENCODE_SKILLS_LEGACY="$(provider_cfg opencode legacy_dir "$OPENCODE_CONFIG_DIR/skill-legacy")"
mkdir -p "$OPENCODE_SKILLS_DIR" "$OPENCODE_SKILLS_LEGACY"

while IFS= read -r prune_root; do
    prune_root="$(resolve_repo_path "$prune_root")"
    [[ -d "$prune_root" ]] || continue

    for skill_dir in "$prune_root"/*; do
        [[ -d "$skill_dir" ]] || continue

        skill_name="$(basename "$skill_dir")"
        target="$OPENCODE_SKILLS_DIR/$skill_name"

        if [[ -L "$target" ]]; then
            legacy="$OPENCODE_SKILLS_LEGACY/$skill_name"
            if [[ ! -e "$legacy" ]]; then
                mv "$target" "$legacy"
            else
                rm "$target"
            fi
        fi
    done
done < <(provider_cfg_list opencode prune_sources "$REPO_ROOT/configs/skills-condensed")

opencode_installed_any=0
while IFS= read -r source_root; do
    source_root="$(resolve_repo_path "$source_root")"
    [[ -d "$source_root" ]] || continue
    opencode_installed_any=1

    info "Linking OpenCode skills from $source_root"

    for skill_dir in "$source_root"/*; do
        [[ -d "$skill_dir" ]] || continue

        skill_name="$(basename "$skill_dir")"
        target="$OPENCODE_SKILLS_DIR/$skill_name"

        if [[ -L "$target" ]]; then
            rm "$target"
        elif [[ -e "$target" ]]; then
            warn "OpenCode skill already exists (skipping): $target"
            continue
        fi

        skill_dir_abs="$(cd "$skill_dir" && pwd)"
        ln -s "$skill_dir_abs" "$target"
    done
done < <(provider_cfg_list opencode sources "$REPO_ROOT/configs/skills-pack")

if [[ "$opencode_installed_any" == "1" ]]; then
    success "Installed foxctl OpenCode skills"
else
    warn "No OpenCode skills to install (sources not found)"
fi

echo -e "${BLUE}5b3. Setting up OpenCode agents...${NC}"

OPENCODE_AGENTS_DIR="$OPENCODE_CONFIG_DIR/agent"
OPENCODE_AGENTS_LEGACY="$OPENCODE_CONFIG_DIR/agent-legacy"
OPENCODE_AGENTS_SOURCE="$REPO_ROOT/configs/opencode/agents-pack"

mkdir -p "$OPENCODE_AGENTS_DIR" "$OPENCODE_AGENTS_LEGACY"

if [[ -d "$OPENCODE_AGENTS_SOURCE" ]]; then
    for p in "$OPENCODE_AGENTS_DIR"/*; do
        [[ -e "$p" ]] || continue

        base="$(basename "$p")"
        if [[ -L "$p" && "$base" != *.md ]]; then
            legacy="$OPENCODE_AGENTS_LEGACY/$base"
            if [[ ! -e "$legacy" ]]; then
                mv "$p" "$legacy"
            else
                rm "$p"
            fi
        fi
    done

    info "Linking OpenCode agents from $OPENCODE_AGENTS_SOURCE"

    for agent_file in "$OPENCODE_AGENTS_SOURCE"/*.md; do
        [[ -f "$agent_file" ]] || continue

        agent_name="$(basename "$agent_file")"
        target="$OPENCODE_AGENTS_DIR/$agent_name"

        if [[ -L "$target" ]]; then
            rm "$target"
        elif [[ -e "$target" ]]; then
            warn "OpenCode agent already exists (skipping): $target"
            continue
        fi

        agent_abs="$(cd "$(dirname "$agent_file")" && pwd)/$(basename "$agent_file")"
        ln -s "$agent_abs" "$target"
    done

    success "Installed foxctl OpenCode agents"
else
    warn "No OpenCode agents to install (configs/opencode/agents-pack not found)"
fi

echo ""

# 5c. Start MCP daemon
echo -e "${BLUE}5c. Starting MCP daemon...${NC}"

# Check if daemon is already running
if "$BIN_DIR/foxctl" mcp status 2>/dev/null | grep -q "running"; then
    success "MCP daemon already running"
else
    # Start the daemon with skills enabled
    if "$BIN_DIR/foxctl" mcp serve --daemon --skills 2>/dev/null; then
        success "MCP daemon started on http://localhost:8091"
        info "Claude Code and OpenCode will use SSE connection"
    else
        warn "Failed to start MCP daemon"
        echo "    Start manually: foxctl mcp serve --daemon --skills"
    fi
fi

# Note about SSE configuration
info "MCP configs updated to use SSE (http://localhost:8091/sse)"
info "Restart Claude Code/OpenCode to use the shared MCP server"
echo ""

echo -e "${BLUE}5d. Setting up Codex CLI integration...${NC}"

mkdir -p "$CODEX_DIR"
success "Created $CODEX_DIR"

CODEX_CONFIG="$CODEX_DIR/config.toml"
if [[ ! -f "$CODEX_CONFIG" ]]; then
    cat > "$CODEX_CONFIG" <<EOF
# Autogenerated by foxctl scripts/init.sh
#
# If you want MCP tools that use network (web search, etc) to work from Codex,
# set sandbox_workspace_write.network_access=true.

approval_policy = "on-failure"
sandbox_mode = "workspace-write"

[sandbox_workspace_write]
network_access = false

# Enable Codex built-in web search tool (optional)
# tools.web_search = true

[mcp_servers.foxctl]
command = "foxctl"
args = ["mcp", "serve", "--skills"]
EOF
    success "Created $CODEX_CONFIG"
else
    info "Codex config already exists at $CODEX_CONFIG"
    info "Ensure it contains [mcp_servers.foxctl] if you want foxctl MCP tools"
fi

CODEX_SKILLS_DIR="$(provider_cfg codex target_dir "$CODEX_DIR/skills")"
CODEX_SKILLS_LEGACY="$(provider_cfg codex legacy_dir "$CODEX_DIR/skills-legacy")"
mkdir -p "$CODEX_SKILLS_DIR" "$CODEX_SKILLS_LEGACY"

while IFS= read -r prune_root; do
    prune_root="$(resolve_repo_path "$prune_root")"
    [[ -d "$prune_root" ]] || continue

    for skill_dir in "$prune_root"/*; do
        [[ -d "$skill_dir" ]] || continue

        skill_name="$(basename "$skill_dir")"
        target="$CODEX_SKILLS_DIR/$skill_name"

        if [[ -L "$target" ]]; then
            legacy="$CODEX_SKILLS_LEGACY/$skill_name"
            if [[ ! -e "$legacy" ]]; then
                mv "$target" "$legacy"
            else
                rm "$target"
            fi
        fi
    done
done < <(provider_cfg_list codex prune_sources "$REPO_ROOT/configs/skills-condensed")

codex_installed_any=0
while IFS= read -r source_root; do
    source_root="$(resolve_repo_path "$source_root")"
    [[ -d "$source_root" ]] || continue
    codex_installed_any=1

    info "Linking Codex skills from $source_root"

    for skill_dir in "$source_root"/*; do
        [[ -d "$skill_dir" ]] || continue

        skill_name="$(basename "$skill_dir")"
        target="$CODEX_SKILLS_DIR/$skill_name"

        if [[ -L "$target" ]]; then
            rm "$target"
        elif [[ -e "$target" ]]; then
            warn "Codex skill already exists (skipping): $target"
            continue
        fi

        skill_dir_abs="$(cd "$skill_dir" && pwd)"
        ln -s "$skill_dir_abs" "$target"
    done
done < <(provider_cfg_list codex sources "$REPO_ROOT/configs/skills-pack")

if [[ "$codex_installed_any" == "1" ]]; then
    success "Installed foxctl Codex skills (restart Codex to load)"
else
    warn "No Codex skills to install (sources not found)"
fi

CODEX_AGENTS_SOURCE="$REPO_ROOT/configs/codex/AGENTS.md"
CODEX_AGENTS="$CODEX_DIR/AGENTS.md"

if [[ -f "$CODEX_AGENTS_SOURCE" ]]; then
    if [[ -L "$CODEX_AGENTS" ]]; then
        rm "$CODEX_AGENTS"
    elif [[ -e "$CODEX_AGENTS" ]]; then
        warn "Codex AGENTS.md already exists (skipping): $CODEX_AGENTS"
        warn "To use foxctl version: rm $CODEX_AGENTS"
    fi

    if [[ ! -e "$CODEX_AGENTS" ]]; then
        source_abs="$(cd "$(dirname "$CODEX_AGENTS_SOURCE")" && pwd)/$(basename "$CODEX_AGENTS_SOURCE")"
        ln -s "$source_abs" "$CODEX_AGENTS"
        success "Symlinked Codex AGENTS.md -> $CODEX_AGENTS"
    fi
else
    warn "Codex AGENTS.md source not found (skipping): $CODEX_AGENTS_SOURCE"
fi

echo ""

echo -e "${BLUE}5d. Setting up Gemini skills...${NC}"

GEMINI_SKILLS_DIR="$(provider_cfg gemini target_dir "$GEMINI_DIR/antigravity/skills")"
GEMINI_SKILLS_LEGACY="$(provider_cfg gemini legacy_dir "$GEMINI_DIR/antigravity/skills-legacy")"
mkdir -p "$GEMINI_SKILLS_DIR" "$GEMINI_SKILLS_LEGACY"

while IFS= read -r prune_root; do
    prune_root="$(resolve_repo_path "$prune_root")"
    [[ -d "$prune_root" ]] || continue

    for skill_dir in "$prune_root"/*; do
        [[ -d "$skill_dir" ]] || continue

        skill_name="$(basename "$skill_dir")"
        target="$GEMINI_SKILLS_DIR/$skill_name"

        if [[ -L "$target" ]]; then
            legacy="$GEMINI_SKILLS_LEGACY/$skill_name"
            if [[ ! -e "$legacy" ]]; then
                mv "$target" "$legacy"
            else
                rm "$target"
            fi
        fi
    done
done < <(provider_cfg_list gemini prune_sources "$REPO_ROOT/configs/skills-condensed")

gemini_installed_any=0
while IFS= read -r source_root; do
    source_root="$(resolve_repo_path "$source_root")"
    [[ -d "$source_root" ]] || continue
    gemini_installed_any=1

    info "Linking Gemini skills from $source_root"

    for skill_dir in "$source_root"/*; do
        [[ -d "$skill_dir" ]] || continue

        skill_name="$(basename "$skill_dir")"
        target="$GEMINI_SKILLS_DIR/$skill_name"

        if [[ -L "$target" ]]; then
            rm "$target"
        elif [[ -e "$target" ]]; then
            warn "Gemini skill already exists (skipping): $target"
            continue
        fi

        skill_dir_abs="$(cd "$skill_dir" && pwd)"
        ln -s "$skill_dir_abs" "$target"
    done
done < <(provider_cfg_list gemini sources "$REPO_ROOT/configs/skills-pack")

if [[ "$gemini_installed_any" == "1" ]]; then
    success "Installed foxctl Gemini skills (restart Gemini to load)"
else
    warn "No Gemini skills to install (sources not found)"
fi

echo ""

# 6. Validate .env
echo -e "${BLUE}6. Checking environment configuration...${NC}"

ENV_FILE="$FOXCTL_HOME/.env"
REPO_ENV="$REPO_ROOT/.env"

# Copy .env if it exists in repo but not in FOXCTL_HOME
if [[ -f "$REPO_ENV" && ! -f "$ENV_FILE" ]]; then
    cp "$REPO_ENV" "$ENV_FILE"
    success "Copied .env to $ENV_FILE"
elif [[ -f "$ENV_FILE" ]]; then
    success ".env exists at $ENV_FILE"
else
    warn "No .env file found"
    echo "    Create $ENV_FILE with required variables (see below)"
fi

# Check required variables
check_var() {
    local var_name="$1"
    local required="$2"
    local description="$3"

    # Source .env if it exists
    if [[ -f "$ENV_FILE" ]]; then
        # shellcheck disable=SC1090
        source "$ENV_FILE" 2>/dev/null || true
    fi

    local value="${!var_name:-}"

    if [[ -n "$value" ]]; then
        success "$var_name is set"
    elif [[ "$required" == "required" ]]; then
        error "$var_name not set ($description)"
    else
        info "$var_name not set ($description) - optional"
    fi
}

echo ""
echo "Embedding providers (at least one required for vector search):"
check_var "VOYAGE_API_KEY" "optional" "Voyage AI - recommended for code/text embeddings"
check_var "GEMINI_API_KEY" "optional" "Google Gemini - alternative embedding provider"
check_var "MISTRAL_API_KEY" "optional" "Mistral AI - alternative embedding provider"

echo ""
echo "Remote storage (optional, for cross-workspace search):"
check_var "TURSO_DATABASE_URL" "optional" "Turso database URL for remote vector search"
check_var "TURSO_AUTH_TOKEN" "optional" "Turso authentication token"

echo ""
echo "Observability (optional):"
check_var "FOXCTL_OBS_DIR" "optional" "Directory for foxcular events logging"
echo ""

# 7. Summary
echo -e "${BLUE}=== Summary ===${NC}"
echo ""

if [[ $ERRORS -gt 0 ]]; then
    echo -e "${RED}Errors: $ERRORS${NC}"
fi
if [[ $WARNINGS -gt 0 ]]; then
    echo -e "${YELLOW}Warnings: $WARNINGS${NC}"
fi

if [[ $ERRORS -eq 0 ]]; then
    success "foxctl initialized successfully!"
    echo ""
    echo "Next steps:"
    echo "  1. Verify installation: foxctl version"
    echo "  2. List skills: foxctl skills list"
    echo "  3. Configure .env at $ENV_FILE"
    echo "  4. Restart Claude Code/OpenCode to use shared MCP server"
    echo ""
    echo "MCP daemon commands:"
    echo "  foxctl mcp status  - Check if daemon is running"
    echo "  foxctl mcp stop    - Stop the daemon"
    echo "  foxctl mcp serve --daemon --skills  - Start daemon"
    echo ""
    echo "For Turso remote search (cross-workspace):"
    echo "  - Set TURSO_DATABASE_URL and TURSO_AUTH_TOKEN"
else
    error "Initialization completed with errors. Please fix and re-run."
    exit 1
fi
