# Troubleshooting Guide

This guide helps you diagnose and resolve common issues with agentctl.

---

## Table of Contents

- [General Troubleshooting](#general-troubleshooting)
- [Installation Issues](#installation-issues)
- [Build Issues](#build-issues)
- [Skill Execution Issues](#skill-execution-issues)
- [Job Issues](#job-issues)
- [Memory and Cache Issues](#memory-and-cache-issues)
- [CAS (Content-Addressable Storage) Issues](#cas-content-addressable-storage-issues)
- [OpenAPI Skill Issues](#openapi-skill-issues)
- [Performance Issues](#performance-issues)
- [Database Issues](#database-issues)
- [Platform-Specific Issues](#platform-specific-issues)
- [Getting Help](#getting-help)

---

## General Troubleshooting

### Check Version and Environment

```bash
# Check agentctl version
agentctl version

# Check Go version (for building from source)
go version

# Check environment
agentctl config show

# Check logs (stderr output)
agentctl run fs/ls --path . 2>debug.log
```

### Enable Verbose Logging

```bash
# Set log level to debug
export AGENTCTL_LOG_LEVEL=debug
agentctl run <skill> 2>debug.log

# Or use config file
cat > ~/.agentctl/config.yaml <<EOF
log:
  level: debug
  format: json
EOF
```

### Common Error Codes

| Code | Meaning | Common Causes |
|------|---------|---------------|
| `EARG` | Invalid argument | Missing/malformed parameters |
| `ENOTFOUND` | Resource not found | Skill, file, or digest doesn't exist |
| `ETIMEOUT` | Operation timed out | Network issues, long-running skill |
| `ERUNTIME` | Runtime error | Skill crashed or panicked |
| `EENVELOPE` | Envelope validation failed | Malformed JSON, missing fields |
| `EPARSE` | Parse error | Invalid input format |
| `EPOLICY` | Policy violation | Path traversal, network access denied |
| `EIO` | I/O error | File access, CAS corruption |
| `EAUTH` | Authentication failed | Invalid credentials |
| `ERATELIMIT` | Rate limit exceeded | Too many API calls |
| `ECANCELED` | Operation canceled | User cancellation, context timeout |

---

## Installation Issues

### Issue: `agentctl: command not found`

**Cause**: Binary not in PATH or not installed.

**Solution**:
```bash
# If built from source
export PATH="$PATH:/path/to/agentctl/bin"

# Or install to a PATH directory
sudo cp ./agentctl /usr/local/bin/

# Verify installation
which agentctl
agentctl version
```

### Issue: Permission denied when running agentctl

**Cause**: Binary not executable.

**Solution**:
```bash
chmod +x ./agentctl
```

---

## Build Issues

### Issue: `go: cannot find main module`

**Cause**: Not in the repository root.

**Solution**:
```bash
cd /path/to/agentctl
make build
```

### Issue: `undefined: some_package.SomeFunc`

**Cause**: Missing dependencies or outdated modules.

**Solution**:
```bash
go mod download
go mod tidy
make build
```

### Issue: Build fails with CGO errors

**Cause**: CGO enabled, but agentctl requires pure Go.

**Solution**:
```bash
CGO_ENABLED=0 make build
```

### Issue: `golangci-lint: command not found`

**Cause**: Linter not installed.

**Solution**:
```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Or use make target (if available)
make install-tools
```

### Issue: Tests fail with `package not found`

**Cause**: Test dependencies not downloaded.

**Solution**:
```bash
go mod download
go test ./...
```

---

## Skill Execution Issues

### Issue: `ENOTFOUND: skill not found`

**Cause**: Skill not installed or not in skill path.

**Solution**:
```bash
# List installed skills
agentctl skills list

# Install missing skill
agentctl skills install --manifest ./dist/skills/<skill>/skill.yaml

# Build skills from source
make skills-build
```

### Issue: `EPOLICY: path outside workspace`

**Cause**: Attempting to access files outside workspace.

**Solution**:
```bash
# Check current workspace
agentctl config show | grep workspace

# Use relative paths within workspace
agentctl run fs/read --path ./src/main.go  # Good
# Not: agentctl run fs/read --path /etc/passwd  # Bad

# Or move to repository root
cd /path/to/project
agentctl run fs/read --path ./README.md
```

### Issue: `ERUNTIME: skill execution failed`

**Cause**: Skill crashed or encountered an error.

**Solution**:
```bash
# Check stderr for details
agentctl run <skill> 2>error.log
cat error.log

# Try with debug logging
export AGENTCTL_LOG_LEVEL=debug
agentctl run <skill>

# Check skill manifest for requirements
agentctl skills describe <skill>
```

### Issue: WASI skill fails to run

**Cause**: WASI runtime issue or incompatible binary.

**Solution**:
```bash
# Rebuild WASI skills
make skills-build

# Check skill distribution type
agentctl skills describe <skill> | grep distribution

# Try exec version if WASI fails
# (check if exec version exists)
```

### Issue: `ETIMEOUT: skill execution timed out`

**Cause**: Skill took too long (default: 5 minutes).

**Solution**:
```bash
# Use jobs for long-running operations
agentctl jobs submit <skill> --args '...'
agentctl jobs tail <job-id>

# Or increase timeout (if supported)
agentctl run <skill> --timeout 10m
```

---

## Job Issues

### Issue: `job not found`

**Cause**: Job ID incorrect or job was cleaned up.

**Solution**:
```bash
# List all jobs
agentctl jobs list

# List recent jobs
agentctl jobs list --status all --limit 10

# Check if job was deleted
agentctl jobs list --include-deleted
```

### Issue: Job stuck in `queued` state

**Cause**: Job queue not processing or system overload.

**Solution**:
```bash
# Check job queue status
agentctl jobs list --status queued

# Restart agentctl (if running as daemon)
# Or submit a simple test job
agentctl jobs submit fs/ls --path .

# Check database for corruption
sqlite3 ~/.agentctl/agentctl.db "SELECT * FROM jobs WHERE status='queued';"
```

### Issue: Job stuck in `running` state after crash

**Cause**: Crash recovery not triggered.

**Solution**:
```bash
# Manually mark as error (if supported)
agentctl jobs cancel <job-id>

# Or clean up via database (last resort)
sqlite3 ~/.agentctl/agentctl.db "UPDATE jobs SET status='error' WHERE id='<job-id>';"
```

### Issue: Job result not found

**Cause**: Job failed or artifact was garbage collected.

**Solution**:
```bash
# Check job status first
agentctl jobs get <job-id>

# If error, check stderr
agentctl jobs get <job-id> --show-stderr

# If artifact missing, check CAS
agentctl cas list | grep <digest>
```

---

## Memory and Cache Issues

### Issue: Cache not working (always running skill)

**Cause**: Cache disabled or cache key mismatch.

**Solution**:
```bash
# Cache is currently disabled
agentctl run <skill> --cache off

# Check memory status
agentctl memory stats
```

### Issue: `memory not found`

**Cause**: Named memory doesn't exist or was deleted.

**Solution**:
```bash
# List all memories
agentctl memory list

# Search for memory
agentctl memory search "keyword"

# Check if memory was in auto-cache (24h TTL expired)
# → Named memories are persistent; auto-cache is not
```

### Issue: Memory search returns no results

**Cause**: Memory doesn't exist or search index issue.

**Solution**:
```bash
# List all memories (bypass search)
agentctl memory list

# Check memory stats
agentctl memory stats

# Rebuild search index (if supported)
agentctl memory reindex
```

---

## CAS (Content-Addressable Storage) Issues

### Issue: `EIO: digest verification failed`

**Cause**: CAS artifact corrupted or tampered with.

**Solution**:
```bash
# Delete corrupted artifact
agentctl cas delete <digest> --force

# Re-run operation to regenerate artifact
agentctl run <skill> --cache off

# Check filesystem for corruption
ls -lh ~/.agentctl/cas/sha256/<digest>
```

### Issue: `ENOTFOUND: artifact not found`

**Cause**: Artifact was garbage collected or never created.

**Solution**:
```bash
# Check if digest exists
agentctl cas list | grep <digest>

# Pin important artifacts to prevent GC
agentctl cas pin <digest>

# Re-run operation to recreate
agentctl run <skill> --remember <name>
```

### Issue: CAS storage growing too large

**Cause**: Many large artifacts not being garbage collected.

**Solution**:
```bash
# Check CAS usage
du -sh ~/.agentctl/cas/

# List unpinned artifacts
agentctl cas list --unpinned

# Run garbage collection (dry-run first)
agentctl cas gc --older-than 168h --dry-run
agentctl cas gc --older-than 168h --confirm

# Pin important artifacts before GC
agentctl cas pin <digest>
```

---

## OpenAPI Skill Issues

### Issue: `EOPENAPI: spec not found`

**Cause**: OpenAPI spec not imported or incorrect reference.

**Solution**:
```bash
# List imported specs
agentctl openapi list

# Import spec
agentctl openapi import https://api.github.com/openapi.yaml --as github

# Or use file path
agentctl openapi import ./openapi.yaml --as myapi

# Or use CAS digest
agentctl openapi import sha256:<digest> --as myapi
```

### Issue: `EOPENAPI: operationId not found`

**Cause**: Operation doesn't exist in spec.

**Solution**:
```bash
# List operations in spec
agentctl openapi operations --spec memory:github

# Search for operation
agentctl openapi operations --spec memory:github | grep -i "list"

# Check spec for correct operationId
```

### Issue: `EAUTH: authentication failed`

**Cause**: Invalid credentials or auth scheme.

**Solution**:
```bash
# Check auth configuration
agentctl run http/openapi \
  --spec memory:github \
  --operationId listRepos \
  --auth '{"type":"bearer","token":"'$GITHUB_TOKEN'"}' \
  --dry-run

# Verify token is set
echo $GITHUB_TOKEN

# Try different auth type (apiKey, basic, oauth2)
agentctl run http/openapi \
  --auth '{"type":"apiKey","name":"X-API-Key","value":"'$API_KEY'","in":"header"}'
```

### Issue: `ERATELIMIT: rate limit exceeded`

**Cause**: Too many API calls, hitting rate limits.

**Solution**:
```bash
# Use retry with backoff
agentctl run http/openapi \
  --spec memory:github \
  --operationId listRepos \
  --retry '{"max_attempts":3,"backoff":"exponential"}'

# Check rate limit headers in response
agentctl run http/openapi ... | jq '.meta.response_headers'

# Wait before retrying or use different credentials
```

### Issue: Pagination not working

**Cause**: Incorrect pagination configuration.

**Solution**:
```bash
# Check pagination strategy
agentctl run http/openapi \
  --spec memory:stripe \
  --operationId CustomerList \
  --paging '{"strategy":"cursor","max_pages":10}' \
  --dry-run

# Try different strategies: link, cursor, offset
agentctl run http/openapi \
  --paging '{"strategy":"link","max_pages":5}'

# Check if API supports pagination
# (some operations don't paginate)
```

---

## Performance Issues

### Issue: Slow skill execution

**Cause**: Large files, slow I/O, or inefficient skill.

**Solution**:
```bash
# Use jobs for large operations
agentctl jobs submit text/grep --pattern "error" --path ./logs/

# Check if result is being artifactized (expected for large outputs)
# → First run may be slow; subsequent runs use cache

# Profile with debug logging
export AGENTCTL_LOG_LEVEL=debug
time agentctl run <skill>
```

### Issue: High memory usage

**Cause**: Large outputs held in memory.

**Solution**:
```bash
# Check if outputs are being artifactized
# (outputs >32 KB should go to CAS automatically)

# Run garbage collection
agentctl cas gc --older-than 24h --confirm

# Check for memory leaks (development only)
go test -memprofile mem.prof ./...
go tool pprof mem.prof
```

### Issue: Database locking errors

**Cause**: Concurrent access to SQLite database.

**Solution**:
```bash
# Ensure only one agentctl instance modifies DB
# SQLite has limited concurrency

# Check for stale locks
rm -f ~/.agentctl/agentctl.db-shm ~/.agentctl/agentctl.db-wal

# If persistent, backup and recreate
cp ~/.agentctl/agentctl.db ~/.agentctl/agentctl.db.bak
sqlite3 ~/.agentctl/agentctl.db "VACUUM;"
```

---

## Database Issues

### Issue: Database corruption

**Cause**: Crash, disk full, or filesystem error.

**Solution**:
```bash
# Check database integrity
sqlite3 ~/.agentctl/agentctl.db "PRAGMA integrity_check;"

# If corrupted, restore from backup (if available)
cp ~/.agentctl/agentctl.db.bak ~/.agentctl/agentctl.db

# Or rebuild database (loses job history and memories)
rm ~/.agentctl/agentctl.db
agentctl config init  # Recreates database
```

### Issue: Migration failed

**Cause**: Schema upgrade error.

**Solution**:
```bash
# Check current schema version
sqlite3 ~/.agentctl/agentctl.db "SELECT version FROM schema_migrations;"

# Backup before manual migration
cp ~/.agentctl/agentctl.db ~/.agentctl/agentctl.db.bak

# Re-run migration (if supported)
agentctl migrate --force
```

---

## Platform-Specific Issues

### Linux

**Issue**: `setrlimit: operation not permitted`

**Cause**: Insufficient privileges for resource limits.

**Solution**:
```bash
# Run as regular user (not root)
# Check ulimits
ulimit -a

# Increase limits if needed (in /etc/security/limits.conf)
```

### macOS

**Issue**: "agentctl is damaged and can't be opened"

**Cause**: Gatekeeper security policy.

**Solution**:
```bash
# Remove quarantine attribute
xattr -d com.apple.quarantine ./agentctl

# Or build from source instead of downloading binary
make build
```

### Windows

**Issue**: Path separators causing errors

**Cause**: Windows uses backslashes `\`, Unix uses `/`.

**Solution**:
```bash
# Use forward slashes in agentctl (even on Windows)
agentctl run fs/read --path ./src/main.go

# Or use PowerShell escaping
agentctl run fs/read --path .\src\main.go
```

**Issue**: `CGO_ENABLED=0` not recognized

**Cause**: Windows command prompt syntax.

**Solution**:
```cmd
REM Command Prompt
set CGO_ENABLED=0
make build

REM PowerShell
$env:CGO_ENABLED=0
make build

REM Or use Go directly
go build ./cmd/agentctl
```

---

## Getting Help

### Self-Service Resources

1. **Documentation**: [docs/](../) directory
   - [Core Profile v1](./spec/v1/core_profile_v1.md)
   - [OpenAPI Skill Guide](./spec/openapi_skill.md)
   - [AGENTS.md](../AGENTS.md)

2. **Examples**: [docs/examples/](./examples/)
   - [Minimum Workflow Skills](./examples/minimum_workflow_skills.md)
   - [Skills Chain](./examples/skills_chain.md)

3. **Specs**: [docs/spec/](./spec/)
   - Protocol and implementation behavior specs

### Community Support

- **GitHub Issues**: [Report bugs](https://github.com/jkatigb/agentctl/issues)
- **GitHub Discussions**: [Ask questions](https://github.com/jkatigb/agentctl/discussions)
- **Contributing**: See [CONTRIBUTING.md](../CONTRIBUTING.md)

### Reporting Bugs

When opening an issue, include:

```bash
# System info
uname -a           # OS and kernel
go version         # Go version
agentctl version   # agentctl version

# Reproduction
agentctl run <skill> --input '{}' 2>error.log

# Relevant logs (redact secrets!)
cat error.log

# Expected vs actual behavior
```

### Before Asking for Help

1. **Check this troubleshooting guide**
2. **Search existing issues**: [GitHub Issues](https://github.com/jkatigb/agentctl/issues)
3. **Enable debug logging**: `export AGENTCTL_LOG_LEVEL=debug`
4. **Try a minimal reproduction**: Simplify to smallest failing case
5. **Check recent changes**: Did it work before? What changed?

---

## Debugging Tips

### Enable Debug Logging

```bash
export AGENTCTL_LOG_LEVEL=debug
agentctl run <skill> 2>debug.log
cat debug.log
```

### Inspect Envelopes

```bash
# Pretty-print JSON envelope
agentctl run fs/ls --path . | jq .

# Check envelope fields
agentctl run fs/ls --path . | jq '.meta'
```

### Test with Simple Skills

```bash
# Test basic functionality
agentctl run fs/ls --path .
agentctl run wasi/echo --message "hello"

# If these fail, issue is in core, not skills
```

### Check Filesystem Permissions

```bash
# Check agentctl directories
ls -la ~/.agentctl/
ls -la ~/.agentctl/cas/
ls -la ~/.agentctl/jobs/

# Ensure writable
touch ~/.agentctl/test && rm ~/.agentctl/test
```

### Reset to Clean State

```bash
# Backup first!
cp -r ~/.agentctl ~/.agentctl.bak

# Remove state
rm -rf ~/.agentctl/

# Reinitialize
agentctl config init
agentctl skills list  # Should be empty
```

---

## Common Pitfalls

### 1. Running from wrong directory

**Problem**: Workspace detection fails.

**Solution**: Run from repository root or specify paths relative to workspace.

### 2. Forgetting to build skills

**Problem**: Skills not found after build.

**Solution**: Run `make skills-build` after building CLI.

### 3. Stale cache

**Problem**: Old results returned despite changes.

**Solution**: Use `--cache off`.

### 4. Secrets in command history

**Problem**: Secrets logged in shell history.

**Solution**: Use environment variables, not CLI args.

### 5. Expecting exec behavior from WASI skills

**Problem**: WASI skills can't access network.

**Solution**: Use exec skills for network operations, WASI for sandboxed operations.

---

## Known Issues

See [GitHub Issues](https://github.com/jkatigb/agentctl/issues) for current known issues.

### Planned Fixes

- **SPEC-011**: PathValidator edge cases (in progress)
- **SPEC-012-016**: OpenAPI skill implementation (in progress)
- **SPEC-018**: Golden test fixtures for regression prevention

---

## Additional Resources

- **[Core Profile v1](./spec/v1/core_profile_v1.md)** — Complete specification
- **[docs/archive/roadmap.md](./archive/roadmap.md)** — Roadmap pointer (current + historical)
- **[CONTRIBUTING.md](../CONTRIBUTING.md)** — Development guide
- **[SECURITY.md](SECURITY.md)** — Security policy

---

**Last Updated**: February 2026

If this guide didn't solve your problem, please [open an issue](https://github.com/jkatigb/agentctl/issues) with details!
