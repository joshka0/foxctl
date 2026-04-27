// foxprox-live is an interactive driver for live multi-agent integration
// against a running atcpd. Given one or more `--agent NAME=CMD` flags, it:
//
//  1. Connects to the daemon's Unix socket.
//  2. Creates a room.
//  3. For each agent: POSTs a session (the agent's CLI runs inside a PTY
//     the daemon owns), then joins the room with CanMutate=true so the
//     member can both receive and be addressed by the router.
//  4. Opens a per-agent SSE tail on GET /v1/events?target=session:<id> and
//     prints decoded PTY bytes to stdout prefixed with "[name] ".
//  5. Forwards each line read from stdin to POST /v1/messages so a human
//     (or an outer script) can inject text into the room.
//
// On SIGINT the driver leaves every member, deletes every session it
// created, and exits. Rooms are left intact so their persisted history
// survives for inspection.
//
// This is the "fast path" demo binary — it deliberately does not cover
// leases, paste mode, or key-level input. Those surfaces already exist in
// the broker and will land in foxproxctl once we have real-agent feedback on
// what's actually needed.
//
// Example:
//
//	foxproxd &
//	foxprox-live \
//	  --agent codex='codex --chat' \
//	  --agent droid='droid run' \
//	  --agent gemini='gemini chat' \
//	  --source human
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/joshka/foxprox/foxprox/client"
	"github.com/joshka/foxprox/foxprox/envelope"
	"github.com/joshka/foxprox/foxprox/transport/httpjson"
	"github.com/joshka/foxprox/foxprox/transport/unixsocket"
	"github.com/oklog/ulid/v2"
)

// agentSpec captures a --agent NAME=CMD flag. Multiple are allowed; Go's
// flag package doesn't natively support repeated flags so we wrap the slice
// behind a flag.Value.
type agentSpec struct {
	Name string
	Cmd  []string
}

type agentFlags struct {
	specs *[]agentSpec
}

type talkbackRule struct {
	Name   string
	Prefix string
}

type talkbackFlags struct {
	rules *[]talkbackRule
}

type liveAgent struct {
	Name      string
	SessionID string
	Adapter   string
}

func (a agentFlags) String() string {
	if a.specs == nil {
		return ""
	}
	parts := make([]string, 0, len(*a.specs))
	for _, s := range *a.specs {
		parts = append(parts, s.Name+"="+strings.Join(s.Cmd, " "))
	}
	return strings.Join(parts, ",")
}

func (a agentFlags) Set(v string) error {
	// We intentionally split only on the FIRST "=" so commands can contain
	// "=" themselves (e.g. `gemini --model=pro`).
	eq := strings.IndexByte(v, '=')
	if eq <= 0 {
		return fmt.Errorf("want NAME=CMD, got %q", v)
	}
	name := strings.TrimSpace(v[:eq])
	cmd := strings.TrimSpace(v[eq+1:])
	if name == "" || cmd == "" {
		return fmt.Errorf("both NAME and CMD must be non-empty in %q", v)
	}
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty cmd for agent %q", name)
	}
	*a.specs = append(*a.specs, agentSpec{Name: name, Cmd: parts})
	return nil
}

func (t talkbackFlags) String() string {
	if t.rules == nil {
		return ""
	}
	parts := make([]string, 0, len(*t.rules))
	for _, r := range *t.rules {
		parts = append(parts, r.Name+"="+r.Prefix)
	}
	return strings.Join(parts, ",")
}

func (t talkbackFlags) Set(v string) error {
	eq := strings.IndexByte(v, '=')
	if eq <= 0 {
		return fmt.Errorf("want NAME=PREFIX, got %q", v)
	}
	name := strings.TrimSpace(v[:eq])
	prefix := v[eq+1:]
	if name == "" || prefix == "" {
		return fmt.Errorf("both NAME and PREFIX must be non-empty in %q", v)
	}
	*t.rules = append(*t.rules, talkbackRule{Name: name, Prefix: prefix})
	return nil
}

func talkbackMap(rules []talkbackRule) map[string]string {
	out := make(map[string]string, len(rules))
	for _, r := range rules {
		out[r.Name] = r.Prefix
	}
	return out
}

