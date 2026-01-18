#!/usr/bin/env bash
# install.sh - One-step agentctl installation
#
# Usage:
#   From repo:     ./install.sh
#   Standalone:    curl -fsSL https://raw.githubusercontent.com/jkatigb/agentctl/main/install.sh | bash
#
# Options:
#   --provider <name>   Install for specific provider (claude-code, opencode, codex, all)
#   --skip-hooks        Skip hooks installation
#   --skip-skills       Skip skills installation
#   --no-build          Skip building (use pre-built or existing binary)
#   --help              Show this help
#
# Environment:
#   AGENTCTL_HOME       Installation directory (default: ~/.agentctl)
#   AGENTCTL_PROVIDER   Default provider (default: auto-detect)

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Logging
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; }
step() { echo -e "\n${CYAN}==> $1${NC}"; }

# Defaults
AGENTCTL_HOME="${AGENTCTL_HOME:-$HOME/.agentctl}"
LOCAL_BIN="${HOME}/.local/bin"
PROVIDER="${AGENTCTL_PROVIDER:-auto}"
SKIP_HOOKS=false
SKIP_SKILLS=false
NO_BUILD=false
REPO_URL="https://github.com/jkatigb/agentctl"

# Detect if running from repo
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "$SCRIPT_DIR/Makefile" && -d "$SCRIPT_DIR/configs" ]]; then
    REPO_ROOT="$SCRIPT_DIR"
    FROM_REPO=true
else
    REPO_ROOT=""
    FROM_REPO=false
fi

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --provider)
            PROVIDER="$2"
            shift 2
            ;;
        --skip-hooks)
            SKIP_HOOKS=true
            shift
            ;;
        --skip-skills)
            SKIP_SKILLS=true
            shift
            ;;
        --no-build)
            NO_BUILD=true
            shift
            ;;
        --help|-h)
            head -17 "$0" | tail -15
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# =============================================================================
# Helper Functions
# =============================================================================

