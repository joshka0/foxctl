#!/usr/bin/env bash

set -euo pipefail

readonly REPO_URL="https://gitlab.com/joshka0/agentctl.git"
readonly GO_VERSION_REQUIRED="1.26.1"
readonly DEFAULT_REPO_DIR="${HOME}/.agentctl/src/agentctl"
readonly AGENTCTL_HOME="${AGENTCTL_HOME:-$HOME/.agentctl}"
readonly LOCAL_BIN="${HOME}/.local/bin"

RED=$'\033[0;31m'
GREEN=$'\033[0;32m'
YELLOW=$'\033[1;33m'
BLUE=$'\033[0;34m'
CYAN=$'\033[0;36m'
NC=$'\033[0m'

ASSUME_YES=false
SKIP_CGO=false
SKIP_BUN=false
SKIP_PROVIDER_SETUP=false
REPO_DIR="${AGENTCTL_REPO_DIR:-}"

info() { printf "%s[INFO]%s %s\n" "$BLUE" "$NC" "$1"; }
success() { printf "%s[OK]%s %s\n" "$GREEN" "$NC" "$1"; }
warn() { printf "%s[WARN]%s %s\n" "$YELLOW" "$NC" "$1" >&2; }
error() { printf "%s[ERROR]%s %s\n" "$RED" "$NC" "$1" >&2; }
step() { printf "\n%s==> %s%s\n" "$CYAN" "$1" "$NC"; }

show_help() {
    cat <<'EOF'
agentctl installer

Usage:
  ./install.sh
  ./install.sh --yes
  curl -fsSL https://gitlab.com/joshka0/agentctl/-/raw/main/install.sh | bash

Options:
  --yes, -y               Run non-interactively with recommended defaults
  --skip-cgo              Skip CGO/SQLite support and agentctl-cgo
  --skip-bun              Skip Bun installation and bun install
  --skip-provider-setup   Skip scripts/init.sh
  --repo-dir <path>       Repo checkout directory when not run from a clone
  --help, -h              Show this help

Environment:
  AGENTCTL_HOME           Defaults to ~/.agentctl
  AGENTCTL_REPO_DIR       Default checkout dir when cloning outside a repo
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --yes|-y)
            ASSUME_YES=true
            shift
            ;;
        --skip-cgo)
            SKIP_CGO=true
            shift
            ;;
        --skip-bun)
            SKIP_BUN=true
            shift
            ;;
        --skip-provider-setup)
            SKIP_PROVIDER_SETUP=true
            shift
            ;;
        --repo-dir)
            if [[ -z "${2:-}" ]]; then
                error "--repo-dir requires a path"
                exit 1
            fi
            REPO_DIR="$2"
            shift 2
            ;;
        --help|-h)
            show_help
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            exit 1
            ;;
    esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "$SCRIPT_DIR/Makefile" && -d "$SCRIPT_DIR/configs" ]]; then
    FROM_REPO=true
    REPO_ROOT="$SCRIPT_DIR"
else
    FROM_REPO=false
    REPO_ROOT=""
fi

is_tty() {
    [[ -t 0 && -t 1 ]]
}

confirm() {
    local prompt="$1"
    local default="${2:-y}"
    local reply=""

    if [[ "$ASSUME_YES" == true ]]; then
        [[ "$default" == "y" ]]
        return
    fi

    if ! is_tty; then
        [[ "$default" == "y" ]]
        return
    fi

    local suffix="[y/N]"
    if [[ "$default" == "y" ]]; then
        suffix="[Y/n]"
    fi

    read -r -p "$prompt $suffix " reply
    reply="${reply:-$default}"
    [[ "$reply" =~ ^[Yy]$ ]]
}

prompt_install_options() {
    if [[ "$ASSUME_YES" == true ]]; then
        return
    fi
    if ! is_tty; then
        return
    fi

    printf "%s\n" "Recommended install profile:"
    printf "%s\n" "- build the pure-Go CLI and skills"
    printf "%s\n" "- build agentctl-cgo with SQLite/libsqlite3 support"
    printf "%s\n" "- install Bun for GUI/TUI/OpenCode workflows"
    printf "%s\n" "- run provider setup for Claude/Codex/OpenCode/Gemini"
    printf "\n"

    if ! confirm "Install CGO/SQLite support?" "y"; then
        SKIP_CGO=true
    fi
    if ! confirm "Install Bun and bootstrap JS workspaces?" "y"; then
        SKIP_BUN=true
    fi
    if ! confirm "Run provider setup via scripts/init.sh?" "y"; then
        SKIP_PROVIDER_SETUP=true
    fi
}

detect_os() {
    case "$(uname -s)" in
        Darwin) echo "darwin" ;;
        Linux) echo "linux" ;;
        *) echo "unknown" ;;
    esac
}

