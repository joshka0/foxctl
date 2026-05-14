package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/runtime/hooks/analysisflow"
	"github.com/joshka0/foxctl/internal/runtime/hooks/contextflow"
	"github.com/joshka0/foxctl/internal/runtime/hooks/inboxflow"
	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
	"github.com/joshka0/foxctl/internal/runtime/hooks/memoryflow"
	"github.com/joshka0/foxctl/internal/runtime/hooks/operationalflow"
	"github.com/joshka0/foxctl/internal/runtime/hooks/promptflow"
	"github.com/joshka0/foxctl/internal/runtime/hooks/taskflow"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/spf13/cobra"
)

type hookSessionEndPayload struct {
	AssistantText string `json:"assistant_text,omitempty"`
}

type hookTodoSyncPayload struct {
	SessionID    string `json:"sessionID,omitempty"`
	AltSessionID string `json:"session_id,omitempty"`
	ToolInput    struct {
		Todos []taskflow.ClaudeTodo `json:"todos"`
	} `json:"tool_input"`
}

type hookTodoContinuationPayload struct {
	SessionID    string `json:"sessionID,omitempty"`
	AltSessionID string `json:"session_id,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
}

type hookTaskFileLinkPayload struct {
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
	} `json:"tool_input"`
}

type hookContextUpdaterDrainPayload struct {
	SessionID    string `json:"sessionID,omitempty"`
	AltSessionID string `json:"session_id,omitempty"`
}

type hookInboxPayload struct {
	SessionID    string `json:"sessionID,omitempty"`
	AltSessionID string `json:"session_id,omitempty"`
	ToolName     string `json:"tool_name,omitempty"`
	ToolInput    any    `json:"tool_input,omitempty"`
}

type hookAnchorDetectPayload struct {
	Prompt       string `json:"prompt,omitempty"`
	SessionID    string `json:"sessionID,omitempty"`
	AltSessionID string `json:"session_id,omitempty"`
}

type hookMemoryDetectorPayload struct {
	Prompt string `json:"prompt,omitempty"`
}

type hookMemoryRecallPayload struct {
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
	} `json:"tool_input"`
}

type hookMemoryLifecyclePayload struct {
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput struct {
		FilePath  string                `json:"file_path,omitempty"`
		Path      string                `json:"path,omitempty"`
		OldString string                `json:"old_string,omitempty"`
		NewString string                `json:"new_string,omitempty"`
		Content   string                `json:"content,omitempty"`
		Operation string                `json:"operation,omitempty"`
		Name      string                `json:"name,omitempty"`
		Todos     []taskflow.ClaudeTodo `json:"todos,omitempty"`
	} `json:"tool_input"`
}

type hookCodeAnalysisPayload struct {
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
	} `json:"tool_input"`
}

type hookLiveIndexPayload struct {
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
		Path     string `json:"path,omitempty"`
	} `json:"tool_input"`
}

type hookLSPDiagnosticsPayload struct {
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
		Path     string `json:"path,omitempty"`
	} `json:"tool_input"`
}

type hookPlanSyncPayload struct {
	SessionCwd string `json:"session_cwd,omitempty"`
}

type hookProposalPacketPayload struct {
	ProposalID    string `json:"proposal_id,omitempty"`
	AltProposalID string `json:"proposalID,omitempty"`
	Action        string `json:"action,omitempty"`
	VaultName     string `json:"vault_name,omitempty"`
	VaultPath     string `json:"vault_path,omitempty"`
	DraftPath     string `json:"draft_path,omitempty"`
	TargetPath    string `json:"target_path,omitempty"`
	Heading       string `json:"heading,omitempty"`
}

type hookProposalPacketResponse struct {
	Context    string         `json:"context,omitempty"`
	Workspace  string         `json:"workspace"`
	ProposalID string         `json:"proposal_id,omitempty"`
	Action     string         `json:"action,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type hookProposalNextMergePayload struct {
	VaultPath string `json:"vault_path,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Claim     bool   `json:"claim,omitempty"`
}

type hookProposalNextMergeResponse struct {
	Context   string         `json:"context,omitempty"`
	Workspace string         `json:"workspace"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func newHooksCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Go-native lifecycle entrypoints for provider hook wrappers",
	}
	cmd.AddCommand(newHooksSessionStartCommand(), newHooksSessionEndCommand(), newHooksSubagentStopCommand(), newHooksTodoSyncCommand(), newHooksTodoContinuationCommand(), newHooksTaskFileLinkCommand(), newHooksContextUpdaterDrainCommand(), newHooksSessionRestorePostcompactCommand(), newHooksOverseerInboxCommand(), newHooksOverseerInboxPostCommand(), newHooksAnchorDetectCommand(), newHooksMemoryDetectorCommand(), newHooksMemoryRecallCommand(), newHooksMemoryLifecycleCommand(), newHooksCodeAnalysisCommand(), newHooksSemanticAnchorsCommand(), newHooksLiveIndexCommand(), newHooksLSPDiagnosticsCommand(), newHooksEmbeddingFlushCommand(), newHooksPlanSyncCommand(), newHooksGraphMaintenanceCommand(), newHooksProposalPacketCommand(), newHooksProposalNextMergeCommand())
	return cmd
}

