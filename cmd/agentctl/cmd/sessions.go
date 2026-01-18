package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/jkatigb/agentctl/cmd/agentctl/cmd/sessionscmd"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/spf13/cobra"
)

func newSessionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage captured Claude Code conversation sessions",
		Long: `Manage captured Claude Code conversation sessions.

Sessions are captured from Claude Code's JSONL conversation files and stored
in agentctl's sessions.db. They can be summarized, searched, and used for
progressive memory retrieval.

Use the Stop hook (session-capture.sh) for automatic capture, or capture
manually with:
  agentctl run session/capture --input '{"workspace": "/path/to/project"}'`,
	}
	cmd.AddCommand(
		newSessionsListCommand(),
		newSessionsShowCommand(),
		newSessionsSearchCommand(),
		newSessionsStatsCommand(),
		newSessionsDeleteCommand(),
		newSessionsCaptureCommand(),
		newSessionsSummarizeCommand(),
		newSessionsImportCommand(),
		newSessionsWindowsCommand(),
		newSessionsExportCommand(),
		// Lineage commands
		newSessionsNewCommand(),
		newSessionsResumeCommand(),
		newSessionsForkCommand(),
		newSessionsChainCommand(),
		newSessionsCloseCommand(),
	)
	return cmd
}

func newSessionsListCommand() *cobra.Command {
	var workspacePath string
	var projectName string
	var tags []string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List captured sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					opts := storage.SessionListOptions{
						WorkspacePath: workspacePath,
						ProjectName:   projectName,
						Tags:          tags,
						Limit:         limit,
					}
					sessionList, err := store.List(ctx, opts)
					if err != nil {
						return err
					}

					payload := struct {
						Sessions []sessionSummary `json:"sessions"`
						Count    int              `json:"count"`
					}{
						Sessions: summarizeSessions(sessionList, 100),
						Count:    len(sessionList),
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.list", payload)
				})
			})
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Filter by workspace path")
	cmd.Flags().StringVar(&projectName, "project", "", "Filter by project name")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "Filter by tags (matches any)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum sessions to return")
	return cmd
}

func newSessionsShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show details of a captured session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					session, err := store.Get(ctx, sessionID)
					if err != nil {
						if errors.Is(err, sessions.ErrNotFound) {
							return sessionscmd.WriteNotFound(cmd.OutOrStdout(), "agentctl.sessions.show", sessionID)
						}
						return err
					}

					payload := sessionDetail{
						ID:              session.ID,
						WorkspacePath:   session.WorkspacePath,
						ProjectName:     session.ProjectName,
						GitBranch:       session.GitBranch,
						ClaudeVersion:   session.ClaudeVersion,
						AgentType:       session.AgentType,
						StartedAt:       session.StartedAt,
						EndedAt:         session.EndedAt,
						Summary:         session.Summary,
						Accomplished:    session.Accomplished,
						Decisions:       session.Decisions,
						Gotchas:         session.Gotchas,
						Tags:            session.Tags,
						KeyFiles:        session.KeyFiles,
						ToolsPattern:    session.ToolsPattern,
						MessageCount:    session.MessageCount,
						UserTurns:       session.UserTurns,
						ToolInvocations: session.ToolInvocations,
						TotalTokens:     session.TotalTokens,
						RawJSONLPath:    session.RawJSONLPath,
						HasEmbedding:    len(session.Embedding) > 0,
						EmbeddingModel:  session.EmbeddingModel,
						CreatedAt:       session.CreatedAt,
						UpdatedAt:       session.UpdatedAt,
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.show", payload)
				})
			})
		},
	}
	return cmd
}

func newSessionsSearchCommand() *cobra.Command {
	var query string
	var limit int

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search sessions by text",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(query) == "" {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.search",
					"--query is required",
					"Provide a search query to find relevant sessions.")
			}
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					sessionList, err := store.Search(ctx, query, limit)
					if err != nil {
						return err
					}

					payload := struct {
						Sessions []sessionSummary `json:"sessions"`
						Query    string           `json:"query"`
						Count    int              `json:"count"`
					}{
						Sessions: summarizeSessions(sessionList, 100),
						Query:    query,
						Count:    len(sessionList),
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.search", payload)
				})
			})
		},
	}

	cmd.Flags().StringVar(&query, "query", "", "Search text")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum sessions to return")
	return cmd
}

func newSessionsStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show sessions store statistics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					stats, err := store.Stats(ctx)
					if err != nil {
						return err
					}

					payload := struct {
						TotalSessions int64  `json:"total_sessions"`
						DatabasePath  string `json:"database_path"`
					}{
						TotalSessions: stats.Count,
						DatabasePath:  stats.Path,
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.stats", payload)
				})
			})
		},
	}
	return cmd
}

func newSessionsDeleteCommand() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a captured session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					// Check if session exists (for both dry-run and actual delete)
					_, err := store.Get(ctx, sessionID)
					if err != nil {
						if errors.Is(err, sessions.ErrNotFound) {
							return sessionscmd.WriteNotFound(cmd.OutOrStdout(), "agentctl.sessions.delete", sessionID)
						}
						return err
					}

					if dryRun {
						payload := struct {
							SessionID string `json:"session_id"`
							DryRun    bool   `json:"dry_run"`
							Message   string `json:"message"`
						}{
							SessionID: sessionID,
							DryRun:    true,
							Message:   "Would delete session",
						}
						return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.delete", payload)
					}

					if err := store.Delete(ctx, sessionID); err != nil {
						return err
					}

					payload := struct {
						SessionID string `json:"session_id"`
						Deleted   bool   `json:"deleted"`
					}{
						SessionID: sessionID,
						Deleted:   true,
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.delete", payload)
				})
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be deleted without making changes")
	return cmd
}

