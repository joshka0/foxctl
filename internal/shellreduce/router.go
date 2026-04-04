package shellreduce

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Route describes one structured shell intent mapped onto an existing skill.
type Route struct {
	Intent string
	Skill  string
	Native string
	Input  map[string]any
	Notes  []string
}

// ErrUnsupported reports that the requested shell command is not yet routable.
type ErrUnsupported struct {
	Command string
	Reason  string
}

var (
	sedRangeRe      = regexp.MustCompile(`(?i)^\s*(\d+)\s*,\s*(\d+)\s*p\s*$`)
	sedRangeTokenRe = regexp.MustCompile(`(?i)^\s*\d+\s*,\s*\d+\s*p\s*$`)
)

func (e ErrUnsupported) Error() string {
	command := strings.TrimSpace(e.Command)
	if command == "" {
		command = "command"
	}
	if strings.TrimSpace(e.Reason) == "" {
		return fmt.Sprintf("%s is not supported", command)
	}
	return fmt.Sprintf("%s is not supported: %s", command, e.Reason)
}

// SupportedFamilies returns the command families the router currently handles.
func SupportedFamilies() []string {
	return []string{
		"ls",
		"tree",
		"find",
		"cat",
		"read",
		"head",
		"tail",
		"wc",
		"grep",
		"rg",
		"git status",
		"git diff",
		"git log",
		"go test",
		"cargo test",
		"mix",
		"pytest",
		"npm test",
		"pnpm test",
		"yarn test",
		"ruff check",
		"docker ps",
	}
}

// SplitCommand tokenizes a shell-like command string.
func SplitCommand(command string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}

	var (
		tokens  []string
		buf     strings.Builder
		quote   rune
		escaped bool
	)

	for _, r := range command {
		switch {
		case escaped:
			buf.WriteRune(r)
			escaped = false
		case r == '\\' && quote == 0:
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			buf.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			if buf.Len() > 0 {
				tokens = append(tokens, buf.String())
				buf.Reset()
			}
		default:
			buf.WriteRune(r)
		}
	}

	if escaped {
		buf.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted string")
	}
	if buf.Len() > 0 {
		tokens = append(tokens, buf.String())
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	return tokens, nil
}

// JoinCommand renders argv back into a shell-safe display string.
func JoinCommand(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			parts = append(parts, "''")
			continue
		}
		if strings.IndexFunc(arg, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune(`"'$&|;<>()[]{}*?!`, r)
		}) < 0 {
			parts = append(parts, arg)
			continue
		}
		if !strings.Contains(arg, "'") {
			parts = append(parts, "'"+arg+"'")
			continue
		}
		parts = append(parts, "'"+strings.ReplaceAll(arg, "'", `'"'"'`)+"'")
	}
	return strings.Join(parts, " ")
}

// RouteArgv maps a supported shell argv slice to a structured skill route.
func RouteArgv(argv []string) (Route, error) {
	if len(argv) == 0 {
		return Route{}, fmt.Errorf("command is required")
	}
	if route, ok, err := routeWCSlice(argv); ok {
		return route, err
	}
	argv = normalizeRouteArgv(argv)
	if len(argv) == 0 {
		return Route{}, fmt.Errorf("command is required")
	}
	if route, ok, err := routeLineRange(argv); ok {
		return route, err
	}
	if route, ok, err := routePipeSlice(argv); ok {
		return route, err
	}

	switch strings.TrimSpace(argv[0]) {
	case "ls":
		return routeLS(argv)
	case "tree":
		return routeTree(argv)
	case "find":
		return routeFind(argv)
	case "cat", "read":
		return routeRead(argv)
	case "head":
		return routeHead(argv)
	case "tail":
		return routeTail(argv)
	case "wc":
		return routeWC(argv)
	case "grep", "rg":
		return routeGrep(argv)
	case "git":
		return routeGit(argv)
	case "go":
		return routeGo(argv)
	case "cargo":
		return routeCargo(argv)
	case "mix":
		return routeMix(argv)
	case "pytest":
		return routePytest(argv)
	case "python":
		return routePython(argv)
	case "npm":
		return routeNPM(argv)
	case "pnpm":
		return routePNPM(argv)
	case "yarn":
		return routeYarn(argv)
	case "ruff":
		return routeRuff(argv)
	case "docker":
		return routeDocker(argv)
	case "kubectl":
		return routeKubectl(argv)
	default:
		return Route{}, ErrUnsupported{
			Command: JoinCommand(argv),
			Reason:  "supported families are ls, tree, find, cat/read/head/tail/wc, grep/rg, git status/diff/log, go/cargo/mix/pytest/npm/pnpm/yarn test, kubectl get/describe/logs/rollout status, ruff check, and docker ps",
		}
	}
}

