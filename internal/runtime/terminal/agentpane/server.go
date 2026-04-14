package agentpane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

const (
	SubmitModeRaw               = "raw"
	SubmitModeNewline           = "newline"
	SubmitModeEnter             = "enter"
	SubmitModeEnterSplit        = "enter_split"
	SubmitModeComposerCtrlEnter = "composer_ctrl_enter"
	StartupProfileDroidAutoHigh = "droid_auto_high"
)

// ServeOptions configures a pane wrapper that owns a child PTY and accepts room
// messages over a unix socket.
type ServeOptions struct {
	SocketPath        string
	ReadyPath         string
	ParticipantID     string
	RoomID            string
	Command           []string
	CWD               string
	Env               []string
	DefaultSubmitMode string
	StartupProfile    string
	Stdin             io.Reader
	Stdout            io.Writer
	Stderr            io.Writer
}

// ControlMessage describes a single delivery request accepted by the pane
// server. The content is transformed into PTY bytes according to SubmitMode.
type ControlMessage struct {
	Kind       string `json:"kind,omitempty"`
	RoomID     string `json:"room_id,omitempty"`
	MessageID  string `json:"message_id,omitempty"`
	Sender     string `json:"sender,omitempty"`
	Recipient  string `json:"recipient,omitempty"`
	Interrupt  bool   `json:"interrupt,omitempty"`
	Content    string `json:"content,omitempty"`
	SubmitMode string `json:"submit_mode,omitempty"`
}