func newSessionsCaptureCommand() *cobra.Command {
	var workspace string
	var sessionID string
	var force bool

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture a Claude Code session (delegates to session/capture skill)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{}
			if workspace != "" {
				payload["workspace"] = workspace
			}
			if sessionID != "" {
				payload["session_id"] = sessionID
			}
			if force {
				payload["force"] = true
			}
			return runSessionSkill(cmd, "session/capture", payload)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Project workspace path (defaults to cwd)")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Specific session ID to capture")
	cmd.Flags().BoolVar(&force, "force", false, "Re-capture even if session exists")
	return cmd
}

func newSessionsSummarizeCommand() *cobra.Command {
	var sessionID string
	var force bool

	cmd := &cobra.Command{
		Use:   "summarize",
		Short: "Generate summary for a captured session (requires CEREBRAS_API_KEY)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sessionID == "" {
				return fmt.Errorf("--session-id is required")
			}
			payload := map[string]any{
				"session_id": sessionID,
			}
			if force {
				payload["force"] = true
			}
			return runSessionSkill(cmd, "session/summarize", payload)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID to summarize (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Re-summarize even if already summarized")
	if err := cmd.MarkFlagRequired("session-id"); err != nil {
		panic(err)
	}
	return cmd
}

func newSessionsImportCommand() *cobra.Command {
	var dryRun bool
	var summarize bool
	var limit int
	var projectFilter string

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import uncaptured Claude Code sessions",
		Long: `Scan Claude Code's JSONL conversation files and import uncaptured sessions.

This command:
1. Scans ~/.claude/projects/ for JSONL session files
2. Compares with sessions.db to find uncaptured sessions
3. Captures each session (parses JSONL, extracts metadata)
4. Optionally summarizes with LLM and generates embeddings

Requires OPENROUTER_API_KEY or GEMINI_API_KEY for summarization and embeddings.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					// Find Claude projects directory
					claudeProjectsDir := sessionscmd.ClaudeProjectsDir()

					// Get captured session IDs
					captured := make(map[string]bool)
					allSessions, err := store.List(ctx, storage.SessionListOptions{Limit: 10000})
					if err != nil {
						return fmt.Errorf("list captured sessions: %w", err)
					}
					for _, s := range allSessions {
						captured[s.ID] = true
					}

					// Find uncaptured sessions
					uncaptured, err := sessionscmd.FindUncapturedSessions(claudeProjectsDir, captured, projectFilter)
					if err != nil {
						return fmt.Errorf("scan for sessions: %w", err)
					}

					if limit > 0 && len(uncaptured) > limit {
						uncaptured = uncaptured[:limit]
					}

					if dryRun {
						payload := struct {
							Uncaptured []sessionscmd.UncapturedSession `json:"uncaptured"`
							Count      int                             `json:"count"`
							DryRun     bool                            `json:"dry_run"`
						}{
							Uncaptured: uncaptured,
							Count:      len(uncaptured),
							DryRun:     true,
						}
						return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.import", payload)
					}

					// Import each session
					imported := []string{}
					failed := []string{}
					for _, u := range uncaptured {
						fmt.Fprintf(cmd.ErrOrStderr(), "Importing %s from %s...\n", u.SessionID, u.ProjectName)

						// Capture
						capturePayload := map[string]any{
							"workspace":  u.WorkspacePath,
							"session_id": u.SessionID,
						}
						if err := runSessionSkillQuiet(cmd, cfg, "session/capture", capturePayload); err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "  capture failed: %v\n", err)
							failed = append(failed, u.SessionID)
							continue
						}

						// Summarize if requested
						if summarize {
							fmt.Fprintf(cmd.ErrOrStderr(), "  summarizing...\n")
							summarizePayload := map[string]any{
								"session_id": u.SessionID,
							}
							if err := runSessionSkillQuiet(cmd, cfg, "session/summarize", summarizePayload); err != nil {
								fmt.Fprintf(cmd.ErrOrStderr(), "  summarize failed: %v\n", err)
								// Continue anyway, capture succeeded
							}
						}

						imported = append(imported, u.SessionID)
					}

					payload := struct {
						Imported []string `json:"imported"`
						Failed   []string `json:"failed,omitempty"`
						Count    int      `json:"count"`
					}{
						Imported: imported,
						Failed:   failed,
						Count:    len(imported),
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.import", payload)
				})
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be imported without importing")
	cmd.Flags().BoolVar(&summarize, "summarize", true, "Generate LLM summary and embeddings after capture")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum sessions to import (0 = unlimited)")
	cmd.Flags().StringVar(&projectFilter, "project", "", "Only import sessions from this project")
	return cmd
}

// runSessionSkillQuiet runs a session skill without output.
func runSessionSkillQuiet(cmd *cobra.Command, cfg config.Config, skillName string, payload map[string]any) error {
	_, err := findSkill(cfg, skillName)
	if err != nil {
		return fmt.Errorf("%s skill not found: %w", skillName, err)
	}

	runCmd := newRunCommand()
	runCmd.SetContext(cmd.Context())
	runCmd.SetOut(io.Discard) // Suppress output
	runCmd.SetErr(io.Discard)

	inputJSON := "{}"
	if len(payload) > 0 {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		inputJSON = string(b)
	}

	runCmd.SetArgs([]string{"--input", inputJSON, skillName})
	return runCmd.Execute()
}

// runSessionSkill runs a session skill via the run command.
func runSessionSkill(cmd *cobra.Command, skillName string, payload map[string]any) error {
	cfg, ok := config.FromContext(cmd.Context())
	if !ok {
		var err error
		cfg, err = loadConfig(cmd.Context())
		if err != nil {
			return err
		}
	}
	if _, err := findSkill(cfg, skillName); err != nil {
		return fmt.Errorf("%s skill not found (run make skills-build): %w", skillName, err)
	}

	runCmd := newRunCommand()
	runCmd.SetContext(cmd.Context())
	runCmd.SetOut(cmd.OutOrStdout())
	runCmd.SetErr(cmd.ErrOrStderr())

	inputJSON := "{}"
	if len(payload) > 0 {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		inputJSON = string(b)
	}

	runCmd.SetArgs([]string{"--input", inputJSON, skillName})
	return runCmd.Execute()
}

// sessionSummary is a compact representation for list/search output.
type sessionSummary struct {
	ID              string    `json:"id"`
	ProjectName     string    `json:"project_name"`
	GitBranch       string    `json:"git_branch,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	MessageCount    int       `json:"message_count"`
	UserTurns       int       `json:"user_turns"`
	ToolInvocations int       `json:"tool_invocations"`
	TotalTokens     int       `json:"total_tokens"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
}

func summarizeSessions(list []storage.Session, maxSummaryLen int) []sessionSummary {
	out := make([]sessionSummary, 0, len(list))
	for _, s := range list {
		out = append(out, sessionSummary{
			ID:              s.ID,
			ProjectName:     s.ProjectName,
			GitBranch:       s.GitBranch,
			Summary:         truncateSummary(s.Summary, maxSummaryLen),
			MessageCount:    s.MessageCount,
			UserTurns:       s.UserTurns,
			ToolInvocations: s.ToolInvocations,
			TotalTokens:     s.TotalTokens,
			StartedAt:       s.StartedAt,
			Tags:            s.Tags,
		})
	}
	return out
}

// sessionDetail is the full session representation for show output.
type sessionDetail struct {
	ID              string    `json:"id"`
	WorkspacePath   string    `json:"workspace_path"`
	ProjectName     string    `json:"project_name"`
	GitBranch       string    `json:"git_branch,omitempty"`
	ClaudeVersion   string    `json:"claude_version,omitempty"`
	AgentType       string    `json:"agent_type,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	EndedAt         time.Time `json:"ended_at,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	Accomplished    []string  `json:"accomplished,omitempty"`
	Decisions       []string  `json:"decisions,omitempty"`
	Gotchas         []string  `json:"gotchas,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
	KeyFiles        []string  `json:"key_files,omitempty"`
	ToolsPattern    string    `json:"tools_pattern,omitempty"`
	MessageCount    int       `json:"message_count"`
	UserTurns       int       `json:"user_turns"`
	ToolInvocations int       `json:"tool_invocations"`
	TotalTokens     int       `json:"total_tokens"`
	RawJSONLPath    string    `json:"raw_jsonl_path,omitempty"`
	HasEmbedding    bool      `json:"has_embedding"`
	EmbeddingModel  string    `json:"embedding_model,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func truncateSummary(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Lineage commands

func newSessionsNewCommand() *cobra.Command {
	var agentID string
	var parentID string
	var workspace string
	var force bool

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new session",
		Long: `Create a new session with optional parent for lineage tracking.

This command creates a new session entry and optionally links it to a parent
session for tracking continuation/fork relationships.

Only one active session is allowed per workspace/agent combination. Use --force
to close any existing active session before creating a new one.

Examples:
  agentctl sessions new
  agentctl sessions new --agent-id claude-code
  agentctl sessions new --parent 01HXYZ --agent-id my-agent
  agentctl sessions new --force  # close existing active session first`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					// Determine workspace
					ws := workspace
					if ws == "" {
						wd, err := os.Getwd()
						if err != nil {
							return fmt.Errorf("get working directory: %w", err)
						}
						ws = wd
					}

					aid := agentID
					if aid == "" {
						aid = "agentctl"
					}

					// Enforce one-active-session rule
					active, err := store.GetActive(ctx, ws, aid)
					if err != nil {
						return fmt.Errorf("check active session: %w", err)
					}
					if active != nil {
						if force {
							// Force-close the existing session as canceled (interrupted)
							if err := store.SetStatus(ctx, active.ID, storage.SessionStatusCanceled); err != nil {
								return fmt.Errorf("close existing session: %w", err)
							}
							fmt.Fprintf(cmd.ErrOrStderr(), "Canceled existing active session: %s\n", active.ID)
						} else {
							return sessionscmd.WriteConflict(cmd.OutOrStdout(), "agentctl.sessions.new",
								fmt.Sprintf("active session already exists: %s", active.ID),
								"Close it with 'agentctl sessions close' or use --force to close and create new")
						}
					}

					// Create new session
					session := storage.Session{
						ID:            ulid.Make().String(),
						WorkspacePath: ws,
						ProjectName:   filepath.Base(ws),
						StartedAt:     time.Now().UTC(),
						AgentID:       aid,
						Status:        storage.SessionStatusRunning,
					}

					if parentID != "" {
						session.ParentSessionID = parentID
					}

					saved, err := store.Save(ctx, session)
					if err != nil {
						return fmt.Errorf("save session: %w", err)
					}

					// Create edge if parent specified
					if parentID != "" {
						edge := storage.SessionEdge{
							ID:          ulid.Make().String(),
							Workspace:   ws,
							FromSession: parentID,
							ToSession:   saved.ID,
							EdgeType:    storage.SessionEdgeContinues,
							CreatedAt:   time.Now().UTC(),
						}
						if err := store.SaveEdge(ctx, edge); err != nil {
							// Non-fatal, session was created
							fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to save edge: %v\n", err)
						}
						// Ingest into graph for PageRank
						sessionscmd.IngestSessionEdgeToGraph(ctx, cmd.ErrOrStderr(), cfg, edge)
					}

					// Write identity file for session discovery
					sessionscmd.SetActiveIdentity(cmd.ErrOrStderr(), cfg, saved.ID, ws, aid, parentID)

					payload := struct {
						SessionID       string `json:"session_id"`
						AgentID         string `json:"agent_id"`
						ParentSessionID string `json:"parent_session_id,omitempty"`
						Workspace       string `json:"workspace"`
					}{
						SessionID:       saved.ID,
						AgentID:         saved.AgentID,
						ParentSessionID: saved.ParentSessionID,
						Workspace:       saved.WorkspacePath,
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.new", payload)
				})
			})
		},
	}

	cmd.Flags().StringVar(&agentID, "agent-id", "", "Agent identifier (default: agentctl)")
	cmd.Flags().StringVar(&parentID, "parent", "", "Parent session ID for lineage")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: cwd)")
	cmd.Flags().BoolVar(&force, "force", false, "Close existing active session before creating new")
	return cmd
}

