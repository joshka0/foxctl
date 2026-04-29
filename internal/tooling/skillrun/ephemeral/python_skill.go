package ephemeral

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

const defaultPythonSkillToolName = "ephemeral_python_skill"

const (
	defaultPythonSkillValidateTimeout = 3 * time.Second
	defaultPythonSkillRunTimeout      = 10 * time.Second
)

// PythonSkillSpec describes one short-lived Python helper.
type PythonSkillSpec struct {
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	Source          string          `json:"source"`
	Parameters      json.RawMessage `json:"parameters,omitempty"`
	PythonPath      string          `json:"python_path,omitempty"`
	ValidateTimeout time.Duration   `json:"-"`
	RunTimeout      time.Duration   `json:"-"`
}

// PythonSkillRunner validates and executes one synthesized Python solve helper.
type PythonSkillRunner struct {
	spec PythonSkillSpec
}

// NewPythonSkillRunner validates a spec and returns an executable runner.
func NewPythonSkillRunner(ctx context.Context, spec PythonSkillSpec) (*PythonSkillRunner, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" {
		spec.Name = defaultPythonSkillToolName
	}
	if spec.Description == "" {
		spec.Description = "Run a short-lived Python domain helper synthesized for this attempt."
	}
	if len(spec.Parameters) == 0 {
		spec.Parameters = json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	spec.Source = normalizePythonSkillSource(spec.Source)
	if strings.TrimSpace(spec.Source) == "" {
		return nil, errors.New("ephemeral Python skill source is empty")
	}
	if _, err := runPythonSkillBridge(ctx, spec.PythonPath, spec.Source, nil, true, pythonSkillTimeout(spec.ValidateTimeout, defaultPythonSkillValidateTimeout)); err != nil {
		return nil, err
	}
	return &PythonSkillRunner{spec: spec}, nil
}

func (r *PythonSkillRunner) ToolDef() engine.ToolDef {
	return engine.ToolDef{
		Name:        r.spec.Name,
		Description: r.spec.Description,
		Parameters:  r.spec.Parameters,
	}
}

func (r *PythonSkillRunner) Run(ctx context.Context, input map[string]any) (GoSkillResult, error) {
	if r == nil {
		return GoSkillResult{}, errors.New("ephemeral Python skill runner is nil")
	}
	normalizedInput, err := normalizeJSONMap(input)
	if err != nil {
		return GoSkillResult{}, fmt.Errorf("normalize input: %w", err)
	}
	output, err := runPythonSkillBridge(ctx, r.spec.PythonPath, r.spec.Source, normalizedInput, false, pythonSkillTimeout(r.spec.RunTimeout, defaultPythonSkillRunTimeout))
	if err != nil {
		return GoSkillResult{}, err
	}
	return GoSkillResult{
		OK:     boolField(output, "ok", true),
		Output: output,
		Metadata: map[string]any{
			"runner": "python",
			"name":   r.spec.Name,
		},
	}, nil
}

// PythonSkillToolExecutor exposes one ephemeral Python skill as an LLM tool.
type PythonSkillToolExecutor struct {
	Runner *PythonSkillRunner
}

func (e PythonSkillToolExecutor) List() []engine.ToolDef {
	if e.Runner == nil {
		return nil
	}
	return []engine.ToolDef{e.Runner.ToolDef()}
}

func (e PythonSkillToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if e.Runner == nil {
		return "", errors.New("ephemeral Python skill executor is not configured")
	}
	if strings.TrimSpace(name) != e.Runner.spec.Name {
		return "", fmt.Errorf("unknown ephemeral Python skill %q", name)
	}
	var input map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("decode ephemeral Python skill args: %w", err)
		}
	}
	result, err := e.Runner.Run(ctx, input)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func normalizePythonSkillSource(source string) string {
	source = strings.TrimSpace(source)
	source = strings.Trim(source, "`")
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "```") {
		source = strings.TrimPrefix(source, "```python")
		source = strings.TrimPrefix(source, "```")
		source = strings.TrimSuffix(source, "```")
		source = strings.TrimSpace(source)
	}
	var parsed string
	if len(source) >= 2 && json.Unmarshal([]byte(source), &parsed) == nil {
		source = strings.TrimSpace(parsed)
	}
	source = normalizeEscapedPythonSource(source)
	source = normalizePythonStringLiteralNewlines(source)
	source = trimPythonJSONSourceFragments(source)
	source = trimJSONSourceFragments(source)
	if extracted, ok := extractWrappedPythonSolveSource(source); ok {
		source = extracted
	}
	return strings.TrimSpace(source)
}

