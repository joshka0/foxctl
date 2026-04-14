package shellreduce

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

// Summarize renders a compact text summary from routed skill output.
func Summarize(route Route, data map[string]any) string {
	switch route.Intent {
	case "ls":
		return summarizeLS(data)
	case "tree":
		return summarizeTree(data)
	case "find":
		return summarizeFind(data)
	case "line_range":
		return summarizeLineRange(data)
	case "pipe_slice":
		return summarizePipeSlice(data)
	case "file_head":
		return summarizeFileSlice(data, "head")
	case "file_tail":
		return summarizeFileSlice(data, "tail")
	case "file_wc":
		return summarizeFileWC(data)
	case "file_wc_slice":
		return summarizeFileWCSlice(data)
	case "read":
		return summarizeRead(data)
	case "grep":
		return summarizeGrep(data)
	case "git_status":
		return summarizeGitStatus(data)
	case "git_status_short":
		return summarizeGitStatusShort(data)
	case "git_diff":
		return summarizeGitDiff(data)
	case "git_diff_names":
		return summarizeGitDiffNames(data)
	case "git_log":
		return summarizeGitLog(data)
	case "go_test":
		return summarizeGoTest(data)
	case "cargo_test":
		return summarizeCargoRun(data)
	case "pytest":
		return summarizePytestRun(data)
	case "npm_test":
		return summarizeNPMRun(data)
	case "pnpm_test":
		return summarizeNPMRun(data)
	case "yarn_test":
		return summarizeNPMRun(data)
	case "ruff_check":
		return summarizeRuffCheck(data)
	case "docker_ps":
		return summarizeDockerPS(data)
	case "kubectl_get":
		return summarizeKubectlGet(data)
	case "kubectl_describe":
		return summarizeGenericNative(data, "kubectl describe")
	case "kubectl_logs":
		return summarizeGenericNative(data, "kubectl logs")
	case "kubectl_rollout_status":
		return summarizeGenericNative(data, "kubectl rollout status")
	case "mix_compile":
		return summarizeMixCompile(data)
	case "mix_test":
		return summarizeMixTest(data)
	case "mix_format":
		return summarizeGenericNative(data, "mix format")
	case "mix_deps_get":
		return summarizeGenericNative(data, "mix deps.get")
	case "mix_run":
		return summarizeGenericNative(data, "mix run")
	case "mix_ecto":
		return summarizeGenericNative(data, "mix ecto")
	default:
		return ""
	}
}