func newSessionsResumeCommand() *cobra.Command {
	var agentID string
	var sessionID string
	var workspace string
	var force bool

	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume an existing session or find the last active session",
		Long: `Resume an existing session by ID, or find the most recent session for the workspace.

This command:
1. If --session is provided, resumes that specific session
2. Otherwise, finds the most recent session for the workspace/agent combination
3. Creates a 'continues' edge from the found session to a new session

Only one active session is allowed per workspace/agent combination. Use --force
to close any existing active session before resuming.

Examples:
  agentctl sessions resume
  agentctl sessions resume --session 01HXYZ
  agentctl sessions resume --agent-id claude-code
  agentctl sessions resume --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					// Determine workspace
					ws := workspace
					if ws == "" {
						wd, err := os.Getwd()
						if err != nil {
							return fmt.Errorf("get working directory: %w", err)
						}
						ws = wd
					}

					aid := agentID
					if aid == "" {
						aid = "agentctl"
					}

					// Enforce one-active-session rule
					active, err := store.GetActive(ctx, ws, aid)
					if err != nil {
						return fmt.Errorf("check active session: %w", err)
					}
					if active != nil {
						if force {
							// Force-close the existing session as canceled (interrupted)
							if err := store.SetStatus(ctx, active.ID, storage.SessionStatusCanceled); err != nil {
								return fmt.Errorf("close existing session: %w", err)
							}
							fmt.Fprintf(cmd.ErrOrStderr(), "Canceled existing active session: %s\n", active.ID)
						} else {
							return sessionscmd.WriteConflict(cmd.OutOrStdout(), "agentctl.sessions.resume",
								fmt.Sprintf("active session already exists: %s", active.ID),
								"Close it with 'agentctl sessions close' or use --force to close and resume")
						}
					}

					var parentSession *storage.Session

					if sessionID != "" {
						// Resume specific session
						s, err := store.Get(ctx, sessionID)
						if err != nil {
							if errors.Is(err, sessions.ErrNotFound) {
								return sessionscmd.WriteNotFound(cmd.OutOrStdout(), "agentctl.sessions.resume", sessionID)
							}
							return err
						}
						parentSession = &s
					} else {
						// Find last successfully completed session for workspace/agent.
						// Only 'ok' sessions are resumable - errored/canceled sessions represent
						// incomplete or problematic states that shouldn't be continued from.
						s, err := store.FindLastSession(ctx, ws, aid, []string{storage.SessionStatusOK})
						if err != nil {
							return fmt.Errorf("find last session: %w", err)
						}
						if s == nil {
							payload := struct {
								Message string `json:"message"`
								Hint    string `json:"hint"`
							}{
								Message: "no previous session found",
								Hint:    "Create a new session with 'agentctl sessions new'",
							}
							return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.resume", payload)
						}
						parentSession = s
					}

					// Create continuation session
					session := storage.Session{
						ID:              ulid.Make().String(),
						WorkspacePath:   ws,
						ProjectName:     filepath.Base(ws),
						StartedAt:       time.Now().UTC(),
						AgentID:         aid,
						Status:          storage.SessionStatusRunning,
						ParentSessionID: parentSession.ID,
					}

					saved, err := store.Save(ctx, session)
					if err != nil {
						return fmt.Errorf("save session: %w", err)
					}

					// Create continues edge
					edge := storage.SessionEdge{
						ID:          ulid.Make().String(),
						Workspace:   ws,
						FromSession: parentSession.ID,
						ToSession:   saved.ID,
						EdgeType:    storage.SessionEdgeContinues,
						CreatedAt:   time.Now().UTC(),
					}
					if err := store.SaveEdge(ctx, edge); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to save edge: %v\n", err)
					}
					// Ingest into graph for PageRank
					sessionscmd.IngestSessionEdgeToGraph(ctx, cmd.ErrOrStderr(), cfg, edge)

					// Write identity file for session discovery
					sessionscmd.SetActiveIdentity(cmd.ErrOrStderr(), cfg, saved.ID, ws, aid, parentSession.ID)

					payload := struct {
						SessionID       string `json:"session_id"`
						ParentSessionID string `json:"parent_session_id"`
						AgentID         string `json:"agent_id"`
						Workspace       string `json:"workspace"`
						ResumedFrom     string `json:"resumed_from"`
					}{
						SessionID:       saved.ID,
						ParentSessionID: saved.ParentSessionID,
						AgentID:         saved.AgentID,
						Workspace:       saved.WorkspacePath,
						ResumedFrom:     parentSession.ID,
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.resume", payload)
				})
			})
		},
	}

	cmd.Flags().StringVar(&agentID, "agent-id", "", "Agent identifier (default: agentctl)")
	cmd.Flags().StringVar(&sessionID, "session", "", "Specific session ID to resume")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: cwd)")
	cmd.Flags().BoolVar(&force, "force", false, "Close existing active session before resuming")
	return cmd
}

func newSessionsForkCommand() *cobra.Command {
	var agentID string
	var parentID string
	var workspace string
	var force bool

	cmd := &cobra.Command{
		Use:   "fork",
		Short: "Fork from an existing session",
		Long: `Create a new session forked from an existing one.

Unlike 'resume' which creates a 'continues' relationship, 'fork' creates a
'forked_from' relationship indicating the new session branches from the parent.

Only one active session is allowed per workspace/agent combination. Use --force
to close any existing active session before forking.

Examples:
  agentctl sessions fork --parent 01HXYZ
  agentctl sessions fork --parent 01HXYZ --agent-id new-agent
  agentctl sessions fork --parent 01HXYZ --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if parentID == "" {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.fork",
					"--parent is required",
					"Specify the session to fork from with --parent <session-id>")
			}

			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					// Verify parent exists
					parent, err := store.Get(ctx, parentID)
					if err != nil {
						if errors.Is(err, sessions.ErrNotFound) {
							return sessionscmd.WriteNotFound(cmd.OutOrStdout(), "agentctl.sessions.fork", parentID)
						}
						return err
					}

					// Determine workspace
					ws := workspace
					if ws == "" {
						ws = parent.WorkspacePath
					}

					aid := agentID
					if aid == "" {
						aid = "agentctl"
					}

					// Enforce one-active-session rule
					active, err := store.GetActive(ctx, ws, aid)
					if err != nil {
						return fmt.Errorf("check active session: %w", err)
					}
					if active != nil {
						if force {
							// Force-close the existing session as canceled (interrupted)
							if err := store.SetStatus(ctx, active.ID, storage.SessionStatusCanceled); err != nil {
								return fmt.Errorf("close existing session: %w", err)
							}
							fmt.Fprintf(cmd.ErrOrStderr(), "Canceled existing active session: %s\n", active.ID)
						} else {
							return sessionscmd.WriteConflict(cmd.OutOrStdout(), "agentctl.sessions.fork",
								fmt.Sprintf("active session already exists: %s", active.ID),
								"Close it with 'agentctl sessions close' or use --force to close and fork")
						}
					}

					// Create forked session
					session := storage.Session{
						ID:              ulid.Make().String(),
						WorkspacePath:   ws,
						ProjectName:     filepath.Base(ws),
						StartedAt:       time.Now().UTC(),
						AgentID:         aid,
						Status:          storage.SessionStatusRunning,
						ParentSessionID: parent.ID,
					}

					saved, err := store.Save(ctx, session)
					if err != nil {
						return fmt.Errorf("save session: %w", err)
					}

					// Create forked_from edge
					edge := storage.SessionEdge{
						ID:          ulid.Make().String(),
						Workspace:   ws,
						FromSession: parent.ID,
						ToSession:   saved.ID,
						EdgeType:    storage.SessionEdgeForkedFrom,
						CreatedAt:   time.Now().UTC(),
					}
					if err := store.SaveEdge(ctx, edge); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to save edge: %v\n", err)
					}
					// Ingest into graph for PageRank
					sessionscmd.IngestSessionEdgeToGraph(ctx, cmd.ErrOrStderr(), cfg, edge)

					// Write identity file for session discovery
					sessionscmd.SetActiveIdentity(cmd.ErrOrStderr(), cfg, saved.ID, ws, aid, parent.ID)

					payload := struct {
						SessionID  string `json:"session_id"`
						ForkedFrom string `json:"forked_from"`
						AgentID    string `json:"agent_id"`
						Workspace  string `json:"workspace"`
					}{
						SessionID:  saved.ID,
						ForkedFrom: parent.ID,
						AgentID:    saved.AgentID,
						Workspace:  saved.WorkspacePath,
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.fork", payload)
				})
			})
		},
	}

	cmd.Flags().StringVar(&agentID, "agent-id", "", "Agent identifier (default: agentctl)")
	cmd.Flags().StringVar(&parentID, "parent", "", "Parent session ID to fork from (required)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: inherited from parent)")
	cmd.Flags().BoolVar(&force, "force", false, "Close existing active session before forking")
	return cmd
}

