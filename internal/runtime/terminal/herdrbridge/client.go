package herdrbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const (
	SubmitModeEscapeEnter = "escape_enter"
	SubmitModeEnterOnly   = "enter_only"

	ReadSourceVisible         = "visible"
	ReadSourceRecent          = "recent"
	ReadSourceRecentUnwrapped = "recent_unwrapped"

	ReadFormatText = "text"
	ReadFormatANSI = "ansi"
)

var sessionNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Options struct {
	SocketPath string
	Session    string
	Env        map[string]string
	Dialer     func(ctx context.Context, network, address string) (net.Conn, error)
}

type Client struct {
	socketPath string
	dialer     func(ctx context.Context, network, address string) (net.Conn, error)
	seq        atomic.Uint64
}

type HerdrError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *HerdrError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Code) == "" {
		return e.Message
	}
	if strings.TrimSpace(e.Message) == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

type RawResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *HerdrError     `json:"error,omitempty"`
}

type PingResult struct {
	Version  string `json:"version,omitempty"`
	Protocol int    `json:"protocol,omitempty"`
}

type Pane struct {
	PaneID       string `json:"pane_id"`
	WorkspaceID  string `json:"workspace_id"`
	TabID        string `json:"tab_id"`
	Focused      bool   `json:"focused"`
	CWD          string `json:"cwd,omitempty"`
	Label        string `json:"label,omitempty"`
	Agent        string `json:"agent,omitempty"`
	AgentStatus  string `json:"agent_status"`
	CustomStatus string `json:"custom_status,omitempty"`
	Revision     uint64 `json:"revision"`
}

type ListOptions struct {
	WorkspaceID string
}

type ReadOptions struct {
	Source       string
	Lines        int
	Format       string
	StripANSI    bool
	StripANSISet bool
}

type ReadResult struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Source      string `json:"source"`
	Format      string `json:"format,omitempty"`
	Text        string `json:"text"`
	Revision    uint64 `json:"revision"`
	Truncated   bool   `json:"truncated"`
}

type SendInputResult struct {
	PaneID     string   `json:"pane_id"`
	Text       string   `json:"text,omitempty"`
	Keys       []string `json:"keys,omitempty"`
	SocketPath string   `json:"socket_path,omitempty"`
}

type SubmitOptions struct {
	Mode string
}

type DeliverOptions struct {
	Interrupt bool `json:"interrupt,omitempty"`
}

type DeliverResult struct {
	PaneID     string   `json:"pane_id"`
	Text       string   `json:"text"`
	Keys       []string `json:"keys"`
	SocketPath string   `json:"socket_path,omitempty"`
}

type ParticipantRef struct {
	Session string
	PaneID  string
}

func New() *Client {
	return NewWithOptions(Options{})
}

func NewWithOptions(opts Options) *Client {
	dialer := opts.Dialer
	if dialer == nil {
		netDialer := &net.Dialer{Timeout: 5 * time.Second}
		dialer = netDialer.DialContext
	}
	return &Client{
		socketPath: ResolveSocketPath(opts),
		dialer:     dialer,
	}
}

func ResolveSocketPath(opts Options) string {
	env := opts.Env
	if env == nil {
		env = processEnv()
	}
	if session := strings.TrimSpace(opts.Session); session != "" {
		return sessionSocketPath(env, session)
	}
	if socketPath := strings.TrimSpace(opts.SocketPath); socketPath != "" {
		return socketPath
	}
	if socketPath := strings.TrimSpace(env["HERDR_SOCKET_PATH"]); socketPath != "" {
		return socketPath
	}
	if session := strings.TrimSpace(env["HERDR_SESSION"]); session != "" {
		return sessionSocketPath(env, session)
	}
	return filepath.Join(herdrConfigHome(env), "herdr", "herdr.sock")
}

func (c *Client) SocketPath() string {
	if c == nil {
		return ""
	}
	return c.socketPath
}

