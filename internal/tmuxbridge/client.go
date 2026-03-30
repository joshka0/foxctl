package tmuxbridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultSocketSentinel = "__default__"
	fieldSep              = "\x1f"
	listFormat            = "#{pane_id}" + fieldSep + "#{session_name}" + fieldSep + "#{window_index}" + fieldSep + "#{pane_index}" + fieldSep + "#{window_name}" + fieldSep + "#{pane_pid}" + fieldSep + "#{pane_width}" + fieldSep + "#{pane_height}" + fieldSep + "#{@name}" + fieldSep + "#{pane_current_path}" + fieldSep + "#{pane_current_command}" + fieldSep + "#{pane_active}"
	labelFormat           = "#{pane_id}" + fieldSep + "#{@name}"
)

var (
	paneIDPattern     = regexp.MustCompile(`^%[0-9]+$`)
	digitsPattern     = regexp.MustCompile(`^[0-9]+$`)
	bridgeLinePattern = regexp.MustCompile(`^\[tmux-bridge\s+from=([^\s\]]+)\s+pane=([^\s\]]+)\s+reply_to=([^\s\]]+)\]\s*(.*)$`)
)

// Runner executes subprocesses for tmuxbridge operations.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout string, stderr string, err error)
}

// OSRunner executes commands using the local OS process runner.
type OSRunner struct{}