func main() {
	var (
		socket        = flag.String("socket", "", "foxproxd unix socket path (default: $FOXCTL_Foxprox_SOCK or platform default)")
		workspace     = flag.String("workspace", "live", "room workspace label")
		roomTitle     = flag.String("room-title", "foxprox-live", "room title")
		roomID        = flag.String("room-id", "", "join this existing room instead of creating one")
		source        = flag.String("source", "human", "source id used when forwarding stdin lines as messages")
		noInput       = flag.Bool("no-input", false, "do not forward stdin lines; behave as an observer")
		render        = flag.Bool("render", false, "render virtual screen snapshots instead of raw PTY byte lines")
		sinceSeq      = flag.Uint64("since-seq", 0, "replay each agent's output log from this seq (0 = from the beginning)")
		readyWait     = flag.Duration("readiness-timeout", 30*time.Second, "how long to wait for each session to go output-idle after startup (0 disables)")
		readyRate     = flag.Float64("idle-threshold-bps", 32, "readiness output-rate threshold in bytes/sec")
		readyDebounce = flag.Duration("idle-debounce", 500*time.Millisecond, "readiness debounce window")
		warmupTimeout = flag.Duration("warmup-timeout", 0, "warn if a session is output-idle for this long during warmup (0 disables)")
		joinOnly      = flag.Bool("join-only", false, "join an existing room (requires --session per agent instead of --cmd); skips session create")
		agents        []agentSpec
		talkbacks     []talkbackRule
		existingID    = flag.String("session", "", "(repeatable when used with --join-only) existing session id to tail")
	)
	flag.Var(agentFlags{specs: &agents}, "agent", "agent definition NAME=CMD; repeatable")
	flag.Var(talkbackFlags{rules: &talkbacks}, "talkback", "agent output bridge NAME=PREFIX; repeatable")
	flag.Parse()
	_ = existingID // reserved for a future --session flag iteration; see joinOnly note below.

	if len(agents) == 0 {
		fatalf("at least one --agent NAME=CMD is required")
	}
	if *joinOnly {
		// join-only mode would need --session per agent to know which
		// existing PTY each NAME maps to. We don't support it yet; the
		// flag is reserved so the CLI shape is stable once it lands.
		fatalf("--join-only is reserved; spawn fresh sessions for now")
	}

	if *socket == "" {
		*socket = unixsocket.DefaultSocketPath()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c := client.ForSocket(*socket)
	if err := c.Health(ctx); err != nil {
		fatalf("daemon health check: %v (is foxproxd running on %s?)", err, *socket)
	}

	// Resolve the room: either reuse an existing one or create a fresh one
	// for this session. Reuse is useful when the human wants to join a
	// long-running coordination (e.g. codex already spawned the room from
	// a prior foxprox-live invocation).
	var room httpjson.RoomResponse
	if *roomID == "" {
		rm, err := c.CreateRoom(ctx, httpjson.CreateRoomRequest{
			Workspace: *workspace, Title: *roomTitle,
		})
		if err != nil {
			fatalf("create room: %v", err)
		}
		room = rm
		fmt.Fprintf(os.Stderr, "foxprox-live: room %s created (workspace=%s title=%q)\n", room.ID, *workspace, *roomTitle)
	} else {
		// There's no GET /v1/rooms/{id} single-resource path in the
		// current CLI surface, but we don't actually need the room
		// details here — just the id, which we already have. Stash
		// minimally.
		room = httpjson.RoomResponse{ID: *roomID}
		fmt.Fprintf(os.Stderr, "foxprox-live: reusing room %s\n", room.ID)
	}

	// Spawn + join every agent. We keep a slice so teardown can run in
	// reverse registration order (LIFO) — that minimises the window where
	// a session's PTY exits but its room member hasn't yet been marked
	// LeftAt on the wire.
	var (
		live []liveAgent
		mu   sync.Mutex
		wg   sync.WaitGroup
	)
	liveSnapshot := func() []liveAgent {
		mu.Lock()
		defer mu.Unlock()
		out := make([]liveAgent, len(live))
		copy(out, live)
		return out
	}
	sseClient := sseHTTPClient(*socket)
	talkbackPrefixes := talkbackMap(talkbacks)

	defer func() {
		// Teardown runs unconditionally so partial setups don't leak
		// daemon-side state. Uses a fresh context so SIGINT during the
		// main loop still gives us time to clean up.
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mu.Lock()
		for i := len(live) - 1; i >= 0; i-- {
			a := live[i]
			if _, err := c.LeaveRoom(shutCtx, room.ID, httpjson.LeaveRoomRequest{AgentID: a.Name}); err != nil {
				fmt.Fprintf(os.Stderr, "foxprox-live: LeaveRoom %s: %v\n", a.Name, err)
			}
			if err := c.DeleteSession(shutCtx, a.SessionID); err != nil {
				fmt.Fprintf(os.Stderr, "foxprox-live: DeleteSession %s: %v\n", a.Name, err)
			}
		}
		mu.Unlock()
		wg.Wait()
	}()

	for _, spec := range agents {
		snap, err := c.CreateSession(ctx, httpjson.CreateSessionRequest{Cmd: spec.Cmd, Adapter: spec.Name})
		if err != nil {
			fatalf("CreateSession %s: %v", spec.Name, err)
		}
		if _, err := c.JoinRoom(ctx, room.ID, httpjson.JoinRoomRequest{
			AgentID:   spec.Name,
			SessionID: snap.ID,
			CanMutate: true,
		}); err != nil {
			fatalf("JoinRoom %s: %v", spec.Name, err)
		}
		mu.Lock()
		live = append(live, liveAgent{Name: spec.Name, SessionID: snap.ID, Adapter: snap.Adapter})
		mu.Unlock()
		fmt.Fprintf(os.Stderr, "foxprox-live: spawned %s session=%s pid=%d cmd=%q\n",
			spec.Name, snap.ID, snap.PID, strings.Join(spec.Cmd, " "))

		wg.Add(1)
		go func(name, sessionID string) {
			defer wg.Done()
			var err error
			if *render {
				err = renderSession(ctx, c, room.ID, sessionID, name, talkbackPrefixes[name], 200*time.Millisecond)
			} else {
				err = tailSession(ctx, sseClient, c, *socket, room.ID, sessionID, name, talkbackPrefixes[name], *sinceSeq)
			}
			if err != nil {
				// Context cancellation on shutdown is expected; only
				// surface genuine errors so SIGINT doesn't spam.
				if !errors.Is(err, context.Canceled) {
					fmt.Fprintf(os.Stderr, "foxprox-live: tail %s ended: %v\n", name, err)
				}
			}
		}(spec.Name, snap.ID)
		if *warmupTimeout > 0 {
			go warnIfWarmupIdle(ctx, c, liveAgent{Name: spec.Name, SessionID: snap.ID, Adapter: snap.Adapter}, *warmupTimeout)
		}
	}

	readiness := readinessConfig{
		Timeout:      *readyWait,
		ThresholdBPS: *readyRate,
		Debounce:     *readyDebounce,
		ScreenRegex:  *render,
	}
	waitForAgentsReady(ctx, c, liveSnapshot(), readiness)

	if *noInput {
		fmt.Fprintln(os.Stderr, "foxprox-live: observer mode (--no-input); waiting for SIGINT")
		<-ctx.Done()
		return
	}

	fmt.Fprintln(os.Stderr, "foxprox-live: type a line and press Enter to broadcast to the room (Ctrl+D or Ctrl+C to exit)")
	if err := forwardStdin(ctx, c, room.ID, *source, liveSnapshot, readiness); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "foxprox-live: stdin loop: %v\n", err)
	}
}