func (c *Client) Ping(ctx context.Context) (PingResult, error) {
	raw, err := c.Raw(ctx, "ping", json.RawMessage(`{}`), "")
	if err != nil {
		return PingResult{}, err
	}
	var envelope struct {
		Type     string `json:"type"`
		Version  string `json:"version"`
		Protocol int    `json:"protocol"`
	}
	if err := decodeResult(raw.Result, &envelope); err != nil {
		return PingResult{}, err
	}
	return PingResult{Version: envelope.Version, Protocol: envelope.Protocol}, nil
}

func (c *Client) List(ctx context.Context, opts ListOptions) ([]Pane, error) {
	params := map[string]any{}
	if workspaceID := strings.TrimSpace(opts.WorkspaceID); workspaceID != "" {
		params["workspace_id"] = workspaceID
	}
	raw, err := c.call(ctx, "pane.list", params)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Type  string `json:"type"`
		Panes []Pane `json:"panes"`
	}
	if err := decodeResult(raw.Result, &envelope); err != nil {
		return nil, err
	}
	return envelope.Panes, nil
}

func (c *Client) Read(ctx context.Context, paneID string, opts ReadOptions) (ReadResult, error) {
	paneID = strings.TrimSpace(paneID)
	if paneID == "" {
		return ReadResult{}, fmt.Errorf("pane id is required")
	}
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = ReadSourceRecent
	}
	format := strings.TrimSpace(opts.Format)
	if format == "" {
		format = ReadFormatText
	}
	params := map[string]any{
		"pane_id":    paneID,
		"source":     source,
		"format":     format,
		"strip_ansi": readStripANSI(opts),
	}
	if opts.Lines > 0 {
		params["lines"] = opts.Lines
	}
	raw, err := c.call(ctx, "pane.read", params)
	if err != nil {
		return ReadResult{}, err
	}
	var envelope struct {
		Type string     `json:"type"`
		Read ReadResult `json:"read"`
	}
	if err := decodeResult(raw.Result, &envelope); err != nil {
		return ReadResult{}, err
	}
	return envelope.Read, nil
}

func (c *Client) SendInput(ctx context.Context, paneID, text string, keys []string) (SendInputResult, error) {
	paneID = strings.TrimSpace(paneID)
	if paneID == "" {
		return SendInputResult{}, fmt.Errorf("pane id is required")
	}
	params := map[string]any{"pane_id": paneID}
	if text != "" {
		params["text"] = text
	}
	cleanKeys := cleanStringSlice(keys)
	if len(cleanKeys) > 0 {
		params["keys"] = cleanKeys
	}
	if _, err := c.call(ctx, "pane.send_input", params); err != nil {
		return SendInputResult{}, err
	}
	return SendInputResult{
		PaneID:     paneID,
		Text:       text,
		Keys:       cleanKeys,
		SocketPath: c.socketPath,
	}, nil
}

func (c *Client) Submit(ctx context.Context, paneID string, opts SubmitOptions) (SendInputResult, error) {
	keys, err := submitKeys(opts.Mode)
	if err != nil {
		return SendInputResult{}, err
	}
	return c.SendInput(ctx, paneID, "", keys)
}

func (c *Client) Interrupt(ctx context.Context, paneID string) (SendInputResult, error) {
	return c.SendInput(ctx, paneID, "", []string{"Esc"})
}

func (c *Client) DeliverTextWithOptions(ctx context.Context, paneID, text string, opts DeliverOptions) (DeliverResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return DeliverResult{}, fmt.Errorf("message text is required")
	}
	if opts.Interrupt {
		if _, err := c.Interrupt(ctx, paneID); err != nil {
			return DeliverResult{}, err
		}
	}
	keys := []string{"Enter"}
	if _, err := c.SendInput(ctx, paneID, text, keys); err != nil {
		return DeliverResult{}, err
	}
	return DeliverResult{
		PaneID:     strings.TrimSpace(paneID),
		Text:       text,
		Keys:       keys,
		SocketPath: c.socketPath,
	}, nil
}

