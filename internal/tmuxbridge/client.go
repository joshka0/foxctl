package tmuxbridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultSocketSentinel         = "__default__"
	defaultSessionName            = "agentctl-collab"
	defaultLabelPrefix            = "agent"
	muxCreateRoomOnboardingHeader = "Started via agentctl mux create."
	fieldSep                      = "\x1f"
	listFormat                    = "#{pane_id}" + fieldSep + "#{session_name}" + fieldSep + "#{window_index}" + fieldSep + "#{pane_index}" + fieldSep + "#{window_name}" + fieldSep + "#{pane_pid}" + fieldSep + "#{pane_width}" + fieldSep + "#{pane_height}" + fieldSep + "#{@name}" + fieldSep + "#{pane_current_path}" + fieldSep + "#{pane_current_command}" + fieldSep + "#{pane_active}"
	labelFormat                   = "#{pane_id}" + fieldSep + "#{@name}"
)

var (
	paneIDPattern     = regexp.MustCompile(`^%[0-9]+$`)
	digitsPattern     = regexp.MustCompile(`^[0-9]+$`)
	bridgeLinePattern = regexp.MustCompile(`^\[tmux-bridge\s+from=([^\s\]]+)\s+pane=([^\s\]]+)\s+reply_to=([^\s\]]+)\]\s*(.*)$`)
	writePaneTTY      = defaultWritePaneTTY
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
	Session           string   `json:"session"`
	Panes             int      `json:"panes"`
	PaneCommand       string   `json:"pane_command,omitempty"`
	Agent             string   `json:"agent,omitempty"`
	AgentMode         string   `json:"agent_mode,omitempty"`
	AgentArgs         []string `json:"agent_args,omitempty"`
	AgentSessionID    string   `json:"agent_session_id,omitempty"`
	CWD               string   `json:"cwd,omitempty"`
	LabelPrefix       string   `json:"label_prefix,omitempty"`
	ParentParticipant string   `json:"parent_participant,omitempty"`
	ParentAgentID     string   `json:"parent_agent_id,omitempty"`
	RoomID            string   `json:"room_id,omitempty"`
	RoomAccess        string   `json:"room_access,omitempty"`
}

// PrepareResult describes one prepared tmux collaboration session.
type PrepareResult struct {
	Session           string   `json:"session"`
	Created           bool     `json:"created"`
	PanesRequested    int      `json:"panes_requested"`
	PaneCommand       string   `json:"pane_command,omitempty"`
	Agent             string   `json:"agent,omitempty"`
	AgentMode         string   `json:"agent_mode,omitempty"`
	AgentArgs         []string `json:"agent_args,omitempty"`
	AgentSessionID    string   `json:"agent_session_id,omitempty"`
	CWD               string   `json:"cwd,omitempty"`
	LabelPrefix       string   `json:"label_prefix,omitempty"`
	ParentParticipant string   `json:"parent_participant,omitempty"`
	ParentAgentID     string   `json:"parent_agent_id,omitempty"`
	RoomID            string   `json:"room_id,omitempty"`
	RoomAccess        string   `json:"room_access,omitempty"`
	AttachCommand     string   `json:"attach_command"`
	SocketMode        string   `json:"socket_mode"`
	Panes             []Pane   `json:"panes"`
}

// CreatePaneOptions describes an exact single-pane allocation request.
type CreatePaneOptions struct {
	Session           string `json:"session"`
	CWD               string `json:"cwd,omitempty"`
	Label             string `json:"label,omitempty"`
	Command           string `json:"command,omitempty"`
	ParticipantID     string `json:"participant_id,omitempty"`
	ParentParticipant string `json:"parent_participant,omitempty"`
	ParentAgentID     string `json:"parent_agent_id,omitempty"`
	RoomID            string `json:"room_id,omitempty"`
	RoomRole          string `json:"room_role,omitempty"`
	RoomAccess        string `json:"room_access,omitempty"`
}

// CreatePaneResult describes one newly allocated pane.
type CreatePaneResult struct {
	Session       string `json:"session"`
	Created       bool   `json:"created"`
	Pane          Pane   `json:"pane"`
	AttachCommand string `json:"attach_command"`
	SocketMode    string `json:"socket_mode"`
}

// RespawnPaneOptions describes one exact pane respawn request.
type RespawnPaneOptions struct {
	Target            string `json:"target"`
	CWD               string `json:"cwd,omitempty"`
	Command           string `json:"command"`
	ParticipantID     string `json:"participant_id,omitempty"`
	ParentParticipant string `json:"parent_participant,omitempty"`
	ParentAgentID     string `json:"parent_agent_id,omitempty"`
	RoomID            string `json:"room_id,omitempty"`
	RoomRole          string `json:"room_role,omitempty"`
	RoomAccess        string `json:"room_access,omitempty"`
}

// RespawnPaneResult describes one pane after respawn.
type RespawnPaneResult struct {
	Target         string `json:"target"`
	ResolvedTarget string `json:"resolved_target"`
	Pane           Pane   `json:"pane"`
}