type readinessConfig struct {
	Timeout      time.Duration
	ThresholdBPS float64
	Debounce     time.Duration
	ScreenRegex  bool
}

func renderSession(ctx context.Context, c *client.Client, roomID, sessionID, name, talkbackPrefix string, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var last []string
	for {
		snap, err := c.SessionScreen(ctx, sessionID)
		if err != nil {
			return err
		}
		prefix := "[" + name + "] "
		rows := snap.DirtyRows
		if len(rows) == 0 && last == nil {
			rows = make([]int, len(snap.Lines))
			for i := range rows {
				rows[i] = i
			}
		}
		for _, row := range rows {
			if row < 0 || row >= len(snap.Lines) {
				continue
			}
			line := snap.Lines[row]
			if line == "" {
				continue
			}
			if row < len(last) && last[row] == line {
				continue
			}
			maybeTalkback(ctx, c, roomID, name, talkbackPrefix, line)
			writeLine(prefix, []byte(line))
		}
		last = append(last[:0], snap.Lines...)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func maybeTalkback(ctx context.Context, c *client.Client, roomID, source, prefix, line string) {
	targetRoomID, text, ok := talkbackMessage(roomID, prefix, line)
	if !ok {
		return
	}
	if _, err := c.SendMessage(ctx, httpjson.SendMessageRequest{
		RoomID:     targetRoomID,
		Source:     source,
		Text:       text,
		SkipAgents: []string{source},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "foxprox-live: talkback %s: %v\n", source, err)
	}
}

func talkbackText(prefix, line string) (string, bool) {
	_, text, ok := talkbackMessage("", prefix, line)
	return text, ok
}

func talkbackMessage(defaultRoomID, prefix, line string) (string, string, bool) {
	if prefix == "" {
		return "", "", false
	}
	idx := strings.Index(line, prefix)
	if idx < 0 || !talkbackLeaderOnly(line[:idx]) {
		return "", "", false
	}
	payload := strings.TrimSpace(line[idx+len(prefix):])
	if payload == "" {
		return "", "", false
	}
	targetRoomID := defaultRoomID
	if candidate, rest, ok := strings.Cut(payload, " "); ok && isRoomID(candidate) {
		targetRoomID = candidate
		payload = strings.TrimSpace(rest)
	}
	if payload == "" {
		return "", "", false
	}
	return targetRoomID, payload, true
}

func isRoomID(s string) bool {
	_, err := ulid.Parse(s)
	return err == nil
}

func talkbackLeaderOnly(s string) bool {
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		return false
	}
	return true
}

// forwardStdin reads a line at a time from os.Stdin and relays each one as
// a room message. EOF on stdin terminates cleanly, letting deferred
// teardown run.
func forwardStdin(ctx context.Context, c *client.Client, roomID, source string, liveSnapshot func() []liveAgent, readiness readinessConfig) error {
	scanner := bufio.NewScanner(os.Stdin)
	// Default bufio scanner tops out at 64 KiB per line. Bump it so pasted
	// blobs (common when a human dumps a traceback into the room) don't
	// truncate.
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		waitForAgentsReady(ctx, c, liveSnapshot(), readiness)
		res, err := c.SendMessage(ctx, httpjson.SendMessageRequest{
			RoomID: roomID, Source: source, Text: line,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "foxprox-live: msg send: %v\n", err)
			continue
		}
		fmt.Fprintf(os.Stderr, "foxprox-live: msg %s delivered=%d failed=%d\n", res.MessageID, res.Delivered, res.Failed)
		waitForAgentsReady(ctx, c, liveSnapshot(), readiness)
	}
	return scanner.Err()
}