func (c *Client) Raw(ctx context.Context, method string, params json.RawMessage, id string) (RawResponse, error) {
	if c == nil {
		return RawResponse{}, fmt.Errorf("herdr client is nil")
	}
	method = strings.TrimSpace(method)
	if method == "" {
		return RawResponse{}, fmt.Errorf("method is required")
	}
	socketPath := strings.TrimSpace(c.socketPath)
	if socketPath == "" {
		return RawResponse{}, fmt.Errorf("herdr socket path is empty")
	}
	if len(strings.TrimSpace(string(params))) == 0 {
		params = json.RawMessage(`{}`)
	}
	if !json.Valid(params) {
		return RawResponse{}, fmt.Errorf("params must be valid JSON")
	}
	if strings.TrimSpace(id) == "" {
		id = c.nextID()
	}
	req := rawRequest{
		ID:     id,
		Method: method,
		Params: params,
	}
	conn, err := c.dialer(ctx, "unix", socketPath)
	if err != nil {
		return RawResponse{}, fmt.Errorf("connect herdr socket %s: %w", socketPath, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(req); err != nil {
		return RawResponse{}, fmt.Errorf("write herdr request: %w", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return RawResponse{}, fmt.Errorf("read herdr response: %w", err)
	}
	var resp RawResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return RawResponse{}, fmt.Errorf("decode herdr response: %w", err)
	}
	if resp.Error != nil {
		return resp, resp.Error
	}
	return resp, nil
}

func (c *Client) call(ctx context.Context, method string, params any) (RawResponse, error) {
	payload, err := json.Marshal(params)
	if err != nil {
		return RawResponse{}, fmt.Errorf("encode herdr params: %w", err)
	}
	return c.Raw(ctx, method, payload, "")
}

func (c *Client) nextID() string {
	n := c.seq.Add(1)
	return fmt.Sprintf("foxctl_%d", n)
}

func FormatParticipantID(session, paneID string) string {
	return "herdr:" + strings.TrimSpace(session) + ":" + strings.TrimSpace(paneID)
}

func ParseParticipantID(value string) (ParticipantRef, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "herdr:") {
		return ParticipantRef{}, false
	}
	parts := strings.SplitN(value, ":", 3)
	if len(parts) != 3 {
		return ParticipantRef{}, false
	}
	session := strings.TrimSpace(parts[1])
	paneID := strings.TrimSpace(parts[2])
	if paneID == "" {
		return ParticipantRef{}, false
	}
	return ParticipantRef{Session: session, PaneID: paneID}, true
}

func submitKeys(mode string) ([]string, error) {
	switch strings.TrimSpace(mode) {
	case "", SubmitModeEscapeEnter:
		return []string{"Esc", "Enter"}, nil
	case SubmitModeEnterOnly:
		return []string{"Enter"}, nil
	default:
		return nil, fmt.Errorf("unsupported submit mode %q", mode)
	}
}

func decodeResult(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return errors.New("herdr response missing result")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("decode herdr result: %w", err)
	}
	return nil
}

type rawRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func processEnv() map[string]string {
	return map[string]string{
		"HERDR_SOCKET_PATH": strings.TrimSpace(os.Getenv("HERDR_SOCKET_PATH")),
		"HERDR_SESSION":     strings.TrimSpace(os.Getenv("HERDR_SESSION")),
		"XDG_CONFIG_HOME":   strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")),
		"HOME":              strings.TrimSpace(os.Getenv("HOME")),
	}
}

func sessionSocketPath(env map[string]string, session string) string {
	session = strings.TrimSpace(session)
	if session == "" || session == "default" {
		return filepath.Join(herdrConfigHome(env), "herdr", "herdr.sock")
	}
	if !validSessionName(session) {
		return ""
	}
	return filepath.Join(herdrConfigHome(env), "herdr", "sessions", session, "herdr.sock")
}

func validSessionName(session string) bool {
	return len(session) <= 64 && session != "." && session != ".." && sessionNamePattern.MatchString(session)
}

func herdrConfigHome(env map[string]string) string {
	if xdg := strings.TrimSpace(env["XDG_CONFIG_HOME"]); xdg != "" {
		return xdg
	}
	if home := strings.TrimSpace(env["HOME"]); home != "" {
		return filepath.Join(home, ".config")
	}
	return filepath.Join(os.TempDir(), "herdr-config")
}

func readStripANSI(opts ReadOptions) bool {
	if opts.StripANSISet {
		return opts.StripANSI
	}
	return true
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
