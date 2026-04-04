package shellreduce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ExecuteNative runs a reducer-backed native command path without routing through a skill.
func ExecuteNative(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	switch route.Native {
	case "ruff_check":
		return executeRuffCheck(ctx, workspace, route)
	case "docker_ps":
		return executeDockerPS(ctx, workspace)
	case "pipe_line_slice":
		return executePipeLineSlice(ctx, workspace, route)
	case "file_head":
		return executeFileHead(ctx, workspace, route)
	case "file_tail":
		return executeFileTail(ctx, workspace, route)
	case "file_wc":
		return executeFileWC(ctx, workspace, route)
	case "file_wc_slice":
		return executeFileWCSlice(ctx, workspace, route)
	case "kubectl_get":
		return executeKubectlGet(ctx, workspace, route)
	case "kubectl_describe":
		return executeKubectlDescribe(ctx, workspace, route)
	case "kubectl_logs":
		return executeKubectlLogs(ctx, workspace, route)
	case "kubectl_rollout_status":
		return executeKubectlRolloutStatus(ctx, workspace, route)
	case "mix_compile":
		return executeMixCompile(ctx, workspace, route)
	case "mix_test":
		return executeMixTest(ctx, workspace, route)
	case "mix_run":
		return executeMixRun(ctx, workspace, route)
	case "mix_format":
		return executeMixFormat(ctx, workspace, route)
	case "mix_deps_get":
		return executeMixDepsGet(ctx, workspace, route)
	case "mix_ecto":
		return executeMixEcto(ctx, workspace, route)
	default:
		return nil, fmt.Errorf("unknown native shell reducer: %s", route.Native)
	}
}

func executePipeLineSlice(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	base := routeStringSlice(route.Input["base_argv"])
	if len(base) == 0 {
		return nil, fmt.Errorf("pipe base command required")
	}
	stdout, stderr, exitCode, err := runNativeCommand(ctx, workspace, base[0], base[1:]...)
	if err != nil && strings.TrimSpace(stdout) == "" {
		return nil, fmt.Errorf("%s failed: %w", JoinCommand(base), err)
	}
	lines := splitLinesPreserve(stdout)
	mode := firstNonEmpty(stringValue(route.Input["slice_mode"]), "head")
	var selected []string
	switch mode {
	case "head":
		count := intValue(route.Input["lines"])
		if count <= 0 {
			count = 10
		}
		selected = lines[:minInt(len(lines), count)]
	case "tail":
		count := intValue(route.Input["lines"])
		if count <= 0 {
			count = 10
		}
		start := 0
		if len(lines) > count {
			start = len(lines) - count
		}
		selected = lines[start:]
	case "range":
		start := intValue(route.Input["line_start"])
		end := intValue(route.Input["line_end"])
		if start <= 0 {
			start = 1
		}
		if end <= 0 || end > len(lines) {
			end = len(lines)
		}
		if start > len(lines) {
			selected = nil
		} else {
			selected = lines[start-1 : end]
		}
	default:
		return nil, fmt.Errorf("unknown pipe slice mode %q", mode)
	}
	return map[string]any{
		"runner":         "native",
		"subcommand":     "pipe_line_slice",
		"base_command":   JoinCommand(base),
		"slice_mode":     mode,
		"total_lines":    len(lines),
		"selected_lines": len(selected),
		"content":        strings.Join(selected, "\n"),
		"stderr":         strings.TrimSpace(stderr),
		"exit_code":      exitCode,
	}, nil
}

func executeFileHead(_ context.Context, workspace string, route Route) (map[string]any, error) {
	path := stringValue(route.Input["path"])
	lines := intValue(route.Input["lines"])
	if lines <= 0 {
		lines = 10
	}
	content, err := os.ReadFile(resolveNativeFilePath(workspace, path))
	if err != nil {
		return nil, fmt.Errorf("head read failed: %w", err)
	}
	all := splitLinesPreserve(string(content))
	head := all[:minInt(len(all), lines)]
	return map[string]any{
		"runner":      "native",
		"subcommand":  "head",
		"path":        path,
		"lines":       lines,
		"total_lines": len(all),
		"content":     strings.Join(head, "\n"),
	}, nil
}