// Run executes a command and returns stdout, stderr, and error.
func (OSRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Pane describes one visible pane in the tmux server.
type Pane struct {
	ID             string `json:"id"`
	Session        string `json:"session"`
	WindowIndex    int    `json:"window_index"`
	PaneIndex      int    `json:"pane_index"`
	SessionPane    string `json:"session_pane"`
	WindowName     string `json:"window_name"`
	PID            int    `json:"pid"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	Size           string `json:"size"`
	Label          string `json:"label,omitempty"`
	CurrentPath    string `json:"current_path,omitempty"`
	CurrentCommand string `json:"current_command,omitempty"`
	Active         bool   `json:"active"`
}

// ReadResult captures one pane snapshot.
type ReadResult struct {
	Target         string   `json:"target"`
	ResolvedTarget string   `json:"resolved_target"`
	Pane           Pane     `json:"pane"`
	LinesRequested int      `json:"lines_requested"`
	Content        string   `json:"content"`
	Lines          []string `json:"lines"`
}

// DoctorReport summarizes tmux connectivity for agentctl.
type DoctorReport struct {
	TmuxPane         string   `json:"tmux_pane,omitempty"`
	TmuxEnv          string   `json:"tmux_env,omitempty"`
	BridgeSocketEnv  string   `json:"bridge_socket_env,omitempty"`
	DetectedSocket   string   `json:"detected_socket,omitempty"`
	DefaultReachable bool     `json:"default_reachable"`
	CurrentPaneSeen  bool     `json:"current_pane_seen"`
	TotalPanes       int      `json:"total_panes"`
	LabeledPanes     int      `json:"labeled_panes"`
	Healthy          bool     `json:"healthy"`
	Issues           []string `json:"issues,omitempty"`
}

// PrepareOptions describes a tmux session preparation request.
type PrepareOptions struct {
	Session        string   `json:"session"`
	Panes          int      `json:"panes"`
	PaneCommand    string   `json:"pane_command,omitempty"`
	Agent          string   `json:"agent,omitempty"`
	AgentArgs      []string `json:"agent_args,omitempty"`
	AgentSessionID string   `json:"agent_session_id,omitempty"`
	CWD            string   `json:"cwd,omitempty"`
	LabelPrefix    string   `json:"label_prefix,omitempty"`
}

// PrepareResult describes one prepared tmux collaboration session.
type PrepareResult struct {
	Session        string   `json:"session"`
	Created        bool     `json:"created"`
	PanesRequested int      `json:"panes_requested"`
	PaneCommand    string   `json:"pane_command,omitempty"`
	Agent          string   `json:"agent,omitempty"`
	AgentArgs      []string `json:"agent_args,omitempty"`
	AgentSessionID string   `json:"agent_session_id,omitempty"`
	CWD            string   `json:"cwd,omitempty"`
	LabelPrefix    string   `json:"label_prefix,omitempty"`
	AttachCommand  string   `json:"attach_command"`
	SocketMode     string   `json:"socket_mode"`
	Panes          []Pane   `json:"panes"`
}

// BridgeMessage is one structured tmux-bridge line parsed from pane scrollback.
type BridgeMessage struct {
	Raw     string `json:"raw"`
	From    string `json:"from"`
	Pane    string `json:"pane"`
	ReplyTo string `json:"reply_to"`
	Content string `json:"content"`
}

// Client exposes read-only access to a reachable tmux server.
type Client struct {
	runner Runner
	env    map[string]string
}

// New returns a client using the process environment and OS runner.
func New() *Client {
	return &Client{
		runner: OSRunner{},
		env:    environmentMap(os.Environ()),
	}
}

// NewWithRunner returns a client with injected runner and environment.
func NewWithRunner(runner Runner, env map[string]string) *Client {
	if runner == nil {
		runner = OSRunner{}
	}
	cloned := make(map[string]string, len(env))
	for k, v := range env {
		cloned[k] = v
	}
	return &Client{runner: runner, env: cloned}
}

// List returns every visible pane from the reachable tmux server.
func (c *Client) List(ctx context.Context) ([]Pane, error) {
	stdout, err := c.runTmux(ctx, "list-panes", "-a", "-F", listFormat)
	if err != nil {
		return nil, err
	}
	return parsePaneList(stdout)
}

// ResolveTarget maps a label or direct pane target to a tmux pane target.
func (c *Client) ResolveTarget(ctx context.Context, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("target is required")
	}
	if paneIDPattern.MatchString(target) || digitsPattern.MatchString(target) || strings.Contains(target, ":") || strings.Contains(target, ".") {
		if err := c.validateTarget(ctx, target); err != nil {
			return "", err
		}
		return target, nil
	}

	stdout, err := c.runTmux(ctx, "list-panes", "-a", "-F", labelFormat)
	if err != nil {
		return "", err
	}
	for _, line := range splitNonEmptyLines(stdout) {
		fields := strings.Split(line, fieldSep)
		if len(fields) < 2 {
			continue
		}
		if fields[1] == target {
			if err := c.validateTarget(ctx, fields[0]); err != nil {
				return "", err
			}
			return fields[0], nil
		}
	}

	return "", fmt.Errorf("no pane found with label %q", target)
}

// Read returns a bounded scrollback capture for one pane.
func (c *Client) Read(ctx context.Context, target string, lines int) (ReadResult, error) {
	if lines <= 0 {
		lines = 50
	}
	resolved, err := c.ResolveTarget(ctx, target)
	if err != nil {
		return ReadResult{}, err
	}
	pane, err := c.describePane(ctx, resolved)
	if err != nil {
		return ReadResult{}, err
	}

	stdout, err := c.runTmux(ctx, "capture-pane", "-t", resolved, "-p", "-J", "-S", fmt.Sprintf("-%d", lines))
	if err != nil {
		return ReadResult{}, err
	}
	content := strings.TrimRight(stdout, "\n")

	return ReadResult{
		Target:         target,
		ResolvedTarget: resolved,
		Pane:           pane,
		LinesRequested: lines,
		Content:        content,
		Lines:          splitPreserveEmpty(content),
	}, nil
}

// Doctor inspects tmux reachability and reports likely issues.
func (c *Client) Doctor(ctx context.Context) (DoctorReport, error) {
	report := DoctorReport{
		TmuxPane:        strings.TrimSpace(c.env["TMUX_PANE"]),
		TmuxEnv:         strings.TrimSpace(c.env["TMUX"]),
		BridgeSocketEnv: strings.TrimSpace(c.env["TMUX_BRIDGE_SOCKET"]),
	}

	if _, _, err := c.runner.Run(ctx, "tmux", "-V"); err != nil {
		return report, fmt.Errorf("tmux is not installed or not in PATH")
	}

	socket, err := c.detectSocket(ctx)
	if err != nil {
		report.Issues = append(report.Issues, err.Error())
	} else {
		report.DetectedSocket = socket
	}

	if _, err := c.runTmuxWithSocket(ctx, defaultSocketSentinel, "list-sessions"); err == nil {
		report.DefaultReachable = true
	}

	if socket != "" && socket != defaultSocketSentinel && report.TmuxPane != "" {
		if _, err := c.runTmuxWithSocket(ctx, socket, "display-message", "-t", report.TmuxPane, "-p", "#{pane_id}"); err == nil {
			report.CurrentPaneSeen = true
		} else {
			report.Issues = append(report.Issues, fmt.Sprintf("current pane %s is not visible to detected server", report.TmuxPane))
		}
	} else if socket == defaultSocketSentinel && report.TmuxPane != "" {
		if _, err := c.runTmuxWithSocket(ctx, socket, "display-message", "-t", report.TmuxPane, "-p", "#{pane_id}"); err == nil {
			report.CurrentPaneSeen = true
		}
	}

	if panes, listErr := c.List(ctx); listErr == nil {
		report.TotalPanes = len(panes)
		for _, pane := range panes {
			if strings.TrimSpace(pane.Label) != "" {
				report.LabeledPanes++
			}
		}
	} else {
		report.Issues = append(report.Issues, listErr.Error())
	}

	report.Healthy = len(report.Issues) == 0
	return report, nil
}

// PrepareSession creates or extends a tmux session for live multi-agent work.
func (c *Client) PrepareSession(ctx context.Context, opts PrepareOptions) (PrepareResult, error) {
	if opts.Panes <= 0 {
		return PrepareResult{}, fmt.Errorf("panes must be positive")
	}
	session := strings.TrimSpace(opts.Session)
	if session == "" {
		session = "agentctl-collab"
	}
	paneCommand := strings.TrimSpace(opts.PaneCommand)
	agentName := strings.TrimSpace(opts.Agent)
	agentSessionID := strings.TrimSpace(opts.AgentSessionID)
	if paneCommand != "" && agentName != "" {
		return PrepareResult{}, fmt.Errorf("pane_command and agent are mutually exclusive")
	}
	if agentSessionID != "" && agentName == "" {
		return PrepareResult{}, fmt.Errorf("agent_session_id requires agent")
	}
	if agentSessionID != "" && opts.Panes != 1 {
		return PrepareResult{}, fmt.Errorf("agent_session_id currently requires panes=1")
	}
	if agentName != "" {
		var buildErr error
		paneCommand, buildErr = buildAgentPaneCommand(agentName, opts.AgentArgs, agentSessionID)
		if buildErr != nil {
			return PrepareResult{}, buildErr
		}
	} else if paneCommand == "" {
		paneCommand = defaultPaneCommand()
	}
	labelPrefix := strings.TrimSpace(opts.LabelPrefix)
	if labelPrefix == "" {
		if agentName != "" {
			labelPrefix = agentLabelPrefix(agentName)
		} else {
			labelPrefix = "agent"
		}
	}

	socket := c.socketForCreate()
	created, err := c.createSessionIfNeeded(ctx, socket, session, opts.CWD, paneCommand)
	if err != nil {
		return PrepareResult{}, err
	}

	panes, err := c.listPanesForSession(ctx, socket, session)
	if err != nil {
		return PrepareResult{}, err
	}
	for len(panes) < opts.Panes {
		args := []string{"split-window", "-d", "-t", session}
		if strings.TrimSpace(opts.CWD) != "" {
			args = append(args, "-c", opts.CWD)
		}
		args = append(args, paneCommand)
		if _, err := c.runTmuxWithSocket(ctx, socket, args...); err != nil {
			return PrepareResult{}, err
		}
		panes, err = c.listPanesForSession(ctx, socket, session)
		if err != nil {
			return PrepareResult{}, err
		}
	}

	if _, err := c.runTmuxWithSocket(ctx, socket, "select-layout", "-t", session, "tiled"); err != nil {
		return PrepareResult{}, err
	}

	panes, err = c.listPanesForSession(ctx, socket, session)
	if err != nil {
		return PrepareResult{}, err
	}
	for i := range panes {
		label := labelForIndex(labelPrefix, i)
		if _, err := c.runTmuxWithSocket(ctx, socket, "set-option", "-p", "-t", panes[i].ID, "@name", label); err != nil {
			return PrepareResult{}, err
		}
		panes[i].Label = label
	}

	return PrepareResult{
		Session:        session,
		Created:        created,
		PanesRequested: opts.Panes,
		PaneCommand:    paneCommand,
		Agent:          agentName,
		AgentArgs:      append([]string(nil), opts.AgentArgs...),
		AgentSessionID: agentSessionID,
		CWD:            strings.TrimSpace(opts.CWD),
		LabelPrefix:    labelPrefix,
		AttachCommand:  c.attachCommand(session, socket),
		SocketMode:     socketMode(socket),
		Panes:          panes,
	}, nil
}

// ParseBridgeMessageLine parses one bridge line if it matches the stable header format.
func ParseBridgeMessageLine(line string) (BridgeMessage, bool) {
	match := bridgeLinePattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) != 5 {
		return BridgeMessage{}, false
	}
	return BridgeMessage{
		Raw:     strings.TrimSpace(line),
		From:    match[1],
		Pane:    match[2],
		ReplyTo: match[3],
		Content: strings.TrimSpace(match[4]),
	}, true
}

// LatestBridgeMessage returns the newest bridge message from a pane capture.
func LatestBridgeMessage(lines []string) (BridgeMessage, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if msg, ok := ParseBridgeMessageLine(lines[i]); ok {
			return msg, true
		}
	}
	return BridgeMessage{}, false
}

// AttachOrSwitch executes the appropriate tmux attach action for the current environment.
func (c *Client) AttachOrSwitch(ctx context.Context, session string) error {
	socket := c.socketForCreate()
	args := c.attachArgs(session, socket)
	cmd := exec.CommandContext(ctx, "tmux", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	return cmd.Run()
}

func (c *Client) validateTarget(ctx context.Context, target string) error {
	_, err := c.runTmux(ctx, "display-message", "-t", target, "-p", "#{pane_id}")
	if err != nil {
		return fmt.Errorf("invalid target %q: %w", target, err)
	}
	return nil
}

func (c *Client) describePane(ctx context.Context, target string) (Pane, error) {
	stdout, err := c.runTmux(ctx, "display-message", "-t", target, "-p", listFormat)
	if err != nil {
		return Pane{}, err
	}
	panes, err := parsePaneList(stdout)
	if err != nil {
		return Pane{}, err
	}
	if len(panes) == 0 {
		return Pane{}, fmt.Errorf("no pane metadata returned for %q", target)
	}
	return panes[0], nil
}

func (c *Client) runTmux(ctx context.Context, args ...string) (string, error) {
	socket, err := c.detectSocket(ctx)
	if err != nil {
		return "", err
	}
	return c.runTmuxWithSocket(ctx, socket, args...)
}

func (c *Client) runTmuxWithSocket(ctx context.Context, socket string, args ...string) (string, error) {
	cmdArgs := make([]string, 0, len(args)+2)
	if socket != "" && socket != defaultSocketSentinel {
		cmdArgs = append(cmdArgs, "-S", socket)
	}
	cmdArgs = append(cmdArgs, args...)
	stdout, stderr, err := c.runner.Run(ctx, "tmux", cmdArgs...)
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = strings.TrimSpace(stdout)
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("tmux %s: %s", strings.Join(args, " "), msg)
	}
	return stdout, nil
}

func (c *Client) detectSocket(ctx context.Context) (string, error) {
	if explicit := strings.TrimSpace(c.env["TMUX_BRIDGE_SOCKET"]); explicit != "" {
		if !isSocket(explicit) {
			return "", fmt.Errorf("TMUX_BRIDGE_SOCKET=%s is not a valid socket", explicit)
		}
		if _, err := c.runTmuxWithSocket(ctx, explicit, "list-sessions"); err != nil {
			return "", err
		}
		return explicit, nil
	}

	if tmuxEnv := strings.TrimSpace(c.env["TMUX"]); tmuxEnv != "" {
		socket := strings.SplitN(tmuxEnv, ",", 2)[0]
		if isSocket(socket) {
			if _, err := c.runTmuxWithSocket(ctx, socket, "list-sessions"); err == nil {
				return socket, nil
			}
		}
	}

	if pane := strings.TrimSpace(c.env["TMUX_PANE"]); pane != "" {
		uid := os.Getuid()
		for _, dir := range []string{
			filepath.Join("/tmp", fmt.Sprintf("tmux-%d", uid)),
			filepath.Join("/private/tmp", fmt.Sprintf("tmux-%d", uid)),
		} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				socket := filepath.Join(dir, entry.Name())
				if !isSocket(socket) {
					continue
				}
				if _, err := c.runTmuxWithSocket(ctx, socket, "display-message", "-t", pane, "-p", "#{pane_id}"); err == nil {
					return socket, nil
				}
			}
		}
		if _, err := c.runTmuxWithSocket(ctx, defaultSocketSentinel, "display-message", "-t", pane, "-p", "#{pane_id}"); err == nil {
			return defaultSocketSentinel, nil
		}
	}

	if _, err := c.runTmuxWithSocket(ctx, defaultSocketSentinel, "list-sessions"); err == nil {
		return defaultSocketSentinel, nil
	}

	return "", errors.New("cannot find a reachable tmux server")
}

func (c *Client) socketForCreate() string {
	if explicit := strings.TrimSpace(c.env["TMUX_BRIDGE_SOCKET"]); explicit != "" {
		return explicit
	}
	if tmuxEnv := strings.TrimSpace(c.env["TMUX"]); tmuxEnv != "" {
		socket := strings.SplitN(tmuxEnv, ",", 2)[0]
		if isSocket(socket) {
			return socket
		}
	}
	return defaultSocketSentinel
}

func (c *Client) createSessionIfNeeded(ctx context.Context, socket, session, cwd, paneCommand string) (bool, error) {
	args := []string{"new-session", "-d", "-s", session}
	if strings.TrimSpace(cwd) != "" {
		args = append(args, "-c", cwd)
	}
	args = append(args, paneCommand)
	if _, err := c.runTmuxWithSocket(ctx, socket, args...); err != nil {
		if strings.Contains(err.Error(), "duplicate session") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *Client) listPanesForSession(ctx context.Context, socket string, session string) ([]Pane, error) {
	stdout, err := c.runTmuxWithSocket(ctx, socket, "list-panes", "-t", session, "-F", listFormat)
	if err != nil {
		return nil, err
	}
	panes, err := parsePaneList(stdout)
	if err != nil {
		return nil, err
	}
	sort.Slice(panes, func(i, j int) bool {
		if panes[i].WindowIndex != panes[j].WindowIndex {
			return panes[i].WindowIndex < panes[j].WindowIndex
		}
		return panes[i].PaneIndex < panes[j].PaneIndex
	})
	return panes, nil
}

func parsePaneList(raw string) ([]Pane, error) {
	lines := splitNonEmptyLines(raw)
	panes := make([]Pane, 0, len(lines))
	for _, line := range lines {
		pane, err := parsePaneLine(line)
		if err != nil {
			return nil, err
		}
		panes = append(panes, pane)
	}
	return panes, nil
}

func parsePaneLine(line string) (Pane, error) {
	fields := strings.Split(line, fieldSep)
	if len(fields) != 12 {
		return Pane{}, fmt.Errorf("unexpected pane metadata field count: got %d", len(fields))
	}

	windowIndex, err := strconv.Atoi(fields[2])
	if err != nil {
		return Pane{}, fmt.Errorf("parse window index: %w", err)
	}
	paneIndex, err := strconv.Atoi(fields[3])
	if err != nil {
		return Pane{}, fmt.Errorf("parse pane index: %w", err)
	}
	pid, err := strconv.Atoi(fields[5])
	if err != nil {
		return Pane{}, fmt.Errorf("parse pane pid: %w", err)
	}
	width, err := strconv.Atoi(fields[6])
	if err != nil {
		return Pane{}, fmt.Errorf("parse pane width: %w", err)
	}
	height, err := strconv.Atoi(fields[7])
	if err != nil {
		return Pane{}, fmt.Errorf("parse pane height: %w", err)
	}

	return Pane{
		ID:             fields[0],
		Session:        fields[1],
		WindowIndex:    windowIndex,
		PaneIndex:      paneIndex,
		SessionPane:    fmt.Sprintf("%s:%d.%d", fields[1], windowIndex, paneIndex),
		WindowName:     fields[4],
		PID:            pid,
		Width:          width,
		Height:         height,
		Size:           fmt.Sprintf("%dx%d", width, height),
		Label:          fields[8],
		CurrentPath:    fields[9],
		CurrentCommand: fields[10],
		Active:         fields[11] == "1",
	}, nil
}

func splitNonEmptyLines(raw string) []string {
	items := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		lines = append(lines, item)
	}
	return lines
}

func splitPreserveEmpty(raw string) []string {
	if raw == "" {
		return []string{}
	}
	return strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
}

func environmentMap(items []string) map[string]string {
	env := make(map[string]string, len(items))
	for _, item := range items {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		env[key] = value
	}
	return env
}

func isSocket(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSocket != 0
}

func defaultPaneCommand() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	return "/bin/sh"
}

func buildAgentPaneCommand(agent string, args []string, sessionID string) (string, error) {
	resolved, err := resolveAgentCommand(agent)
	if err != nil {
		return "", fmt.Errorf("resolve agent %q: %w", agent, err)
	}

	label := agentLabelPrefix(agent)
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, resolved)
	switch label {
	case "codex":
		if sessionID != "" {
			parts = append(parts, "resume")
			parts = append(parts, args...)
			parts = append(parts, sessionID)
			return shellQuoteArgs(parts), nil
		}
	case "claude":
		if sessionID != "" {
			parts = append(parts, "--resume", sessionID)
		}
	case "gemini":
		if sessionID != "" {
			return "", fmt.Errorf("agent_session_id is not supported for gemini; use its own resume selector flags instead")
		}
	default:
		if sessionID != "" {
			return "", fmt.Errorf("agent_session_id is only supported for codex and claude")
		}
	}
	parts = append(parts, args...)
	return shellQuoteArgs(parts), nil
}

func resolveAgentCommand(agent string) (string, error) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "", fmt.Errorf("agent is required")
	}
	resolved, err := exec.LookPath(agent)
	if err == nil {
		return resolved, nil
	}
	if strings.ContainsAny(agent, `/\`) {
		return "", err
	}
	return agent, nil
}

func labelForIndex(prefix string, idx int) string {
	if idx < 26 {
		return fmt.Sprintf("%s-%c", prefix, rune('a'+idx))
	}
	return fmt.Sprintf("%s-%d", prefix, idx+1)
}

func agentLabelPrefix(agent string) string {
	base := strings.TrimSpace(filepath.Base(agent))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.ToLower(base)
	base = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		return "agent"
	}
	return base
}

func (c *Client) attachCommand(session, socket string) string {
	return "tmux " + strings.Join(c.attachArgs(session, socket), " ")
}

func (c *Client) attachArgs(session, socket string) []string {
	args := make([]string, 0, 4)
	if socket != "" && socket != defaultSocketSentinel {
		args = append(args, "-S", socket)
	}
	if strings.TrimSpace(c.env["TMUX"]) != "" {
		args = append(args, "switch-client", "-t", session)
		return args
	}
	args = append(args, "attach-session", "-t", session)
	return args
}

func socketMode(socket string) string {
	if socket == defaultSocketSentinel {
		return "default"
	}
	if strings.TrimSpace(socket) == "" {
		return "unknown"
	}
	return "explicit"
}

func shellQuoteArgs(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`()[]{}*?!&;|<>") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