func newHooksSessionStartCommand() *cobra.Command {
	var workspacePath string
	var source string

	cmd := &cobra.Command{
		Use:   "session-start",
		Short: "Handle SessionStart lifecycle orchestration and return provider hook context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			response, err := lifecycle.Start(ctx, lifecycle.NewDependencies(cfg), lifecycle.StartRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Source:    source,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/session-start", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&source, "source", "startup", "Session start trigger source (startup, resume, compact)")
	return cmd
}

func newHooksSessionEndCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "session-end",
		Short: "Handle SessionEnd lifecycle orchestration and return provider hook metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalHookPayload(cmd)
			if err != nil {
				return err
			}
			response, err := lifecycle.End(ctx, lifecycle.NewDependencies(cfg), lifecycle.EndRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: lifecycle.EndPayload{
					AssistantText: payload.AssistantText,
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/session-end", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksSubagentStopCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "subagent-stop",
		Short: "Handle SubagentStop ContextWiki capture and promotion metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalHookPayload(cmd)
			if err != nil {
				return err
			}
			response, err := lifecycle.SubagentStop(ctx, lifecycle.NewDependencies(cfg), lifecycle.SubagentStopRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: lifecycle.EndPayload{
					AssistantText: payload.AssistantText,
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/subagent-stop", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksTodoSyncCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "todo-sync",
		Short: "Handle provider TodoWrite synchronization and return hook context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalTodoSyncPayload(cmd)
			if err != nil {
				return err
			}
			response, err := taskflow.SyncTodoWrite(ctx, taskflow.NewDependencies(cfg), taskflow.TodoSyncRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: taskflow.TodoSyncPayload{
					SessionID:    payload.SessionID,
					AltSessionID: payload.AltSessionID,
					ToolInput:    payload.ToolInput,
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/todo-sync", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksTodoContinuationCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "todo-continuation",
		Short: "Handle stop-time todo continuation gating and return hook metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalTodoContinuationPayload(cmd)
			if err != nil {
				return err
			}
			response, err := taskflow.ContinueTodoSession(ctx, taskflow.NewDependencies(cfg), taskflow.TodoContinuationRequest{
				Workspace: resolveContextWorkspace(firstNonEmpty(workspacePath, payload.Cwd)),
				Payload: taskflow.TodoContinuationPayload{
					SessionID:    payload.SessionID,
					AltSessionID: payload.AltSessionID,
					Cwd:          payload.Cwd,
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/todo-continuation", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksTaskFileLinkCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "task-file-link",
		Short: "Handle edited-file to active-task graph linking and return hook metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalTaskFileLinkPayload(cmd)
			if err != nil {
				return err
			}
			response, err := taskflow.LinkTaskFile(ctx, taskflow.NewDependencies(cfg), taskflow.TaskFileLinkRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload:   taskflow.TaskFileLinkPayload{ToolInput: payload.ToolInput},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/task-file-link", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksContextUpdaterDrainCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "context-updater-drain",
		Short: "Drain context-updater entries for prompt-time injection",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalContextUpdaterDrainPayload(cmd)
			if err != nil {
				return err
			}
			response, err := contextflow.DrainUpdaterContext(ctx, contextflow.Dependencies{
				StorageRoot:    cfg.Storage.Root,
				DetectIdentity: lifecycle.NewDependencies(cfg).DetectIdentity,
			}, contextflow.DrainRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				SessionID: firstNonEmpty(payload.SessionID, payload.AltSessionID),
				Sources:   []string{"context-updater"},
				Limit:     10,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/context-updater-drain", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksSessionRestorePostcompactCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "session-restore-postcompact",
		Short: "Restore session context once after compaction using the pending marker",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			response, err := lifecycle.RestorePostcompact(ctx, lifecycle.NewDependencies(cfg), lifecycle.PostcompactRestoreRequest{
				Workspace: resolveContextWorkspace(workspacePath),
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/session-restore-postcompact", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksOverseerInboxCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "overseer-inbox",
		Short: "Read overseer inbox messages for PreToolUse context injection",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalInboxPayload(cmd)
			if err != nil {
				return err
			}
			response, err := inboxflow.ReadInboxPreTool(ctx, inboxflow.NewDependencies(lifecycle.NewDependencies(cfg)), inboxflow.PreToolRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: inboxflow.InboxPayload{
					SessionID:    payload.SessionID,
					AltSessionID: payload.AltSessionID,
					ToolName:     payload.ToolName,
					ToolInput:    payload.ToolInput,
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/overseer-inbox", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksOverseerInboxPostCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "overseer-inbox-post",
		Short: "Read overseer inbox messages for PostToolUse enqueue/injection",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalInboxPayload(cmd)
			if err != nil {
				return err
			}
			response, err := inboxflow.ReadInboxPostTool(ctx, inboxflow.NewDependencies(lifecycle.NewDependencies(cfg)), inboxflow.PostToolRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: inboxflow.InboxPayload{
					SessionID:    payload.SessionID,
					AltSessionID: payload.AltSessionID,
					ToolName:     payload.ToolName,
					ToolInput:    payload.ToolInput,
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/overseer-inbox-post", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksAnchorDetectCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "anchor-detect",
		Short: "Handle /anchor and /todo prompt-trigger logic and return hook context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalAnchorDetectPayload(cmd)
			if err != nil {
				return err
			}
			response, err := promptflow.DetectAnchor(ctx, promptflow.NewDependencies(lifecycle.NewDependencies(cfg)), promptflow.AnchorRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: promptflow.AnchorPayload{
					Prompt:       payload.Prompt,
					SessionID:    payload.SessionID,
					AltSessionID: payload.AltSessionID,
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/anchor-detect", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksMemoryDetectorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory-detector",
		Short: "Handle memory save/recall/todo prompt hints and return hook context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, err := readOptionalMemoryDetectorPayload(cmd)
			if err != nil {
				return err
			}
			response := memoryflow.DetectPrompt(memoryflow.DetectorRequest{Prompt: payload.Prompt})
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/memory-detector", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}
	return cmd
}

func newHooksMemoryRecallCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "memory-recall",
		Short: "Recall file-related memories before edits and return hook context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalMemoryRecallPayload(cmd)
			if err != nil {
				return err
			}
			response, err := memoryflow.RecallFile(ctx, memoryflow.NewDependencies(cfg, lifecycle.NewDependencies(cfg)), memoryflow.RecallRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: memoryflow.RecallPayload{
					ToolInput: payload.ToolInput,
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/memory-recall", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksMemoryLifecycleCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "memory-lifecycle",
		Short: "Handle post-tool memory capture, prompts, and refresh behavior",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalMemoryLifecyclePayload(cmd)
			if err != nil {
				return err
			}
			response, err := memoryflow.HandleLifecycle(ctx, memoryflow.NewDependencies(cfg, lifecycle.NewDependencies(cfg)), memoryflow.LifecycleRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: memoryflow.LifecyclePayload{
					ToolName: payload.ToolName,
					ToolInput: struct {
						FilePath  string                  `json:"file_path,omitempty"`
						Path      string                  `json:"path,omitempty"`
						OldString string                  `json:"old_string,omitempty"`
						NewString string                  `json:"new_string,omitempty"`
						Content   string                  `json:"content,omitempty"`
						Operation string                  `json:"operation,omitempty"`
						Name      string                  `json:"name,omitempty"`
						Todos     []memoryflow.ClaudeTodo `json:"todos,omitempty"`
					}{
						FilePath:  payload.ToolInput.FilePath,
						Path:      payload.ToolInput.Path,
						OldString: payload.ToolInput.OldString,
						NewString: payload.ToolInput.NewString,
						Content:   payload.ToolInput.Content,
						Operation: payload.ToolInput.Operation,
						Name:      payload.ToolInput.Name,
						Todos:     payload.ToolInput.Todos,
					},
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/memory-lifecycle", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksCodeAnalysisCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "code-analysis",
		Short: "Handle post-edit complexity and impact analysis and return hook context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalCodeAnalysisPayload(cmd)
			if err != nil {
				return err
			}
			response, err := analysisflow.AnalyzeEditedFile(ctx, analysisflow.NewDependencies(lifecycle.NewDependencies(cfg)), analysisflow.Request{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: analysisflow.Payload{
					ToolInput: payload.ToolInput,
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/code-analysis", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksSemanticAnchorsCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "semantic-anchors",
		Short: "Advisory lint and context for semantic anchors in touched files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			payload, err := readOptionalCodeAnalysisPayload(cmd)
			if err != nil {
				return err
			}
			response, err := analysisflow.AnalyzeSemanticAnchors(ctx, analysisflow.Request{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: analysisflow.Payload{
					ToolInput: payload.ToolInput,
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/semantic-anchors", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksLiveIndexCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "live-index",
		Short: "Handle post-edit incremental symbol indexing and return hook context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalLiveIndexPayload(cmd)
			if err != nil {
				return err
			}
			response, err := operationalflow.IndexEditedFile(ctx, operationalflow.NewDependencies(cfg, lifecycle.NewDependencies(cfg)), operationalflow.LiveIndexRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: operationalflow.LiveIndexPayload{
					ToolName: payload.ToolName,
					ToolInput: struct {
						FilePath string `json:"file_path,omitempty"`
						Path     string `json:"path,omitempty"`
					}{
						FilePath: payload.ToolInput.FilePath,
						Path:     payload.ToolInput.Path,
					},
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/live-index", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksLSPDiagnosticsCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "lsp-diagnostics",
		Short: "Run post-edit diagnostics and return hook context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalLSPDiagnosticsPayload(cmd)
			if err != nil {
				return err
			}
			response, err := operationalflow.DiagnoseEditedFile(ctx, operationalflow.NewDependencies(cfg, lifecycle.NewDependencies(cfg)), operationalflow.LSPDiagnosticsRequest{
				Workspace: resolveContextWorkspace(workspacePath),
				Payload: operationalflow.LSPDiagnosticsPayload{
					ToolInput: struct {
						FilePath string `json:"file_path,omitempty"`
						Path     string `json:"path,omitempty"`
					}{
						FilePath: payload.ToolInput.FilePath,
						Path:     payload.ToolInput.Path,
					},
				},
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/lsp-diagnostics", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksEmbeddingFlushCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "embedding-flush",
		Short: "Flush queued embeddings before stop-time exit",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			response, err := operationalflow.FlushEmbeddings(ctx, operationalflow.NewDependencies(cfg, lifecycle.NewDependencies(cfg)), operationalflow.EmbeddingFlushRequest{
				Workspace: resolveContextWorkspace(workspacePath),
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/embedding-flush", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newHooksPlanSyncCommand() *cobra.Command {
	var workspacePath string
	var syncMode bool
	var logFile string

	cmd := &cobra.Command{
		Use:   "plan-sync",
		Short: "Run or schedule async plan synchronization before stop-time exit",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			payload, err := readOptionalPlanSyncPayload(cmd)
			if err != nil {
				return err
			}
			target := resolveContextWorkspace(firstNonEmpty(workspacePath, payload.SessionCwd))
			if syncMode || operationalflow.PlanSyncSyncEnabled() {
				response, err := operationalflow.SyncPlans(ctx, operationalflow.NewDependencies(cfg, lifecycle.NewDependencies(cfg)), operationalflow.PlanSyncRequest{
					Workspace: target,
					Payload: operationalflow.PlanSyncPayload{
						SessionCwd: payload.SessionCwd,
					},
				})
				if err != nil {
					return err
				}
				if strings.TrimSpace(logFile) != "" {
					response.LogFile = strings.TrimSpace(logFile)
				}
				return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/plan-sync", map[string]any{
					"response": response,
				}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
			}

			response, err := launchBackgroundPlanSync(target, strings.TrimSpace(logFile))
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/plan-sync", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().BoolVar(&syncMode, "sync", false, "Run plan synchronization synchronously instead of background scheduling")
	cmd.Flags().StringVar(&logFile, "log-file", "", "Optional log file path for background or sync runs")
	return cmd
}

func newHooksProposalPacketCommand() *cobra.Command {
	var workspacePath string
	var proposalID string
	var action string
	var vaultName string
	var vaultPath string
	var draftPath string
	var targetPath string
	var heading string

	cmd := &cobra.Command{
		Use:   "proposal-packet",
		Short: "Resolve a ContextWiki proposal into hook-ready context and metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, err := readOptionalProposalPacketPayload(cmd)
			if err != nil {
				return err
			}
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			resolvedID := firstNonEmpty(proposalID, payload.ProposalID, payload.AltProposalID)
			resolvedAction := strings.ToLower(strings.TrimSpace(firstNonEmpty(action, payload.Action, "apply")))
			if strings.TrimSpace(resolvedID) == "" {
				return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/proposal-packet", map[string]any{
					"response": hookProposalPacketResponse{
						Workspace: target,
						Action:    resolvedAction,
					},
				}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
			}

			resolvedVaultName := firstNonEmpty(vaultName, payload.VaultName)
			resolvedVaultPath := firstNonEmpty(vaultPath, payload.VaultPath)
			resolvedDraftPath := firstNonEmpty(draftPath, payload.DraftPath)
			resolvedTargetPath := firstNonEmpty(targetPath, payload.TargetPath)
			resolvedHeading := firstNonEmpty(heading, payload.Heading)

			var packet contextplane.ProposalWorkPacket
			switch resolvedAction {
			case "merge":
				_, _, workPacket, err := store.MergeMemoryProposal(cmd.Context(), resolvedVaultName, resolvedVaultPath, resolvedID, resolvedDraftPath, resolvedTargetPath, resolvedHeading)
				if err != nil {
					return err
				}
				packet = workPacket
			default:
				_, _, workPacket, err := store.ApplyMemoryProposal(cmd.Context(), resolvedID)
				if err != nil {
					return err
				}
				packet = workPacket
				if strings.TrimSpace(resolvedVaultPath) != "" {
					packet.VaultPath = strings.TrimSpace(resolvedVaultPath)
				}
			}

			response := hookProposalPacketResponse{
				Context:    contextplane.RenderHookContextForProposalPacket(packet),
				Workspace:  target,
				ProposalID: resolvedID,
				Action:     resolvedAction,
				Metadata: map[string]any{
					"proposal_work_packet": packet,
				},
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/proposal-packet", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&proposalID, "proposal-id", "", "Proposal ID")
	cmd.Flags().StringVar(&action, "action", "apply", "Proposal action: apply or merge")
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name for merge actions")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path for merge or packet enrichment")
	cmd.Flags().StringVar(&draftPath, "draft-path", "", "Optional draft path override")
	cmd.Flags().StringVar(&targetPath, "target-path", "", "Optional target path override")
	cmd.Flags().StringVar(&heading, "heading", "", "Optional heading override")
	return cmd
}

func newHooksProposalNextMergeCommand() *cobra.Command {
	var workspacePath string
	var vaultPath string
	var limit int
	var claim bool

	cmd := &cobra.Command{
		Use:   "proposal-next-merge",
		Short: "Resolve the next prepared proposal-merge task into hook-ready context and metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, err := readOptionalProposalNextMergePayload(cmd)
			if err != nil {
				return err
			}
			target := resolveContextWorkspace(workspacePath)
			resolvedVaultPath := firstNonEmpty(vaultPath, payload.VaultPath)
			resolvedLimit := limit
			if resolvedLimit <= 0 {
				resolvedLimit = payload.Limit
			}
			if resolvedLimit <= 0 {
				resolvedLimit = 50
			}
			store := contextplane.NewWorkspaceStore(target)
			if strings.TrimSpace(resolvedVaultPath) != "" {
				cfg, err := loadConfig(cmd.Context())
				if err != nil {
					return err
				}
				index, err := obsidianindex.Open(cmd.Context(), cfg.Storage.Root, resolvedVaultPath)
				if err != nil {
					return err
				}
				defer func() { _ = index.Close() }()
				health, err := index.Health(cmd.Context())
				if err != nil {
					return err
				}
				if _, err := store.GenerateMaintenanceTasksWithHealth(cmd.Context(), resolvedLimit, &health); err != nil {
					return err
				}
			} else {
				if _, err := store.GenerateMaintenanceTasks(cmd.Context(), resolvedLimit); err != nil {
					return err
				}
			}
			resolvedClaim := claim || payload.Claim
			var task *contextplane.MaintenanceTask
			if resolvedClaim {
				task, err = store.ClaimNextProposalMergeTask(cmd.Context(), resolvedLimit)
			} else {
				task, err = store.NextProposalMergeTask(cmd.Context(), resolvedLimit)
			}
			if err != nil {
				return err
			}
			response := hookProposalNextMergeResponse{
				Workspace: target,
				Metadata:  map[string]any{},
			}
			if task != nil && task.WorkPacket != nil {
				packet := *task.WorkPacket
				if packet.VaultPath == "" && strings.TrimSpace(resolvedVaultPath) != "" {
					packet.VaultPath = strings.TrimSpace(resolvedVaultPath)
				}
				response.Context = contextplane.RenderHookContextForProposalPacket(packet)
				response.Metadata["proposal_work_packet"] = packet
				response.Metadata["maintenance_task_id"] = task.ID
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/proposal-next-merge", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Optional vault path for health refresh before selection")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maintenance-task scan limit")
	cmd.Flags().BoolVar(&claim, "claim", false, "Claim the selected proposal-merge task so it is not re-offered")
	return cmd
}

func newHooksGraphMaintenanceCommand() *cobra.Command {
	var workspacePath string
	var syncMode bool
	var logFile string

	cmd := &cobra.Command{
		Use:   "graph-maintenance",
		Short: "Run or schedule async graph cleanup and PageRank maintenance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			target := resolveContextWorkspace(workspacePath)
			if syncMode || operationalflow.GraphMaintenanceSyncEnabled() {
				response, err := operationalflow.MaintainGraphSync(ctx, operationalflow.NewDependencies(cfg, lifecycle.NewDependencies(cfg)), operationalflow.GraphMaintenanceRequest{
					Workspace: target,
				})
				if err != nil {
					return err
				}
				if strings.TrimSpace(logFile) != "" {
					response.LogFile = strings.TrimSpace(logFile)
				}
				return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/graph-maintenance", map[string]any{
					"response": response,
				}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
			}

			response, err := launchBackgroundGraphMaintenance(target, strings.TrimSpace(logFile))
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("hooks/graph-maintenance", map[string]any{
				"response": response,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().BoolVar(&syncMode, "sync", false, "Run maintenance synchronously instead of background scheduling")
	cmd.Flags().StringVar(&logFile, "log-file", "", "Optional log file path for background or sync runs")
	return cmd
}

func readOptionalHookPayload(cmd *cobra.Command) (hookSessionEndPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookSessionEndPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookSessionEndPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookSessionEndPayload{}, nil
	}
	var payload hookSessionEndPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookSessionEndPayload{}, err
	}
	return payload, nil
}

func readOptionalTodoSyncPayload(cmd *cobra.Command) (hookTodoSyncPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookTodoSyncPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookTodoSyncPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookTodoSyncPayload{}, nil
	}
	var payload hookTodoSyncPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookTodoSyncPayload{}, err
	}
	return payload, nil
}

func readOptionalTodoContinuationPayload(cmd *cobra.Command) (hookTodoContinuationPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookTodoContinuationPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookTodoContinuationPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookTodoContinuationPayload{}, nil
	}
	var payload hookTodoContinuationPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookTodoContinuationPayload{}, err
	}
	return payload, nil
}

func readOptionalTaskFileLinkPayload(cmd *cobra.Command) (hookTaskFileLinkPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookTaskFileLinkPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookTaskFileLinkPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookTaskFileLinkPayload{}, nil
	}
	var payload hookTaskFileLinkPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookTaskFileLinkPayload{}, err
	}
	return payload, nil
}

func readOptionalContextUpdaterDrainPayload(cmd *cobra.Command) (hookContextUpdaterDrainPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookContextUpdaterDrainPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookContextUpdaterDrainPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookContextUpdaterDrainPayload{}, nil
	}
	var payload hookContextUpdaterDrainPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookContextUpdaterDrainPayload{}, err
	}
	return payload, nil
}

func readOptionalInboxPayload(cmd *cobra.Command) (hookInboxPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookInboxPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookInboxPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookInboxPayload{}, nil
	}
	var payload hookInboxPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookInboxPayload{}, err
	}
	return payload, nil
}

func readOptionalAnchorDetectPayload(cmd *cobra.Command) (hookAnchorDetectPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookAnchorDetectPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookAnchorDetectPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookAnchorDetectPayload{}, nil
	}
	var payload hookAnchorDetectPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookAnchorDetectPayload{}, err
	}
	return payload, nil
}

func readOptionalMemoryDetectorPayload(cmd *cobra.Command) (hookMemoryDetectorPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookMemoryDetectorPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookMemoryDetectorPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookMemoryDetectorPayload{}, nil
	}
	var payload hookMemoryDetectorPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookMemoryDetectorPayload{}, err
	}
	return payload, nil
}

func readOptionalMemoryRecallPayload(cmd *cobra.Command) (hookMemoryRecallPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookMemoryRecallPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookMemoryRecallPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookMemoryRecallPayload{}, nil
	}
	var payload hookMemoryRecallPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookMemoryRecallPayload{}, err
	}
	return payload, nil
}

func readOptionalMemoryLifecyclePayload(cmd *cobra.Command) (hookMemoryLifecyclePayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookMemoryLifecyclePayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookMemoryLifecyclePayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookMemoryLifecyclePayload{}, nil
	}
	var payload hookMemoryLifecyclePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookMemoryLifecyclePayload{}, err
	}
	return payload, nil
}

func readOptionalCodeAnalysisPayload(cmd *cobra.Command) (hookCodeAnalysisPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookCodeAnalysisPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookCodeAnalysisPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookCodeAnalysisPayload{}, nil
	}
	var payload hookCodeAnalysisPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookCodeAnalysisPayload{}, err
	}
	return payload, nil
}

func readOptionalLiveIndexPayload(cmd *cobra.Command) (hookLiveIndexPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookLiveIndexPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookLiveIndexPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookLiveIndexPayload{}, nil
	}
	var payload hookLiveIndexPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookLiveIndexPayload{}, err
	}
	return payload, nil
}

func readOptionalLSPDiagnosticsPayload(cmd *cobra.Command) (hookLSPDiagnosticsPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookLSPDiagnosticsPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookLSPDiagnosticsPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookLSPDiagnosticsPayload{}, nil
	}
	var payload hookLSPDiagnosticsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookLSPDiagnosticsPayload{}, err
	}
	return payload, nil
}

func readOptionalPlanSyncPayload(cmd *cobra.Command) (hookPlanSyncPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookPlanSyncPayload{}, nil
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return hookPlanSyncPayload{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return hookPlanSyncPayload{}, nil
	}
	var payload hookPlanSyncPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return hookPlanSyncPayload{}, err
	}
	return payload, nil
}

func readOptionalProposalPacketPayload(cmd *cobra.Command) (hookProposalPacketPayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookProposalPacketPayload{}, nil
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return hookProposalPacketPayload{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return hookProposalPacketPayload{}, nil
	}
	var payload hookProposalPacketPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return hookProposalPacketPayload{}, err
	}
	return payload, nil
}

func readOptionalProposalNextMergePayload(cmd *cobra.Command) (hookProposalNextMergePayload, error) {
	in := cmd.InOrStdin()
	if isTerminalReader(in) {
		return hookProposalNextMergePayload{}, nil
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return hookProposalNextMergePayload{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return hookProposalNextMergePayload{}, nil
	}
	var payload hookProposalNextMergePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return hookProposalNextMergePayload{}, err
	}
	return payload, nil
}

func launchBackgroundGraphMaintenance(workspacePath, logFile string) (operationalflow.GraphMaintenanceResponse, error) {
	target := resolveContextWorkspace(workspacePath)
	if strings.TrimSpace(logFile) == "" {
		resolved, err := defaultGraphMaintenanceLogFile()
		if err != nil {
			return operationalflow.GraphMaintenanceResponse{}, err
		}
		logFile = resolved
	}
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return operationalflow.GraphMaintenanceResponse{}, err
	}
	if _, err := io.WriteString(file, "=== Graph Maintenance "+time.Now().UTC().Format(time.RFC3339)+" ===\nWorkspace: "+target+"\n"); err != nil {
		_ = file.Close()
		return operationalflow.GraphMaintenanceResponse{}, err
	}
	execPath, err := os.Executable()
	if err != nil {
		_ = file.Close()
		return operationalflow.GraphMaintenanceResponse{}, err
	}
	child := exec.Command(execPath, "hooks", "graph-maintenance", "--workspace", target, "--sync", "--log-file", logFile)
	child.Env = os.Environ()
	child.Stdout = file
	child.Stderr = file
	setDetachedProcessGroup(child)
	if err := child.Start(); err != nil {
		_ = file.Close()
		return operationalflow.GraphMaintenanceResponse{}, err
	}
	_ = file.Close()
	return operationalflow.GraphMaintenanceResponse{
		Workspace: target,
		Mode:      "background",
		LogFile:   logFile,
	}, nil
}

func launchBackgroundPlanSync(workspacePath, logFile string) (operationalflow.PlanSyncResponse, error) {
	target := resolveContextWorkspace(workspacePath)
	if strings.TrimSpace(logFile) == "" {
		resolved, err := defaultPlanSyncLogFile()
		if err != nil {
			return operationalflow.PlanSyncResponse{}, err
		}
		logFile = resolved
	}
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return operationalflow.PlanSyncResponse{}, err
	}
	if _, err := io.WriteString(file, "=== Plan Sync "+time.Now().UTC().Format(time.RFC3339)+" ===\nWorkspace: "+target+"\n"); err != nil {
		_ = file.Close()
		return operationalflow.PlanSyncResponse{}, err
	}
	execPath, err := os.Executable()
	if err != nil {
		_ = file.Close()
		return operationalflow.PlanSyncResponse{}, err
	}
	child := exec.Command(execPath, "hooks", "plan-sync", "--workspace", target, "--sync", "--log-file", logFile)
	child.Env = os.Environ()
	child.Stdout = file
	child.Stderr = file
	setDetachedProcessGroup(child)
	if err := child.Start(); err != nil {
		_ = file.Close()
		return operationalflow.PlanSyncResponse{}, err
	}
	_ = file.Close()
	return operationalflow.PlanSyncResponse{
		Workspace: target,
		Mode:      "background",
		LogFile:   logFile,
	}, nil
}

func defaultGraphMaintenanceLogFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".foxctl", "logs", "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "graph-maintenance-"+time.Now().Format("20060102-150405")+".log"), nil
}

func defaultPlanSyncLogFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".foxctl", "logs", "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "plan-sync-"+time.Now().Format("20060102-150405")+".log"), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func init() {
	rootCmd.AddCommand(newHooksCommand())
}