func routeWCSlice(argv []string) (Route, bool, error) {
	segments := splitControlSegments(argv)
	if len(segments) < 2 {
		return Route{}, false, nil
	}

	var (
		wcPath string
		mode   string
	)
	for _, segment := range segments {
		clean := stripRedirectionTokens(segment)
		if len(clean) == 0 {
			continue
		}
		if clean[0] == "wc" {
			m, p, ok := parseWCSegment(clean)
			if ok {
				mode = m
				wcPath = p
				break
			}
		}
	}
	if wcPath == "" {
		return Route{}, false, nil
	}

	for _, segment := range segments {
		clean := stripRedirectionTokens(segment)
		if len(clean) == 0 || clean[0] == "wc" || clean[0] == "true" || clean[0] == "printf" || clean[0] == "echo" {
			continue
		}
		if route, ok, err := routeSingleSliceOnPath(clean, wcPath, mode); ok {
			return route, true, err
		}
	}

	return Route{}, false, nil
}

func routeSingleSliceOnPath(tokens []string, path, wcMode string) (Route, bool, error) {
	switch tokens[0] {
	case "tail", "head":
		count, ok := parseLineCountSegment(tokens)
		if !ok {
			return Route{}, true, ErrUnsupported{Command: JoinCommand(tokens), Reason: "invalid line count in slice command"}
		}
		pathArg := lastNonFlagToken(tokens[1:])
		if pathArg != "" {
			path = trimQuotes(pathArg)
		}
		intent := "file_wc_slice"
		return Route{
			Intent: intent,
			Native: intent,
			Input: map[string]any{
				"path":       path,
				"wc_mode":    wcMode,
				"slice_mode": tokens[0],
				"lines":      count,
			},
		}, true, nil
	case "sed":
		start, end, ok := parseSedRangeTokens(tokens)
		if !ok {
			return Route{}, true, ErrUnsupported{Command: JoinCommand(tokens), Reason: "invalid sed range in slice command"}
		}
		return Route{
			Intent: "file_wc_slice",
			Native: "file_wc_slice",
			Input: map[string]any{
				"path":       path,
				"wc_mode":    wcMode,
				"slice_mode": "range",
				"line_start": start,
				"line_end":   end,
			},
		}, true, nil
	default:
		return Route{}, false, nil
	}
}

func normalizeRouteArgv(argv []string) []string {
	normalized := trimTailRouteTokens(argv)
	normalized = stripLeadingAssignments(normalized)
	if inner := unwrapRouteShellInvocation(normalized); len(inner) > 0 {
		return normalizeRouteArgv(inner)
	}
	return normalized
}

func trimTailRouteTokens(argv []string) []string {
	last := 0
	for i, token := range argv {
		if token == "&&" || token == ";" {
			last = i + 1
		}
	}
	if last <= 0 || last >= len(argv) {
		return append([]string(nil), argv...)
	}
	return append([]string(nil), argv[last:]...)
}

func stripLeadingAssignments(argv []string) []string {
	index := 0
	for index < len(argv) && looksLikeRouteAssignment(argv[index]) {
		index++
	}
	return argv[index:]
}

func looksLikeRouteAssignment(token string) bool {
	if !strings.Contains(token, "=") || strings.HasPrefix(token, "=") {
		return false
	}
	if strings.Contains(token, "/") {
		return false
	}
	key := token[:strings.Index(token, "=")]
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case i == 0 && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_'):
		case i > 0 && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'):
		default:
			return false
		}
	}
	return true
}

func unwrapRouteShellInvocation(argv []string) []string {
	if len(argv) >= 3 && (argv[0] == "bash" || argv[0] == "zsh" || argv[0] == "sh") && argv[1] == "-lc" {
		inner, err := SplitCommand(argv[2])
		if err == nil {
			return inner
		}
	}
	return nil
}

func routeLineRange(argv []string) (Route, bool, error) {
	segments := splitPipeSegments(argv)
	if len(segments) == 0 {
		return Route{}, false, nil
	}

	var (
		lineStart int
		lineEnd   int
		filePath  string
		hasSed    bool
	)

	for idx, segment := range segments {
		start, end, ok := parseSedRangeTokens(segment)
		if !ok {
			continue
		}
		hasSed = true
		lineStart = start
		lineEnd = end
		filePath = extractSedFileTokens(segment)
		if filePath == "" && idx > 0 {
			filePath = extractSegmentFile(segments[idx-1])
		}
		break
	}
	if !hasSed {
		return Route{}, false, nil
	}
	if filePath == "" {
		if len(segments) > 1 {
			return Route{}, false, nil
		}
		return Route{}, true, ErrUnsupported{Command: JoinCommand(argv), Reason: "could not determine file path for sed line-range command"}
	}

	return Route{
		Intent: "line_range",
		Skill:  "code/context_grep",
		Input: map[string]any{
			"mode":       "line",
			"file_path":  filePath,
			"line_start": lineStart,
			"line_end":   lineEnd,
		},
	}, true, nil
}

