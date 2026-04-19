// atcpctl is a minimal CLI for talking to an ATCP daemon.
//
// Surface is intentionally narrow while the wire protocol stabilises —
// enough to demonstrate every intent from the shell, not a full replacement
// for the legacy foxctl CLI. When the legacy room/blackboard code is
// finally hard-cut, these commands can fold into `foxctl atcp ...` without
// contract changes.
//
// Usage:
//
//	atcpctl [--socket PATH] <command> [args]
//	  health
//	  session create --cmd "bash -i"
//	  session list
//	  session delete SESSION_ID
//	  room create --workspace ws [--title T]
//	  room list
//	  room join ROOM_ID --agent A --session S [--can-mutate]
//	  room leave ROOM_ID --agent A
//	  room members ROOM_ID
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

	"github.com/joshka0/foxctl/internal/atcp/client"
	"github.com/joshka0/foxctl/internal/atcp/daemon"
	"github.com/joshka0/foxctl/internal/atcp/transport/httpjson"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	// Allow a global --socket flag before the subcommand for operator
	// convenience: atcpctl --socket /tmp/s health.
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
		_ = fs.Parse(args[1:])
		if *cmd == "" {
			fatal(errors.New("--cmd is required"))
		}
		parts := strings.Fields(*cmd)
		out, err := c.CreateSession(ctx, httpjson.CreateSessionRequest{Cmd: parts})
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
		source := fs.String("source", "", "sender id (default \"atcpctl\")")
		_ = fs.Parse(args[1:])
		if *room == "" || *text == "" {
			fatal(errors.New("--room and --text are required"))
		}
		if *source == "" {
			*source = "atcpctl"
		}
		out, err := c.SendMessage(ctx, httpjson.SendMessageRequest{
			RoomID: *room, Text: *text, Source: *source,
		})
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
	fmt.Fprintf(os.Stderr, "atcpctl: %v\n", err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: atcpctl [--socket PATH] <command> [args]
commands:
  health
  session create --cmd "bash -i"
  session list
  session delete SESSION_ID
  room create --workspace ws [--title T]
  room list
  room join ROOM_ID --agent A --session S [--can-mutate]
  room leave ROOM_ID --agent A
  room members ROOM_ID
  msg send --room R --text "..." [--source S]`)
}
