package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/runtime/terminal/agentpane"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newPaneCommand())
}

func newPaneCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pane",
		Short: "Run pane-local transports for agent panes",
	}
	cmd.AddCommand(newPaneServeCommand())
	return cmd
}

func newPaneServeCommand() *cobra.Command {
	var (
		roomID            string
		participantID     string
		socketPath        string
		readyPath         string
		cwd               string
		defaultSubmitMode string
		startupProfile    string
		workspace         string
	)

	cmd := &cobra.Command{
		Use:                "serve --participant <id> [--room-id <id>] [--socket-path <path>] -- <command> [args...]",
		Short:              "Own a child PTY and accept room messages over a unix socket",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("child command is required after --")
			}
			participantID = strings.TrimSpace(firstNonEmpty(participantID, os.Getenv("AGENTCTL_PARTICIPANT")))
			if participantID == "" {
				return fmt.Errorf("participant id is required")
			}
			roomID = strings.TrimSpace(firstNonEmpty(roomID, os.Getenv("AGENTCTL_ROOM_ID")))
			if strings.TrimSpace(socketPath) == "" {
				socketPath = agentpane.DefaultSocketPath(firstNonEmpty(os.Getenv("AGENTCTL_MUX_SESSION"), roomID), participantID)
			}
			if strings.TrimSpace(readyPath) == "" {
				readyPath = agentpane.DefaultReadyPath(firstNonEmpty(os.Getenv("AGENTCTL_MUX_SESSION"), roomID), participantID)
			}
			if strings.TrimSpace(cwd) == "" {
				if wd, err := os.Getwd(); err == nil {
					cwd = wd
				}
			}

			// Auto-register participant transport when room metadata is present.
			// This makes pane serve a first-class transport registration path:
			// the participant's transport_endpoint and transport_kind are persisted
			// to the room member row so relay/status surfaces can see them immediately.
			if roomID != "" && participantID != "" && strings.TrimSpace(socketPath) != "" {
				_ = registerParticipantTransport(cmd, workspace, roomID, participantID, socketPath)
			}

			err := agentpane.Serve(cmd.Context(), agentpane.ServeOptions{
				SocketPath:        socketPath,
				ReadyPath:         readyPath,
				ParticipantID:     participantID,
				RoomID:            roomID,
				Command:           append([]string(nil), args...),
				CWD:               cwd,
				Env:               paneChildEnv(participantID, roomID),
				DefaultSubmitMode: defaultSubmitMode,
				StartupProfile:    startupProfile,
				Stdin:             cmd.InOrStdin(),
				Stdout:            cmd.OutOrStdout(),
				Stderr:            cmd.ErrOrStderr(),
			})
			if errors.Is(err, cmd.Context().Err()) {
				return nil
			}
			return err
		},
	}

	cmd.Flags().StringVar(&participantID, "participant", "", "Participant id for this pane wrapper")
	cmd.Flags().StringVar(&roomID, "room-id", "", "Room id associated with this pane wrapper")
	cmd.Flags().StringVar(&socketPath, "socket-path", "", "Unix socket path for control messages (defaults to a room/participant-derived tmp path)")
	cmd.Flags().StringVar(&readyPath, "ready-path", "", "Readiness marker path written after the child PTY emits output")
	cmd.Flags().StringVar(&cwd, "cwd", "", "Working directory for the child command (defaults to current directory)")
	cmd.Flags().StringVar(&defaultSubmitMode, "default-submit-mode", agentpane.SubmitModeNewline, "Default submit mode for inbound control messages (newline|enter|raw)")
	cmd.Flags().StringVar(&startupProfile, "startup-profile", "", "Optional pane startup profile applied after the child PTY becomes ready")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override (for room member transport registration)")
	return cmd
}

func paneChildEnv(participantID, roomID string) []string {
	env := []string{
		"AGENTCTL_PARTICIPANT_ID=" + strings.TrimSpace(participantID),
		"AGENTCTL_PARTICIPANT=" + strings.TrimSpace(participantID),
	}
	if strings.TrimSpace(roomID) != "" {
		env = append(env, "AGENTCTL_ROOM_ID="+strings.TrimSpace(roomID))
	}
	return env
}

// registerParticipantTransport persists the pane socket transport endpoint on
// the room member row. Errors are logged to stderr but do not block pane serve
// startup -- transport registration is best-effort to avoid blocking the child PTY.
func registerParticipantTransport(cmd *cobra.Command, workspace, roomID, participantID, socketPath string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "pane transport registration: resolve workspace: %v\n", err)
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "pane transport registration: open store: %v\n", err)
		return err
	}
	defer store.Close()

	roomID = strings.TrimSpace(roomID)
	summary, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, "")
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "pane transport registration: get room %s: %v\n", roomID, err)
		return err
	}

	// Existing members should take the surgical transport update path so burst
	// pane startups do not clobber peer transport fields with a full member-list
	// replace.
	if err := store.UpdateRoomMemberTransport(cmd.Context(), absWorkspace, roomID, participantID, socketPath, agent.PaneSocketTransportKind); err == nil {
		return nil
	} else if !errors.Is(err, blackboard.ErrRoomMemberNotFound) {
		fmt.Fprintf(cmd.ErrOrStderr(), "pane transport registration: update member transport: %v\n", err)
		return err
	}

	// Missing members still need a create path so detached pane serve can act as
	// a first-class registration flow on its own.
	member := agent.RoomMember{
		ActorID:           participantID,
		TransportEndpoint: socketPath,
		TransportKind:     agent.PaneSocketTransportKind,
	}

	updatedMembers := mergeRoomMembers(summary.Members, member)
	if _, err := store.ReplaceRoomMembers(cmd.Context(), absWorkspace, roomID, updatedMembers); err != nil {
		if !errors.Is(err, blackboard.ErrRoomNotFound) {
			fmt.Fprintf(cmd.ErrOrStderr(), "pane transport registration: update member: %v\n", err)
		}
		return err
	}
	return nil
}