// BridgeMessage is one structured tmux-bridge line parsed from pane scrollback.
type BridgeMessage struct {
	Raw     string `json:"raw"`
	From    string `json:"from"`
	Pane    string `json:"pane"`
	ReplyTo string `json:"reply_to"`
	Content string `json:"content"`
}

// SendResult describes one bridge message injected into a target pane.
type SendResult struct {
	Target         string        `json:"target"`
	ResolvedTarget string        `json:"resolved_target"`
	Pane           Pane          `json:"pane"`
	Sender         Pane          `json:"sender"`
	BridgeMessage  BridgeMessage `json:"bridge_message"`
}

// SubmitResult describes an Escape+Enter submit action for a target pane.
type SubmitResult struct {
	Target         string `json:"target"`
	ResolvedTarget string `json:"resolved_target"`
	Pane           Pane   `json:"pane"`
	Mode           string `json:"mode"`
}

// DeliverResult describes one plain-text delivery into a pane.
type DeliverResult struct {
	Target         string `json:"target"`
	ResolvedTarget string `json:"resolved_target"`
	Pane           Pane   `json:"pane"`
	Mode           string `json:"mode"`
	Payload        string `json:"payload"`
}

type DeliverOptions struct {
	Interrupt bool `json:"interrupt,omitempty"`
}

// Client exposes read-only access to a reachable tmux server.
type Client struct {
	runner Runner
	env    map[string]string
}

type ParticipantRef struct {
	Session string
	Target  string
}

