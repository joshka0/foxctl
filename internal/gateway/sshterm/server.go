package sshterm

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/ssh"
)

// WhoIsFunc looks up the Tailscale identity for a remote address.
// Returns identity info or an error if the address is not on the tailnet.
type WhoIsFunc func(ctx context.Context, addr string) (*IdentityInfo, error)

// Server is the SSH terminal server.
type Server struct {
	config        SSHServerConfig
	rooms         *RoomManager
	log           zerolog.Logger
	sshConfig     *ssh.ServerConfig
	whoIs         WhoIsFunc
	listenerMu    sync.Mutex
	listener      net.Listener
	running       atomic.Bool
	closeOnce     sync.Once
	done          chan struct{}
	nextSessionID atomic.Uint64
}

// NewServer creates a new SSH terminal server.
func NewServer(config SSHServerConfig, rooms *RoomManager, whoIs WhoIsFunc, log zerolog.Logger) *Server {
	if config.DefaultTerm == "" {
		config.DefaultTerm = "xterm-256color"
	}
	if config.DefaultShell == "" {
		config.DefaultShell = "/bin/sh"
	}

	sshConfig := &ssh.ServerConfig{
		NoClientAuth: true, // Authentication is via Tailscale WhoIs
	}

	// Generate or use provided host key
	if len(config.HostKey) > 0 {
		signer, err := ssh.ParsePrivateKey(config.HostKey)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to parse host key, generating ephemeral key")
			generateAndAddHostKey(sshConfig, log)
		} else {
			sshConfig.AddHostKey(signer)
		}
	} else {
		generateAndAddHostKey(sshConfig, log)
	}

	return &Server{
		config:    config,
		rooms:     rooms,
		log:       log.With().Str("component", "sshterm").Logger(),
		sshConfig: sshConfig,
		whoIs:     whoIs,
		done:      make(chan struct{}),
	}
}

// generateAndAddHostKey generates an ephemeral ED25519 host key.
func generateAndAddHostKey(cfg *ssh.ServerConfig, log zerolog.Logger) {
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Error().Err(err).Msg("Failed to generate host key")
		return
	}
	signer, err := ssh.NewSignerFromSigner(privKey)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create signer from generated key")
		return
	}
	cfg.AddHostKey(signer)
}

// Serve accepts SSH connections on the given listener.
// It blocks until the listener is closed or the context is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.listenerMu.Lock()
	s.listener = ln
	s.listenerMu.Unlock()
	s.running.Store(true)

	defer func() {
		s.running.Store(false)
		close(s.done)
	}()

	s.log.Info().Str("addr", ln.Addr().String()).Msg("SSH server listening")

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Check if we're shutting down
			if !s.running.Load() {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.log.Error().Err(err).Msg("Accept failed")
			continue
		}

		go s.handleConn(ctx, conn)
	}
}

// handleConn handles a single SSH connection.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()

	// WhoIs identity verification
	identity, err := s.whoIs(ctx, remoteAddr)
	if err != nil {
		s.log.Warn().
			Err(err).
			Str("remote", remoteAddr).
			Msg("WhoIs lookup failed, rejecting connection")
		conn.Close()
		return
	}

	// Log the identity
	s.log.Info().
		Str("user", identity.UserLogin).
		Str("node", identity.NodeName).
		Str("remote", remoteAddr).
		Msg("SSH connection authenticated via Tailscale WhoIs")

	// Perform SSH handshake
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, s.sshConfig)
	if err != nil {
		s.log.Error().
			Err(err).
			Str("remote", remoteAddr).
			Str("user", identity.UserLogin).
			Msg("SSH handshake failed")
		return
	}
	defer sshConn.Close()

	// Parse room ID from SSH username
	roomID := ParseRoomIDFromUser(sshConn.User())
	if roomID == "" {
		s.log.Warn().
			Str("user", sshConn.User()).
			Str("remote", remoteAddr).
			Str("identity", identity.UserLogin).
			Msg("Invalid SSH username format (expected room-<id>)")
		return
	}

	// Check if room exists
	if !s.rooms.HasRoom(roomID) {
		s.log.Warn().
			Str("room", roomID).
			Str("remote", remoteAddr).
			Msg("Room not found for SSH connection")
		return
	}

	s.log.Info().
		Str("room", roomID).
		Str("user", identity.UserLogin).
		Str("node", identity.NodeName).
		Msg("SSH session routing to room")

	// Create session tracking
	sessionID := fmt.Sprintf("ssh-%d", s.nextSessionID.Add(1))
	sess := &SSHSession{
		ID:         sessionID,
		RoomID:     roomID,
		RemoteAddr: remoteAddr,
		Identity:   *identity,
		StartedAt:  time.Now(),
	}

	// Add session to room
	if err := s.rooms.AddSession(roomID, sess); err != nil {
		s.log.Error().
			Err(err).
			Str("room", roomID).
			Msg("Failed to add SSH session to room")
		return
	}
	defer s.rooms.RemoveSession(sessionID)

	// Discard global requests
	go ssh.DiscardRequests(reqs)

	// Handle channels
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			s.log.Error().Err(err).Msg("Failed to accept channel")
			continue
		}

		go s.handleSession(ctx, sess, channel, requests)
	}
}

