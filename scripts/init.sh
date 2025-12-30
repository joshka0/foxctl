#!/usr/bin/env bash
# scripts/init.sh - Initialize agentctl system setup
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
AGENTCTL_HOME="${AGENTCTL_HOME:-$HOME/.agentctl}"
CLAUDE_DIR="$HOME/.claude"

echo -e "${BLUE}=== agentctl System Initialization ===${NC}"
echo ""

# Track status
WARNINGS=0
ERRORS=0

# Helper functions
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; ((WARNINGS++)); }
error() { echo -e "${RED}[ERROR]${NC} $1"; ((ERRORS++)); }

# 1. Check binaries exist
echo -e "${BLUE}1. Checking binaries...${NC}"
if [[ ! -f "$BIN_DIR/agentctl" ]]; then
    error "bin/agentctl not found. Run 'make build' first."
else
    success "bin/agentctl exists"
fi

if [[ ! -f "$BIN_DIR/agentctl-cgo" ]]; then
    warn "bin/agentctl-cgo not found. Run 'make build-cgo' for Turso support."
else
    success "bin/agentctl-cgo exists"
fi
echo ""

# 2. Create directories
echo -e "${BLUE}2. Creating directories...${NC}"
mkdir -p "$LOCAL_BIN"
success "Created $LOCAL_BIN"

mkdir -p "$AGENTCTL_HOME"/{storage,cache,cas,skills,jobs,observability/events,backups}
success "Created $AGENTCTL_HOME structure"

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

symlink_binary "$BIN_DIR/agentctl" "$LOCAL_BIN/agentctl" "agentctl"
symlink_binary "$BIN_DIR/agentctl-cgo" "$LOCAL_BIN/agentctl-cgo" "agentctl-cgo"
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

# Copy hooks if they exist
if [[ -d "$REPO_ROOT/.claude/hooks" ]]; then
    mkdir -p "$CLAUDE_DIR/hooks"
    # Note: Don't overwrite user customizations, just ensure structure exists
    success "Claude hooks directory ready at $CLAUDE_DIR/hooks"
else
    info "No hooks to copy (repo .claude/hooks not found)"
fi

# Copy skills manifest reference
if [[ -d "$REPO_ROOT/.claude/skills" ]]; then
    mkdir -p "$CLAUDE_DIR/skills"
    success "Claude skills directory ready at $CLAUDE_DIR/skills"
fi
echo ""

# 6. Validate .env
echo -e "${BLUE}6. Checking environment configuration...${NC}"

ENV_FILE="$AGENTCTL_HOME/.env"
REPO_ENV="$REPO_ROOT/.env"

# Copy .env if it exists in repo but not in AGENTCTL_HOME
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
check_var "AGENTCTL_OBS_DIR" "optional" "Directory for wide events logging"
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
    success "agentctl initialized successfully!"
    echo ""
    echo "Next steps:"
    echo "  1. Verify installation: agentctl version"
    echo "  2. List skills: agentctl skills list"
    echo "  3. Configure .env at $ENV_FILE"
    echo ""
    echo "For Turso remote search (cross-workspace):"
    echo "  - Use agentctl-cgo binary (built with CGO)"
    echo "  - Set TURSO_DATABASE_URL and TURSO_AUTH_TOKEN"
else
    error "Initialization completed with errors. Please fix and re-run."
    exit 1
fi