func routePipeSlice(argv []string) (Route, bool, error) {
	segments := splitPipeSegments(argv)
	if len(segments) != 2 {
		return Route{}, false, nil
	}
	base := segments[0]
	sliceSeg := segments[1]
	if len(base) == 0 || len(sliceSeg) == 0 {
		return Route{}, false, nil
	}
	// Prefer the file-backed line-range reducer when the left side is cat/nl.
	if (base[0] == "cat" || base[0] == "nl") && parseSliceMode(sliceSeg) == "range" {
		return Route{}, false, nil
	}

	mode, count, start, end, ok := parseSliceSegment(sliceSeg)
	if !ok {
		return Route{}, false, nil
	}
	input := map[string]any{
		"base_argv":  base,
		"slice_mode": mode,
	}
	if count > 0 {
		input["lines"] = count
	}
	if start > 0 {
		input["line_start"] = start
	}
	if end > 0 {
		input["line_end"] = end
	}
	return Route{
		Intent: "pipe_slice",
		Native: "pipe_line_slice",
		Input:  input,
	}, true, nil
}

func parseSliceMode(tokens []string) string {
	mode, _, _, _, ok := parseSliceSegment(tokens)
	if !ok {
		return ""
	}
	return mode
}

func parseSliceSegment(tokens []string) (mode string, count int, lineStart int, lineEnd int, ok bool) {
	if len(tokens) == 0 {
		return "", 0, 0, 0, false
	}
	switch tokens[0] {
	case "head":
		n, ok := parseLineCountSegment(tokens)
		return "head", n, 0, 0, ok
	case "tail":
		n, ok := parseLineCountSegment(tokens)
		return "tail", n, 0, 0, ok
	case "sed":
		start, end, ok := parseSedRangeTokens(tokens)
		return "range", 0, start, end, ok
	default:
		return "", 0, 0, 0, false
	}
}

func parseLineCountSegment(tokens []string) (int, bool) {
	if len(tokens) == 1 {
		return 10, true
	}
	for i := 1; i < len(tokens); i++ {
		token := tokens[i]
		switch {
		case token == "-n":
			if i+1 >= len(tokens) {
				return 0, false
			}
			n, err := strconv.Atoi(tokens[i+1])
			if err != nil || n <= 0 {
				return 0, false
			}
			return n, true
		case strings.HasPrefix(token, "-n="):
			n, err := strconv.Atoi(strings.TrimPrefix(token, "-n="))
			if err != nil || n <= 0 {
				return 0, false
			}
			return n, true
		case strings.HasPrefix(token, "-") && len(token) > 1 && allDigits(token[1:]):
			n, err := strconv.Atoi(token[1:])
			if err != nil || n <= 0 {
				return 0, false
			}
			return n, true
		case strings.HasPrefix(token, "-"):
			return 0, false
		default:
			return 0, false
		}
	}
	return 10, true
}

func splitControlSegments(argv []string) [][]string {
	if len(argv) == 0 {
		return nil
	}
	var (
		segments [][]string
		current  []string
	)
	for _, token := range argv {
		if token == "&&" || token == "||" || token == ";" {
			if len(current) > 0 {
				segments = append(segments, current)
				current = nil
			}
			continue
		}
		current = append(current, token)
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}
	return segments
}

func parseWCSegment(tokens []string) (mode, path string, ok bool) {
	if len(tokens) < 2 || tokens[0] != "wc" {
		return "", "", false
	}
	mode = "all"
	for i := 1; i < len(tokens); i++ {
		token := strings.TrimSpace(tokens[i])
		switch token {
		case "-l":
			mode = "lines"
		case "-c":
			mode = "bytes"
		case "-w":
			mode = "words"
		default:
			if strings.HasPrefix(token, "-") {
				return "", "", false
			}
			if path != "" {
				return "", "", false
			}
			path = trimQuotes(token)
		}
	}
	if path == "" {
		return "", "", false
	}
	return mode, path, true
}

func stripRedirectionTokens(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	skipNext := false
	for _, token := range tokens {
		if skipNext {
			skipNext = false
			continue
		}
		switch token {
		case ">", ">>", "2>", "2>>":
			skipNext = true
			continue
		case "2>&1", "1>&2":
			continue
		}
		if strings.HasPrefix(token, ">") || strings.HasPrefix(token, ">>") || strings.HasPrefix(token, "2>") || strings.HasPrefix(token, "2>>") {
			continue
		}
		out = append(out, token)
	}
	return out
}

func lastNonFlagToken(tokens []string) string {
	for i := len(tokens) - 1; i >= 0; i-- {
		token := strings.TrimSpace(tokens[i])
		if token == "" || strings.HasPrefix(token, "-") {
			continue
		}
		return token
	}
	return ""
}

