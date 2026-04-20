// atcp-live is an interactive driver for live multi-agent integration
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
// the broker and will land in atcpctl once we have real-agent feedback on
// what's actually needed.
//
// Example:
//
//	atcpd &
//	atcp-live \
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

	"github.com/joshka0/foxctl/internal/atcp/client"
	"github.com/joshka0/foxctl/internal/atcp/envelope"
	"github.com/joshka0/foxctl/internal/atcp/transport/httpjson"
	"github.com/joshka0/foxctl/internal/atcp/transport/unixsocket"
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

func main() {
	var (
		socket     = flag.String("socket", "", "atcpd unix socket path (default: $FOXCTL_ATCP_SOCK or platform default)")
		workspace  = flag.String("workspace", "live", "room workspace label")
		roomTitle  = flag.String("room-title", "atcp-live", "room title")
		roomID     = flag.String("room-id", "", "join this existing room instead of creating one")
		source     = flag.String("source", "human", "source id used when forwarding stdin lines as messages")
		noInput    = flag.Bool("no-input", false, "do not forward stdin lines; behave as an observer")
		sinceSeq   = flag.Uint64("since-seq", 0, "replay each agent's output log from this seq (0 = from the beginning)")
		joinOnly   = flag.Bool("join-only", false, "join an existing room (requires --session per agent instead of --cmd); skips session create")
		agents     []agentSpec
		existingID = flag.String("session", "", "(repeatable when used with --join-only) existing session id to tail")
	)
	flag.Var(agentFlags{specs: &agents}, "agent", "agent definition NAME=CMD; repeatable")
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
		fatalf("daemon health check: %v (is atcpd running on %s?)", err, *socket)
	}

	// Resolve the room: either reuse an existing one or create a fresh one
	// for this session. Reuse is useful when the human wants to join a
	// long-running coordination (e.g. codex already spawned the room from
	// a prior atcp-live invocation).
	var room httpjson.RoomResponse
	if *roomID == "" {
		rm, err := c.CreateRoom(ctx, httpjson.CreateRoomRequest{
			Workspace: *workspace, Title: *roomTitle,
		})
		if err != nil {
			fatalf("create room: %v", err)
		}
		room = rm
		fmt.Fprintf(os.Stderr, "atcp-live: room %s created (workspace=%s title=%q)\n", room.ID, *workspace, *roomTitle)
	} else {
		// There's no GET /v1/rooms/{id} single-resource path in the
		// current CLI surface, but we don't actually need the room
		// details here — just the id, which we already have. Stash
		// minimally.
		room = httpjson.RoomResponse{ID: *roomID}
		fmt.Fprintf(os.Stderr, "atcp-live: reusing room %s\n", room.ID)
	}

	// Spawn + join every agent. We keep a slice so teardown can run in
	// reverse registration order (LIFO) — that minimises the window where
	// a session's PTY exits but its room member hasn't yet been marked
	// LeftAt on the wire.
	type liveAgent struct {
		Name      string
		SessionID string
	}
	var (
		live []liveAgent
		mu   sync.Mutex
		wg   sync.WaitGroup
	)
	sseClient := sseHTTPClient(*socket)

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
				fmt.Fprintf(os.Stderr, "atcp-live: LeaveRoom %s: %v\n", a.Name, err)
			}
			if err := c.DeleteSession(shutCtx, a.SessionID); err != nil {
				fmt.Fprintf(os.Stderr, "atcp-live: DeleteSession %s: %v\n", a.Name, err)
			}
		}
		mu.Unlock()
		wg.Wait()
	}()

	for _, spec := range agents {
		snap, err := c.CreateSession(ctx, httpjson.CreateSessionRequest{Cmd: spec.Cmd})
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
		live = append(live, liveAgent{Name: spec.Name, SessionID: snap.ID})
		mu.Unlock()
		fmt.Fprintf(os.Stderr, "atcp-live: spawned %s session=%s pid=%d cmd=%q\n",
			spec.Name, snap.ID, snap.PID, strings.Join(spec.Cmd, " "))

		wg.Add(1)
		go func(name, sessionID string) {
			defer wg.Done()
			if err := tailSession(ctx, sseClient, *socket, sessionID, name, *sinceSeq); err != nil {
				// Context cancellation on shutdown is expected; only
				// surface genuine errors so SIGINT doesn't spam.
				if !errors.Is(err, context.Canceled) {
					fmt.Fprintf(os.Stderr, "atcp-live: tail %s ended: %v\n", name, err)
				}
			}
		}(spec.Name, snap.ID)
	}

	if *noInput {
		fmt.Fprintln(os.Stderr, "atcp-live: observer mode (--no-input); waiting for SIGINT")
		<-ctx.Done()
		return
	}

	fmt.Fprintln(os.Stderr, "atcp-live: type a line and press Enter to broadcast to the room (Ctrl+D or Ctrl+C to exit)")
	if err := forwardStdin(ctx, c, room.ID, *source); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "atcp-live: stdin loop: %v\n", err)
	}
}

// forwardStdin reads a line at a time from os.Stdin and relays each one as
// a room message. EOF on stdin terminates cleanly, letting deferred
// teardown run.
func forwardStdin(ctx context.Context, c *client.Client, roomID, source string) error {
	scanner := bufio.NewScanner(os.Stdin)
	// Default bufio scanner tops out at 64 KiB per line. Bump it so pasted
	// blobs (common when a human dumps a traceback into the room) don't
	// truncate.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
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
		res, err := c.SendMessage(ctx, httpjson.SendMessageRequest{
			RoomID: roomID, Source: source, Text: line,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "atcp-live: msg send: %v\n", err)
			continue
		}
		fmt.Fprintf(os.Stderr, "atcp-live: msg %s delivered=%d failed=%d\n", res.MessageID, res.Delivered, res.Failed)
	}
	return scanner.Err()
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
func tailSession(ctx context.Context, httpc *http.Client, socket, sessionID, name string, since uint64) error {
	q := url.Values{}
	q.Set("target", "session:"+sessionID)
	if since > 0 {
		q.Set("since", strconv.FormatUint(since, 10))
	}
	// "http://atcp" is a synthetic authority — the unix DialContext
	// ignores it but net/http still requires a syntactically valid URL.
	endpoint := "http://atcp/v1/events?" + q.Encode()
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
			fmt.Fprintf(os.Stderr, "atcp-live: %s: decode envelope: %v\n", name, err)
			continue
		}
		var body httpjson.TerminalOutputBody
		if err := json.Unmarshal(env.Body, &body); err != nil {
			fmt.Fprintf(os.Stderr, "atcp-live: %s: decode body: %v\n", name, err)
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(body.BytesB64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atcp-live: %s: base64: %v\n", name, err)
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
	fmt.Fprintf(os.Stderr, "atcp-live: "+format+"\n", args...)
	os.Exit(1)
}