func waitForAgentsReady(ctx context.Context, c *client.Client, agents []liveAgent, cfg readinessConfig) {
	if cfg.Timeout <= 0 || len(agents) == 0 {
		return
	}
	var (
		wg      sync.WaitGroup
		writeMu sync.Mutex
	)
	for _, agent := range agents {
		agent := agent
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready, err := waitSessionReady(ctx, c, agent, cfg)
			writeMu.Lock()
			defer writeMu.Unlock()
			if err != nil {
				fmt.Fprintf(os.Stderr, "foxprox-live: [%s] readiness: %v\n", agent.Name, err)
				return
			}
			fmt.Fprintf(os.Stdout, "[%s] ready (idle_for=%dms rate=%.1fB/s)\n",
				agent.Name, ready.IdleForMS, ready.OutputRateBPS)
		}()
	}
	wg.Wait()
}

func waitSessionReady(ctx context.Context, c *client.Client, agent liveAgent, cfg readinessConfig) (httpjson.ReadinessResponse, error) {
	waitCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	opts := client.SessionReadinessOptions{
		ThresholdBPS: cfg.ThresholdBPS,
		DebounceMS:   int(cfg.Debounce / time.Millisecond),
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var last httpjson.ReadinessResponse
	var lastErr error
	for {
		ready, err := c.SessionReadiness(waitCtx, agent.SessionID, opts)
		if err == nil {
			last = ready
			if ready.Idle {
				return ready, nil
			}
		} else {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
			if lastErr != nil {
				return last, fmt.Errorf("not ready within %v: %w", cfg.Timeout, lastErr)
			}
			return last, fmt.Errorf("not ready within %v (idle_for=%dms rate=%.1fB/s)",
				cfg.Timeout, last.IdleForMS, last.OutputRateBPS)
		case <-ticker.C:
		}
	}
}

func warnIfWarmupIdle(ctx context.Context, c *client.Client, agent liveAgent, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	ready, err := c.SessionReadiness(ctx, agent.SessionID, client.SessionReadinessOptions{
		ThresholdBPS: 1,
		DebounceMS:   int(timeout / time.Millisecond),
	})
	if err != nil || !shouldWarnWarmup(ready, timeout) {
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", formatWarmupWarning(agent.Name, timeout))
}

func shouldWarnWarmup(ready httpjson.ReadinessResponse, timeout time.Duration) bool {
	return ready.Idle && time.Duration(ready.IdleForMS)*time.Millisecond >= timeout
}

func formatWarmupWarning(name string, timeout time.Duration) string {
	return fmt.Sprintf("[%s] no new output for %s - likely stuck in init", name, timeout)
}

// --- SSE tailing ------------------------------------------------------------

// sseHTTPClient builds an http.Client with NO timeout (SSE streams are long
// lived by design) that dials the same unix socket as the rest of the
// driver. We can't reuse client.ForSocket's http.Client because that one
// has a 30s timeout baked in for request/response calls.
func sseHTTPClient(socket string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
		// Timeout is explicitly zero. The per-request context carries
		// cancellation so Ctrl+C still tears the reader down.
	}
}

// tailSession subscribes to GET /v1/events?target=session:<id> and prints
// each decoded PTY chunk to stdout with an "[name] " prefix on line
// boundaries. "Line boundaries" here is best-effort — we buffer partial
// lines per agent so a lone "[codex] " prefix never lands mid-word.
func tailSession(ctx context.Context, httpc *http.Client, c *client.Client, socket, roomID, sessionID, name, talkbackPrefix string, since uint64) error {
	q := url.Values{}
	q.Set("target", "session:"+sessionID)
	if since > 0 {
		q.Set("since", strconv.FormatUint(since, 10))
	}
	// "http://foxprox" is a synthetic authority — the unix DialContext
	// ignores it but net/http still requires a syntactically valid URL.
	endpoint := "http://foxprox/v1/events?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build events request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := httpc.Do(req)
	if err != nil {
		return fmt.Errorf("open events stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("events stream status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	prefix := "[" + name + "] "
	reader := bufio.NewReader(resp.Body)
	// partial holds any bytes from the previous decode that didn't end
	// with a newline. We flush on newline only to keep per-agent streams
	// aligned in the mixed stdout.
	var partial []byte
	for {
		dataLine, err := readSSEDataFrame(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if dataLine == "" {
			continue
		}
		var env envelope.Envelope
		if err := json.Unmarshal([]byte(dataLine), &env); err != nil {
			fmt.Fprintf(os.Stderr, "foxprox-live: %s: decode envelope: %v\n", name, err)
			continue
		}
		var body httpjson.TerminalOutputBody
		if err := json.Unmarshal(env.Body, &body); err != nil {
			fmt.Fprintf(os.Stderr, "foxprox-live: %s: decode body: %v\n", name, err)
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(body.BytesB64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "foxprox-live: %s: base64: %v\n", name, err)
			continue
		}
		partial = append(partial, raw...)
		// Split on \n, keeping the trailing fragment (if any) for the
		// next frame. We translate bare \r to \n so agents using
		// carriage-return-only line endings still look right.
		for {
			idx := indexNewline(partial)
			if idx < 0 {
				break
			}
			line := partial[:idx]
			partial = partial[idx+1:]
			maybeTalkback(ctx, c, roomID, name, talkbackPrefix, string(line))
			writeLine(prefix, line)
		}
	}
}

// readSSEDataFrame reads lines until the blank frame separator, joining
// any consecutive "data:" lines into the returned string. Ignores "id:",
// "event:", and comment lines per the SSE spec.
func readSSEDataFrame(r *bufio.Reader) (string, error) {
	var data strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			// If we got partial data before EOF, discard it — an
			// incomplete frame is ambiguous.
			if errors.Is(err, io.EOF) && data.Len() == 0 {
				return "", io.EOF
			}
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			// End of frame.
			return data.String(), nil
		}
		if strings.HasPrefix(line, ":") {
			continue // comment
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			rest = strings.TrimPrefix(rest, " ")
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(rest)
		}
		// id: / event: / retry: / unknown fields are intentionally
		// ignored — we only need `data`.
	}
}

// indexNewline returns the first \n or \r in b, or -1. We treat either as
// a line terminator so shells that emit bare \r survive.
func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' || c == '\r' {
			return i
		}
	}
	return -1
}

// writeLine writes "[name] <line>\n" atomically enough for our purposes.
// Stdout is line-buffered when it's a terminal so a single Write is good
// enough to avoid interleaving across goroutines in practice.
func writeLine(prefix string, line []byte) {
	// Strip trailing \r when the line terminator was \r\n.
	line = trimTrailingCR(line)
	var buf []byte
	buf = append(buf, prefix...)
	buf = append(buf, line...)
	buf = append(buf, '\n')
	_, _ = os.Stdout.Write(buf)
}

func trimTrailingCR(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\r' {
		return b[:len(b)-1]
	}
	return b
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "foxprox-live: "+format+"\n", args...)
	os.Exit(1)
}
