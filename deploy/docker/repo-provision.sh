#!/bin/sh
# Repo provisioning script for agent-runner sandbox pods.
#
# Runs on pod startup to clone the target repo, checkout the right branch,
# and set up git credentials. Configured via environment variables:
#
#   GIT_REPO_URL    - git clone URL (https or ssh)
#   GIT_BRANCH      - branch to checkout (default: main)
#   GIT_DEPLOY_KEY  - SSH deploy key content (base64-encoded, for ssh repos)
#   GIT_TOKEN       - HTTPS token (for https repos with token auth)
#
# After provisioning, the repo is available at /workspace/repo.

set -e

BRANCH="${GIT_BRANCH:-main}"
WORKDIR="/workspace/repo"

echo "[provision] Starting repo provisioning..."
echo "[provision] Repo: $GIT_REPO_URL"
echo "[provision] Branch: $BRANCH"

# Set up SSH deploy key if provided
if [ -n "$GIT_DEPLOY_KEY" ]; then
    echo "[provision] Setting up SSH deploy key..."
    mkdir -p ~/.ssh
    echo "$GIT_DEPLOY_KEY" | base64 -d > ~/.ssh/deploy_key
    chmod 600 ~/.ssh/deploy_key
    cat > ~/.ssh/config << 'SSHEOF'
Host github.com
    HostName github.com
    User git
    IdentityFile ~/.ssh/deploy_key
    StrictHostKeyChecking no
Host gitlab.com
    HostName gitlab.com
    User git
    IdentityFile ~/.ssh/deploy_key
    StrictHostKeyChecking no
SSHEOF
    ssh-keyscan github.com gitlab.com >> ~/.ssh/known_hosts 2>/dev/null || true
    echo "[provision] SSH deploy key configured."
fi

# Set up HTTPS token if provided
if [ -n "$GIT_TOKEN" ]; then
    echo "[provision] Configuring HTTPS token auth..."
    git config --global credential.helper store
    # Extract host from URL for token auth
    echo "[provision] HTTPS token configured."
fi

# Clone the repo
if [ -d "$WORKDIR/.git" ]; then
    echo "[provision] Repo already cloned, pulling latest..."
    cd "$WORKDIR"
    git fetch origin
    git checkout "$BRANCH" || true
    git pull origin "$BRANCH" || true
else
    echo "[provision] Cloning repo..."
    git clone --depth 1 -b "$BRANCH" "$GIT_REPO_URL" "$WORKDIR" || {
        echo "[provision] Branch clone failed, cloning default and checking out..."
        git clone --depth 1 "$GIT_REPO_URL" "$WORKDIR"
        cd "$WORKDIR"
        git fetch --unshallow || true
        git checkout "$BRANCH" || true
    }
fi

cd "$WORKDIR"
echo "[provision] Repo ready at $WORKDIR"
echo "[provision] HEAD: $(git rev-parse --short HEAD)"
echo "[provision] Branch: $(git branch --show-current)"
echo "[provision] Done."