func splitPipeSegments(argv []string) [][]string {
	if len(argv) == 0 {
		return nil
	}
	var (
		segments [][]string
		current  []string
	)
	for _, token := range argv {
		if token == "|" {
			if len(current) > 0 {
				segments = append(segments, current)
				current = nil
			}
			continue
		}
		current = append(current, token)
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}
	return segments
}

func parseSedRangeTokens(tokens []string) (int, int, bool) {
	for i, token := range tokens {
		if strings.EqualFold(strings.TrimSpace(token), "sed") {
			for j := i + 1; j < len(tokens); j++ {
				if strings.TrimSpace(tokens[j]) == "-n" {
					if j+1 >= len(tokens) {
						return 0, 0, false
					}
					return parseSedRangeToken(tokens[j+1])
				}
			}
		}
	}
	return 0, 0, false
}

func parseSedRangeToken(token string) (int, int, bool) {
	matches := sedRangeRe.FindStringSubmatch(strings.TrimSpace(token))
	if len(matches) != 3 {
		return 0, 0, false
	}
	start, err := strconv.Atoi(matches[1])
	if err != nil || start <= 0 {
		return 0, 0, false
	}
	end, err := strconv.Atoi(matches[2])
	if err != nil || end < start {
		return 0, 0, false
	}
	return start, end, true
}

func extractSedFileTokens(tokens []string) string {
	rangeIndex := -1
	for i, token := range tokens {
		if sedRangeTokenRe.MatchString(strings.TrimSpace(token)) {
			rangeIndex = i
			break
		}
	}
	if rangeIndex == -1 {
		return ""
	}
	for i := rangeIndex + 1; i < len(tokens); i++ {
		token := strings.TrimSpace(tokens[i])
		if token == "" {
			continue
		}
		if token == "<" && i+1 < len(tokens) {
			return trimQuotes(tokens[i+1])
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		return trimQuotes(token)
	}
	return ""
}

func extractSegmentFile(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	switch strings.TrimSpace(tokens[0]) {
	case "cat":
		return extractCatFileTokens(tokens)
	case "nl":
		return extractNLFileTokens(tokens)
	default:
		return ""
	}
}

func extractCatFileTokens(tokens []string) string {
	for i := 1; i < len(tokens); i++ {
		token := strings.TrimSpace(tokens[i])
		if token == "" {
			continue
		}
		if token == "<" && i+1 < len(tokens) {
			return trimQuotes(tokens[i+1])
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		return trimQuotes(token)
	}
	return ""
}

func extractNLFileTokens(tokens []string) string {
	for i := 1; i < len(tokens); i++ {
		token := strings.TrimSpace(tokens[i])
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		return trimQuotes(token)
	}
	return ""
}

func trimQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
		return value[1 : len(value)-1]
	}
	return value
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func routeLS(argv []string) (Route, error) {
	showHidden := false
	var paths []string

	for i := 1; i < len(argv); i++ {
		token := argv[i]
		if token == "--" {
			paths = append(paths, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(token, "--") {
			switch token {
			case "--all", "--almost-all":
				showHidden = true
			case "--long", "--human-readable", "--classify", "--group-directories-first":
			default:
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported ls flag " + token}
			}
			continue
		}
		if strings.HasPrefix(token, "-") && token != "-" {
			for _, ch := range token[1:] {
				switch ch {
				case 'a', 'A':
					showHidden = true
				case 'l', 'h', 'F':
				default:
					return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: fmt.Sprintf("unsupported ls flag -%c", ch)}
				}
			}
			continue
		}
		paths = append(paths, token)
	}

	path, err := singlePathOrDefault(paths, ".")
	if err != nil {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: err.Error()}
	}

	return Route{
		Intent: "ls",
		Skill:  "fs/ls",
		Input: map[string]any{
			"path":        path,
			"show_hidden": showHidden,
			"max_entries": 200,
		},
	}, nil
}

func routeTree(argv []string) (Route, error) {
	showHidden := false
	dirsOnly := false
	maxDepth := 3
	var paths []string

	for i := 1; i < len(argv); i++ {
		token := argv[i]
		if token == "--" {
			paths = append(paths, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(token, "--") {
			switch token {
			case "--all":
				showHidden = true
			case "--dirsfirst":
			default:
				if strings.HasPrefix(token, "--level=") {
					value := strings.TrimPrefix(token, "--level=")
					n, err := strconv.Atoi(value)
					if err != nil || n < 0 {
						return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "invalid tree depth"}
					}
					maxDepth = n
					continue
				}
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported tree flag " + token}
			}
			continue
		}
		if strings.HasPrefix(token, "-") && token != "-" {
			if strings.HasPrefix(token, "-L") && len(token) > 2 {
				n, err := strconv.Atoi(token[2:])
				if err != nil || n < 0 {
					return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "invalid tree depth"}
				}
				maxDepth = n
				continue
			}
			for _, ch := range token[1:] {
				switch ch {
				case 'a':
					showHidden = true
				case 'd':
					dirsOnly = true
				case 'L':
					if i+1 >= len(argv) {
						return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "tree -L requires a depth"}
					}
					i++
					n, err := strconv.Atoi(argv[i])
					if err != nil || n < 0 {
						return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "invalid tree depth"}
					}
					maxDepth = n
				default:
					return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: fmt.Sprintf("unsupported tree flag -%c", ch)}
				}
			}
			continue
		}
		paths = append(paths, token)
	}

	path, err := singlePathOrDefault(paths, ".")
	if err != nil {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: err.Error()}
	}

	return Route{
		Intent: "tree",
		Skill:  "fs/tree",
		Input: map[string]any{
			"path":           path,
			"include_hidden": showHidden,
			"dirs_only":      dirsOnly,
			"include_size":   true,
			"max_depth":      maxDepth,
			"format":         "tree",
		},
	}, nil
}