type preparePlan struct {
	session           string
	panes             int
	paneCommand       string
	explicitPane      bool
	agent             string
	agentMode         string
	agentArgs         []string
	agentSessionID    string
	cwd               string
	labelPrefix       string
	parentParticipant string
	parentAgentID     string
	roomID            string
	roomAccess        string
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
	target = normalizeTarget(target)
	if target == "" {
		return "", fmt.Errorf("target is required")
	}
	if ref, ok := ParseParticipantID(target); ok {
		target = ref.Target
	}
	if isDirectTarget(target) {
		if err := c.validateTarget(ctx, target); err != nil {
			return "", err
		}
		return target, nil
	}

	resolved, err := c.resolveTargetByLabel(ctx, target)
	if err != nil {
		return "", err
	}
	if err := c.validateTarget(ctx, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func normalizeTarget(target string) string {
	return strings.TrimSpace(target)
}

func isDirectTarget(target string) bool {
	return paneIDPattern.MatchString(target) ||
		digitsPattern.MatchString(target) ||
		strings.Contains(target, ":") ||
		strings.Contains(target, ".")
}

func (c *Client) resolveTargetByLabel(ctx context.Context, label string) (string, error) {
	stdout, err := c.runTmux(ctx, "list-panes", "-a", "-F", labelFormat)
	if err != nil {
		return "", err
	}
	for _, pane := range parseLabelLines(stdout) {
		if pane.Label == label {
			return pane.ID, nil
		}
	}
	return "", fmt.Errorf("no pane found with label %q", label)
}

func parseLabelLines(raw string) []Pane {
	lines := splitNonEmptyLines(raw)
	panes := make([]Pane, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, fieldSep)
		if len(fields) < 2 {
			continue
		}
		panes = append(panes, Pane{
			ID:    fields[0],
			Label: fields[1],
		})
	}
	return panes
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

// Send injects one structured bridge message into the target pane and presses Enter.
// When running outside tmux, sender must resolve to an existing pane label or target.
func (c *Client) Send(ctx context.Context, sender string, target string, text string) (SendResult, error) {
	content := strings.TrimSpace(text)
	if content == "" {
		return SendResult{}, fmt.Errorf("message text is required")
	}

	resolvedTarget, err := c.ResolveTarget(ctx, target)
	if err != nil {
		return SendResult{}, err
	}
	targetPane, err := c.describePane(ctx, resolvedTarget)
	if err != nil {
		return SendResult{}, err
	}

	senderPane, err := c.resolveSenderPane(ctx, sender)
	if err != nil {
		return SendResult{}, err
	}
	from := strings.TrimSpace(senderPane.Label)
	if from == "" {
		from = senderPane.ID
	}
	header := fmt.Sprintf("[tmux-bridge from=%s pane=%s reply_to=%s]", from, senderPane.ID, from)
	raw := header + " " + content

	if _, err := c.runTmux(ctx, "send-keys", "-t", resolvedTarget, "-l", "--", raw); err != nil {
		return SendResult{}, err
	}
	if _, err := c.runTmux(ctx, "send-keys", "-t", resolvedTarget, "Enter"); err != nil {
		return SendResult{}, err
	}

	return SendResult{
		Target:         target,
		ResolvedTarget: resolvedTarget,
		Pane:           targetPane,
		Sender:         senderPane,
		BridgeMessage: BridgeMessage{
			Raw:     raw,
			From:    from,
			Pane:    senderPane.ID,
			ReplyTo: from,
			Content: content,
		},
	}, nil
}

// Submit injects an Escape+Enter sequence into the target pane.
func (c *Client) Submit(ctx context.Context, target string) (SubmitResult, error) {
	resolvedTarget, err := c.ResolveTarget(ctx, target)
	if err != nil {
		return SubmitResult{}, err
	}
	targetPane, err := c.describePane(ctx, resolvedTarget)
	if err != nil {
		return SubmitResult{}, err
	}
	if _, err := c.runTmux(ctx, "send-keys", "-t", resolvedTarget, "Escape"); err != nil {
		return SubmitResult{}, err
	}
	if _, err := c.runTmux(ctx, "send-keys", "-t", resolvedTarget, "Enter"); err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{
		Target:         target,
		ResolvedTarget: resolvedTarget,
		Pane:           targetPane,
		Mode:           "escape_enter",
	}, nil
}

// DeliverText injects plain text into a target pane. Shell-like panes receive a
// shell-safe printf payload; non-shell panes receive the raw text.
func (c *Client) DeliverText(ctx context.Context, target string, text string) (DeliverResult, error) {
	return c.DeliverTextWithOptions(ctx, target, text, DeliverOptions{})
}

func (c *Client) DeliverTextWithOptions(ctx context.Context, target string, text string, opts DeliverOptions) (DeliverResult, error) {
	content := strings.TrimSpace(text)
	if content == "" {
		return DeliverResult{}, fmt.Errorf("message text is required")
	}
	resolvedTarget, err := c.ResolveTarget(ctx, target)
	if err != nil {
		return DeliverResult{}, err
	}
	targetPane, err := c.describePane(ctx, resolvedTarget)
	if err != nil {
		return DeliverResult{}, err
	}
	payload, mode, err := c.deliverPayload(ctx, resolvedTarget, targetPane, content, opts.Interrupt)
	if err != nil {
		return DeliverResult{}, err
	}
	return DeliverResult{
		Target:         target,
		ResolvedTarget: resolvedTarget,
		Pane:           targetPane,
		Mode:           mode,
		Payload:        payload,
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

	if socket != "" && report.TmuxPane != "" {
		currentPaneSeen, currentPaneIssue := c.inspectCurrentPane(ctx, socket, report.TmuxPane)
		report.CurrentPaneSeen = currentPaneSeen
		if currentPaneIssue != "" {
			report.Issues = append(report.Issues, currentPaneIssue)
		}
	}

	if panes, listErr := c.List(ctx); listErr == nil {
		report.TotalPanes, report.LabeledPanes = summarizePaneLabels(panes)
	} else {
		report.Issues = append(report.Issues, listErr.Error())
	}

	report.Healthy = len(report.Issues) == 0
	return report, nil
}

// PrepareSession creates or extends a tmux session for live multi-agent work.
func (c *Client) PrepareSession(ctx context.Context, opts PrepareOptions) (PrepareResult, error) {
	plan, err := normalizePrepareOptions(opts)
	if err != nil {
		return PrepareResult{}, err
	}

	socket := c.socketForCreate()
	created, err := c.createSessionIfNeeded(ctx, socket, plan.session, plan.cwd, plan.paneCommand)
	if err != nil {
		return PrepareResult{}, err
	}

	_, err = c.ensureSessionPaneCount(ctx, socket, plan)
	if err != nil {
		return PrepareResult{}, err
	}
	if _, err := c.runTmuxWithSocket(ctx, socket, "select-layout", "-t", plan.session, "tiled"); err != nil {
		return PrepareResult{}, err
	}
	panes, err := c.listPanesForSession(ctx, socket, plan.session)
	if err != nil {
		return PrepareResult{}, err
	}
	if err := c.labelSessionPanes(ctx, socket, panes, plan.labelPrefix); err != nil {
		return PrepareResult{}, err
	}
	if !created || plan.hasInjectedEnv() {
		if err := c.ensurePaneCommands(ctx, socket, panes, plan); err != nil {
			return PrepareResult{}, err
		}
		panes, err = c.listPanesForSession(ctx, socket, plan.session)
		if err != nil {
			return PrepareResult{}, err
		}
	}
	if err := c.injectRoomAgentOnboarding(ctx, socket, panes, plan); err != nil {
		return PrepareResult{}, err
	}

	return PrepareResult{
		Session:           plan.session,
		Created:           created,
		PanesRequested:    plan.panes,
		PaneCommand:       plan.paneCommand,
		Agent:             plan.agent,
		AgentMode:         plan.agentMode,
		AgentArgs:         append([]string(nil), plan.agentArgs...),
		AgentSessionID:    plan.agentSessionID,
		CWD:               plan.cwd,
		LabelPrefix:       plan.labelPrefix,
		ParentParticipant: plan.parentParticipant,
		ParentAgentID:     plan.parentAgentID,
		RoomID:            plan.roomID,
		RoomAccess:        plan.roomAccess,
		AttachCommand:     c.attachCommand(plan.session, socket),
		SocketMode:        socketMode(socket),
		Panes:             panes,
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

// CreatePane allocates one pane in a tmux session, labels it, and optionally
// respawns it with injected AGENTCTL_* identity metadata.
func (c *Client) CreatePane(ctx context.Context, opts CreatePaneOptions) (CreatePaneResult, error) {
	session := strings.TrimSpace(opts.Session)
	if session == "" {
		session = defaultSessionName
	}
	socket := c.socketForCreate()
	command := strings.TrimSpace(opts.Command)
	if command == "" {
		command = defaultPaneCommand()
	}
	cwd := strings.TrimSpace(opts.CWD)
	created, paneID, err := c.createSinglePane(ctx, socket, session, cwd, defaultPaneCommand())
	if err != nil {
		return CreatePaneResult{}, err
	}
	label := strings.TrimSpace(opts.Label)
	if label != "" {
		if _, err := c.runTmuxWithSocket(ctx, socket, "set-option", "-p", "-t", paneID, "@name", label); err != nil {
			return CreatePaneResult{}, err
		}
	}
	participantID := strings.TrimSpace(opts.ParticipantID)
	if participantID == "" {
		participantID = formatTmuxParticipantID(session, paneID)
	}
	if _, err := c.runTmuxWithSocket(ctx, socket, "select-layout", "-t", session, "tiled"); err != nil {
		return CreatePaneResult{}, err
	}
	if strings.TrimSpace(command) != defaultPaneCommand() || hasPaneIdentityEnv(participantID, opts.ParentParticipant, opts.ParentAgentID, opts.RoomID, opts.RoomRole) {
		if _, err := c.respawnPaneWithSocket(ctx, socket, paneID, session, cwd, command, participantID, opts.ParentParticipant, opts.ParentAgentID, opts.RoomID, opts.RoomRole, opts.RoomAccess); err != nil {
			return CreatePaneResult{}, err
		}
	}
	pane, err := c.describePaneWithSocket(ctx, socket, paneID)
	if err != nil {
		return CreatePaneResult{}, err
	}
	return CreatePaneResult{
		Session:       session,
		Created:       created,
		Pane:          pane,
		AttachCommand: c.attachCommand(session, socket),
		SocketMode:    socketMode(socket),
	}, nil
}

// RespawnPane replaces an exact pane command while preserving pane identity metadata.
func (c *Client) RespawnPane(ctx context.Context, opts RespawnPaneOptions) (RespawnPaneResult, error) {
	command := strings.TrimSpace(opts.Command)
	if command == "" {
		return RespawnPaneResult{}, fmt.Errorf("command is required")
	}
	resolved, err := c.ResolveTarget(ctx, opts.Target)
	if err != nil {
		return RespawnPaneResult{}, err
	}
	socket, err := c.detectSocket(ctx)
	if err != nil {
		return RespawnPaneResult{}, err
	}
	currentPane, err := c.describePaneWithSocket(ctx, socket, resolved)
	if err != nil {
		return RespawnPaneResult{}, err
	}
	pane, err := c.respawnPaneWithSocket(ctx, socket, resolved, currentPane.Session, strings.TrimSpace(opts.CWD), command, opts.ParticipantID, opts.ParentParticipant, opts.ParentAgentID, opts.RoomID, opts.RoomRole, opts.RoomAccess)
	if err != nil {
		return RespawnPaneResult{}, err
	}
	return RespawnPaneResult{
		Target:         opts.Target,
		ResolvedTarget: resolved,
		Pane:           pane,
	}, nil
}

func (c *Client) validateTarget(ctx context.Context, target string) error {
	_, err := c.runTmux(ctx, "display-message", "-t", target, "-p", "#{pane_id}")
	if err != nil {
		return fmt.Errorf("invalid target %q: %w", target, err)
	}
	return nil
}

func (c *Client) describePane(ctx context.Context, target string) (Pane, error) {
	socket, err := c.detectSocket(ctx)
	if err != nil {
		return Pane{}, err
	}
	return c.describePaneWithSocket(ctx, socket, target)
}

func (c *Client) describePaneWithSocket(ctx context.Context, socket, target string) (Pane, error) {
	stdout, err := c.runTmuxWithSocket(ctx, socket, "display-message", "-t", target, "-p", listFormat)
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

func (c *Client) resolveSenderPane(ctx context.Context, sender string) (Pane, error) {
	explicit := strings.TrimSpace(sender)
	if explicit == "" {
		explicit = strings.TrimSpace(c.env["TMUX_BRIDGE_SENDER"])
	}
	if explicit != "" {
		if ref, ok := ParseParticipantID(explicit); ok {
			return c.describePane(ctx, ref.Target)
		}
		resolved, err := c.ResolveTarget(ctx, explicit)
		if err != nil {
			return Pane{}, fmt.Errorf("resolve sender %q: %w", explicit, err)
		}
		return c.describePane(ctx, resolved)
	}

	currentPane := strings.TrimSpace(c.env["TMUX_PANE"])
	if currentPane == "" {
		return Pane{}, fmt.Errorf("sender is required when not running inside tmux; pass --sender <pane-label>")
	}
	return c.describePane(ctx, currentPane)
}

// CurrentPane returns metadata for the current pane when running inside tmux.
func (c *Client) CurrentPane(ctx context.Context) (Pane, error) {
	return c.resolveSenderPane(ctx, "")
}

// CurrentParticipantID returns a stable tmux participant identifier for the current pane.
// If the pane has a label, the label is preferred for human-friendly routing.
// Otherwise a canonical tmux:<session>:<pane> identifier is returned.
func (c *Client) CurrentParticipantID(ctx context.Context) (string, Pane, error) {
	pane, err := c.CurrentPane(ctx)
	if err != nil {
		return "", Pane{}, err
	}
	if label := strings.TrimSpace(pane.Label); label != "" {
		return label, pane, nil
	}
	return formatTmuxParticipantID(pane.Session, pane.ID), pane, nil
}

func formatTmuxParticipantID(session, paneID string) string {
	return "tmux:" + strings.TrimSpace(session) + ":" + strings.TrimSpace(paneID)
}

func (c *Client) deliverPayload(ctx context.Context, resolvedTarget string, pane Pane, content string, interrupt bool) (string, string, error) {
	if isShellCommand(pane.CurrentCommand) {
		if tty, err := c.paneTTY(ctx, resolvedTarget); err == nil && strings.TrimSpace(tty) != "" {
			if err := writePaneTTY(tty, formatTTYRelayWrite(content)); err == nil {
				return content, "tty_write", nil
			}
		}
	}
	payload, mode := relayPayloadForPane(pane, content)
	return c.deliverPreparedPayload(ctx, resolvedTarget, pane, payload, mode, interrupt)
}

func (c *Client) deliverPreparedPayload(ctx context.Context, resolvedTarget string, pane Pane, payload, mode string, interrupt bool) (string, string, error) {
	if interrupt {
		if _, err := c.runTmux(ctx, "send-keys", "-t", resolvedTarget, "Escape"); err != nil {
			return "", "", err
		}
	}
	if _, err := c.runTmux(ctx, "send-keys", "-t", resolvedTarget, "-l", "--", payload); err != nil {
		return "", "", err
	}
	if relayNeedsEscape(pane) {
		if _, err := c.runTmux(ctx, "send-keys", "-t", resolvedTarget, "Escape"); err != nil {
			return "", "", err
		}
	}
	if _, err := c.runTmux(ctx, "send-keys", "-t", resolvedTarget, "Enter"); err != nil {
		return "", "", err
	}
	return payload, mode, nil
}

func (c *Client) deliverPreparedPayloadWithSocket(ctx context.Context, socket, resolvedTarget string, pane Pane, payload, mode string, interrupt bool) (string, string, error) {
	if interrupt {
		if _, err := c.runTmuxWithSocket(ctx, socket, "send-keys", "-t", resolvedTarget, "Escape"); err != nil {
			return "", "", err
		}
	}
	if _, err := c.runTmuxWithSocket(ctx, socket, "send-keys", "-t", resolvedTarget, "-l", "--", payload); err != nil {
		return "", "", err
	}
	if relayNeedsEscape(pane) {
		if _, err := c.runTmuxWithSocket(ctx, socket, "send-keys", "-t", resolvedTarget, "Escape"); err != nil {
			return "", "", err
		}
	}
	if _, err := c.runTmuxWithSocket(ctx, socket, "send-keys", "-t", resolvedTarget, "Enter"); err != nil {
		return "", "", err
	}
	return payload, mode, nil
}

func (c *Client) injectRoomAgentOnboarding(ctx context.Context, socket string, panes []Pane, plan preparePlan) error {
	if strings.TrimSpace(plan.agent) == "" || strings.TrimSpace(plan.roomID) == "" || strings.TrimSpace(plan.roomAccess) != "direct" {
		return nil
	}
	for _, pane := range panes {
		participantID := strings.TrimSpace(pane.Label)
		if participantID == "" {
			participantID = formatTmuxParticipantID(plan.session, pane.ID)
		}
		payload := buildMuxCreateRoomOnboarding(plan.roomID, participantID)
		if strings.TrimSpace(payload) == "" {
			continue
		}
		if _, _, err := c.deliverPreparedPayloadWithSocket(ctx, socket, pane.ID, pane, payload, "raw", false); err != nil {
			return err
		}
	}
	return nil
}

func buildMuxCreateRoomOnboarding(roomID, participantID string) string {
	roomID = strings.TrimSpace(roomID)
	participantID = strings.TrimSpace(participantID)
	if roomID == "" {
		return ""
	}
	if participantID == "" {
		participantID = "<you>"
	}
	return fmt.Sprintf(
		"%s Read skills agentctl-tmux and agentctl-room. Room %s. Participant %s. Start with: agentctl room status %s ; agentctl room inbox %s --actor %s ; agentctl room task list %s. Use agentctl mux submit if text is left drafted.",
		muxCreateRoomOnboardingHeader,
		roomID,
		participantID,
		roomID,
		roomID,
		participantID,
		roomID,
	)
}

func relayPayloadForPane(pane Pane, content string) (string, string) {
	if isShellCommand(pane.CurrentCommand) {
		return shellPrintfCommand(content), "shell_printf"
	}
	return content, "raw"
}

func relayNeedsEscape(pane Pane) bool {
	label := strings.ToLower(strings.TrimSpace(pane.Label))
	return strings.HasPrefix(label, "gemini")
}

func isShellCommand(command string) bool {
	switch strings.TrimSpace(command) {
	case "bash", "zsh", "sh", "fish", "nu", "dash", "ksh", "tcsh", "csh":
		return true
	default:
		return false
	}
}

func shellPrintfCommand(content string) string {
	return "printf '%s\\n' " + shellQuote(content)
}

func (c *Client) paneTTY(ctx context.Context, target string) (string, error) {
	stdout, err := c.runTmux(ctx, "display-message", "-t", target, "-p", "#{pane_tty}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout), nil
}

func defaultWritePaneTTY(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()
	_, err = io.WriteString(f, content)
	return err
}

func formatTTYRelayWrite(content string) string {
	return "\r\x1b[2K" + content + "\r\n"
}

// ParseParticipantID parses a canonical tmux participant identifier.
func ParseParticipantID(value string) (ParticipantRef, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "tmux:") {
		return ParticipantRef{}, false
	}
	parts := strings.SplitN(value, ":", 3)
	if len(parts) != 3 {
		return ParticipantRef{}, false
	}
	session := strings.TrimSpace(parts[1])
	target := strings.TrimSpace(parts[2])
	if session == "" || target == "" {
		return ParticipantRef{}, false
	}
	return ParticipantRef{Session: session, Target: target}, true
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
	if socket, err := c.detectSocketFromBridgeEnv(ctx); socket != "" || err != nil {
		return socket, err
	}
	if socket := c.detectSocketFromTmuxEnv(ctx); socket != "" {
		return socket, nil
	}
	if socket := c.detectSocketFromPaneEnv(ctx); socket != "" {
		return socket, nil
	}
	if c.isDefaultSocketReachable(ctx) {
		return defaultSocketSentinel, nil
	}

	return "", errors.New("cannot find a reachable tmux server")
}

func (c *Client) inspectCurrentPane(ctx context.Context, socket, tmuxPane string) (bool, string) {
	if tmuxPane == "" {
		return false, ""
	}
	if _, err := c.runTmuxWithSocket(ctx, socket, "display-message", "-t", tmuxPane, "-p", "#{pane_id}"); err == nil {
		return true, ""
	}
	if socket != defaultSocketSentinel {
		return false, fmt.Sprintf("current pane %s is not visible to detected server", tmuxPane)
	}
	return false, ""
}

func summarizePaneLabels(panes []Pane) (total int, labeled int) {
	for _, pane := range panes {
		if strings.TrimSpace(pane.Label) != "" {
			labeled++
		}
	}
	return len(panes), labeled
}

func (c *Client) detectSocketFromBridgeEnv(ctx context.Context) (string, error) {
	explicit := strings.TrimSpace(c.env["TMUX_BRIDGE_SOCKET"])
	if explicit == "" {
		return "", nil
	}
	if !isSocket(explicit) {
		return "", fmt.Errorf("TMUX_BRIDGE_SOCKET=%s is not a valid socket", explicit)
	}
	if _, err := c.runTmuxWithSocket(ctx, explicit, "list-sessions"); err != nil {
		return "", err
	}
	return explicit, nil
}

func (c *Client) detectSocketFromTmuxEnv(ctx context.Context) string {
	tmuxEnv := strings.TrimSpace(c.env["TMUX"])
	if tmuxEnv == "" {
		return ""
	}
	socket := strings.SplitN(tmuxEnv, ",", 2)[0]
	if !isSocket(socket) {
		return ""
	}
	if _, err := c.runTmuxWithSocket(ctx, socket, "list-sessions"); err == nil {
		return socket
	}
	return ""
}

func (c *Client) detectSocketFromPaneEnv(ctx context.Context) string {
	pane := strings.TrimSpace(c.env["TMUX_PANE"])
	if pane == "" {
		return ""
	}
	for _, socket := range candidateSocketsForUID(os.Getuid()) {
		if _, err := c.runTmuxWithSocket(ctx, socket, "display-message", "-t", pane, "-p", "#{pane_id}"); err == nil {
			return socket
		}
	}
	if _, err := c.runTmuxWithSocket(ctx, defaultSocketSentinel, "display-message", "-t", pane, "-p", "#{pane_id}"); err == nil {
		return defaultSocketSentinel
	}
	return ""
}

func candidateSocketsForUID(uid int) []string {
	candidates := make([]string, 0)
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
			if isSocket(socket) {
				candidates = append(candidates, socket)
			}
		}
	}
	return candidates
}

func (c *Client) isDefaultSocketReachable(ctx context.Context) bool {
	_, err := c.runTmuxWithSocket(ctx, defaultSocketSentinel, "list-sessions")
	return err == nil
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

func (c *Client) createSinglePane(ctx context.Context, socket, session, cwd, paneCommand string) (bool, string, error) {
	args := []string{"new-session", "-d", "-P", "-F", "#{pane_id}", "-s", session}
	if strings.TrimSpace(cwd) != "" {
		args = append(args, "-c", cwd)
	}
	args = append(args, paneCommand)
	stdout, err := c.runTmuxWithSocket(ctx, socket, args...)
	if err == nil {
		return true, strings.TrimSpace(stdout), nil
	}
	if !strings.Contains(err.Error(), "duplicate session") {
		return false, "", err
	}
	args = []string{"split-window", "-d", "-P", "-F", "#{pane_id}", "-t", session}
	if strings.TrimSpace(cwd) != "" {
		args = append(args, "-c", cwd)
	}
	args = append(args, paneCommand)
	stdout, err = c.runTmuxWithSocket(ctx, socket, args...)
	if err != nil {
		return false, "", err
	}
	return false, strings.TrimSpace(stdout), nil
}

func (c *Client) ensureSessionPaneCount(ctx context.Context, socket string, plan preparePlan) ([]Pane, error) {
	panes, err := c.listPanesForSession(ctx, socket, plan.session)
	if err != nil {
		return nil, err
	}
	for len(panes) < plan.panes {
		if err := c.splitSessionPane(ctx, socket, plan); err != nil {
			return nil, err
		}
		panes, err = c.listPanesForSession(ctx, socket, plan.session)
		if err != nil {
			return nil, err
		}
	}
	return panes, nil
}

func (c *Client) splitSessionPane(ctx context.Context, socket string, plan preparePlan) error {
	args := []string{"split-window", "-d", "-t", plan.session}
	if plan.cwd != "" {
		args = append(args, "-c", plan.cwd)
	}
	args = append(args, plan.paneCommand)
	_, err := c.runTmuxWithSocket(ctx, socket, args...)
	return err
}

func (c *Client) ensurePaneCommands(ctx context.Context, socket string, panes []Pane, plan preparePlan) error {
	if plan.agent == "" && !plan.explicitPane && !plan.hasInjectedEnv() {
		return nil
	}
	for _, pane := range panes {
		if !isShellCommand(pane.CurrentCommand) {
			continue
		}
		command := paneCommandForPane(plan, pane)
		args := []string{"respawn-pane", "-k", "-t", pane.ID}
		if plan.cwd != "" {
			args = append(args, "-c", plan.cwd)
		}
		args = append(args, command)
		if _, err := c.runTmuxWithSocket(ctx, socket, args...); err != nil {
			return err
		}
	}
	return nil
}

func hasPaneIdentityEnv(participantID, parentParticipant, parentAgentID, roomID, roomRole string) bool {
	return strings.TrimSpace(participantID) != "" ||
		strings.TrimSpace(parentParticipant) != "" ||
		strings.TrimSpace(parentAgentID) != "" ||
		strings.TrimSpace(roomID) != "" ||
		strings.TrimSpace(roomRole) != ""
}

func (c *Client) respawnPaneWithSocket(ctx context.Context, socket, target, session, cwd, command, participantID, parentParticipant, parentAgentID, roomID, roomRole, roomAccess string) (Pane, error) {
	args := []string{"respawn-pane", "-k", "-t", target}
	if strings.TrimSpace(cwd) != "" {
		args = append(args, "-c", cwd)
	}
	args = append(args, paneCommandForIdentity(command, participantID, parentParticipant, parentAgentID, roomID, roomRole, roomAccess, session, target))
	if _, err := c.runTmuxWithSocket(ctx, socket, args...); err != nil {
		return Pane{}, err
	}
	return c.describePaneWithSocket(ctx, socket, target)
}

func (p preparePlan) hasInjectedEnv() bool {
	return p.parentParticipant != "" || p.parentAgentID != "" || p.roomID != ""
}

func paneCommandForPane(plan preparePlan, pane Pane) string {
	return paneCommandForIdentity(
		plan.paneCommand,
		strings.TrimSpace(pane.Label),
		plan.parentParticipant,
		plan.parentAgentID,
		plan.roomID,
		"",
		plan.roomAccess,
		plan.session,
		pane.ID,
	)
}

func paneCommandForIdentity(command, participantID, parentParticipant, parentAgentID, roomID, roomRole, roomAccess, session, paneID string) string {
	env := []string{
		"AGENTCTL_PARTICIPANT_ID=" + shellQuote(strings.TrimSpace(participantID)),
		"AGENTCTL_MUX_BACKEND=tmux",
		"AGENTCTL_MUX_SESSION=" + shellQuote(strings.TrimSpace(session)),
		"AGENTCTL_MUX_PANE_ID=" + shellQuote(strings.TrimSpace(paneID)),
	}
	if value := strings.TrimSpace(parentParticipant); value != "" {
		env = append(env, "AGENTCTL_PARENT_PARTICIPANT_ID="+shellQuote(value))
	}
	if value := strings.TrimSpace(parentAgentID); value != "" {
		env = append(env, "AGENTCTL_PARENT_AGENT_ID="+shellQuote(value))
	}
	if strings.TrimSpace(roomAccess) == "direct" {
		if value := strings.TrimSpace(roomID); value != "" {
			env = append(env, "AGENTCTL_ROOM_ID="+shellQuote(value))
		}
		if value := strings.TrimSpace(roomRole); value != "" {
			env = append(env, "AGENTCTL_ROOM_ROLE="+shellQuote(value))
		}
	}
	if strings.TrimSpace(command) == "" {
		command = defaultPaneCommand()
	}
	return "env " + strings.Join(env, " ") + " " + command
}

func (c *Client) labelSessionPanes(ctx context.Context, socket string, panes []Pane, labelPrefix string) error {
	for i := range panes {
		label := labelForIndex(labelPrefix, i)
		if _, err := c.runTmuxWithSocket(ctx, socket, "set-option", "-p", "-t", panes[i].ID, "@name", label); err != nil {
			return err
		}
		panes[i].Label = label
	}
	return nil
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

func normalizePrepareOptions(opts PrepareOptions) (preparePlan, error) {
	if opts.Panes <= 0 {
		return preparePlan{}, fmt.Errorf("panes must be positive")
	}

	plan := preparePlan{
		session:           strings.TrimSpace(opts.Session),
		panes:             opts.Panes,
		paneCommand:       strings.TrimSpace(opts.PaneCommand),
		explicitPane:      strings.TrimSpace(opts.PaneCommand) != "",
		agent:             strings.TrimSpace(opts.Agent),
		agentMode:         normalizeAgentMode(opts.AgentMode),
		agentArgs:         append([]string(nil), opts.AgentArgs...),
		agentSessionID:    strings.TrimSpace(opts.AgentSessionID),
		cwd:               strings.TrimSpace(opts.CWD),
		labelPrefix:       strings.TrimSpace(opts.LabelPrefix),
		parentParticipant: strings.TrimSpace(opts.ParentParticipant),
		parentAgentID:     strings.TrimSpace(opts.ParentAgentID),
		roomID:            strings.TrimSpace(opts.RoomID),
		roomAccess:        normalizeRoomAccess(opts.RoomAccess, strings.TrimSpace(opts.ParentParticipant)),
	}
	if plan.session == "" {
		plan.session = defaultSessionName
	}

	if plan.paneCommand != "" && plan.agent != "" {
		return preparePlan{}, fmt.Errorf("pane_command and agent are mutually exclusive")
	}
	if plan.agentSessionID != "" && plan.agent == "" {
		return preparePlan{}, fmt.Errorf("agent_session_id requires agent")
	}
	if plan.agentSessionID != "" && plan.panes != 1 {
		return preparePlan{}, fmt.Errorf("agent_session_id currently requires panes=1")
	}

	if plan.agent != "" {
		paneCommand, err := buildAgentPaneCommand(plan.agent, plan.agentMode, plan.agentArgs, plan.agentSessionID)
		if err != nil {
			return preparePlan{}, err
		}
		plan.paneCommand = paneCommand
	} else if plan.paneCommand == "" {
		plan.paneCommand = defaultPaneCommand()
	}

	if plan.labelPrefix == "" {
		if plan.agent != "" {
			plan.labelPrefix = agentLabelPrefix(plan.agent)
		} else {
			plan.labelPrefix = defaultLabelPrefix
		}
	}
	return plan, nil
}

func normalizeRoomAccess(value, parentParticipant string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "default":
		if strings.TrimSpace(parentParticipant) != "" {
			return "none"
		}
		return "direct"
	case "direct":
		return "direct"
	case "none":
		return "none"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeAgentMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", "interactive":
		return "interactive"
	case "auto":
		return "auto"
	default:
		return mode
	}
}

func buildAgentPaneCommand(agent, mode string, args []string, sessionID string) (string, error) {
	resolved, err := resolveAgentCommand(agent)
	if err != nil {
		return "", fmt.Errorf("resolve agent %q: %w", agent, err)
	}

	label := agentLabelPrefix(agent)
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, resolved)
	autoArgs, err := agentAutoModeArgs(label, mode)
	if err != nil {
		return "", err
	}
	switch label {
	case "codex":
		if sessionID != "" {
			parts = append(parts, "resume")
			parts = append(parts, autoArgs...)
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
	parts = append(parts, autoArgs...)
	parts = append(parts, args...)
	return shellQuoteArgs(parts), nil
}

func agentAutoModeArgs(label, mode string) ([]string, error) {
	if mode == "" || mode == "interactive" {
		return nil, nil
	}
	if mode != "auto" {
		return nil, fmt.Errorf("unsupported agent mode %q", mode)
	}
	switch label {
	case "codex":
		return []string{"--full-auto"}, nil
	case "claude":
		return []string{"--dangerously-skip-permissions"}, nil
	case "gemini":
		return []string{"--yolo"}, nil
	case "agent":
		return []string{"--yolo"}, nil
	default:
		return nil, fmt.Errorf("agent mode auto is not mapped for %s yet", label)
	}
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
