# Security Policy

## Supported Versions

agentctl is currently in pre-1.0 development. Security updates are provided for:

| Version | Supported          |
| ------- | ------------------ |
| main    | :white_check_mark: |
| < 1.0   | :construction:     |

Once v1.0 is released, we will maintain security updates for the current major version and the previous major version.

---

## Security Model

agentctl implements defense-in-depth security across multiple layers:

### 1. Workspace Confinement

**Goal**: Prevent skills from accessing files outside allowed paths.

**Mechanisms**:
- All file operations route through `policy.PathValidator`
- Workspace detection via Git repository root or current directory
- Explicit allowlist for safe directories (`$HOME/.agentctl/`, `$TMPDIR`)
- Rejection of:
  - Path traversal attempts (`../`, absolute paths outside workspace)
  - Symlinks pointing outside workspace
  - Null byte injection
  - Hidden files outside workspace (unless explicitly allowed)

**Validation**:
```go
// Example: PathValidator in action
validator := policy.NewPathValidator(workspace, allowedRoots)
safePath, err := validator.Validate(userInput)
if err != nil {
    return fmt.Errorf("path policy violation: %w", err)
}
```

### 2. Network Isolation

**WASI Skills** (Core v1 requirement):
- **Network policy**: `network:"none"` (mandatory)
- No network access whatsoever
- Enforced at skill installation via manifest validation
- Checked by `scripts/checkmanifests`

**Exec Skills**:
- Default: no network access
- Optional: `network:"egress"` with `egressAllow` list
- Egress policy allows specific domains/IPs:
  ```yaml
  network: egress
  egressAllow:
    - "api.github.com:443"
    - "*.stripe.com:443"
  ```
- Loopback and Unix sockets denied by default

### 3. Resource Limits

**Exec Runner**:
- CPU time limits via `setrlimit(RLIMIT_CPU)`
- Memory limits via `setrlimit(RLIMIT_AS)`
- Process limits via `setrlimit(RLIMIT_NPROC)`
- File descriptor limits
- Core dump prevention

**WASI Runner**:
- Sandboxed execution with no host access
- Memory limits enforced by wazero runtime
- No filesystem access (except pre-opened directories)
- Deterministic execution

### 4. Secrets Management

**Storage**:
- Secrets live in `/run/secrets/<name>` (mounted at runtime)
- Environment variables for tokens (e.g., `GITHUB_TOKEN`)
- Never stored in code, Git history, or CAS
- File permissions: 0600 for sensitive files

**Redaction**:
- Automatic redaction in logs: `"***"`
- Headers sanitized in OpenAPI responses
- Sensitive fields redacted in envelopes:
  - `Authorization` headers
  - `Cookie` headers
  - API keys in query parameters

**Example**:
```go
// Redact sensitive headers
func redactHeaders(headers map[string]string) map[string]string {
    redacted := make(map[string]string)
    for k, v := range headers {
        if isSensitive(k) {
            redacted[k] = "***"
        } else {
            redacted[k] = v
        }
    }
    return redacted
}
```

### 5. Content Integrity

**CAS (Content-Addressable Storage)**:
- SHA-256 digests for all artifacts
- Integrity verification on every read
- Fail with `EIO` on digest mismatch
- Immutable storage (writes are idempotent)

**Workflow**:
```bash
# Store content
digest=$(agentctl cas put < data.json)
# → sha256:abc123...

# Retrieve with verification
agentctl cas get sha256:abc123...
# → Recomputes digest, fails if mismatch
```

### 6. Input Validation

**Envelope Validation**:
- JSON schema validation for all envelopes
- Required fields enforced
- Type checking for all fields
- Size limits (32 KB inline, larger → CAS)

**Parameter Validation** (OpenAPI skill):
- Path parameters validated against patterns
- Query parameters type-checked
- Request body validated against schema
- Actionable error messages for invalid inputs

**File Operations**:
- UTF-8 validation for text operations
- Binary detection and appropriate handling
- Size limits to prevent memory exhaustion
- Non-UTF-8 bodies rejected with `EPARSE`

### 7. Secure Defaults

- **No network access** for WASI skills
- **Workspace confinement** enabled by default
- **CAS integrity checks** mandatory
- **Secret redaction** automatic
- **Resource limits** applied to exec skills
- **TLS verification** enabled (no insecure skip verify)

---

## Threat Model

### In Scope