func routeRead(argv []string) (Route, error) {
	var (
		maxBytes int
		paths    []string
	)

	for i := 1; i < len(argv); i++ {
		token := argv[i]
		if token == "--" {
			paths = append(paths, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(token, "--max-bytes=") {
			n, err := strconv.Atoi(strings.TrimPrefix(token, "--max-bytes="))
			if err != nil || n <= 0 {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "invalid max bytes"}
			}
			maxBytes = n
			continue
		}
		if strings.HasPrefix(token, "-") && token != "-" {
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "flags are not supported for cat/read yet"}
		}
		paths = append(paths, token)
	}

	path, err := singlePathOrDefault(paths, "")
	if err != nil {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: err.Error()}
	}
	if path == "" {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "cat/read requires exactly one file path"}
	}

	input := map[string]any{"path": path}
	if maxBytes > 0 {
		input["max_bytes"] = maxBytes
	}

	return Route{
		Intent: "read",
		Skill:  "fs/read",
		Input:  input,
	}, nil
}

func routeHead(argv []string) (Route, error) {
	return routeFileSlice("head", "file_head", argv)
}

func routeTail(argv []string) (Route, error) {
	return routeFileSlice("tail", "file_tail", argv)
}

func routeFileSlice(commandName, intent string, argv []string) (Route, error) {
	lines := 10
	var paths []string
	for i := 1; i < len(argv); i++ {
		token := argv[i]
		switch {
		case token == "-n":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: commandName + " -n requires a value"}
			}
			i++
			n, err := strconv.Atoi(argv[i])
			if err != nil || n <= 0 {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "invalid " + commandName + " line count"}
			}
			lines = n
		case strings.HasPrefix(token, "-n="):
			n, err := strconv.Atoi(strings.TrimPrefix(token, "-n="))
			if err != nil || n <= 0 {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "invalid " + commandName + " line count"}
			}
			lines = n
		case strings.HasPrefix(token, "-") && len(token) > 1 && allDigits(token[1:]):
			n, err := strconv.Atoi(token[1:])
			if err != nil || n <= 0 {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "invalid " + commandName + " line count"}
			}
			lines = n
		case strings.HasPrefix(token, "-"):
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported " + commandName + " flag " + token}
		default:
			paths = append(paths, token)
		}
	}
	path, err := singlePathOrDefault(paths, "")
	if err != nil || path == "" {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: commandName + " requires exactly one file path"}
	}
	return Route{
		Intent: intent,
		Native: intent,
		Input: map[string]any{
			"path":  path,
			"lines": lines,
		},
	}, nil
}

func routeWC(argv []string) (Route, error) {
	mode := "all"
	var paths []string
	for i := 1; i < len(argv); i++ {
		token := argv[i]
		switch token {
		case "-l":
			mode = "lines"
		case "-c":
			mode = "bytes"
		case "-w":
			mode = "words"
		default:
			if strings.HasPrefix(token, "-") {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported wc flag " + token}
			}
			paths = append(paths, token)
		}
	}
	path, err := singlePathOrDefault(paths, "")
	if err != nil || path == "" {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "wc requires exactly one file path"}
	}
	return Route{
		Intent: "file_wc",
		Native: "file_wc",
		Input: map[string]any{
			"path": path,
			"mode": mode,
		},
	}, nil
}

func routeFind(argv []string) (Route, error) {
	path := "."
	pattern := ""
	entryType := "any"
	maxDepth := 0
	hidden := true

	for i := 1; i < len(argv); i++ {
		token := argv[i]
		switch token {
		case "-print":
			continue
		case "-name":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "find -name requires a pattern"}
			}
			i++
			pattern = argv[i]
		case "-type":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "find -type requires a value"}
			}
			i++
			switch argv[i] {
			case "f":
				entryType = "file"
			case "d":
				entryType = "directory"
			default:
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "find -type only supports f or d"}
			}
		case "-maxdepth":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "find -maxdepth requires a value"}
			}
			i++
			n, err := strconv.Atoi(argv[i])
			if err != nil || n < 0 {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "invalid find maxdepth"}
			}
			maxDepth = n
		default:
			if strings.HasPrefix(token, "-") {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported find flag " + token}
			}
			if path != "." {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "multiple find roots are not supported yet"}
			}
			path = token
		}
	}

	input := map[string]any{
		"path":        path,
		"type":        entryType,
		"hidden":      hidden,
		"max_results": 200,
		"sort_by":     "name",
	}
	if pattern != "" {
		input["pattern"] = pattern
	}
	if maxDepth > 0 {
		input["max_depth"] = maxDepth
	}

	return Route{
		Intent: "find",
		Skill:  "fs/find",
		Input:  input,
	}, nil
}

