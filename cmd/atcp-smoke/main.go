// atcp-smoke spawns an agent CLI under the ATCP broker, injects a prompt as a
// TerminalSubmit intent, and prints the streamed PTY output.
//
// Usage:
//
//	atcp-smoke --agent droid   --prompt "say hi in one sentence"
//	atcp-smoke --agent codex   --prompt "explain bubble sort in 1 line"
//	atcp-smoke --agent gemini  --prompt "what is 2+2"
//	atcp-smoke --agent claude  --prompt "greet me"
//	atcp-smoke --cmd "bash"    --prompt "echo hello world"
//
// The tool is deliberately a thin wrapper around the broker so it exercises
// the same injection path used by the HTTP transport and (later) rooms.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker"
	"github.com/joshka0/foxctl/internal/atcp/broker/safeprompt"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
	"github.com/joshka0/foxctl/internal/atcp/intents"
)

// agentProfiles maps well-known agent names to a launch command. Each profile
// runs the agent in whatever interactive default the binary ships with; in a
// real deployment the caller would pass explicit args via --agent-arg.
var agentProfiles = map[string][]string{
	"droid":  {"droid"},
	"codex":  {"codex"},
	"gemini": {"gemini"},
	"claude": {"claude"},
	"bash":   {"bash", "--noprofile", "--norc", "-i"},
	"sh":     {"sh"},
}