func normalizeEscapedPythonSource(source string) string {
	hasRealNewlines := strings.Contains(source, "\n")
	replacements := []struct {
		old string
		new string
	}{
		{`\\n`, "\n"},
		{`\n`, "\n"},
		{`\\t`, "\t"},
		{`\t`, "\t"},
		{`\\r`, "\r"},
		{`\r`, "\r"},
		{`\"`, `"`},
	}
	for _, replacement := range replacements {
		if hasRealNewlines && strings.Contains(replacement.old, `\n`) {
			continue
		}
		source = strings.ReplaceAll(source, replacement.old, replacement.new)
	}
	return source
}

func normalizePythonStringLiteralNewlines(source string) string {
	if !strings.Contains(source, "\n") {
		return source
	}
	var b strings.Builder
	b.Grow(len(source))
	inQuote := rune(0)
	triple := false
	escaped := false
	inComment := false
	runes := []rune(source)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if inComment {
			b.WriteRune(ch)
			if ch == '\n' || ch == '\r' {
				inComment = false
			}
			continue
		}
		if inQuote != 0 {
			if escaped {
				b.WriteRune(ch)
				escaped = false
				continue
			}
			if ch == '\\' {
				b.WriteRune(ch)
				escaped = true
				continue
			}
			if !triple && (ch == '\n' || ch == '\r') {
				b.WriteString(`\n`)
				if ch == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
					i++
				}
				continue
			}
			if ch == inQuote {
				if triple {
					if i+2 < len(runes) && runes[i+1] == inQuote && runes[i+2] == inQuote {
						b.WriteRune(ch)
						b.WriteRune(runes[i+1])
						b.WriteRune(runes[i+2])
						i += 2
						inQuote = 0
						triple = false
						continue
					}
				} else {
					b.WriteRune(ch)
					inQuote = 0
					continue
				}
			}
			b.WriteRune(ch)
			continue
		}
		if ch == '#' {
			inComment = true
			b.WriteRune(ch)
			continue
		}
		if ch == '\'' || ch == '"' {
			inQuote = ch
			triple = i+2 < len(runes) && runes[i+1] == ch && runes[i+2] == ch
			b.WriteRune(ch)
			if triple {
				b.WriteRune(runes[i+1])
				b.WriteRune(runes[i+2])
				i += 2
			}
			continue
		}
		b.WriteRune(ch)
	}
	return b.String()
}

func trimPythonJSONSourceFragments(source string) string {
	for _, marker := range []string{`", "input"`, `", \"input\"`, `\n", "input"`, `\n\", \"input\"`} {
		if cut := strings.Index(source, marker); cut > 0 {
			source = source[:cut]
		}
	}
	return strings.TrimSpace(source)
}

func extractWrappedPythonSolveSource(source string) (string, bool) {
	if sourceStartsWithPythonDeclaration(source) {
		return "", false
	}
	return extractPythonSolveSource(source)
}

func sourceStartsWithPythonDeclaration(source string) bool {
	source = strings.TrimSpace(source)
	return strings.HasPrefix(source, "def ") || strings.HasPrefix(source, "import ") || strings.HasPrefix(source, "from ")
}

func extractPythonSolveSource(source string) (string, bool) {
	idx := strings.Index(source, "def solve")
	alt := strings.Index(source, "def Solve")
	if idx < 0 || (alt >= 0 && alt < idx) {
		idx = alt
	}
	if idx < 0 {
		return "", false
	}
	source = strings.TrimSpace(source[idx:])
	return strings.TrimSpace(source), true
}

func runPythonSkillBridge(ctx context.Context, pythonPath, source string, input map[string]any, validateOnly bool, timeout time.Duration) (map[string]any, error) {
	if strings.TrimSpace(pythonPath) == "" {
		resolved, err := findPythonSkillBinary()
		if err != nil {
			return nil, err
		}
		pythonPath = resolved
	}
	request := map[string]any{
		"source":        source,
		"input":         input,
		"validate_only": validateOnly,
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	runCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(runCtx, pythonPath, "-I", "-S", "-c", pythonSkillBridgeScript) //nolint:gosec // pythonPath is explicit/discovered; argv is fixed.
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("run Python skill bridge: timed out after %s", timeout)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("run Python skill bridge: %s", detail)
	}
	var response struct {
		OK     bool           `json:"ok"`
		Output map[string]any `json:"output,omitempty"`
		Error  string         `json:"error,omitempty"`
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Python skill bridge response: %w (stdout=%q stderr=%q)", err, strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()))
	}
	if !response.OK {
		return nil, fmt.Errorf("python skill validation/run failed: %s", strings.TrimSpace(response.Error))
	}
	if validateOnly {
		return map[string]any{"ok": true}, nil
	}
	if response.Output == nil {
		return nil, errors.New("python skill returned no output")
	}
	return normalizeJSONMap(response.Output)
}