1. **Malicious Skills**: Untrusted skills attempting to:
   - Access files outside workspace
   - Exfiltrate data via network
   - Consume excessive resources
   - Leak secrets in logs/output

2. **Path Traversal**: User-controlled paths attempting to:
   - Escape workspace via `../`
   - Follow symlinks to sensitive files
   - Use null bytes to bypass validation

3. **Supply Chain**: Dependencies attempting to:
   - Include backdoors or malware
   - Require CGO (platform-specific risks)
   - Phone home without disclosure

4. **Data Integrity**: Adversaries attempting to:
   - Tamper with CAS artifacts
   - Corrupt job state
   - Forge cache entries

### Out of Scope (for Core v1)

1. **Multi-Tenancy**: Core v1 is single-user, single-workspace
2. **Sandboxing of agentctl itself**: Assumes trusted execution environment
3. **Side-Channel Attacks**: Timing, cache-based attacks not addressed
4. **Physical Access**: Assumes adversary cannot access host filesystem directly

---

## Known Limitations

### Current Gaps (Pre-1.0)

1. **PathValidator Hardening** (SPEC-011, in progress):
   - Some edge cases in symlink resolution
   - Incomplete TOCTOU (Time-of-Check-Time-of-Use) mitigations
   - Platform-specific path handling

2. **Egress Policy Enforcement**:
   - Exec runner network restrictions rely on OS-level controls
   - Not fully sandboxed (compared to WASI)
   - `egressAllow` validation is basic (no CIDR, wildcards limited)

3. **Secret Detection**:
   - Manual redaction (no automatic secret scanning)
   - Risk of accidental logging if developer error

4. **Plugin Security** (deferred to v1.1):
   - Plugins run as exec (not WASI)
   - Limited sandboxing compared to core skills
   - Trust model: plugins are trusted (no untrusted plugin support)

### Mitigations in Progress

- **SPEC-011**: Hardening PathValidator with comprehensive tests
- **SPEC-017**: Plugin protocol with sandboxing guidelines
- **Golden tests**: Regression prevention for security-critical paths

---

## Reporting a Vulnerability

**Please do not open public GitHub issues for security vulnerabilities.**

### Reporting Process

