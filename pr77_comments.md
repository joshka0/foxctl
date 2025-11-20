# PR #77 Review Comments

## Summary
CodeRabbit review with 4 actionable comments (2 nitpicks, 2 major issues)

---

## 🔴 Major Issues

### 1. **Network capability "none" contradicts MCP bridge requirements**
**Location**: `skills/mcp_install/main.go:468`

**Issue**: The generated skill manifest sets `network: "none"`, but the skill's wrapper script invokes `mcp_bridge` which requires network access to communicate with MCP servers (especially for HTTP/SSE transport). This will cause runtime failures when the skill is executed under agentctl's policy enforcement.

**Fix**: Change network capability to allow egress:
```go
"capabilities": map[string]any{
    "network": "egress",
    "egressAllow": []string{"*"}, // Or restrict to specific MCP server domains if known
    "filesystem": []map[string]string{
        {"type": "workdir"},
    },
},
```

---

### 2. **Sanitize tool name before using in filesystem paths**
**Location**: `skills/mcp_install/main.go:285`

**Issue**: The tool name from the MCP server is used directly in `filepath.Join` without validation or sanitization. A malicious or misconfigured MCP server could provide tool names containing path traversal sequences (e.g., `../../../etc/passwd`) or other filesystem-unsafe characters, potentially writing skills outside the intended directory.

**Fix**: Add validation before line 285:
```go
// Sanitize tool name to prevent path traversal
sanitized := filepath.Base(tool.Name)
if sanitized != tool.Name || sanitized == "." || sanitized == ".." {
    return fmt.Errorf("invalid tool name: %s", tool.Name)
}
skillDir := filepath.Join(baseDir, sanitized)
```

---

## 🟡 Nitpick Comments

### 3. **Consider validating that the bridge binary exists**
**Location**: `skills/mcp_install/main.go:519-523`

**Issue**: The default `BridgePath` is set to `"mcp_bridge"` assuming it's available in the system PATH, but there's no validation to confirm the binary exists or is executable. This could lead to confusing runtime failures when the generated skills attempt to invoke a missing bridge.

**Suggestion**: Add validation after setting the default:
```go
if in.BridgePath == "" {
    in.BridgePath = "mcp_bridge" // Default assuming it's in PATH or aliased
}
// Validate bridge exists
if _, err := exec.LookPath(in.BridgePath); err != nil {
    return input{}, fmt.Errorf("bridge binary not found: %s (use -bridge-path or ensure mcp_bridge is in PATH)", in.BridgePath)
}
```

Note: This requires adding `"os/exec"` to imports.

---

### 4. **Condense comment and remove excessive blank lines**
**Location**: `skills/mcp_install/main.go:221-230`

**Issue**: The comment block explaining output directory validation could be more concise, and there are unnecessary blank lines that reduce code density.

**Suggestion**: Apply this diff:
```diff
-
-       // Validate output directory using PathValidator if possible, 
-       // but for "installing skills" we often write to a skills directory which might be outside workspace.
-       // However, agentctl policy enforces strict workspace.
-       // So we assume in.OutputDir is within the workspace.
+       // Validate output directory (must be within workspace per agentctl policy)
        validDir, err := rc.PathValidator.ValidatePath(in.OutputDir)
```

---

### 5. **Normalize indentation and remove extra blank lines**
**Location**: `skills/mcp_install/main.go:97-157`

**Issue**: The block that chooses between HTTP (ServerURL) and stdio (ServerCmd) transports contains inconsistent indentation and many unnecessary blank lines.

**Suggestion**: Normalize indentation to match surrounding Go style (tabs for block indentation), remove extra blank lines, align braces and if/else blocks consistently.

---

## Priority Order

1. **CRITICAL**: Fix network capability (Issue #1) - will cause runtime failures
2. **CRITICAL**: Sanitize tool names (Issue #2) - security vulnerability
3. **NICE TO HAVE**: Validate bridge binary exists (Issue #3)
4. **STYLE**: Clean up formatting (Issues #4, #5)