// handleSession handles a single SSH session channel.
//
//nolint:gocyclo // SSH session handling is an imperative boundary that multiplexes PTY, shell, exec, resize, and teardown paths.
func (s *Server) handleSession(ctx context.Context, sess *SSHSession, channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()

	var (
		ptyRequested bool
		term         string
		cols, rows   uint16
		cmd          *exec.Cmd
		ptmx         *os.File
		cmdDone      chan struct{}
		closePty     sync.Once
		shellStarted bool
	)

	// Clean up PTY on exit
	defer func() {
		closePty.Do(func() {
			if ptmx != nil {
				// Closing the PTY master causes tmux to detach cleanly.
				// The tmux session survives because tmux attach/new -A only
				// detaches the client, it doesn't kill the session.
				_ = ptmx.Close()
			}
			// Wait for the tmux process to exit (it should detach quickly)
			if cmdDone != nil {
				select {
				case <-cmdDone:
				case <-time.After(5 * time.Second):
					// Force kill if it hasn't exited
					if cmd != nil && cmd.Process != nil {
						_ = cmd.Process.Kill()
						<-cmdDone
					}
				}
			}
		})
	}()

	// Process SSH requests
	for req := range requests {
		switch req.Type {
		case "pty-req":
			if ptyRequested {
				_ = req.Reply(false, nil)
				continue
			}
			ptyRequested = true

			// Parse PTY request
			term, cols, rows = parsePtyRequest(req.Payload)
			if cols == 0 {
				cols = 80
			}
			if rows == 0 {
				rows = 24
			}
			if term == "" {
				term = s.config.DefaultTerm
			}

			sess.SetTerminal(term, cols, rows)

			s.log.Debug().
				Str("term", term).
				Uint16("cols", cols).
				Uint16("rows", rows).
				Str("session", sess.ID).
				Msg("PTY requested")

			_ = req.Reply(true, nil)

		case "window-change":
			if !ptyRequested {
				_ = req.Reply(false, nil)
				continue
			}

			newCols, newRows := parseWindowChange(req.Payload)
			if newCols > 0 && newRows > 0 {
				cols = newCols
				rows = newRows
				sess.SetTerminal(term, cols, rows)

				if ptmx != nil {
					_ = pty.Setsize(ptmx, &pty.Winsize{
						Cols: cols,
						Rows: rows,
					})
				}
			}

			_ = req.Reply(true, nil)

		case "shell":
			if shellStarted {
				_ = req.Reply(false, nil)
				continue
			}
			if !ptyRequested {
				_ = req.Reply(false, nil)
				continue
			}

			_ = req.Reply(true, nil)
			shellStarted = true

			// Get tmux session name
			tmuxSession, err := s.rooms.TmuxSessionForRoom(ctx, sess.RoomID)
			if err != nil {
				s.log.Error().Err(err).Str("room", sess.RoomID).Msg("Failed to get tmux session")
				_, _ = fmt.Fprintf(channel, "Error: %s\r\n", err)
				return
			}

			// Start tmux attach in a PTY.
			// Use exec.Command (not CommandContext) so that context cancellation
			// doesn't kill the tmux process. We want tmux to detach cleanly,
			// not be killed, so the session survives for reconnection.
			tmuxBin := s.config.TmuxPath
			if tmuxBin == "" {
				tmuxBin = "tmux"
			}

			cmd = exec.Command(tmuxBin, "new", "-A", "-s", tmuxSession)
			cmd.Env = append(os.Environ(),
				fmt.Sprintf("TERM=%s", term),
			)

			ptmx, err = pty.StartWithSize(cmd, &pty.Winsize{
				Cols: cols,
				Rows: rows,
			})
			if err != nil {
				s.log.Error().Err(err).
					Str("session", tmuxSession).
					Msg("Failed to start tmux in PTY")
				_, _ = fmt.Fprintf(channel, "Error starting terminal: %s\r\n", err)
				return
			}

			cmdDone = make(chan struct{})
			go func() {
				defer close(cmdDone)
				cmd.Wait()
			}()

			// Pipe PTY output to SSH channel
			go func() {
				buf := make([]byte, 4096)
				for {
					n, readErr := ptmx.Read(buf)
					if n > 0 {
						if _, writeErr := channel.Write(buf[:n]); writeErr != nil {
							return
						}
					}
					if readErr != nil {
						return
					}
				}
			}()

			// Pipe SSH channel input to PTY
			go func() {
				buf := make([]byte, 4096)
				for {
					n, readErr := channel.Read(buf)
					if n > 0 {
						if _, writeErr := ptmx.Write(buf[:n]); writeErr != nil {
							return
						}
					}
					if readErr != nil {
						return
					}
				}
			}()

			// Don't block: continue processing requests (window-change, signal)
			// The request loop will keep running and handle resize/signal requests.

		case "exec":
			// Handle exec requests — reply with failure for now
			_ = req.Reply(false, nil)

		case "signal":
			// Forward signals to the PTY process
			sig := parseSignal(req.Payload)
			if sig != 0 && cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
			// Don't reply to signal requests (per RFC 4254)

		default:
			// Reply to other requests as needed
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}

	// When the request channel closes (client disconnected), the defer will clean up.
	// The cmdDone channel ensures we wait for the tmux process to detach.
	if cmdDone != nil {
		select {
		case <-cmdDone:
		case <-time.After(5 * time.Second):
		}
	}
}

// Close shuts down the SSH server.
func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.running.Store(false)
		s.listenerMu.Lock()
		ln := s.listener
		s.listenerMu.Unlock()
		if ln != nil {
			err = ln.Close()
		}
		if s.rooms != nil {
			s.rooms.Close()
		}
	})
	return err
}