1. **Email**: Send details to security@agentctl.dev (or maintainer directly if available)
2. **GitHub Security Advisory**: Use [private security advisory](https://github.com/jkatigb/agentctl/security/advisories/new) (preferred)
3. **PGP Key**: Available at [https://keybase.io/agentctl](https://keybase.io/agentctl) (if available)

### What to Include

- **Description**: Nature of the vulnerability
- **Impact**: What an attacker could achieve
- **Reproduction**: Steps to reproduce (including code snippets)
- **Environment**: OS, Go version, agentctl version
- **Severity**: Your assessment (Critical/High/Medium/Low)
- **Suggested Fix**: If you have ideas (optional)

### Response Timeline

- **Initial Response**: Within 48 hours
- **Triage**: Within 7 days (severity assessment, impact analysis)
- **Fix Development**: Depends on severity:
  - Critical: 7-14 days
  - High: 14-30 days
  - Medium: 30-60 days
  - Low: Next release cycle
- **Disclosure**: After fix is released (coordinated disclosure)

### Recognition

We appreciate responsible disclosure. With your permission, we will:
- Credit you in the security advisory
- Mention you in release notes
- Add you to `SECURITY.md` hall of fame (optional)

---

## Security Advisories

Security advisories are published at:
- **GitHub**: [Security Advisories](https://github.com/jkatigb/agentctl/security/advisories)
- **Releases**: Mentioned in release notes for patched versions

---

## Security Best Practices for Users

### 1. Verify Skill Sources

```bash
# Only install skills from trusted sources
agentctl skills install --manifest ./skill.yaml --binary ./skill

# Check skill manifest before installation
cat skill.yaml  # Review network, filesystem policies
```

### 2. Use WASI Skills When Possible

```yaml
# Prefer WASI (network:none, better isolation)
distribution: wasi
network: none  # Mandatory for WASI

# Avoid exec unless necessary
distribution: exec
network: egress  # Only if API calls needed
egressAllow:
  - "api.example.com:443"
```

### 3. Protect Secrets

```bash
# Use environment variables (not CLI args)
export GITHUB_TOKEN=ghp_...
agentctl run http/openapi --spec memory:github

# Avoid secrets in command history
# BAD:  agentctl run ... --auth '{"token":"ghp_..."}'
# GOOD: agentctl run ... --auth '{"token":"$GITHUB_TOKEN"}'

# Mount secrets in /run/secrets/ when possible
mkdir -p /run/secrets
echo "$API_KEY" > /run/secrets/api-key
chmod 0600 /run/secrets/api-key
```

### 4. Review Logs Before Sharing

```bash
# Logs may contain workspace paths, not secrets (redacted)
agentctl run fs/ls --path ./src 2>agentctl.log

# Before sharing logs, review for sensitive info:
grep -i "token\|password\|key" agentctl.log
```

### 5. Pin CAS Artifacts

```bash
# Pin important artifacts to prevent GC deletion
agentctl cas pin sha256:abc123...

# Unpin when no longer needed
agentctl cas unpin sha256:abc123...
```

### 6. Workspace Isolation

```bash
# Run agentctl from repository root (automatic workspace detection)
cd /path/to/project
agentctl run fs/ls --path ./src

# Avoid running with elevated privileges
# BAD:  sudo agentctl run ...
# GOOD: agentctl run ...  (as regular user)
```

### 7. Keep agentctl Updated

```bash
# Check version
agentctl version

# Update to latest (once releases available)
curl -sSL https://install.agentctl.dev | sh
```

---

## Security Testing

### Automated Security Checks

Our CI/CD pipeline includes:
- **Static Analysis**: `golangci-lint` with security rules
- **Dependency Scanning**: `go list -m all` for known vulnerabilities
- **Race Detection**: `go test -race` to catch data races
- **Manifest Validation**: `scripts/checkmanifests` for policy violations

### Manual Security Testing

Before each release:
1. **Path Traversal Tests**: Attempt workspace escapes
2. **Network Isolation Tests**: Verify WASI skills cannot access network
3. **Secret Redaction Tests**: Ensure logs/envelopes redact secrets
4. **CAS Integrity Tests**: Verify digest mismatches are caught
5. **Resource Limit Tests**: Ensure exec skills respect rlimits

### Fuzzing (Planned)

Future plans include:
- Fuzzing `policy.PathValidator` with go-fuzz
- Fuzzing envelope parsing with random inputs
- Fuzzing OpenAPI parameter handling

---

## Compliance and Attestations

### Supply Chain Security

- **Go Modules**: Vendored dependencies with `go.sum` verification
- **No CGO**: Pure Go builds reduce supply chain risk
- **Reproducible Builds**: `goreleaser` with SBOM generation (planned)
- **SLSA Provenance**: Level 3 attestation (planned for v1.0)

### Code Provenance

- All commits signed with GPG (maintainers)
- AI-generated code labeled `codex/*` branches
- Human review required for all changes

### License Compliance

- Apache 2.0 License (permissive, OSI-approved)
- Dependency licenses checked (no GPL, AGPL, or proprietary)

---

## Security Roadmap

### v1.0 (Current)
- [x] Workspace confinement (PathValidator)
- [x] WASI runner with network isolation
- [x] CAS integrity verification
- [x] Secret redaction in logs
- [ ] SPEC-011: PathValidator hardening (in progress)
- [ ] Golden tests for security-critical paths (SPEC-018)

### v1.1 (Future)
- [ ] Plugin sandboxing (SPEC-017)
- [ ] Enhanced egress policy (CIDR, wildcards)
- [ ] Automatic secret detection in code
- [ ] SLSA provenance and SBOM

### v2.0+ (Long-term)
- [ ] Multi-tenancy with user isolation
- [ ] Audit logging and tamper-evident logs
- [ ] WASI componentization for better isolation
- [ ] Formal verification of security-critical code

---

## Additional Resources

- **[Core Profile v1](../spec/core_profile_v1.md)** — Security model details
- **[AGENTS.md](../AGENTS.md)** — Security considerations for AI assistants
- **[CONTRIBUTING.md](../CONTRIBUTING.md)** — Secure development practices

---

## Contact

- **Security Issues**: Use GitHub Security Advisory (private)
- **General Questions**: GitHub Discussions
- **Maintainers**: See [CONTRIBUTING.md](../CONTRIBUTING.md)

---

**Last Updated**: November 2025
**Version**: Pre-1.0 (subject to change)

We take security seriously. Thank you for helping us keep agentctl secure!