func routeGrep(argv []string) (Route, error) {
	ignoreCase := false
	var positional []string

	for i := 1; i < len(argv); i++ {
		token := argv[i]
		if token == "--" {
			positional = append(positional, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(token, "--") {
			switch token {
			case "--ignore-case":
				ignoreCase = true
			case "--line-number", "--with-filename", "--hidden":
			default:
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported grep flag " + token}
			}
			continue
		}
		if strings.HasPrefix(token, "-") && token != "-" {
			for _, ch := range token[1:] {
				switch ch {
				case 'i':
					ignoreCase = true
				case 'n', 'r', 'R', 'H':
				default:
					return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: fmt.Sprintf("unsupported grep flag -%c", ch)}
				}
			}
			continue
		}
		positional = append(positional, token)
	}

	if len(positional) == 0 {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "grep/rg requires a pattern"}
	}
	if len(positional) > 2 {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "multiple search roots are not supported yet"}
	}

	pattern := positional[0]
	path := "."
	if len(positional) == 2 {
		path = positional[1]
	}

	return Route{
		Intent: "grep",
		Skill:  "text/grep",
		Input: map[string]any{
			"path":        path,
			"pattern":     pattern,
			"ci":          ignoreCase,
			"max_matches": 200,
		},
	}, nil
}

func routeGit(argv []string) (Route, error) {
	if len(argv) < 2 {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "git requires a subcommand"}
	}

	switch argv[1] {
	case "status":
		short := false
		for _, token := range argv[2:] {
			switch token {
			case "--short", "--porcelain", "--branch":
				if token == "--short" || token == "--porcelain" {
					short = true
				}
			case "-s":
				short = true
			default:
				if strings.HasPrefix(token, "-") && token != "-" {
					for _, ch := range token[1:] {
						switch ch {
						case 's':
							short = true
						case 'b':
						default:
							return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported git status flag " + token}
						}
					}
					continue
				}
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported git status flag " + token}
			}
		}
		intent := "git_status"
		if short {
			intent = "git_status_short"
		}
		return Route{
			Intent: intent,
			Skill:  "git/status",
			Input: map[string]any{
				"operation": "status",
				"repo_path": ".",
			},
		}, nil
	case "diff":
		return routeGitDiff(argv)
	case "log":
		return routeGitLog(argv)
	default:
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported git subcommands are status, diff, and log"}
	}
}

func routeGitDiff(argv []string) (Route, error) {
	staged := false
	stat := false
	nameOnly := false
	var refs []string

	for _, token := range argv[2:] {
		if strings.HasPrefix(token, "--") {
			switch token {
			case "--staged", "--cached":
				staged = true
			case "--stat":
				stat = true
			case "--name-only":
				nameOnly = true
			default:
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported git diff flag " + token}
			}
			continue
		}
		if strings.HasPrefix(token, "-") && token != "-" {
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported short git diff flag " + token}
		}
		refs = append(refs, token)
	}

	if len(refs) > 1 {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "git diff supports at most one ref in this reduced mode"}
	}
	if stat && nameOnly {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "git diff reduced mode supports either --stat or --name-only, not both"}
	}

	input := map[string]any{
		"operation": "diff",
		"repo_path": ".",
		"staged":    staged,
		"stat":      stat,
		"name_only": nameOnly,
	}
	if len(refs) == 1 {
		input["commit"] = refs[0]
	}
	intent := "git_diff"
	if nameOnly {
		intent = "git_diff_names"
	}

	return Route{
		Intent: intent,
		Skill:  "git/status",
		Input:  input,
	}, nil
}

func routeGitLog(argv []string) (Route, error) {
	limit := 10
	var (
		notes []string
		refs  []string
	)

	for i := 2; i < len(argv); i++ {
		token := argv[i]
		switch {
		case token == "--stat":
			notes = append(notes, "ignored --stat and returned compact commit log output")
		case token == "-n":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "git log -n requires a value"}
			}
			i++
			n, err := strconv.Atoi(argv[i])
			if err != nil || n <= 0 {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "invalid git log limit"}
			}
			limit = n
		case strings.HasPrefix(token, "-") && len(token) > 1 && isAllDigits(token[1:]):
			n, err := strconv.Atoi(token[1:])
			if err != nil || n <= 0 {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "invalid git log limit"}
			}
			limit = n
		case strings.HasPrefix(token, "--"):
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported git log flag " + token}
		case strings.HasPrefix(token, "-"):
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported short git log flag " + token}
		default:
			refs = append(refs, token)
		}
	}

	if len(refs) > 1 {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "git log supports at most one ref in this reduced mode"}
	}

	input := map[string]any{
		"operation": "log",
		"repo_path": ".",
		"limit":     limit,
	}
	if len(refs) == 1 {
		input["commit"] = refs[0]
	}

	return Route{
		Intent: "git_log",
		Skill:  "git/status",
		Input:  input,
		Notes:  notes,
	}, nil
}