detect_providers() {
    local providers=()

    # Claude Code
    if [[ -d "$HOME/.claude" ]] || command -v claude &>/dev/null; then
        providers+=("claude-code")
    fi

    # OpenCode
    if [[ -d "$HOME/.opencode" ]] || command -v opencode &>/dev/null; then
        providers+=("opencode")
    fi

    # Codex (OpenAI)
    if [[ -d "$HOME/.codex" ]] || command -v codex &>/dev/null; then
        providers+=("codex")
    fi

    # Default to claude-code if nothing detected
    if [[ ${#providers[@]} -eq 0 ]]; then
        providers+=("claude-code")
    fi

    echo "${providers[*]}"
}

check_dependencies() {
    local missing=()

    for cmd in jq git; do
        if ! command -v "$cmd" &>/dev/null; then
            missing+=("$cmd")
        fi
    done

    if [[ ${#missing[@]} -gt 0 ]]; then
        error "Missing dependencies: ${missing[*]}"
        info "Install with: brew install ${missing[*]} (macOS) or apt install ${missing[*]} (Linux)"
        exit 1
    fi

    # Check for Go if building from source
    if [[ "$FROM_REPO" == true && "$NO_BUILD" == false ]]; then
        if ! command -v go &>/dev/null; then
            warn "Go not found - will try to use pre-built binary"
            NO_BUILD=true
        fi
    fi
}

create_directories() {
    local dirs=(
        "$AGENTCTL_HOME"
        "$AGENTCTL_HOME/storage"
        "$AGENTCTL_HOME/skills"
        "$AGENTCTL_HOME/cache"
        "$AGENTCTL_HOME/cas"
        "$LOCAL_BIN"
    )

    for dir in "${dirs[@]}"; do
        if [[ ! -d "$dir" ]]; then
            mkdir -p "$dir"
            success "Created $dir"
        fi
    done
}

# =============================================================================
# Build/Download
# =============================================================================

build_from_repo() {
    if [[ "$NO_BUILD" == true ]]; then
        info "Skipping build (--no-build)"
        return
    fi

    step "Building agentctl from source"
    cd "$REPO_ROOT"

    # Try CGO build first, fall back to pure Go
    if make build-cgo 2>/dev/null; then
        success "Built agentctl with CGO"
    elif make build 2>/dev/null; then
        success "Built agentctl (pure Go)"
    else
        error "Build failed"
        exit 1
    fi

    # Symlink to local bin
    local binary="$REPO_ROOT/bin/agentctl"
    if [[ -f "${binary}-cgo" ]]; then
        binary="${binary}-cgo"
    fi

    if [[ -f "$binary" ]]; then
        ln -sf "$binary" "$LOCAL_BIN/agentctl"
        success "Linked agentctl to $LOCAL_BIN/agentctl"
    fi
}

download_binary() {
    step "Downloading agentctl binary"

    local os arch url
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$arch" in
        x86_64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
    esac

    # TODO: Replace with actual release URL when available
    url="$REPO_URL/releases/latest/download/agentctl-${os}-${arch}"

    info "Downloading from $url"
    if curl -fsSL "$url" -o "$LOCAL_BIN/agentctl"; then
        chmod +x "$LOCAL_BIN/agentctl"
        success "Downloaded agentctl to $LOCAL_BIN/agentctl"
    else
        error "Download failed - try installing from source"
        info "  git clone $REPO_URL && cd agentctl && ./install.sh"
        exit 1
    fi
}

# =============================================================================
# Provider-Specific Hooks Installation
# =============================================================================

install_claude_code_hooks() {
    step "Installing Claude Code hooks"

    local claude_dir="$HOME/.claude"
    local hooks_dir="$claude_dir/hooks/agentctl"

    # Create directories
    mkdir -p "$hooks_dir"
    mkdir -p "$claude_dir/commands"

    if [[ "$FROM_REPO" == true ]]; then
        # Symlink hooks from repo
        local hooks_source="$REPO_ROOT/configs/hooks"
        if [[ -d "$hooks_source" ]]; then
            # Resolve to absolute paths to prevent symlink loops
            local abs_source=$(cd "$hooks_source" && pwd)
            local abs_target=$(cd "$hooks_dir" && pwd)
            
            # Safety check: source and target must be different
            if [[ "$abs_source" == "$abs_target" ]]; then
                warn "Source and target directories are the same, skipping symlinks"
            else
                for hook in "$hooks_source"/*.sh; do
                    if [[ -f "$hook" && ! -L "$hook" ]]; then
                        # Only symlink regular files, not existing symlinks
                        local name=$(basename "$hook")
                        local target_path="$hooks_dir/$name"
                        local source_path=$(cd "$(dirname "$hook")" && pwd)/$(basename "$hook")
                        
                        # Verify we're not creating a self-referential symlink
                        if [[ "$source_path" != "$target_path" ]]; then
                            ln -sf "$source_path" "$target_path"
                        fi
                    fi
                done
                success "Linked hooks to $hooks_dir"
            fi
        fi

        # Merge settings.json
        local settings_source="$REPO_ROOT/configs/claude-settings.json"
        local settings_target="$claude_dir/settings.json"

        if [[ -f "$settings_source" ]]; then
            if [[ -f "$settings_target" ]]; then
                # Merge hooks from source into target
                info "Merging hooks into existing settings.json"
                local merged
                merged=$(jq -s '.[0] + {hooks: ((.[0].hooks // {}) + (.[1].hooks // {}))}' "$settings_target" "$settings_source")
                echo "$merged" > "$settings_target"
                success "Merged hooks configuration"
            else
                cp "$settings_source" "$settings_target"
                success "Installed settings.json"
            fi
        fi

        # Link CLAUDE.md if it exists
        local claude_md="$REPO_ROOT/.claude/CLAUDE.md"
        if [[ -f "$claude_md" ]]; then
            mkdir -p "$claude_dir/projects"
            # This is project-specific, handled elsewhere
        fi
    else
        # Standalone mode - download hooks
        info "Downloading hooks configuration..."
        curl -fsSL "$REPO_URL/raw/main/configs/claude-settings.json" -o "$claude_dir/settings.json.new"

        if [[ -f "$claude_dir/settings.json" ]]; then
            jq -s '.[0] * .[1]' "$claude_dir/settings.json" "$claude_dir/settings.json.new" > "$claude_dir/settings.json.merged"
            mv "$claude_dir/settings.json.merged" "$claude_dir/settings.json"
            rm "$claude_dir/settings.json.new"
        else
            mv "$claude_dir/settings.json.new" "$claude_dir/settings.json"
        fi

        # Download individual hook scripts
        local hooks=(
            "agentctl-mode-enforce.sh"
            "semantic-search.sh"
            "file-memory-recall.sh"
            "session-restore.sh"
        )

        for hook in "${hooks[@]}"; do
            curl -fsSL "$REPO_URL/raw/main/configs/hooks/$hook" -o "$hooks_dir/$hook"
            chmod +x "$hooks_dir/$hook"
        done
        success "Downloaded hook scripts"
    fi
}

install_opencode_hooks() {
    step "Installing OpenCode hooks"

    local opencode_dir="$HOME/.opencode"
    local plugins_dir="$opencode_dir/plugins"

    mkdir -p "$plugins_dir"

    if [[ "$FROM_REPO" == true ]]; then
        local source_dir="$REPO_ROOT/configs/opencode-hooks"

        if [[ -d "$source_dir" ]]; then
            # Check for bun/npm
            if command -v bun &>/dev/null; then
                info "Building OpenCode plugin with bun..."
                cd "$source_dir"
                bun install
                bun run build

                # Link the built plugin
                ln -sf "$source_dir" "$plugins_dir/agentctl"
                success "Installed OpenCode plugin"
            elif command -v npm &>/dev/null; then
                info "Building OpenCode plugin with npm..."
                cd "$source_dir"
                npm install
                npm run build
                ln -sf "$source_dir" "$plugins_dir/agentctl"
                success "Installed OpenCode plugin"
            else
                warn "Neither bun nor npm found - skipping OpenCode plugin build"
                warn "Install bun (https://bun.sh) and run: cd $source_dir && bun install && bun run build"
            fi
        fi
    else
        warn "OpenCode standalone installation not yet supported"
        info "Clone the repo and run ./install.sh for OpenCode support"
    fi
}

install_codex_hooks() {
    step "Installing Codex configuration"

    local codex_dir="$HOME/.codex"
    mkdir -p "$codex_dir"

    if [[ "$FROM_REPO" == true ]]; then
        local agents_source="$REPO_ROOT/configs/codex/AGENTS.md"
        if [[ -f "$agents_source" ]]; then
            cp "$agents_source" "$codex_dir/AGENTS.md"
            success "Installed AGENTS.md for Codex"
        fi
    else
        info "Downloading Codex AGENTS.md..."
        curl -fsSL "$REPO_URL/raw/main/configs/codex/AGENTS.md" -o "$codex_dir/AGENTS.md"
        success "Downloaded AGENTS.md"
    fi

    info "Note: Codex has limited hook support - using AGENTS.md for guidance"
}

# =============================================================================
# Skills Installation
# =============================================================================

install_skills() {
    if [[ "$SKIP_SKILLS" == true ]]; then
        info "Skipping skills installation (--skip-skills)"
        return
    fi

    step "Installing skills"

    if [[ "$FROM_REPO" == true ]]; then
        cd "$REPO_ROOT"
        if make skills-install-all 2>/dev/null; then
            success "Installed all skills"
        else
            warn "Skills installation failed - you can install manually with: make skills-install-all"
        fi
    else
        info "Skills will be installed on first use via agentctl"
    fi
}

# =============================================================================
# Validation
# =============================================================================

validate_installation() {
    step "Validating installation"

    local errors=0

    # Check binary
    if command -v agentctl &>/dev/null; then
        success "agentctl binary found: $(which agentctl)"
        info "Version: $(agentctl --version 2>/dev/null || echo 'unknown')"
    else
        error "agentctl binary not found in PATH"
        info "Add $LOCAL_BIN to your PATH: export PATH=\"\$PATH:$LOCAL_BIN\""
        ((errors++))
    fi

    # Check directories
    for dir in "$AGENTCTL_HOME" "$AGENTCTL_HOME/storage" "$AGENTCTL_HOME/skills"; do
        if [[ -d "$dir" ]]; then
            success "Directory exists: $dir"
        else
            error "Missing directory: $dir"
            ((errors++))
        fi
    done

    # Check environment variables
    echo ""
    info "Checking environment variables..."

    local env_file="$AGENTCTL_HOME/.env"
    [[ -f "$HOME/.env" ]] && env_file="$HOME/.env"
    [[ -f "$REPO_ROOT/.env" ]] && env_file="$REPO_ROOT/.env"

    if [[ -f "$env_file" ]]; then
        source "$env_file" 2>/dev/null || true
    fi

    if [[ -n "${VOYAGE_API_KEY:-}" ]]; then
        success "VOYAGE_API_KEY is set (recommended for embeddings)"
    else
        warn "VOYAGE_API_KEY not set - semantic search will be limited"
        info "Get a key at https://www.voyageai.com/"
    fi

    return $errors
}

# =============================================================================
# Main
# =============================================================================

main() {
    echo -e "${CYAN}"
    echo "╔═══════════════════════════════════════════════════════════════╗"
    echo "║                    agentctl installer                         ║"
    echo "╚═══════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"

    if [[ "$FROM_REPO" == true ]]; then
        info "Installing from repository: $REPO_ROOT"
    else
        info "Standalone installation mode"
    fi

    # Pre-flight checks
    step "Checking dependencies"
    check_dependencies

    # Create directories
    step "Creating directories"
    create_directories

    # Build or download
    if [[ "$FROM_REPO" == true ]]; then
        build_from_repo
    else
        download_binary
    fi

    # Install hooks based on provider
    if [[ "$SKIP_HOOKS" == false ]]; then
        if [[ "$PROVIDER" == "auto" ]]; then
            PROVIDER=$(detect_providers)
        fi

        info "Installing hooks for: $PROVIDER"

        for p in $PROVIDER; do
            case "$p" in
                claude-code|claude)
                    install_claude_code_hooks
                    ;;
                opencode)
                    install_opencode_hooks
                    ;;
                codex)
                    install_codex_hooks
                    ;;
                all)
                    install_claude_code_hooks
                    install_opencode_hooks
                    install_codex_hooks
                    ;;
                *)
                    warn "Unknown provider: $p"
                    ;;
            esac
        done
    fi

    # Install skills
    install_skills

    # Validate
    validate_installation

    # Summary
    echo ""
    echo -e "${GREEN}════════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}  Installation complete!${NC}"
    echo -e "${GREEN}════════════════════════════════════════════════════════════════${NC}"
    echo ""
    echo "Next steps:"
    echo "  1. Ensure $LOCAL_BIN is in your PATH"
    echo "  2. Set VOYAGE_API_KEY for semantic search (optional)"
    echo "  3. Start your AI coding assistant (Claude Code, OpenCode, etc.)"
    echo ""
    echo "Quick test:"
    echo "  agentctl --version"
    echo "  agentctl skills list"
    echo ""
}

main "$@"
