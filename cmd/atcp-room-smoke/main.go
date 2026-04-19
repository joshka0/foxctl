// atcp-room-smoke is a tiny binary that proves the room + router vertical:
// it starts N broker-owned PTYs (default 2), joins them all into one room,
// fans out a message via SendMessage, and prints each session's tail so the
// operator can visually confirm every member received it.
//
// The binary is deliberately minimal — no flags for log files, custom
// prompts, or fancy shells — because this is a proof of plumbing, not a
// replacement for the existing single-session atcp-smoke.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker"
	"github.com/joshka0/foxctl/internal/atcp/broker/room"
	"github.com/joshka0/foxctl/internal/atcp/broker/router"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
)

func main() {
	var (
		members  = flag.Int("members", 2, "number of broker-owned sessions to join the room")
		shellCmd = flag.String("shell", "cat", "command each member runs; cat echoes input so the fan-out is visually obvious")
		text     = flag.String("text", "ROOM_HELLO", "message to broadcast after all members joined")
		settle   = flag.Duration("settle", 300*time.Millisecond, "how long to wait for PTY output after SendMessage")
	)
	flag.Parse()
	if *members < 1 {
		fatalf("--members must be >= 1")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	b := broker.New(broker.Options{})
	defer b.Stop()

	// Spawn sessions.
	type memberPair struct {
		agentID   string
		sessionID string
	}
	pairs := make([]memberPair, 0, *members)
	for i := 0; i < *members; i++ {
		snap, err := b.CreateSession(session.Spec{Cmd: []string{*shellCmd}}, session.OutputLogOptions{
			MaxChunks: 1024, MaxBytes: 256 * 1024,
		})
		if err != nil {
			fatalf("CreateSession %d: %v", i, err)
		}
		agentID := fmt.Sprintf("agent-%d", i)
		pairs = append(pairs, memberPair{agentID: agentID, sessionID: snap.ID})
		fmt.Fprintf(os.Stderr, "spawned session %s (pid=%d) as %s\n", snap.ID, snap.PID, agentID)
	}

	// Create a room and bind every session.
	r, err := b.CreateRoom(room.CreateRoomRequest{Workspace: "smoke", Title: "atcp-room-smoke"})
	if err != nil {
		fatalf("CreateRoom: %v", err)
	}
	fmt.Fprintf(os.Stderr, "room %s created\n", r.ID)
	for _, p := range pairs {
		if _, err := b.JoinRoom(room.JoinRequest{
			RoomID:    r.ID,
			AgentID:   p.agentID,
			SessionID: p.sessionID,
			CanMutate: true,
		}); err != nil {
			fatalf("JoinRoom %s: %v", p.agentID, err)
		}
	}

	// Small settle so `cat` is definitely reading stdin before we submit.
	select {
	case <-time.After(200 * time.Millisecond):
	case <-ctx.Done():
		return
	}

	// Fan out.
	res, err := b.SendMessage(router.Message{
		RoomID: r.ID,
		Source: "atcp-room-smoke",
		Text:   *text,
	})
	if err != nil {
		fatalf("SendMessage: %v", err)
	}
	fmt.Fprintf(os.Stderr, "SendMessage: delivered=%d failed=%d message_id=%s\n", res.Delivered, res.Failed, res.MessageID)
	for _, mr := range res.Members {
		if mr.Delivered {
			fmt.Fprintf(os.Stderr, "  OK   %s (session=%s)\n", mr.AgentID, mr.SessionID)
		} else {
			fmt.Fprintf(os.Stderr, "  FAIL %s (session=%s): %v\n", mr.AgentID, mr.SessionID, mr.Err)
		}
	}

	// Wait for PTY echo then dump each session's tail.
	select {
	case <-time.After(*settle):
	case <-ctx.Done():
		return
	}

	exit := 0
	for _, p := range pairs {
		sess, err := b.Sessions().Get(p.sessionID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] missing session: %v\n", p.agentID, err)
			exit = 1
			continue
		}
		var buf strings.Builder
		for _, c := range sess.Log().Since(0, 0) {
			buf.Write(c.Bytes)
		}
		got := buf.String()
		ok := strings.Contains(got, *text)
		marker := "OK"
		if !ok {
			marker = "MISS"
			exit = 2
		}
		fmt.Fprintf(os.Stderr, "[%s] %s tail=%q\n", p.agentID, marker, tail(got, 80))
	}

	// Clean teardown.
	for _, p := range pairs {
		_ = b.DeleteSession(p.sessionID)
	}
	os.Exit(exit)
}

func tail(s string, n int) string {
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "atcp-room-smoke: "+format+"\n", args...)
	os.Exit(1)
}