func newSessionsChainCommand() *cobra.Command {
	var sessionID string
	var depth int

	cmd := &cobra.Command{
		Use:   "chain",
		Short: "Show the ancestor chain of a session",
		Long: `Display the lineage of a session by traversing parent_session_id links.

This shows the chain of sessions leading up to the specified session,
useful for understanding how a session evolved from its origins.

Examples:
  agentctl sessions chain --session 01HXYZ
  agentctl sessions chain --session 01HXYZ --depth 10`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sessionID == "" {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.chain",
					"--session is required",
					"Specify the session to trace with --session <session-id>")
			}

			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					chain, err := store.GetAncestorChain(ctx, sessionID, depth)
					if err != nil {
						return fmt.Errorf("get ancestor chain: %w", err)
					}

					type chainEntry struct {
						ID              string    `json:"id"`
						ParentSessionID string    `json:"parent_session_id,omitempty"`
						AgentID         string    `json:"agent_id"`
						Status          string    `json:"status"`
						Summary         string    `json:"summary,omitempty"`
						StartedAt       time.Time `json:"started_at"`
						Depth           int       `json:"depth"`
					}

					entries := make([]chainEntry, len(chain))
					for i, s := range chain {
						entries[i] = chainEntry{
							ID:              s.ID,
							ParentSessionID: s.ParentSessionID,
							AgentID:         s.AgentID,
							Status:          s.Status,
							Summary:         truncateSummary(s.Summary, 80),
							StartedAt:       s.StartedAt,
							Depth:           i,
						}
					}

					payload := struct {
						SessionID string       `json:"session_id"`
						Chain     []chainEntry `json:"chain"`
						Count     int          `json:"count"`
					}{
						SessionID: sessionID,
						Chain:     entries,
						Count:     len(entries),
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.chain", payload)
				})
			})
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID to trace (required)")
	cmd.Flags().IntVar(&depth, "depth", 5, "Maximum depth to traverse")
	return cmd
}