// IsRunning returns whether the server is running.
func (s *Server) IsRunning() bool {
	return s.running.Load()
}

// Done returns a channel that is closed when the server stops.
func (s *Server) Done() <-chan struct{} {
	return s.done
}

// parsePtyRequest parses a pty-req payload.
// Returns term, cols, rows.
// Format per RFC 4254: string (TERM), uint32 (cols), uint32 (rows), uint32 (px), uint32 (py), string (modes)
func parsePtyRequest(payload []byte) (string, uint16, uint16) {
	if len(payload) == 0 {
		return "", 0, 0
	}

	term, rest, ok := parseString(payload)
	if !ok {
		return "", 0, 0
	}

	if len(rest) < 16 {
		return term, 0, 0
	}

	cols := parseUint32(rest[0:4])
	rows := parseUint32(rest[4:8])
	// px and py at rest[8:12] and rest[12:16] — ignored

	return term, uint16(cols), uint16(rows)
}

// parseWindowChange parses a window-change payload.
// Format per RFC 4254: uint32 (cols), uint32 (rows), uint32 (px), uint32 (py)
func parseWindowChange(payload []byte) (uint16, uint16) {
	if len(payload) < 8 {
		return 0, 0
	}
	cols := parseUint32(payload[0:4])
	rows := parseUint32(payload[4:8])
	return uint16(cols), uint16(rows)
}

// parseSignal parses a signal request payload.
// Format per RFC 4254: string (signal name without "SIG" prefix)
func parseSignal(payload []byte) syscall.Signal {
	sigName, _, ok := parseString(payload)
	if !ok {
		return 0
	}

	// Map SSH signal names to syscall signals
	switch sigName {
	case "INT":
		return syscall.SIGINT
	case "TSTP":
		return syscall.SIGTSTP
	case "TERM":
		return syscall.SIGTERM
	case "QUIT":
		return syscall.SIGQUIT
	case "KILL":
		return syscall.SIGKILL
	case "USR1":
		return syscall.SIGUSR1
	case "USR2":
		return syscall.SIGUSR2
	case "SEGV":
		return syscall.SIGSEGV
	case "ABRT":
		return syscall.SIGABRT
	case "HUP":
		return syscall.SIGHUP
	default:
		return 0
	}
}

// parseString parses an SSH-encoded string (uint32 length + bytes).
func parseString(data []byte) (string, []byte, bool) {
	if len(data) < 4 {
		return "", nil, false
	}
	length := parseUint32(data[0:4])
	if len(data) < 4+int(length) {
		return "", nil, false
	}
	return string(data[4 : 4+length]), data[4+length:], true
}

// parseUint32 parses a big-endian uint32.
func parseUint32(data []byte) uint32 {
	if len(data) < 4 {
		return 0
	}
	return uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
}