func executeFileTail(_ context.Context, workspace string, route Route) (map[string]any, error) {
	path := stringValue(route.Input["path"])
	lines := intValue(route.Input["lines"])
	if lines <= 0 {
		lines = 10
	}
	content, err := os.ReadFile(resolveNativeFilePath(workspace, path))
	if err != nil {
		return nil, fmt.Errorf("tail read failed: %w", err)
	}
	all := splitLinesPreserve(string(content))
	start := 0
	if len(all) > lines {
		start = len(all) - lines
	}
	tail := all[start:]
	return map[string]any{
		"runner":      "native",
		"subcommand":  "tail",
		"path":        path,
		"lines":       lines,
		"total_lines": len(all),
		"content":     strings.Join(tail, "\n"),
	}, nil
}

func executeFileWC(_ context.Context, workspace string, route Route) (map[string]any, error) {
	path := stringValue(route.Input["path"])
	mode := firstNonEmpty(stringValue(route.Input["mode"]), "all")
	content, err := os.ReadFile(resolveNativeFilePath(workspace, path))
	if err != nil {
		return nil, fmt.Errorf("wc read failed: %w", err)
	}
	text := string(content)
	lines := len(splitLinesPreserve(text))
	words := len(strings.Fields(text))
	bytesCount := len(content)
	data := map[string]any{
		"runner":     "native",
		"subcommand": "wc",
		"path":       path,
		"mode":       mode,
		"lines":      lines,
		"words":      words,
		"bytes":      bytesCount,
	}
	return data, nil
}

func executeFileWCSlice(_ context.Context, workspace string, route Route) (map[string]any, error) {
	path := stringValue(route.Input["path"])
	mode := firstNonEmpty(stringValue(route.Input["wc_mode"]), "all")
	content, err := os.ReadFile(resolveNativeFilePath(workspace, path))
	if err != nil {
		return nil, fmt.Errorf("wc slice read failed: %w", err)
	}
	text := string(content)
	lines := splitLinesPreserve(text)
	selected := []string{}
	sliceMode := firstNonEmpty(stringValue(route.Input["slice_mode"]), "tail")
	switch sliceMode {
	case "head":
		count := intValue(route.Input["lines"])
		if count <= 0 {
			count = 10
		}
		selected = lines[:minInt(len(lines), count)]
	case "tail":
		count := intValue(route.Input["lines"])
		if count <= 0 {
			count = 10
		}
		start := 0
		if len(lines) > count {
			start = len(lines) - count
		}
		selected = lines[start:]
	case "range":
		start := intValue(route.Input["line_start"])
		end := intValue(route.Input["line_end"])
		if start <= 0 {
			start = 1
		}
		if end <= 0 || end > len(lines) {
			end = len(lines)
		}
		if start <= len(lines) {
			selected = lines[start-1 : end]
		}
	default:
		return nil, fmt.Errorf("unknown wc slice mode %q", sliceMode)
	}
	return map[string]any{
		"runner":         "native",
		"subcommand":     "wc_slice",
		"path":           path,
		"wc_mode":        mode,
		"slice_mode":     sliceMode,
		"lines":          len(lines),
		"words":          len(strings.Fields(text)),
		"bytes":          len(content),
		"selected_lines": len(selected),
		"content":        strings.Join(selected, "\n"),
	}, nil
}

func executeKubectlGet(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	args := routeStringSlice(route.Input["args"])
	if len(args) == 0 {
		return nil, fmt.Errorf("kubectl get args required")
	}
	if !hasKubectlOutputFlag(args) {
		args = append(args, "-o", "json")
	}
	stdout, stderr, exitCode, err := runNativeCommand(ctx, workspace, "kubectl", args...)
	if err != nil {
		return nil, fmt.Errorf("kubectl get failed: %w", err)
	}
	if hasKubectlOutputFlag(routeStringSlice(route.Input["args"])) {
		return map[string]any{
			"runner":     "kubectl",
			"subcommand": "get",
			"stdout":     truncateString(strings.TrimSpace(stdout), 1200),
			"stderr":     strings.TrimSpace(stderr),
			"exit_code":  exitCode,
		}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil, fmt.Errorf("parse kubectl get json: %w", err)
	}
	kind := stringValue(payload["kind"])
	if items, ok := payload["items"].([]any); ok {
		preview := make([]map[string]any, 0, minInt(len(items), 12))
		for _, raw := range items[:minInt(len(items), 12)] {
			row := asMap(raw)
			meta := asMap(row["metadata"])
			preview = append(preview, map[string]any{
				"name":      stringValue(meta["name"]),
				"namespace": stringValue(meta["namespace"]),
				"kind":      stringValue(row["kind"]),
			})
		}
		return map[string]any{
			"runner":     "kubectl",
			"subcommand": "get",
			"kind":       kind,
			"item_count": len(items),
			"preview":    preview,
			"stderr":     strings.TrimSpace(stderr),
			"exit_code":  exitCode,
		}, nil
	}
	meta := asMap(payload["metadata"])
	return map[string]any{
		"runner":     "kubectl",
		"subcommand": "get",
		"kind":       kind,
		"name":       stringValue(meta["name"]),
		"namespace":  stringValue(meta["namespace"]),
		"stderr":     strings.TrimSpace(stderr),
		"exit_code":  exitCode,
	}, nil
}