func newSessionsCloseCommand() *cobra.Command {
	var sessionID string
	var status string
	var workspace string
	var agentID string

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Close a session with a status",
		Long: `Close a session by setting its status.

If no session ID is provided, closes the most recent active session for the
workspace/agent combination.

Status options:
  ok       - Session completed successfully (default)
  error    - Session ended with an error
  canceled - Session was canceled

Examples:
  agentctl sessions close
  agentctl sessions close --status error
  agentctl sessions close --session 01HXYZ --status canceled`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Validate status
			switch status {
			case storage.SessionStatusOK, storage.SessionStatusError, storage.SessionStatusCanceled:
				// Valid
			default:
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.close",
					fmt.Sprintf("invalid status: %s", status),
					"Use one of: ok, error, canceled")
			}

			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					var targetID string
					var targetWorkspace string

					if sessionID != "" {
						targetID = sessionID
						// Get session to find workspace for identity file cleanup
						if sess, err := store.Get(ctx, sessionID); err == nil {
							targetWorkspace = sess.WorkspacePath
						}
					} else {
						// Find active session
						ws := workspace
						if ws == "" {
							wd, err := os.Getwd()
							if err != nil {
								return fmt.Errorf("get working directory: %w", err)
							}
							ws = wd
						}

						aid := agentID
						if aid == "" {
							aid = "agentctl"
						}

						active, err := store.GetActive(ctx, ws, aid)
						if err != nil {
							return fmt.Errorf("get active session: %w", err)
						}
						if active == nil {
							payload := struct {
								Message string `json:"message"`
								Hint    string `json:"hint"`
							}{
								Message: "no active session found",
								Hint:    "Specify a session with --session or create one with 'agentctl sessions new'",
							}
							return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.close", payload)
						}
						targetID = active.ID
						targetWorkspace = active.WorkspacePath
					}

					// Update status
					if err := store.SetStatus(ctx, targetID, status); err != nil {
						if errors.Is(err, sessions.ErrNotFound) {
							return sessionscmd.WriteNotFound(cmd.OutOrStdout(), "agentctl.sessions.close", targetID)
						}
						return err
					}

					// Clear identity file for the workspace
					if targetWorkspace != "" {
						sessionscmd.ClearActiveIdentity(cmd.ErrOrStderr(), cfg, targetWorkspace)
					}

					payload := struct {
						SessionID string `json:"session_id"`
						Status    string `json:"status"`
						Closed    bool   `json:"closed"`
					}{
						SessionID: targetID,
						Status:    status,
						Closed:    true,
					}
					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.close", payload)
				})
			})
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID to close (default: most recent active)")
	cmd.Flags().StringVar(&status, "status", "ok", "Status to set (ok, error, canceled)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: cwd)")
	cmd.Flags().StringVar(&agentID, "agent-id", "", "Agent identifier (default: agentctl)")
	return cmd
}