func pythonSkillTimeout(value time.Duration, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func findPythonSkillBinary() (string, error) {
	for _, candidate := range []string{"python3", "python"} {
		path, err := exec.LookPath(candidate)
		if err == nil {
			return path, nil
		}
	}
	return "", errors.New("python binary not found (checked python3, python)")
}

const pythonSkillBridgeScript = `
import ast
import contextlib
import io
import json
import sys
import traceback

ALLOWED_IMPORTS = {
    "ast", "bisect", "collections", "copy", "functools", "heapq", "itertools", "json",
    "math", "operator", "re", "statistics",
}
SAFE_BUILTINS = {
    "abs": abs, "all": all, "any": any, "bool": bool, "dict": dict,
    "enumerate": enumerate, "filter": filter, "float": float, "int": int,
    "isinstance": isinstance,
    "len": len, "list": list, "map": map, "max": max, "min": min,
    "next": next, "pow": pow, "print": print,
    "range": range, "reversed": reversed, "round": round,
    "set": set, "sorted": sorted, "str": str, "sum": sum, "tuple": tuple,
    "type": type,
    "zip": zip, "Exception": Exception, "ValueError": ValueError,
    "TypeError": TypeError, "IndexError": IndexError, "KeyError": KeyError,
}
DISALLOWED_CALLS = {
    "eval", "exec", "compile", "open", "input", "__import__",
    "globals", "locals", "vars", "dir", "getattr", "setattr", "delattr",
}
DISALLOWED_ROOTS = {
    "os", "sys", "subprocess", "socket", "pathlib", "shutil", "builtins",
    "requests", "urllib", "http", "importlib",
}

def respond(ok, output=None, error=""):
    sys.stdout.write(json.dumps({"ok": ok, "output": output, "error": error}, separators=(",", ":")))

def safe_import(name, globals=None, locals=None, fromlist=(), level=0):
    root = name.split(".", 1)[0]
    if level != 0 or root not in ALLOWED_IMPORTS:
        raise ImportError(f"disallowed import {name}")
    return __import__(name, globals, locals, fromlist, level)

def validate(tree):
    solve_count = 0
    for node in ast.walk(tree):
        if isinstance(node, (ast.Import, ast.ImportFrom)):
            if isinstance(node, ast.ImportFrom):
                if node.level != 0 or not node.module:
                    raise ValueError("relative imports are not allowed")
                names = [node.module]
            else:
                names = [alias.name for alias in node.names]
            for name in names:
                root = name.split(".", 1)[0]
                if root not in ALLOWED_IMPORTS:
                    raise ValueError(f"disallowed import {name}")
        if isinstance(node, ast.FunctionDef) and node.name in ("solve", "Solve"):
            solve_count += 1
            if len(node.args.args) != 1 or node.args.vararg or node.args.kwarg:
                raise ValueError("solve must accept exactly one input argument")
        if isinstance(node, ast.Call):
            fn = node.func
            if isinstance(fn, ast.Name) and fn.id in DISALLOWED_CALLS:
                raise ValueError(f"disallowed call {fn.id}")
            if isinstance(fn, ast.Attribute) and isinstance(fn.value, ast.Name):
                if fn.value.id in DISALLOWED_ROOTS:
                    raise ValueError(f"disallowed selector {fn.value.id}.{fn.attr}")
        if isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name):
            if node.value.id in DISALLOWED_ROOTS:
                raise ValueError(f"disallowed selector {node.value.id}.{node.attr}")
    if solve_count == 0:
        raise ValueError("source must define solve(input) or Solve(input)")

try:
    req = json.load(sys.stdin)
    source = str(req.get("source", ""))
    tree = ast.parse(source, filename="ephemeral_skill.py", mode="exec")
    validate(tree)
    if req.get("validate_only"):
        respond(True, {"ok": True})
        raise SystemExit(0)
    builtins = dict(SAFE_BUILTINS)
    builtins["__import__"] = safe_import
    namespace = {"__builtins__": builtins}
    with contextlib.redirect_stdout(io.StringIO()):
        exec(compile(tree, "ephemeral_skill.py", "exec"), namespace, namespace)
        fn = namespace.get("solve") or namespace.get("Solve")
        if not callable(fn):
            raise ValueError("solve is not callable")
        result = fn(req.get("input") or {})
    if not isinstance(result, dict):
        raise ValueError("solve must return a dict")
    respond(True, result)
except SystemExit:
    raise
except BaseException as exc:
    respond(False, error=f"{type(exc).__name__}: {exc}")
`