version_ge() {
    local left="$1"
    local right="$2"
    local IFS=.
    local left_parts=()
    local right_parts=()
    local count=0
    local i=0

    read -r -a left_parts <<< "$left"
    read -r -a right_parts <<< "$right"

    count="${#left_parts[@]}"
    if (( ${#right_parts[@]} > count )); then
        count="${#right_parts[@]}"
    fi

    for ((i = 0; i < count; i++)); do
        local left_part="${left_parts[i]:-0}"
        local right_part="${right_parts[i]:-0}"
        if (( 10#$left_part > 10#$right_part )); then
            return 0
        fi
        if (( 10#$left_part < 10#$right_part )); then
            return 1
        fi
    done

    return 0
}

detect_pkg_manager() {
    if command -v brew >/dev/null 2>&1; then
        echo "brew"
    elif command -v apt-get >/dev/null 2>&1; then
        echo "apt-get"
    elif command -v dnf >/dev/null 2>&1; then
        echo "dnf"
    elif command -v yum >/dev/null 2>&1; then
        echo "yum"
    elif command -v pacman >/dev/null 2>&1; then
        echo "pacman"
    else
        echo "unknown"
    fi
}

go_version_ok() {
    if ! command -v go >/dev/null 2>&1; then
        return 1
    fi

    local current
    current="$(go version | awk '{print $3}' | sed 's/^go//')"
    [[ -n "$current" ]] || return 1

    version_ge "$current" "$GO_VERSION_REQUIRED"
}

need_cmd() {
    command -v "$1" >/dev/null 2>&1
}

run_with_sudo() {
    if [[ "$(id -u)" -eq 0 ]]; then
        "$@"
    else
        sudo "$@"
    fi
}

install_core_packages() {
    local pkg_manager="$1"
    local want_bun="$2"
    local want_cgo="$3"

    case "$pkg_manager" in
        brew)
            local packages=(git make jq go)
            if [[ "$want_cgo" == "true" ]]; then
                packages+=(sqlite)
            fi
            info "Installing core packages with Homebrew: ${packages[*]}"
            brew install "${packages[@]}"
            if [[ "$want_bun" == "true" ]] && ! need_cmd bun; then
                info "Installing Bun with Homebrew"
                brew install oven-sh/bun/bun
            fi
            ;;
        apt-get)
            info "Installing core packages with apt-get"
            run_with_sudo apt-get update
            local packages=(git make jq curl golang-go)
            if [[ "$want_cgo" == "true" ]]; then
                packages+=(build-essential pkg-config libsqlite3-dev)
            fi
            run_with_sudo apt-get install -y "${packages[@]}"
            ;;
        dnf)
            info "Installing core packages with dnf"
            local packages=(git make jq curl golang)
            if [[ "$want_cgo" == "true" ]]; then
                packages+=(gcc gcc-c++ make pkgconf-pkg-config sqlite-devel)
            fi
            run_with_sudo dnf install -y "${packages[@]}"
            ;;
        yum)
            info "Installing core packages with yum"
            local packages=(git make jq curl golang)
            if [[ "$want_cgo" == "true" ]]; then
                packages+=(gcc gcc-c++ make pkgconfig sqlite-devel)
            fi
            run_with_sudo yum install -y "${packages[@]}"
            ;;
        pacman)
            info "Installing core packages with pacman"
            local packages=(git make jq curl go)
            if [[ "$want_cgo" == "true" ]]; then
                packages+=(base-devel pkgconf sqlite)
            fi
            run_with_sudo pacman -Sy --noconfirm "${packages[@]}"
            ;;
        *)
            warn "No supported package manager found. Install git, make, jq, and Go ${GO_VERSION_REQUIRED}+ manually."
            if [[ "$want_cgo" == "true" ]]; then
                warn "Also install a C toolchain and SQLite development headers for CGO support."
            fi
            ;;
    esac
}

install_bun_if_needed() {
    local pkg_manager="$1"
    if [[ "$SKIP_BUN" == true ]]; then
        return
    fi
    if need_cmd bun; then
        return
    fi

    if ! confirm "Bun is missing. Install it now?" "y"; then
        warn "Skipping Bun install. GUI/TUI/OpenCode plugin setup will be limited."
        SKIP_BUN=true
        return
    fi

    case "$pkg_manager" in
        brew)
            brew install oven-sh/bun/bun
            ;;
        *)
            if need_cmd curl; then
                info "Installing Bun via the official install script"
                curl -fsSL https://bun.sh/install | bash
                export PATH="${HOME}/.bun/bin:${PATH}"
            else
                warn "curl is required to install Bun automatically. Install Bun manually and re-run if needed."
                SKIP_BUN=true
            fi
            ;;
    esac
}