func newSessionsExportCommand() *cobra.Command {
	var sessionID string
	var outputPath string
	var format string
	var includeTurns bool
	var includeChunks bool
	var includeSummaries bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export a session to MV2 format (memvid)",
		Long: `Export a captured session to memvid's MV2 file format.

MV2 is a single-file format with embedded search capabilities (Tantivy + HNSW).
Exported sessions can be shared, searched offline, and imported into other tools.

Requires memvid CLI to be installed:
  npm install -g memvid-cli

Examples:
  agentctl sessions export --session 01HXYZ
  agentctl sessions export --session 01HXYZ --output ./session.mv2
  agentctl sessions export --session 01HXYZ --include-turns --include-chunks
  agentctl sessions export --session 01HXYZ --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sessionID == "" {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.export",
					"--session is required",
					"Specify the session to export with --session <session-id>")
			}

			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					// Get session
					session, err := store.Get(ctx, sessionID)
					if err != nil {
						if errors.Is(err, sessions.ErrNotFound) {
							return sessionscmd.WriteNotFound(cmd.OutOrStdout(), "agentctl.sessions.export", sessionID)
						}
						return err
					}

					// Determine output path
					outPath := outputPath
					if outPath == "" {
						outPath = fmt.Sprintf("%s.mv2", sessionID)
					}

					// Get turns if requested
					var turns []storage.SessionTurn
					if includeTurns {
						turns, err = store.GetTurns(ctx, sessionID, storage.SessionTurnListOptions{})
						if err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to get turns: %v\n", err)
						}
					}

					// Get chunks if requested
					var chunks []storage.SessionChunk
					if includeChunks {
						chunks, err = store.GetChunks(ctx, sessionID, 0)
						if err != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to get chunks: %v\n", err)
						}
					}

					// Dry run - just show what would be exported
					if dryRun {
						payload := struct {
							SessionID        string `json:"session_id"`
							OutputPath       string `json:"output_path"`
							Format           string `json:"format"`
							IncludeTurns     bool   `json:"include_turns"`
							IncludeChunks    bool   `json:"include_chunks"`
							IncludeSummaries bool   `json:"include_summaries"`
							ProjectName      string `json:"project_name"`
							MessageCount     int    `json:"message_count"`
							TurnCount        int    `json:"turn_count"`
							ChunkCount       int    `json:"chunk_count"`
							TotalTokens      int    `json:"total_tokens"`
							DryRun           bool   `json:"dry_run"`
						}{
							SessionID:        sessionID,
							OutputPath:       outPath,
							Format:           format,
							IncludeTurns:     includeTurns,
							IncludeChunks:    includeChunks,
							IncludeSummaries: includeSummaries,
							ProjectName:      session.ProjectName,
							MessageCount:     session.MessageCount,
							TurnCount:        len(turns),
							ChunkCount:       len(chunks),
							TotalTokens:      session.TotalTokens,
							DryRun:           true,
						}
						return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.export", payload)
					}

					// Export using memvid CLI
					result, err := exportSessionToMV2(ctx, cmd.ErrOrStderr(), session, turns, chunks, outPath, includeSummaries)
					if err != nil {
						return fmt.Errorf("export failed: %w", err)
					}

					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.export", result)
				})
			})
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID to export (required)")
	cmd.Flags().StringVar(&outputPath, "output", "", "Output file path (default: <session-id>.mv2)")
	cmd.Flags().StringVar(&format, "format", "mv2", "Export format (mv2)")
	cmd.Flags().BoolVar(&includeTurns, "include-turns", true, "Include individual turns as frames")
	cmd.Flags().BoolVar(&includeChunks, "include-chunks", false, "Include session chunks as frames")
	cmd.Flags().BoolVar(&includeSummaries, "include-summaries", true, "Include L1/L2 summaries as frames")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be exported without creating file")
	return cmd
}

// exportSessionToMV2 exports a session to MV2 format using the memvid CLI.
func exportSessionToMV2(ctx context.Context, stderr io.Writer, session storage.Session, turns []storage.SessionTurn, chunks []storage.SessionChunk, outPath string, includeSummaries bool) (map[string]any, error) {
	// Check if memvid CLI is available
	cli := newMemvidCLI()
	if err := cli.Available(ctx); err != nil {
		return nil, err
	}

	// Create the MV2 file
	fmt.Fprintf(stderr, "Creating %s...\n", outPath)
	if err := cli.Create(ctx, outPath); err != nil {
		return nil, fmt.Errorf("create MV2 file: %w", err)
	}

	frameCount := 0

	// Add session metadata frame
	metaContent := fmt.Sprintf(`# Session: %s

**Project:** %s
**Branch:** %s
**Started:** %s
**Messages:** %d
**Tokens:** %d

## Summary
%s`, session.ID, session.ProjectName, session.GitBranch, session.StartedAt.Format(time.RFC3339),
		session.MessageCount, session.TotalTokens, session.Summary)

	if err := cli.Put(ctx, outPath, metaContent, "Session Metadata", map[string]string{
		"type":       "session_meta",
		"session_id": session.ID,
		"project":    session.ProjectName,
	}); err != nil {
		fmt.Fprintf(stderr, "warning: failed to add metadata frame: %v\n", err)
	} else {
		frameCount++
	}

	// Add turns as frames
	for i, turn := range turns {
		if turn.ContentPreview == "" {
			continue
		}

		title := fmt.Sprintf("Turn %d (%s)", turn.TurnIndex, turn.Role)
		tags := map[string]string{
			"type":       "turn",
			"session_id": session.ID,
			"role":       turn.Role,
			"turn_index": fmt.Sprintf("%d", turn.TurnIndex),
		}
		if turn.HasError {
			tags["has_error"] = "true"
			tags["error_type"] = turn.ErrorType
		}

		if err := cli.Put(ctx, outPath, turn.ContentPreview, title, tags); err != nil {
			fmt.Fprintf(stderr, "warning: failed to add turn %d: %v\n", i, err)
		} else {
			frameCount++
		}
	}

	// Add chunks as frames
	for i, chunk := range chunks {
		if chunk.ContentPreview == "" {
			continue
		}

		title := fmt.Sprintf("Chunk %d (%s)", chunk.ChunkIndex, chunk.ChunkType)
		tags := map[string]string{
			"type":        "chunk",
			"session_id":  session.ID,
			"chunk_index": fmt.Sprintf("%d", chunk.ChunkIndex),
			"chunk_type":  chunk.ChunkType,
		}
		if chunk.HasError {
			tags["has_error"] = "true"
			tags["error_type"] = chunk.ErrorType
		}

		if err := cli.Put(ctx, outPath, chunk.ContentPreview, title, tags); err != nil {
			fmt.Fprintf(stderr, "warning: failed to add chunk %d: %v\n", i, err)
		} else {
			frameCount++
		}
	}

	// Add summary frame if available
	if includeSummaries && session.Summary != "" {
		if err := cli.Put(ctx, outPath, session.Summary, "Session Summary", map[string]string{
			"type":       "summary",
			"session_id": session.ID,
		}); err != nil {
			fmt.Fprintf(stderr, "warning: failed to add summary frame: %v\n", err)
		} else {
			frameCount++
		}
	}

	// Get file stats
	fileInfo, _ := os.Stat(outPath)
	var fileSize int64
	if fileInfo != nil {
		fileSize = fileInfo.Size()
	}

	return map[string]any{
		"session_id":   session.ID,
		"output_path":  outPath,
		"frame_count":  frameCount,
		"turn_count":   len(turns),
		"chunk_count":  len(chunks),
		"file_size":    fileSize,
		"status":       "exported",
		"project_name": session.ProjectName,
	}, nil
}

// memvidCLI is a minimal CLI wrapper for session export.
type memvidCLI struct{}

func newMemvidCLI() *memvidCLI {
	return &memvidCLI{}
}

func (c *memvidCLI) Available(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "memvid", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("memvid CLI not available: %w (install with: npm install -g memvid-cli)", err)
	}
	return nil
}

func (c *memvidCLI) Create(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "memvid", "create", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create failed: %w\n%s", err, output)
	}
	return nil
}

func (c *memvidCLI) Put(ctx context.Context, path, content, title string, tags map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	args := []string{"put", path, "--title", title}
	for k, v := range tags {
		args = append(args, "--tag", fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, "memvid", args...)
	cmd.Stdin = strings.NewReader(content)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("put failed: %w\n%s", err, output)
	}
	return nil
}

func newSessionsWindowsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "windows <session-id>",
		Short: "List context windows for a session",
		Long: `List context windows (compaction-bounded work spans) for a session.

Context windows are created when Claude Code compacts its conversation context.
Each window represents a coherent span of work before a compaction boundary,
enabling granular retrieval within long sessions.

Examples:
  agentctl sessions windows 01HXYZ...
  agentctl sessions windows 01HXYZ... --limit 5
  agentctl sessions windows 01HXYZ... --index 2 --full
  agentctl sessions windows 01HXYZ... --offset 5 --limit 5`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			showChunks, _ := cmd.Flags().GetBool("show-chunks")
			limit, _ := cmd.Flags().GetInt("limit")
			offset, _ := cmd.Flags().GetInt("offset")
			windowIndex, _ := cmd.Flags().GetInt("index")
			fullSummary, _ := cmd.Flags().GetBool("full")

			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				return sessionscmd.WithSessionStore(ctx, cfg, func(store storage.SessionStore) error {
					// Verify session exists
					session, err := store.Get(ctx, sessionID)
					if err != nil {
						if errors.Is(err, sessions.ErrNotFound) {
							return sessionscmd.WriteNotFound(cmd.OutOrStdout(), "agentctl.sessions.windows", sessionID)
						}
						return err
					}

					// Get context windows
					windows, err := store.GetContextWindows(ctx, sessionID)
					if err != nil {
						return fmt.Errorf("get context windows: %w", err)
					}

					totalWindows := len(windows)

					// Filter by specific index if requested
					if windowIndex >= 0 {
						if windowIndex >= len(windows) {
							return fmt.Errorf("window index %d out of range (session has %d windows)", windowIndex, len(windows))
						}
						windows = []storage.ContextWindow{windows[windowIndex]}
					} else {
						// Apply offset
						if offset > 0 {
							if offset >= len(windows) {
								windows = nil
							} else {
								windows = windows[offset:]
							}
						}
						// Apply limit
						if limit > 0 && len(windows) > limit {
							windows = windows[:limit]
						}
					}

					type windowEntry struct {
						ID               string    `json:"id"`
						WindowIndex      int       `json:"window_index"`
						StartedAt        time.Time `json:"started_at,omitempty"`
						EndedAt          time.Time `json:"ended_at,omitempty"`
						PreCompactTokens int       `json:"pre_compact_tokens,omitempty"`
						Trigger          string    `json:"trigger,omitempty"`
						ChunkStart       int       `json:"chunk_start"`
						ChunkEnd         int       `json:"chunk_end"`
						MessageCount     int       `json:"message_count"`
						Summary          string    `json:"summary,omitempty"`
						HasEmbedding     bool      `json:"has_embedding"`
					}

					entries := make([]windowEntry, len(windows))
					for i, w := range windows {
						summary := w.Summary
						if !fullSummary {
							summary = truncateSummary(summary, 80)
						}
						entries[i] = windowEntry{
							ID:               w.ID,
							WindowIndex:      w.WindowIndex,
							StartedAt:        w.StartedAt,
							EndedAt:          w.EndedAt,
							PreCompactTokens: w.PreCompactTokens,
							Trigger:          w.Trigger,
							ChunkStart:       w.ChunkStart,
							ChunkEnd:         w.ChunkEnd,
							MessageCount:     w.MessageCount,
							Summary:          summary,
							HasEmbedding:     len(w.Embedding) > 0,
						}
					}

					payload := struct {
						SessionID    string        `json:"session_id"`
						ProjectName  string        `json:"project_name,omitempty"`
						Windows      []windowEntry `json:"windows"`
						Count        int           `json:"count"`
						TotalWindows int           `json:"total_windows"`
						Offset       int           `json:"offset,omitempty"`
						Limit        int           `json:"limit,omitempty"`
					}{
						SessionID:    sessionID,
						ProjectName:  session.ProjectName,
						Windows:      entries,
						Count:        len(entries),
						TotalWindows: totalWindows,
						Offset:       offset,
						Limit:        limit,
					}

					// Optionally show chunk counts
					if showChunks && len(windows) > 0 {
						// Get total chunk count for context
						chunks, err := store.GetChunks(ctx, sessionID, 0)
						if err == nil {
							payloadWithChunks := struct {
								SessionID    string        `json:"session_id"`
								ProjectName  string        `json:"project_name,omitempty"`
								Windows      []windowEntry `json:"windows"`
								Count        int           `json:"count"`
								TotalWindows int           `json:"total_windows"`
								Offset       int           `json:"offset,omitempty"`
								Limit        int           `json:"limit,omitempty"`
								TotalChunks  int           `json:"total_chunks"`
							}{
								SessionID:    sessionID,
								ProjectName:  session.ProjectName,
								Windows:      entries,
								Count:        len(entries),
								TotalWindows: totalWindows,
								Offset:       offset,
								Limit:        limit,
								TotalChunks:  len(chunks),
							}
							return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.windows", payloadWithChunks)
						}
					}

					return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.windows", payload)
				})
			})
		},
	}

	cmd.Flags().Bool("show-chunks", false, "Include total chunk count in output")
	cmd.Flags().Int("limit", 0, "Limit number of windows returned (default: all)")
	cmd.Flags().Int("offset", 0, "Skip first N windows (for pagination)")
	cmd.Flags().Int("index", -1, "Get a specific window by index (0-based)")
	cmd.Flags().Bool("full", false, "Show full summaries instead of truncated")
	return cmd
}

func init() {
	rootCmd.AddCommand(newSessionsCommand())
}