func executeKubectlDescribe(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	args := routeStringSlice(route.Input["args"])
	stdout, stderr, exitCode, err := runNativeCommand(ctx, workspace, "kubectl", args...)
	if err != nil {
		return nil, fmt.Errorf("kubectl describe failed: %w", err)
	}
	return map[string]any{
		"runner":     "kubectl",
		"subcommand": "describe",
		"stdout":     truncateString(strings.TrimSpace(stdout), 1800),
		"stderr":     strings.TrimSpace(stderr),
		"exit_code":  exitCode,
	}, nil
}

func executeKubectlLogs(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	args := routeStringSlice(route.Input["args"])
	if !containsAnyArg(args, "--tail") && !containsAnyArg(args, "--tail=") {
		args = append(args, "--tail=50")
	}
	stdout, stderr, exitCode, err := runNativeCommand(ctx, workspace, "kubectl", args...)
	if err != nil {
		return nil, fmt.Errorf("kubectl logs failed: %w", err)
	}
	return map[string]any{
		"runner":     "kubectl",
		"subcommand": "logs",
		"stdout":     truncateString(strings.TrimSpace(stdout), 1800),
		"stderr":     strings.TrimSpace(stderr),
		"exit_code":  exitCode,
	}, nil
}

func executeKubectlRolloutStatus(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	args := routeStringSlice(route.Input["args"])
	stdout, stderr, exitCode, err := runNativeCommand(ctx, workspace, "kubectl", args...)
	if err != nil {
		return nil, fmt.Errorf("kubectl rollout status failed: %w", err)
	}
	return map[string]any{
		"runner":     "kubectl",
		"subcommand": "rollout status",
		"stdout":     strings.TrimSpace(stdout),
		"stderr":     strings.TrimSpace(stderr),
		"exit_code":  exitCode,
	}, nil
}

func executeMixCompile(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	return executeMixCommand(ctx, workspace, "compile", routeStringSlice(route.Input["args"]))
}

func executeMixTest(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	return executeMixCommand(ctx, workspace, "test", routeStringSlice(route.Input["args"]))
}

func executeMixFormat(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	return executeMixCommand(ctx, workspace, "format", routeStringSlice(route.Input["args"]))
}

func executeMixDepsGet(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	return executeMixCommand(ctx, workspace, "deps.get", routeStringSlice(route.Input["args"]))
}

func executeMixRun(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	return executeMixCommand(ctx, workspace, "run", routeStringSlice(route.Input["args"]))
}

func executeMixEcto(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	return executeMixCommand(ctx, workspace, "ecto", routeStringSlice(route.Input["args"]))
}

func executeMixCommand(ctx context.Context, workspace, mode string, args []string) (map[string]any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("mix args required")
	}
	stdout, stderr, exitCode, err := runNativeCommand(ctx, workspace, "mix", args...)
	if err != nil {
		return nil, fmt.Errorf("mix %s failed: %w", mode, err)
	}
	data := map[string]any{
		"runner":     "mix",
		"subcommand": mode,
		"stdout":     truncateString(strings.TrimSpace(stdout), 1600),
		"stderr":     strings.TrimSpace(stderr),
		"exit_code":  exitCode,
	}
	switch mode {
	case "compile":
		data["warning_count"] = strings.Count(strings.ToLower(stdout+"\n"+stderr), "warning:")
		data["error_count"] = strings.Count(strings.ToLower(stdout+"\n"+stderr), "error:")
	case "test":
		passed, failed, ignored := parseMixTestSummary(stdout + "\n" + stderr)
		data["passed"] = passed
		data["failed"] = failed
		data["ignored"] = ignored
	case "format":
		data["formatted"] = true
	case "deps.get":
		data["fetched"] = true
	}
	return data, nil
}

var mixTestSummaryRe = regexp.MustCompile(`(?i)(\d+)\s+tests?,\s+(\d+)\s+failures?(?:,\s+(\d+)\s+ignored)?`)