func summarizeLS(data map[string]any) string {
	type item struct {
		Name  string
		IsDir bool
		Size  int64
	}

	preview := asSlice(data["preview"])
	items := make([]item, 0, len(preview))
	for _, raw := range preview {
		entry := asMap(raw)
		if len(entry) == 0 {
			continue
		}
		items = append(items, item{
			Name:  stringValue(entry["name"]),
			IsDir: boolValue(entry["is_dir"]),
			Size:  int64Value(entry["size_bytes"]),
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return items[i].Name < items[j].Name
	})

	var lines []string
	if path := stringValue(data["path"]); path != "" {
		lines = append(lines, path)
	}
	for _, item := range items[:minInt(len(items), 18)] {
		if item.IsDir {
			lines = append(lines, item.Name+"/")
			continue
		}
		lines = append(lines, fmt.Sprintf("%s  %s", item.Name, humanSize(item.Size)))
	}
	if len(items) > 18 || boolValue(data["truncated"]) || boolValue(data["preview_truncated"]) {
		lines = append(lines, "...")
	}
	files := intValue(data["files"])
	dirs := intValue(data["directories"])
	lines = append(lines, fmt.Sprintf("%d files, %d dirs", files, dirs))
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeTree(data map[string]any) string {
	tree := asMap(data["tree"])
	if len(tree) == 0 {
		return ""
	}

	var lines []string
	if text := stringValue(tree["tree_text"]); text != "" {
		lines = append(lines, strings.TrimSpace(text))
	}
	stats := asMap(tree["stats"])
	if len(stats) > 0 {
		lines = append(lines, fmt.Sprintf("%d files, %d dirs", intValue(stats["total_files"]), intValue(stats["total_dirs"])))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeRead(data map[string]any) string {
	if preview := stringValue(data["preview_numbered"]); preview != "" {
		return trimParagraph(preview, 2200)
	}
	if preview := stringValue(data["preview_raw"]); preview != "" {
		return trimParagraph(preview, 2200)
	}
	if summary := stringValue(data["summary"]); summary != "" {
		lines := []string{summary}
		if artifact := stringValue(data["artifact"]); artifact != "" {
			lines = append(lines, "artifact: "+artifact)
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

func summarizeFind(data map[string]any) string {
	preview := asSlice(data["preview"])
	var lines []string
	lines = append(lines, fmt.Sprintf("%d results:", intValue(data["result_count"])))
	for _, raw := range preview[:minInt(len(preview), 16)] {
		entry := asMap(raw)
		if len(entry) == 0 {
			continue
		}
		path := stringValue(entry["path"])
		if path == "" {
			continue
		}
		if stringValue(entry["type"]) == "directory" {
			lines = append(lines, path+"/")
			continue
		}
		lines = append(lines, path)
	}
	if len(preview) > 16 {
		lines = append(lines, "...")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeLineRange(data map[string]any) string {
	rendered := stringValue(data["rendered_context"])
	if rendered != "" {
		return trimParagraph(rendered, 2200)
	}
	preview := asSlice(data["preview"])
	if len(preview) == 0 {
		return ""
	}
	entry := asMap(preview[0])
	var lines []string
	if file := stringValue(entry["file"]); file != "" {
		lines = append(lines, fmt.Sprintf("%s:%d-%d", file, intValue(entry["start_line"]), intValue(entry["end_line"])))
	}
	if header := stringValue(entry["header_line"]); header != "" {
		lines = append(lines, header)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizePipeSlice(data map[string]any) string {
	content := stringValue(data["content"])
	if content == "" {
		return ""
	}
	base := stringValue(data["base_command"])
	mode := stringValue(data["slice_mode"])
	return strings.TrimSpace(fmt.Sprintf("%s (%s)\n%s", base, mode, trimParagraph(content, 2200)))
}

func summarizeFileSlice(data map[string]any, label string) string {
	content := stringValue(data["content"])
	if content == "" {
		return ""
	}
	path := stringValue(data["path"])
	return strings.TrimSpace(fmt.Sprintf("%s %s\n%s", label, path, trimParagraph(content, 2200)))
}

func summarizeFileWC(data map[string]any) string {
	path := stringValue(data["path"])
	mode := stringValue(data["mode"])
	switch mode {
	case "lines":
		return fmt.Sprintf("%d %s", intValue(data["lines"]), path)
	case "words":
		return fmt.Sprintf("%d %s", intValue(data["words"]), path)
	case "bytes":
		return fmt.Sprintf("%d %s", intValue(data["bytes"]), path)
	default:
		return fmt.Sprintf("%d %d %d %s", intValue(data["lines"]), intValue(data["words"]), intValue(data["bytes"]), path)
	}
}

func summarizeFileWCSlice(data map[string]any) string {
	prefix := summarizeFileWC(data)
	content := stringValue(data["content"])
	if content == "" {
		return prefix
	}
	return strings.TrimSpace(prefix + "\n" + trimParagraph(content, 2200))
}

func summarizeGrep(data map[string]any) string {
	total := intValue(data["match_count"])
	filesTouched := intValue(data["files_touched"])
	preview := asSlice(data["preview"])
	topFiles := asSlice(data["top_files"])

	type match struct {
		Line    int
		Snippet string
	}
	grouped := map[string][]match{}
	for _, raw := range preview {
		entry := asMap(raw)
		if len(entry) == 0 {
			continue
		}
		file := stringValue(entry["file"])
		if file == "" {
			continue
		}
		grouped[file] = append(grouped[file], match{
			Line:    firstPositiveInt(entry["line_no"], entry["line"]),
			Snippet: skillout.TruncateSingleLine(firstNonEmpty(stringValue(entry["snippet"]), stringValue(entry["text"])), 140),
		})
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("%d matches in %d files:", total, filesTouched))
	for _, raw := range topFiles[:minInt(len(topFiles), 4)] {
		pair := asSlice(raw)
		if len(pair) < 2 {
			continue
		}
		file := stringValue(pair[0])
		count := intValue(pair[1])
		if file == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s (%d):", file, count))
		for _, hit := range grouped[file][:minInt(len(grouped[file]), 3)] {
			if hit.Snippet == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("  %d: %s", hit.Line, hit.Snippet))
		}
	}
	if boolValue(data["truncated"]) {
		lines = append(lines, "...")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeGitStatus(data map[string]any) string {
	var lines []string
	branch := firstNonEmpty(stringValue(data["branch"]), "detached")
	upstream := stringValue(data["upstream"])
	if upstream != "" {
		lines = append(lines, fmt.Sprintf("%s -> %s", branch, upstream))
	} else {
		lines = append(lines, branch)
	}
	lines = append(lines, fmt.Sprintf(
		"%d modified, %d added, %d deleted, %d untracked",
		intValue(data["modified_count"]),
		intValue(data["added_count"]),
		intValue(data["deleted_count"]),
		intValue(data["untracked_count"]),
	))

	for _, raw := range asSlice(data["files"])[:minInt(len(asSlice(data["files"])), 12)] {
		entry := asMap(raw)
		if len(entry) == 0 {
			continue
		}
		status := firstNonEmpty(stringValue(entry["status"]), strings.TrimSpace(stringValue(entry["staging_area"])+stringValue(entry["working_tree"])))
		lines = append(lines, fmt.Sprintf("%s %s", status, stringValue(entry["path"])))
	}
	if boolValue(data["clean"]) {
		lines = append(lines, "working tree clean")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeGitStatusShort(data map[string]any) string {
	files := asSlice(data["files"])
	if len(files) == 0 && boolValue(data["clean"]) {
		return "working tree clean"
	}

	statusCounts := map[string]int{}
	previewLines := make([]string, 0, minInt(len(files), 6))
	for _, raw := range files[:minInt(len(files), 20)] {
		entry := asMap(raw)
		if len(entry) == 0 {
			continue
		}
		staging := stringValue(entry["staging_area"])
		working := stringValue(entry["working_tree"])
		if staging == "" {
			staging = " "
		}
		if working == "" {
			working = " "
		}
		code := strings.TrimSpace(staging + working)
		if code == "" {
			code = strings.TrimSpace(stringValue(entry["status"]))
		}
		if code == "" {
			code = "?"
		}
		statusCounts[code]++
		if len(previewLines) < 6 {
			previewLines = append(previewLines, strings.TrimRight(staging+working+" "+stringValue(entry["path"]), " "))
		}
	}

	if len(files) <= 6 {
		return strings.TrimSpace(strings.Join(previewLines, "\n"))
	}

	parts := make([]string, 0, len(statusCounts))
	for code, count := range statusCounts {
		parts = append(parts, fmt.Sprintf("%s:%d", code, count))
	}
	sort.Strings(parts)
	lines := []string{fmt.Sprintf("%d paths (%s)", len(files), strings.Join(parts, ", "))}
	lines = append(lines, previewLines...)
	if len(files) > len(previewLines) {
		lines = append(lines, "...")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeGitDiff(data map[string]any) string {
	if stats := asMap(data["stats"]); len(stats) > 0 {
		type stat struct {
			File      string
			Additions int
			Deletions int
		}
		items := make([]stat, 0, len(stats))
		for file, raw := range stats {
			entry := asMap(raw)
			items = append(items, stat{
				File:      file,
				Additions: intValue(entry["additions"]),
				Deletions: intValue(entry["deletions"]),
			})
		}
		sort.Slice(items, func(i, j int) bool {
			left := items[i].Additions + items[i].Deletions
			right := items[j].Additions + items[j].Deletions
			if left != right {
				return left > right
			}
			return items[i].File < items[j].File
		})
		lines := []string{fmt.Sprintf("%d files changed:", intValue(data["files_changed"]))}
		for _, item := range items[:minInt(len(items), 10)] {
			lines = append(lines, fmt.Sprintf("%s  +%d -%d", item.File, item.Additions, item.Deletions))
		}
		return strings.Join(lines, "\n")
	}
	if preview := stringValue(data["preview"]); preview != "" {
		return trimParagraph(preview, 2200)
	}
	return ""
}

func summarizeGitDiffNames(data map[string]any) string {
	files := asStringSlice(data["files"])
	if len(files) == 0 {
		if preview := stringValue(data["preview"]); preview != "" {
			return trimParagraph(preview, 2200)
		}
		return ""
	}
	lines := make([]string, 0, minInt(len(files), 20)+1)
	lines = append(lines, files[:minInt(len(files), 20)]...)
	if len(files) > 20 {
		lines = append(lines, "...")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeGitLog(data map[string]any) string {
	var lines []string
	for _, raw := range asSlice(data["commits"])[:minInt(len(asSlice(data["commits"])), 12)] {
		entry := asMap(raw)
		if len(entry) == 0 {
			continue
		}
		shortHash := firstNonEmpty(stringValue(entry["short_hash"]), stringValue(entry["hash"]))
		message := stringValue(entry["message"])
		if shortHash == "" && message == "" {
			continue
		}
		lines = append(lines, strings.TrimSpace(shortHash+" "+message))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeGoTest(data map[string]any) string {
	lines := []string{
		fmt.Sprintf(
			"%d packages: %d passed, %d failed, %d skipped",
			intValue(data["total_packages"]),
			intValue(data["passed"]),
			intValue(data["failed"]),
			intValue(data["skipped"]),
		),
	}
	for _, raw := range asSlice(data["results"]) {
		entry := asMap(raw)
		if stringValue(entry["status"]) != "fail" {
			continue
		}
		lines = append(lines, "FAIL "+stringValue(entry["package"]))
	}
	if preview := stringValue(data["stderr_preview"]); preview != "" {
		lines = append(lines, "stderr: "+skillout.TruncateSingleLine(preview, 160))
	} else if preview := stringValue(data["output_preview"]); preview != "" {
		lines = append(lines, "output: "+skillout.TruncateSingleLine(preview, 160))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizePytestRun(data map[string]any) string {
	lines := []string{
		fmt.Sprintf("pytest: %d passed, %d failed, %d skipped", intValue(data["passed"]), intValue(data["failed"]), intValue(data["skipped"])),
	}
	if preview := firstNonEmpty(stringValue(data["stderr_preview"]), stringValue(data["output_preview"])); preview != "" {
		lines = append(lines, skillout.TruncateSingleLine(preview, 180))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeNPMRun(data map[string]any) string {
	lines := []string{
		fmt.Sprintf(
			"npm test: %d passed, %d failed, %d skipped",
			intValue(data["passed"]),
			intValue(data["failed"]),
			intValue(data["skipped"]),
		),
	}
	if totalSuites := intValue(data["total_suites"]); totalSuites > 0 {
		lines = append(lines, fmt.Sprintf("suites: %d passed, %d failed, %d total", intValue(data["passed_suites"]), intValue(data["failed_suites"]), totalSuites))
	}
	if preview := firstNonEmpty(stringValue(data["stderr_preview"]), stringValue(data["output_preview"])); preview != "" {
		lines = append(lines, skillout.TruncateSingleLine(preview, 180))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeCargoRun(data map[string]any) string {
	lines := []string{
		fmt.Sprintf("cargo test: %d passed, %d failed, %d skipped", intValue(data["passed"]), intValue(data["failed"]), intValue(data["skipped"])),
	}
	if preview := firstNonEmpty(stringValue(data["stderr_preview"]), stringValue(data["output_preview"])); preview != "" {
		lines = append(lines, skillout.TruncateSingleLine(preview, 180))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeRuffCheck(data map[string]any) string {
	lines := []string{
		fmt.Sprintf("ruff: %d findings", intValue(data["issue_count"])),
	}
	for _, raw := range asSlice(data["preview"])[:minInt(len(asSlice(data["preview"])), 5)] {
		entry := asMap(raw)
		if len(entry) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s:%d %s %s", stringValue(entry["file"]), intValue(entry["line"]), stringValue(entry["code"]), stringValue(entry["message"])))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeDockerPS(data map[string]any) string {
	lines := []string{fmt.Sprintf("docker ps: %d containers", intValue(data["container_count"]))}
	for _, raw := range asSlice(data["preview"])[:minInt(len(asSlice(data["preview"])), 6)] {
		entry := asMap(raw)
		if len(entry) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", stringValue(entry["name"]), stringValue(entry["image"]), stringValue(entry["status"])))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeKubectlGet(data map[string]any) string {
	if count := intValue(data["item_count"]); count > 0 {
		lines := []string{fmt.Sprintf("kubectl get: %d items", count)}
		for _, raw := range asSlice(data["preview"])[:minInt(len(asSlice(data["preview"])), 8)] {
			entry := asMap(raw)
			lines = append(lines, strings.TrimSpace(firstNonEmpty(stringValue(entry["namespace"])+" ", "")+stringValue(entry["name"])))
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	name := stringValue(data["name"])
	kind := stringValue(data["kind"])
	if name != "" || kind != "" {
		return strings.TrimSpace("kubectl get: " + strings.TrimSpace(kind+" "+name))
	}
	return summarizeGenericNative(data, "kubectl get")
}

func summarizeGenericNative(data map[string]any, label string) string {
	lines := []string{label}
	if out := firstNonEmpty(stringValue(data["stdout"]), stringValue(data["stderr"])); out != "" {
		lines = append(lines, skillout.TruncateSingleLine(out, 220))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func summarizeMixCompile(data map[string]any) string {
	return strings.TrimSpace(fmt.Sprintf("mix compile: %d warnings, %d errors", intValue(data["warning_count"]), intValue(data["error_count"])))
}

func summarizeMixTest(data map[string]any) string {
	lines := []string{fmt.Sprintf("mix test: %d passed, %d failed, %d ignored", intValue(data["passed"]), intValue(data["failed"]), intValue(data["ignored"]))}
	if out := firstNonEmpty(stringValue(data["stdout"]), stringValue(data["stderr"])); out != "" {
		lines = append(lines, skillout.TruncateSingleLine(out, 220))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func asMap(value any) map[string]any {
	if out, ok := value.(map[string]any); ok {
		return out
	}
	return map[string]any{}
}

func asSlice(value any) []any {
	if out, ok := value.([]any); ok {
		return out
	}
	return nil
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func boolValue(value any) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	return false
}

func floatValue(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return n
		}
	}
	return 0
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n
		}
	}
	return 0
}

func asStringSlice(value any) []string {
	raw := asSlice(value)
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s := stringValue(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositiveInt(values ...any) int {
	for _, value := range values {
		if n := intValue(value); n > 0 {
			return n
		}
	}
	return 0
}

func trimParagraph(value string, maxLen int) string {
	return strings.TrimSpace(skillout.TruncateStringWithSuffix(strings.TrimSpace(value), maxLen, "\n... (truncated)"))
}

func humanSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	units := []string{"K", "M", "G", "T"}
	value := float64(n)
	unit := "B"
	for _, next := range units {
		value /= 1024
		unit = next
		if value < 1024 {
			break
		}
	}
	if value >= 100 {
		return fmt.Sprintf("%.0f%s", value, unit)
	}
	if value >= 10 {
		return fmt.Sprintf("%.1f%s", value, unit)
	}
	return fmt.Sprintf("%.2f%s", value, unit)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