// ControlResponse reports whether the pane server accepted and wrote a message
// into the child PTY.
type ControlResponse struct {
	OK           bool      `json:"ok"`
	AcceptedAt   time.Time `json:"accepted_at,omitempty"`
	BytesWritten int       `json:"bytes_written,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// PaneMetadata is persisted alongside wrapped pane sockets so presentation
// layers can recover participant identity and room ownership without guessing
// from bounded socket filenames.
type PaneMetadata struct {
	ParticipantID string `json:"participant_id,omitempty"`
	RoomID        string `json:"room_id,omitempty"`
	SocketPath    string `json:"socket_path,omitempty"`
	ReadyPath     string `json:"ready_path,omitempty"`
}

// Deliver sends a control message to a running pane server.
func Deliver(ctx context.Context, socketPath string, msg ControlMessage) (ControlResponse, error) {
	var resp ControlResponse
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return resp, fmt.Errorf("dial pane socket: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := json.NewEncoder(conn).Encode(msg); err != nil {
		return resp, fmt.Errorf("encode control message: %w", err)
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return resp, fmt.Errorf("decode control response: %w", err)
	}
	if !resp.OK {
		if strings.TrimSpace(resp.Error) == "" {
			resp.Error = "pane delivery rejected"
		}
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

// DefaultSocketPath derives a stable unix socket path for a participant-scoped
// pane server. The scope is typically a mux session or room id.
func DefaultSocketPath(scopeID, participantID string) string {
	scope := boundedSocketComponent(scopeID, 24)
	if scope == "" {
		scope = "detached"
	}
	participant := boundedSocketComponent(participantID, 24)
	if participant == "" {
		participant = "pane"
	}
	return filepath.Join(socketBaseDir(), scope, participant+".sock")
}

// DefaultReadyPath derives a stable readiness marker path for a participant-scoped
// pane server. The scope is typically a mux session or room id.
func DefaultReadyPath(scopeID, participantID string) string {
	scope := boundedSocketComponent(scopeID, 24)
	if scope == "" {
		scope = "detached"
	}
	participant := boundedSocketComponent(participantID, 24)
	if participant == "" {
		participant = "pane"
	}
	return filepath.Join(socketBaseDir(), scope, participant+".ready")
}

// DefaultMetaPath derives a stable metadata sidecar path for a participant-scoped
// pane server.
func DefaultMetaPath(scopeID, participantID string) string {
	scope := boundedSocketComponent(scopeID, 24)
	if scope == "" {
		scope = "detached"
	}
	participant := boundedSocketComponent(participantID, 24)
	if participant == "" {
		participant = "pane"
	}
	return filepath.Join(socketBaseDir(), scope, participant+".json")
}

func socketBaseDir() string {
	defaultRoot := filepath.Join(os.TempDir(), "foxctl-pane")
	if runtime.GOOS == "windows" {
		return defaultRoot
	}
	shortRoot := filepath.Join(string(filepath.Separator), "tmp", "foxctl-pane")
	if len(shortRoot) < len(defaultRoot) {
		return shortRoot
	}
	return defaultRoot
}

// MetadataPathForSocket returns the metadata sidecar path for a given socket path.
func MetadataPathForSocket(socketPath string) string {
	socketPath = strings.TrimSpace(socketPath)
	if strings.HasSuffix(socketPath, ".sock") {
		return strings.TrimSuffix(socketPath, ".sock") + ".json"
	}
	return socketPath + ".json"
}

// ReadMetadata loads pane metadata from disk.
func ReadMetadata(path string) (PaneMetadata, error) {
	var meta PaneMetadata
	data, err := os.ReadFile(path)
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return PaneMetadata{}, err
	}
	return meta, nil
}

// SocketReachable reports whether the pane control socket currently accepts
// unix-domain connections.
func SocketReachable(socketPath string) bool {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return false
	}
	conn, err := net.DialTimeout("unix", socketPath, 150*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Serve starts the pane wrapper, spawns the child command behind a PTY, and
// blocks until the child exits or the context is canceled.
//
//nolint:gocyclo // This is the imperative shell for pane lifecycle orchestration; branching is driven by startup, socket, and PTY teardown concerns.
func Serve(ctx context.Context, opts ServeOptions) error {
	if strings.TrimSpace(opts.SocketPath) == "" {
		return fmt.Errorf("socket path is required")
	}
	if len(opts.Command) == 0 {
		return fmt.Errorf("child command is required")
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	defaultMode, err := normalizeSubmitMode(opts.DefaultSubmitMode)
	if err != nil {
		return err
	}
	startupProfile, err := normalizeStartupProfile(opts.StartupProfile)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(opts.SocketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	_ = os.Remove(opts.SocketPath)
	defer os.Remove(opts.SocketPath)
	if strings.TrimSpace(opts.ReadyPath) != "" {
		if err := os.MkdirAll(filepath.Dir(opts.ReadyPath), 0o755); err != nil {
			return fmt.Errorf("create ready directory: %w", err)
		}
		_ = os.Remove(opts.ReadyPath)
		defer os.Remove(opts.ReadyPath)
	}
	metaPath := MetadataPathForSocket(opts.SocketPath)
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return fmt.Errorf("create metadata directory: %w", err)
	}
	meta := PaneMetadata{
		ParticipantID: strings.TrimSpace(opts.ParticipantID),
		RoomID:        strings.TrimSpace(opts.RoomID),
		SocketPath:    strings.TrimSpace(opts.SocketPath),
		ReadyPath:     strings.TrimSpace(opts.ReadyPath),
	}
	if data, err := json.Marshal(meta); err == nil {
		if writeErr := os.WriteFile(metaPath, data, 0o600); writeErr != nil {
			return fmt.Errorf("write pane metadata: %w", writeErr)
		}
	} else {
		return fmt.Errorf("marshal pane metadata: %w", err)
	}
	defer os.Remove(metaPath)

	child := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
	if strings.TrimSpace(opts.CWD) != "" {
		child.Dir = opts.CWD
	}
	if len(opts.Env) > 0 {
		child.Env = append(os.Environ(), opts.Env...)
	}

	ptmx, err := pty.Start(child)
	if err != nil {
		return fmt.Errorf("start child pty: %w", err)
	}
	defer ptmx.Close()

	listener, err := net.Listen("unix", opts.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on pane socket: %w", err)
	}
	defer listener.Close()
	_ = os.Chmod(opts.SocketPath, 0o600)

	var writeMu sync.Mutex
	var readyOnce sync.Once
	readyCh := make(chan struct{})
	startupTriggerCh := make(chan struct{})
	var startupTriggerOnce sync.Once
	var startupTriggerBuf []byte
	markReady := func() {
		if strings.TrimSpace(opts.ReadyPath) == "" {
			readyOnce.Do(func() {
				close(readyCh)
			})
			return
		}
		readyOnce.Do(func() {
			_ = os.WriteFile(opts.ReadyPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600)
			close(readyCh)
		})
	}
	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				markReady()
				if startupProfile != "" {
					startupTriggerBuf, _ = updateStartupTrigger(startupProfile, startupTriggerBuf, buf[:n])
					if startupTriggerMatched(startupProfile, startupTriggerBuf) {
						startupTriggerOnce.Do(func() {
							close(startupTriggerCh)
						})
					}
				}
				if _, writeErr := stdout.Write(buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		_, _ = io.Copy(ptmx, stdin)
	}()

	acceptDone := make(chan error, 1)
	go func() {
		acceptDone <- serveSocket(ctx, listener, ptmx, &writeMu, defaultMode)
	}()
	if startupProfile != "" {
		go applyStartupProfile(ctx, readyCh, startupTriggerCh, ptmx, &writeMu, startupProfile)
	}

	childDone := make(chan error, 1)
	go func() {
		childDone <- child.Wait()
	}()

	select {
	case err := <-childDone:
		_ = listener.Close()
		<-acceptDone
		<-copyDone
		<-inputDone
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("child exited: %w", err)
	case err := <-acceptDone:
		_ = ptmx.Close()
		<-copyDone
		<-inputDone
		if err == nil || errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("pane socket server: %w", err)
	case <-ctx.Done():
		_ = listener.Close()
		_ = ptmx.Close()
		<-acceptDone
		<-copyDone
		<-inputDone
		<-childDone
		return nil
	}
}

func serveSocket(ctx context.Context, listener net.Listener, ptmx *os.File, writeMu *sync.Mutex, defaultMode string) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			var opErr *net.OpError
			if errors.As(err, &opErr) && strings.Contains(strings.ToLower(opErr.Err.Error()), "closed") {
				return nil
			}
			return err
		}
		handleControlConn(conn, ptmx, writeMu, defaultMode)
	}
}

func handleControlConn(conn net.Conn, ptmx *os.File, writeMu *sync.Mutex, defaultMode string) {
	defer conn.Close()

	var msg ControlMessage
	if err := json.NewDecoder(conn).Decode(&msg); err != nil {
		_ = json.NewEncoder(conn).Encode(ControlResponse{OK: false, Error: fmt.Sprintf("decode control message: %v", err)})
		return
	}
	payloads, err := renderControlMessageParts(msg, defaultMode)
	if err != nil {
		_ = json.NewEncoder(conn).Encode(ControlResponse{OK: false, Error: err.Error()})
		return
	}

	totalWritten := 0
	for i, payload := range payloads {
		writeMu.Lock()
		n, writeErr := ptmx.Write(payload)
		writeMu.Unlock()
		totalWritten += n
		if writeErr != nil {
			_ = json.NewEncoder(conn).Encode(ControlResponse{OK: false, Error: fmt.Sprintf("write to child pty: %v", writeErr)})
			return
		}
		if i < len(payloads)-1 {
			time.Sleep(75 * time.Millisecond)
		}
	}

	_ = json.NewEncoder(conn).Encode(ControlResponse{
		OK:           true,
		AcceptedAt:   time.Now().UTC(),
		BytesWritten: totalWritten,
	})
}

func renderControlMessageParts(msg ControlMessage, defaultMode string) ([][]byte, error) {
	switch strings.ToLower(strings.TrimSpace(msg.Kind)) {
	case "submit":
		return renderSubmitParts(msg.SubmitMode, defaultMode), nil
	case "interrupt":
		return [][]byte{{0x1b}}, nil
	default:
		return renderPayloadParts(msg.Content, msg.SubmitMode, defaultMode, msg.Interrupt)
	}
}

func renderPayload(content, mode, defaultMode string, interrupt bool) ([]byte, error) {
	parts, err := renderPayloadParts(content, mode, defaultMode, interrupt)
	if err != nil {
		return nil, err
	}
	return bytes.Join(parts, nil), nil
}

func renderSubmitParts(mode, defaultMode string) [][]byte {
	resolvedMode, err := normalizeSubmitMode(firstNonEmpty(mode, defaultMode))
	if err != nil {
		resolvedMode = SubmitModeNewline
	}
	switch resolvedMode {
	case SubmitModeRaw, SubmitModeNewline:
		return [][]byte{{'\n'}}
	case SubmitModeEnter, SubmitModeEnterSplit:
		return [][]byte{{'\r'}}
	case SubmitModeComposerCtrlEnter:
		return [][]byte{[]byte("\x1b[13;5u")}
	default:
		return [][]byte{{'\n'}}
	}
}

func renderPayloadParts(content, mode, defaultMode string, interrupt bool) ([][]byte, error) {
	resolvedMode, err := normalizeSubmitMode(firstNonEmpty(mode, defaultMode))
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	if interrupt && resolvedMode != SubmitModeComposerCtrlEnter {
		b.WriteByte(0x1b)
	}
	b.WriteString(content)
	switch resolvedMode {
	case SubmitModeRaw:
		return [][]byte{[]byte(b.String())}, nil
	case SubmitModeNewline:
		b.WriteByte('\n')
		return [][]byte{[]byte(b.String())}, nil
	case SubmitModeEnter:
		b.WriteByte('\r')
		return [][]byte{[]byte(b.String())}, nil
	case SubmitModeEnterSplit:
		return [][]byte{[]byte(b.String()), {'\r'}}, nil
	case SubmitModeComposerCtrlEnter:
		b.WriteString("\x1b[13;5u")
		return [][]byte{[]byte(b.String())}, nil
	default:
		return nil, fmt.Errorf("unsupported submit mode %q", resolvedMode)
	}
}

func normalizeSubmitMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SubmitModeNewline:
		return SubmitModeNewline, nil
	case SubmitModeRaw:
		return SubmitModeRaw, nil
	case SubmitModeEnter:
		return SubmitModeEnter, nil
	case SubmitModeEnterSplit:
		return SubmitModeEnterSplit, nil
	case SubmitModeComposerCtrlEnter:
		return SubmitModeComposerCtrlEnter, nil
	default:
		return "", fmt.Errorf("unsupported submit mode %q", mode)
	}
}

func normalizeStartupProfile(profile string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "":
		return "", nil
	case StartupProfileDroidAutoHigh:
		return StartupProfileDroidAutoHigh, nil
	default:
		return "", fmt.Errorf("unsupported startup profile %q", profile)
	}
}

func applyStartupProfile(ctx context.Context, readyCh <-chan struct{}, triggerCh <-chan struct{}, ptmx *os.File, writeMu *sync.Mutex, profile string) {
	select {
	case <-readyCh:
	case <-ctx.Done():
		return
	}
	if startupTriggerDeadline(profile) > 0 {
		timer := time.NewTimer(startupTriggerDeadline(profile))
		defer timer.Stop()
		select {
		case <-triggerCh:
		case <-timer.C:
		case <-ctx.Done():
			return
		}
	}

	switch profile {
	case StartupProfileDroidAutoHigh:
		applyStartupSequence(ctx, ptmx, writeMu, []startupStep{
			{Delay: 150 * time.Millisecond, Payload: []byte{0x0c}},
			{Delay: 250 * time.Millisecond, Payload: []byte{0x0c}},
			{Delay: 250 * time.Millisecond, Payload: []byte{0x0c}},
		})
	}
}

func startupTriggerDeadline(profile string) time.Duration {
	switch profile {
	case StartupProfileDroidAutoHigh:
		return 5 * time.Second
	default:
		return 0
	}
}

type startupStep struct {
	Delay   time.Duration
	Payload []byte
}

func applyStartupSequence(ctx context.Context, ptmx *os.File, writeMu *sync.Mutex, steps []startupStep) {
	for _, step := range steps {
		if step.Delay > 0 {
			timer := time.NewTimer(step.Delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
		if len(step.Payload) == 0 {
			continue
		}
		writeMu.Lock()
		_, _ = ptmx.Write(step.Payload)
		writeMu.Unlock()
	}
}

func updateStartupTrigger(profile string, window, chunk []byte) ([]byte, bool) {
	maxLen := startupTriggerWindowSize(profile)
	if maxLen <= 0 {
		return nil, false
	}
	window = append(window, chunk...)
	if len(window) > maxLen {
		window = append([]byte(nil), window[len(window)-maxLen:]...)
	} else {
		window = append([]byte(nil), window...)
	}
	return window, startupTriggerMatched(profile, window)
}

func startupTriggerMatched(profile string, window []byte) bool {
	switch profile {
	case StartupProfileDroidAutoHigh:
		return bytes.Contains(window, []byte("Auto (Off)"))
	default:
		return false
	}
}

func startupTriggerWindowSize(profile string) int {
	switch profile {
	case StartupProfileDroidAutoHigh:
		return 512
	default:
		return 0
	}
}

func sanitizeSocketComponent(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._-")
}

func boundedSocketComponent(value string, maxLen int) string {
	sanitized := sanitizeSocketComponent(value)
	if maxLen <= 0 || len(sanitized) <= maxLen {
		return sanitized
	}
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(sanitized))
	suffix := fmt.Sprintf("-%08x", sum.Sum32())
	if maxLen <= len(suffix) {
		return sanitized[:maxLen]
	}
	prefixLen := maxLen - len(suffix)
	return sanitized[:prefixLen] + suffix
}