ensure_dependencies() {
    local pkg_manager
    pkg_manager="$(detect_pkg_manager)"
    local want_bun="true"
    local want_cgo="true"

    if [[ "$SKIP_BUN" == true ]]; then
        want_bun="false"
    fi
    if [[ "$SKIP_CGO" == true ]]; then
        want_cgo="false"
    fi

    local missing_core=false
    if ! need_cmd git || ! need_cmd make || ! need_cmd jq || ! go_version_ok; then
        missing_core=true
    fi

    if [[ "$missing_core" == true ]]; then
        step "Installing core dependencies"
        info "Package manager: $pkg_manager"
        if confirm "Install or update the required toolchain packages?" "y"; then
            install_core_packages "$pkg_manager" "$want_bun" "$want_cgo"
        else
            warn "Skipping automatic dependency installation."
        fi
    fi

    if ! need_cmd git; then
        error "git is required"
        exit 1
    fi
    if ! need_cmd make; then
        error "make is required"
        exit 1
    fi
    if ! need_cmd jq; then
        error "jq is required"
        exit 1
    fi
    if ! go_version_ok; then
        error "Go ${GO_VERSION_REQUIRED}+ is required"
        exit 1
    fi

    install_bun_if_needed "$pkg_manager"
}

prepare_repo() {
    if [[ "$FROM_REPO" == true ]]; then
        REPO_ROOT="$SCRIPT_DIR"
        success "Using current repository checkout: $REPO_ROOT"
        return
    fi

    REPO_ROOT="${REPO_DIR:-$DEFAULT_REPO_DIR}"
    mkdir -p "$(dirname "$REPO_ROOT")"

    step "Preparing repository checkout"
    if [[ -d "$REPO_ROOT/.git" ]]; then
        info "Updating existing checkout at $REPO_ROOT"
        git -C "$REPO_ROOT" fetch --depth 1 origin main
        git -C "$REPO_ROOT" checkout main
        git -C "$REPO_ROOT" pull --ff-only origin main
    else
        info "Cloning $REPO_URL into $REPO_ROOT"
        git clone --depth 1 "$REPO_URL" "$REPO_ROOT"
    fi
}

ensure_local_layout() {
    mkdir -p "$LOCAL_BIN" "$AGENTCTL_HOME"
    mkdir -p "$AGENTCTL_HOME"/{storage,cache,cas,skills,jobs,observability/events,backups}
    export PATH="$LOCAL_BIN:$PATH"
}

build_agentctl() {
    step "Building agentctl"
    cd "$REPO_ROOT"

    make build
    if [[ "$SKIP_CGO" == false ]]; then
        make build-cgo
    fi
}

install_skills() {
    step "Installing skills"
    cd "$REPO_ROOT"

    make skills-install
    if [[ "$SKIP_CGO" == false ]]; then
        make skills-install-cgo
    fi
}

link_binaries() {
    step "Linking binaries into $LOCAL_BIN"
    cd "$REPO_ROOT"

    ln -sf "$REPO_ROOT/bin/agentctl" "$LOCAL_BIN/agentctl"
    if [[ -f "$REPO_ROOT/bin/agentctl-cgo" ]]; then
        ln -sf "$REPO_ROOT/bin/agentctl-cgo" "$LOCAL_BIN/agentctl-cgo"
    fi
    if [[ -f "$REPO_ROOT/bin/agentctl-mail" ]]; then
        ln -sf "$REPO_ROOT/bin/agentctl-mail" "$LOCAL_BIN/agentctl-mail"
    fi
}

bootstrap_bun_workspace() {
    if [[ "$SKIP_BUN" == true ]]; then
        info "Skipping Bun workspace bootstrap"
        return
    fi
    if ! need_cmd bun; then
        warn "Bun is not available; skipping bun install"
        return
    fi

    step "Installing Bun workspace dependencies"
    cd "$REPO_ROOT"
    bun install
}

run_provider_setup() {
    if [[ "$SKIP_PROVIDER_SETUP" == true ]]; then
        info "Skipping provider setup"
        return
    fi

    step "Running provider bootstrap"
    cd "$REPO_ROOT"
    ./scripts/init.sh
}

print_summary() {
    printf "\n"
    success "agentctl installation finished"
    printf "\n"
    printf "%s\n" "Repo:        $REPO_ROOT"
    printf "%s\n" "AGENTCTL_HOME: $AGENTCTL_HOME"
    printf "%s\n" "PATH link:   $LOCAL_BIN/agentctl"
    printf "\n"
    printf "%s\n" "Next steps:"
    printf "%s\n" "  1. Ensure $LOCAL_BIN is in your PATH"
    printf "%s\n" "  2. Run 'agentctl version'"
    printf "%s\n" "  3. Run 'agentctl skills list'"
    printf "%s\n" "  4. Add API keys to ~/.agentctl/.env as needed"
    if [[ "$SKIP_PROVIDER_SETUP" == false ]]; then
        printf "%s\n" "  5. Restart Claude/Codex/OpenCode/Gemini if you want them to pick up new config"
    fi
}

main() {
    printf "%s" "$CYAN"
    printf "╔═══════════════════════════════════════════════════════════════╗\n"
    printf "║                    agentctl installer                        ║\n"
    printf "╚═══════════════════════════════════════════════════════════════╝\n"
    printf "%s" "$NC"
    printf "\n"

    prompt_install_options
    ensure_dependencies
    prepare_repo
    ensure_local_layout
    build_agentctl
    install_skills
    link_binaries
    bootstrap_bun_workspace
    run_provider_setup
    print_summary
}

main "$@"