func routeGo(argv []string) (Route, error) {
	if len(argv) < 2 || argv[1] != "test" {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported go command is go test"}
	}

	mode := "test"
	short := false
	verbose := false
	pattern := ""
	timeout := ""
	var paths []string

	for i := 2; i < len(argv); i++ {
		token := argv[i]
		switch {
		case token == "-race":
			mode = "race"
		case token == "-short":
			short = true
		case token == "-v":
			verbose = true
		case token == "-run":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "go test -run requires a value"}
			}
			i++
			pattern = argv[i]
		case strings.HasPrefix(token, "-run="):
			pattern = strings.TrimPrefix(token, "-run=")
		case token == "-timeout":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "go test -timeout requires a value"}
			}
			i++
			timeout = argv[i]
		case strings.HasPrefix(token, "-timeout="):
			timeout = strings.TrimPrefix(token, "-timeout=")
		case strings.HasPrefix(token, "-"):
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported go test flag " + token}
		default:
			paths = append(paths, token)
		}
	}

	path, err := singlePathOrDefault(paths, "./...")
	if err != nil {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: err.Error()}
	}

	input := map[string]any{
		"path":    path,
		"mode":    mode,
		"short":   short,
		"verbose": verbose,
	}
	if pattern != "" {
		input["pattern"] = pattern
	}
	if timeout != "" {
		input["timeout"] = timeout
	}

	return Route{
		Intent: "go_test",
		Skill:  "test/run",
		Input:  input,
	}, nil
}

func routeCargo(argv []string) (Route, error) {
	if len(argv) < 2 || argv[1] != "test" {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported cargo shell route is cargo test"}
	}
	pattern := ""
	seenPattern := false
	for i := 2; i < len(argv); i++ {
		token := argv[i]
		switch {
		case strings.HasPrefix(token, "-"):
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported cargo test flag " + token}
		default:
			if !seenPattern {
				pattern = token
				seenPattern = true
			} else {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "multiple cargo test selectors are not supported yet"}
			}
		}
	}
	input := map[string]any{
		"path": ".",
		"mode": "cargo",
	}
	if pattern != "" {
		input["pattern"] = pattern
	}
	return Route{Intent: "cargo_test", Skill: "test/run", Input: input}, nil
}

func routeMix(argv []string) (Route, error) {
	if len(argv) < 2 {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported mix routes are compile, test, format, and deps.get"}
	}
	args := append([]string(nil), argv[1:]...)
	switch argv[1] {
	case "compile":
		return Route{Intent: "mix_compile", Native: "mix_compile", Input: map[string]any{"args": args}}, nil
	case "test":
		return Route{Intent: "mix_test", Native: "mix_test", Input: map[string]any{"args": args}}, nil
	case "run":
		return Route{Intent: "mix_run", Native: "mix_run", Input: map[string]any{"args": args}}, nil
	case "format":
		return Route{Intent: "mix_format", Native: "mix_format", Input: map[string]any{"args": args}}, nil
	case "deps.get":
		return Route{Intent: "mix_deps_get", Native: "mix_deps_get", Input: map[string]any{"args": args}}, nil
	case "ecto.create", "ecto.migrate", "ecto.drop":
		return Route{Intent: "mix_ecto", Native: "mix_ecto", Input: map[string]any{"args": args}}, nil
	default:
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported mix routes are compile, test, run, format, deps.get, and ecto.*"}
	}
}

func routePytest(argv []string) (Route, error) {
	verbose := false
	pattern := ""
	path := "."
	seenPath := false

	for i := 1; i < len(argv); i++ {
		token := argv[i]
		switch {
		case token == "-v":
			verbose = true
		case token == "-q":
			// quiet is the default reduced path behavior; no-op
		case token == "-k":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "pytest -k requires a value"}
			}
			i++
			pattern = argv[i]
		case strings.HasPrefix(token, "-k="):
			pattern = strings.TrimPrefix(token, "-k=")
		case strings.HasPrefix(token, "-"):
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported pytest flag " + token}
		default:
			if seenPath {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "multiple pytest paths are not supported yet"}
			}
			path = token
			seenPath = true
		}
	}

	input := map[string]any{
		"path":    path,
		"mode":    "pytest",
		"verbose": verbose,
	}
	if pattern != "" {
		input["pattern"] = pattern
	}

	return Route{
		Intent: "pytest",
		Skill:  "test/run",
		Input:  input,
	}, nil
}

