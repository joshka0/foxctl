// foxproxctl is a minimal CLI for talking to an Foxprox daemon.
//
// Surface is intentionally narrow while the wire protocol stabilises —
// enough to demonstrate every intent from the shell, not a full replacement
// for the legacy foxctl CLI. When the legacy room/blackboard code is
// finally hard-cut, these commands can fold into `foxctl foxprox ...` without
// contract changes.
//
// Usage:
//
//	foxproxctl [--socket PATH] <command> [args]
//	  health
//	  session create --cmd "bash -i"
//	  session list
//	  session activity SESSION_ID [--since-seq N] [--since-output-bytes-total N]
//	  session delete SESSION_ID
//	  room create --workspace ws [--title T]
//	  room list
//	  room join ROOM_ID --agent A --session S [--can-mutate]
//	  room leave ROOM_ID --agent A
//	  room members ROOM_ID
//	  room messages ROOM_ID [--limit N]
//	  msg send --room R --text "..."
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joshka/foxprox/foxprox/client"
	"github.com/joshka/foxprox/foxprox/daemon"
	"github.com/joshka/foxprox/foxprox/transport/httpjson"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	// Allow a global --socket flag before the subcommand for operator
	// convenience: foxproxctl --socket /tmp/s health.
	args := os.Args[1:]
	socket := ""
	if len(args) >= 2 && args[0] == "--socket" {
		socket = args[1]
		args = args[2:]
	}
	if socket == "" {
		socket = daemon.DefaultSocketPath()
	}
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	c := client.ForSocket(socket)
	ctx := context.Background()

	switch args[0] {
	case "health":
		if err := c.Health(ctx); err != nil {
			fatal(err)
		}
		fmt.Println("ok")
	case "session":
		runSession(ctx, c, args[1:])
	case "room":
		runRoom(ctx, c, args[1:])
	case "msg":
		runMsg(ctx, c, args[1:])
	default:
		usage()
		os.Exit(2)
	}
}

func runSession(ctx context.Context, c *client.Client, args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("session create", flag.ExitOnError)
		cmd := fs.String("cmd", "", "command to run (split on whitespace)")
		submitKey := fs.String("submit-key", "", "default submit key for terminal.submit")
		enableRawBytes := fs.Bool("enable-raw-bytes", false, "allow terminal.write_bytes for this trusted session")
		_ = fs.Parse(args[1:])
		if *cmd == "" {
			fatal(errors.New("--cmd is required"))
		}
		parts := strings.Fields(*cmd)
		out, err := c.CreateSession(ctx, httpjson.CreateSessionRequest{
			Cmd:            parts,
			SubmitKey:      *submitKey,
			EnableRawBytes: *enableRawBytes,
		})
		if err != nil {
			fatal(err)
		}
		emit(out)
	case "list":
		out, err := c.ListSessions(ctx)
		if err != nil {
			fatal(err)
		}
		emit(out)
	case "activity":
		if len(args) < 2 {
			fatal(errors.New("session activity SESSION_ID [flags]"))
		}
		sessionID := args[1]
		fs := flag.NewFlagSet("session activity", flag.ExitOnError)
		sinceSeq := fs.Uint64("since-seq", 0, "previous heartbeat last_seq cursor")
		sinceBytes := fs.Int64("since-output-bytes-total", 0, "previous heartbeat output_bytes_total cursor")
		_ = fs.Parse(args[2:])
		out, err := c.SessionActivity(ctx, sessionID, client.SessionActivityOptions{
			SinceSeq:              *sinceSeq,
			SinceOutputBytesTotal: *sinceBytes,
		})
		if err != nil {
			fatal(err)
		}
		emit(out)
	case "delete":
		if len(args) < 2 {
			fatal(errors.New("session delete SESSION_ID"))
		}
		if err := c.DeleteSession(ctx, args[1]); err != nil {
			fatal(err)
		}
		fmt.Println("ok")
	default:
		usage()
		os.Exit(2)
	}
}