func main() {
	var (
		agent        = flag.String("agent", "", "agent profile: droid|codex|gemini|claude|bash|sh")
		rawCmd       = flag.String("cmd", "", "override command (e.g. \"bash -i\")")
		prompt       = flag.String("prompt", "", "text to submit to the agent")
		wait         = flag.Duration("wait", 8*time.Second, "how long to wait for output after submitting")
		spawn        = flag.Duration("spawn-delay", 1500*time.Millisecond, "fallback boot delay when --wait-ready is off")
		rows         = flag.Uint("rows", 40, "PTY rows")
		cols         = flag.Uint("cols", 140, "PTY cols")
		waitReady    = flag.Bool("wait-ready", true, "wait for the safe-prompt gate (idle + regex) before submitting")
		readyTimeout = flag.Duration("ready-timeout", 30*time.Second, "max time to wait for safe-prompt ready")
		idleWindow   = flag.Duration("idle-window", 600*time.Millisecond, "how long the PTY must be quiet before ready")
		promptRegex  = flag.String("prompt-regex", "", "override the prompt regex (empty = built-in default)")
		logFile      = flag.String("log-file", "", "also write all raw PTY output to this file (useful for debugging prompt regexes)")
	)
	flag.Parse()

	if *prompt == "" {
		fatalf("--prompt is required")
	}
	cmd, err := resolveCommand(*agent, *rawCmd)
	if err != nil {
		fatalf("resolve command: %v", err)
	}
	if _, err := exec.LookPath(cmd[0]); err != nil {
		fatalf("executable not found on PATH: %s (%v)", cmd[0], err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	b := broker.New(broker.Options{})
	defer b.Stop()

	var logSink *os.File
	if *logFile != "" {
		f, err := os.Create(*logFile)
		if err != nil {
			fatalf("create --log-file: %v", err)
		}
		defer f.Close()
		logSink = f
	}

	fmt.Fprintf(os.Stderr, "atcp-smoke: spawning %v\n", cmd)
	snap, err := b.CreateSession(session.Spec{
		Cmd:  cmd,
		Cwd:  ".",
		Rows: uint16(*rows),
		Cols: uint16(*cols),
	}, session.OutputLogOptions{MaxChunks: 4096, MaxBytes: 4 * 1024 * 1024})
	if err != nil {
		fatalf("CreateSession: %v", err)
	}
	sess, err := b.Sessions().Get(snap.ID)
	if err != nil {
		fatalf("Get session: %v", err)
	}

	fmt.Fprintf(os.Stderr, "atcp-smoke: session %s pid=%d\n", snap.ID, snap.PID)
	if *waitReady {
		safeOpts := safeprompt.Options{IdleWindow: *idleWindow}
		if *promptRegex != "" {
			re, err := regexp.Compile(*promptRegex)
			if err != nil {
				fatalf("bad --prompt-regex: %v", err)
			}
			safeOpts.Regex = re
		}
		readyCtx, cancelReady := context.WithTimeout(ctx, *readyTimeout)
		fmt.Fprintf(os.Stderr, "atcp-smoke: waiting for safe prompt (idle=%v, timeout=%v)\n", *idleWindow, *readyTimeout)
		d, reason, err := safeprompt.Wait(readyCtx, sess, safeOpts, 75*time.Millisecond)
		cancelReady()
		if err != nil {
			fmt.Fprintf(os.Stderr, "atcp-smoke: WARN not ready (%s: %s); injecting anyway\n", d, reason)
		} else {
			fmt.Fprintf(os.Stderr, "atcp-smoke: ready (%s)\n", reason)
		}
	} else {
		select {
		case <-time.After(*spawn):
		case <-ctx.Done():
			return
		case <-sess.Done():
			drain(sess)
			fatalf("session exited before submit; output above")
		}
	}

	startSeq := sess.Log().NextSeq() - 1

	fmt.Fprintf(os.Stderr, "atcp-smoke: submitting prompt: %q\n", *prompt)
	if _, err := b.Submit(snap.ID, intents.TerminalSubmit{Text: *prompt}); err != nil {
		fatalf("submit: %v", err)
	}

	fmt.Fprintf(os.Stderr, "atcp-smoke: streaming output for %v (Ctrl-C to stop early)\n", *wait)
	stream(ctx, sess, startSeq, *wait)

	fmt.Fprintf(os.Stderr, "\natcp-smoke: stopping session\n")
	if logSink != nil {
		for _, c := range sess.Log().Since(0, 0) {
			_, _ = logSink.Write(c.Bytes)
		}
	}
	b.DeleteSession(snap.ID)
	<-sess.Done()
}

func resolveCommand(agent, raw string) ([]string, error) {
	if raw != "" {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			return nil, fmt.Errorf("--cmd is empty")
		}
		return fields, nil
	}
	if agent == "" {
		return nil, fmt.Errorf("--agent or --cmd is required")
	}
	cmd, ok := agentProfiles[agent]
	if !ok {
		return nil, fmt.Errorf("unknown --agent %q (known: %s)", agent, strings.Join(profileNames(), ", "))
	}
	return append([]string(nil), cmd...), nil
}

func profileNames() []string {
	out := make([]string, 0, len(agentProfiles))
	for k := range agentProfiles {
		out = append(out, k)
	}
	return out
}

// stream copies live output from the session log to stdout for the given
// duration or until ctx is cancelled or the session exits.
func stream(ctx context.Context, sess *session.Session, fromSeq uint64, d time.Duration) {
	ch, _, cancel := sess.Log().Subscribe(ctx, fromSeq)
	defer cancel()

	deadline := time.After(d)
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-sess.Done():
			// Flush any remaining chunks buffered in ch.
			drainChannel(ch)
			return
		case c, ok := <-ch:
			if !ok {
				return
			}
			_, _ = os.Stdout.Write(c.Bytes)
		}
	}
}

func drainChannel(ch <-chan session.Chunk) {
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				return
			}
			_, _ = os.Stdout.Write(c.Bytes)
		default:
			return
		}
	}
}

// drain writes every chunk currently in the log to stderr. Used on early
// session exit so the user can see why the child died.
func drain(sess *session.Session) {
	var buf bytes.Buffer
	for _, c := range sess.Log().Since(0, 0) {
		buf.Write(c.Bytes)
	}
	_, _ = os.Stderr.Write(buf.Bytes())
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "atcp-smoke: "+format+"\n", args...)
	os.Exit(1)
}