func routePython(argv []string) (Route, error) {
	if len(argv) >= 3 && argv[1] == "-m" && argv[2] == "pytest" {
		return routePytest(append([]string{"pytest"}, argv[3:]...))
	}
	return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported python shell route is python -m pytest"}
}

func routeNPM(argv []string) (Route, error) {
	if len(argv) < 2 || argv[1] != "test" {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported npm shell route is npm test"}
	}

	path := "."
	for i := 2; i < len(argv); i++ {
		token := argv[i]
		switch token {
		case "--prefix":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "npm --prefix requires a path"}
			}
			i++
			path = argv[i]
		default:
			if strings.HasPrefix(token, "--prefix=") {
				path = strings.TrimPrefix(token, "--prefix=")
				continue
			}
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported npm test flag " + token}
		}
	}

	return Route{
		Intent: "npm_test",
		Skill:  "test/run",
		Input: map[string]any{
			"path": path,
			"mode": "npm",
		},
	}, nil
}

func routePNPM(argv []string) (Route, error) {
	if len(argv) < 2 || argv[1] != "test" {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported pnpm shell route is pnpm test"}
	}
	path := "."
	for i := 2; i < len(argv); i++ {
		token := argv[i]
		switch token {
		case "--dir":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "pnpm --dir requires a path"}
			}
			i++
			path = argv[i]
		default:
			if strings.HasPrefix(token, "--dir=") {
				path = strings.TrimPrefix(token, "--dir=")
				continue
			}
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported pnpm test flag " + token}
		}
	}
	return Route{
		Intent: "pnpm_test",
		Skill:  "test/run",
		Input: map[string]any{
			"path": path,
			"mode": "pnpm",
		},
	}, nil
}

func routeYarn(argv []string) (Route, error) {
	if len(argv) < 2 || argv[1] != "test" {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported yarn shell route is yarn test"}
	}
	path := "."
	for i := 2; i < len(argv); i++ {
		token := argv[i]
		switch token {
		case "--cwd":
			if i+1 >= len(argv) {
				return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "yarn --cwd requires a path"}
			}
			i++
			path = argv[i]
		default:
			if strings.HasPrefix(token, "--cwd=") {
				path = strings.TrimPrefix(token, "--cwd=")
				continue
			}
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported yarn test flag " + token}
		}
	}
	return Route{
		Intent: "yarn_test",
		Skill:  "test/run",
		Input: map[string]any{
			"path": path,
			"mode": "yarn",
		},
	}, nil
}

func routeRuff(argv []string) (Route, error) {
	if len(argv) < 2 || argv[1] != "check" {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported ruff shell route is ruff check"}
	}
	path := "."
	for i := 2; i < len(argv); i++ {
		token := argv[i]
		if strings.HasPrefix(token, "-") {
			return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "unsupported ruff check flag " + token}
		}
		path = token
	}
	return Route{
		Intent: "ruff_check",
		Native: "ruff_check",
		Input: map[string]any{
			"path": path,
		},
	}, nil
}

func routeDocker(argv []string) (Route, error) {
	if len(argv) < 2 || argv[1] != "ps" {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported docker shell route is docker ps"}
	}
	if len(argv) > 2 {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "docker ps flags are not supported yet"}
	}
	return Route{
		Intent: "docker_ps",
		Native: "docker_ps",
		Input:  map[string]any{},
	}, nil
}

func routeKubectl(argv []string) (Route, error) {
	if len(argv) < 2 {
		return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported kubectl routes are get, describe, logs, and rollout status"}
	}
	args := append([]string(nil), argv[1:]...)
	switch argv[1] {
	case "get":
		return Route{Intent: "kubectl_get", Native: "kubectl_get", Input: map[string]any{"args": args}}, nil
	case "describe":
		return Route{Intent: "kubectl_describe", Native: "kubectl_describe", Input: map[string]any{"args": args}}, nil
	case "logs":
		return Route{Intent: "kubectl_logs", Native: "kubectl_logs", Input: map[string]any{"args": args}}, nil
	case "rollout":
		if len(argv) > 2 && argv[2] == "status" {
			return Route{Intent: "kubectl_rollout_status", Native: "kubectl_rollout_status", Input: map[string]any{"args": args}}, nil
		}
	}
	return Route{}, ErrUnsupported{Command: JoinCommand(argv), Reason: "supported kubectl routes are get, describe, logs, and rollout status"}
}

func singlePathOrDefault(paths []string, fallback string) (string, error) {
	switch len(paths) {
	case 0:
		return fallback, nil
	case 1:
		return paths[0], nil
	default:
		return "", fmt.Errorf("multiple paths are not supported yet")
	}
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