func parseMixTestSummary(text string) (int, int, int) {
	matches := mixTestSummaryRe.FindStringSubmatch(text)
	if len(matches) < 3 {
		return 0, 0, 0
	}
	passed := 0
	failed := 0
	ignored := 0
	total, _ := strconv.Atoi(matches[1])
	failed, _ = strconv.Atoi(matches[2])
	passed = total - failed
	if len(matches) > 3 && matches[3] != "" {
		ignored, _ = strconv.Atoi(matches[3])
	}
	return passed, failed, ignored
}

func executeRuffCheck(ctx context.Context, workspace string, route Route) (map[string]any, error) {
	path := stringValue(route.Input["path"])
	if path == "" {
		path = "."
	}

	args := []string{"check", "--output-format", "json", path}
	cmd := exec.CommandContext(ctx, "ruff", args...)
	if strings.TrimSpace(workspace) != "" {
		cmd.Dir = workspace
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("ruff check failed: %w", err)
		}
	}

	var findings []struct {
		Code     string `json:"code"`
		Message  string `json:"message"`
		Filename string `json:"filename"`
		Location struct {
			Row int `json:"row"`
		} `json:"location"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &findings); err != nil {
		return nil, fmt.Errorf("parse ruff output: %w", err)
	}

	fileCounts := map[string]int{}
	preview := make([]map[string]any, 0, minInt(len(findings), 12))
	for _, finding := range findings {
		file := strings.TrimSpace(finding.Filename)
		if file == "" {
			continue
		}
		fileCounts[file]++
		if len(preview) < 12 {
			preview = append(preview, map[string]any{
				"file":    file,
				"line":    finding.Location.Row,
				"code":    strings.TrimSpace(finding.Code),
				"message": strings.TrimSpace(finding.Message),
			})
		}
	}

	return map[string]any{
		"runner":      "ruff",
		"path":        path,
		"issue_count": len(findings),
		"exit_code":   exitCode,
		"preview":     preview,
		"top_files":   summarizeCounts(fileCounts, 5),
		"stderr":      strings.TrimSpace(stderr.String()),
	}, nil
}

func executeDockerPS(ctx context.Context, workspace string) (map[string]any, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "--format", "{{json .}}")
	if strings.TrimSpace(workspace) != "" {
		cmd.Dir = workspace
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	preview := make([]map[string]any, 0, minInt(len(lines), 10))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		item := map[string]any{
			"id":     stringValue(row["ID"]),
			"image":  stringValue(row["Image"]),
			"name":   stringValue(row["Names"]),
			"status": stringValue(row["Status"]),
		}
		preview = append(preview, item)
	}

	return map[string]any{
		"runner":          "docker",
		"container_count": len(preview),
		"preview":         preview,
	}, nil
}

func summarizeCounts(counts map[string]int, limit int) []map[string]any {
	type row struct {
		Key   string
		Count int
	}
	rows := make([]row, 0, len(counts))
	for key, count := range counts {
		rows = append(rows, row{Key: key, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Key < rows[j].Key
	})
	out := make([]map[string]any, 0, minInt(len(rows), limit))
	for _, item := range rows[:minInt(len(rows), limit)] {
		out = append(out, map[string]any{
			"key":   item.Key,
			"count": item.Count,
		})
	}
	return out
}

func resolveNativeFilePath(workspace, path string) string {
	if path == "" {
		return path
	}
	if filepath.IsAbs(path) {
		return path
	}
	if strings.TrimSpace(workspace) == "" {
		return path
	}
	return filepath.Join(workspace, path)
}

func splitLinesPreserve(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func routeStringSlice(value any) []string {
	switch raw := value.(type) {
	case []string:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func runNativeCommand(ctx context.Context, workspace, name string, args ...string) (stdout string, stderr string, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if strings.TrimSpace(workspace) != "" {
		cmd.Dir = workspace
	}
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode, err
}

func hasKubectlOutputFlag(args []string) bool {
	for i, arg := range args {
		if arg == "-o" || arg == "--output" {
			return i+1 < len(args)
		}
		if strings.HasPrefix(arg, "-o=") || strings.HasPrefix(arg, "--output=") {
			return true
		}
	}
	return false
}

func containsAnyArg(args []string, targets ...string) bool {
	for _, arg := range args {
		for _, target := range targets {
			if arg == target || strings.HasPrefix(arg, target) {
				return true
			}
		}
	}
	return false
}

func truncateString(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "\n... (truncated)"
}