func runRoom(ctx context.Context, c *client.Client, args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("room create", flag.ExitOnError)
		ws := fs.String("workspace", "", "workspace name (required)")
		title := fs.String("title", "", "room title")
		_ = fs.Parse(args[1:])
		if *ws == "" {
			fatal(errors.New("--workspace is required"))
		}
		out, err := c.CreateRoom(ctx, httpjson.CreateRoomRequest{Workspace: *ws, Title: *title})
		if err != nil {
			fatal(err)
		}
		emit(out)
	case "list":
		out, err := c.ListRooms(ctx)
		if err != nil {
			fatal(err)
		}
		emit(out)
	case "join":
		if len(args) < 2 {
			fatal(errors.New("room join ROOM_ID [flags]"))
		}
		roomID := args[1]
		fs := flag.NewFlagSet("room join", flag.ExitOnError)
		agent := fs.String("agent", "", "agent id (required)")
		session := fs.String("session", "", "session id (required)")
		canMutate := fs.Bool("can-mutate", false, "member can hold terminal.input leases")
		_ = fs.Parse(args[2:])
		if *agent == "" || *session == "" {
			fatal(errors.New("--agent and --session are required"))
		}
		out, err := c.JoinRoom(ctx, roomID, httpjson.JoinRoomRequest{
			AgentID: *agent, SessionID: *session, CanMutate: *canMutate,
		})
		if err != nil {
			fatal(err)
		}
		emit(out)
	case "leave":
		if len(args) < 2 {
			fatal(errors.New("room leave ROOM_ID [flags]"))
		}
		roomID := args[1]
		fs := flag.NewFlagSet("room leave", flag.ExitOnError)
		agent := fs.String("agent", "", "agent id (required)")
		_ = fs.Parse(args[2:])
		if *agent == "" {
			fatal(errors.New("--agent is required"))
		}
		out, err := c.LeaveRoom(ctx, roomID, httpjson.LeaveRoomRequest{AgentID: *agent})
		if err != nil {
			fatal(err)
		}
		emit(out)
	case "members":
		if len(args) < 2 {
			fatal(errors.New("room members ROOM_ID"))
		}
		out, err := c.RoomMembers(ctx, args[1])
		if err != nil {
			fatal(err)
		}
		emit(out)
	case "messages":
		if len(args) < 2 {
			fatal(errors.New("room messages ROOM_ID [flags]"))
		}
		roomID := args[1]
		fs := flag.NewFlagSet("room messages", flag.ExitOnError)
		limit := fs.Int("limit", 100, "maximum messages to return; 0 means all")
		_ = fs.Parse(args[2:])
		if *limit < 0 {
			fatal(errors.New("--limit must be >= 0"))
		}
		out, err := c.RoomMessages(ctx, roomID, *limit)
		if err != nil {
			fatal(err)
		}
		emit(out)
	default:
		usage()
		os.Exit(2)
	}
}

func runMsg(ctx context.Context, c *client.Client, args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "send":
		fs := flag.NewFlagSet("msg send", flag.ExitOnError)
		room := fs.String("room", "", "room id (required)")
		text := fs.String("text", "", "message text (required)")
		source := fs.String("source", "", "sender id (default \"foxproxctl\")")
		correlationID := fs.String("correlation-id", "", "correlation id for this message")
		replyToMessageID := fs.String("reply-to-message", "", "message id this message replies to")
		submitKey := fs.String("submit-key", "", "override submit key for this message")
		noReceiptPreamble := fs.Bool("no-receipt-preamble", false, "do not prepend the Foxprox receipt preamble to terminal delivery")
		awaitActivity := fs.Duration("await-activity", 0, "wait up to this duration for first recipient output after delivery")
		awaitReady := fs.Duration("await-ready", 0, "wait up to this duration for recipient readiness after first output")
		terminalPolicy := fs.String("terminal-policy", "", "terminal delivery policy: immediate, queue, safe-prompt-only, reject, interrupt")
		policyTimeout := fs.Duration("policy-timeout", 0, "max wait for terminal-policy=queue")
		interruptKey := fs.String("interrupt-key", "", "key sent before terminal-policy=interrupt delivery (default Escape)")
		_ = fs.Parse(args[1:])
		if *room == "" || *text == "" {
			fatal(errors.New("--room and --text are required"))
		}
		if *source == "" {
			*source = "foxproxctl"
		}
		req := httpjson.SendMessageRequest{
			RoomID:           *room,
			Text:             *text,
			Source:           *source,
			CorrelationID:    *correlationID,
			ReplyToMessageID: *replyToMessageID,
			SubmitKey:        *submitKey,
			AwaitActivityMS:  int64((*awaitActivity) / time.Millisecond),
			AwaitReadyMS:     int64((*awaitReady) / time.Millisecond),
			TerminalPolicy:   *terminalPolicy,
			PolicyTimeoutMS:  int64((*policyTimeout) / time.Millisecond),
			InterruptKey:     *interruptKey,
		}
		if *noReceiptPreamble {
			visible := false
			req.ReceiptVisible = &visible
		}
		out, err := c.SendMessage(ctx, req)
		if err != nil {
			fatal(err)
		}
		emit(out)
	default:
		usage()
		os.Exit(2)
	}
}

func emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "foxproxctl: %v\n", err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: foxproxctl [--socket PATH] <command> [args]
commands:
  health
  session create --cmd "bash -i" [--submit-key KEY] [--enable-raw-bytes]
  session list
  session activity SESSION_ID [--since-seq N] [--since-output-bytes-total N]
  session delete SESSION_ID
  room create --workspace ws [--title T]
  room list
  room join ROOM_ID --agent A --session S [--can-mutate]
  room leave ROOM_ID --agent A
  room members ROOM_ID
  room messages ROOM_ID [--limit N]
  msg send --room R --text "..." [--source S] [--submit-key KEY] [--terminal-policy P] [--await-activity D] [--await-ready D]`)
}
