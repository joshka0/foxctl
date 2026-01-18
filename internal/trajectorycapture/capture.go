// Package trajectorycapture implements trajectory capture helpers.
package trajectorycapture

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/secrets"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/oklog/ulid/v2"
)

func extractArtifactDigest(data any) string {
	if data == nil {
		return ""
	}
	switch v := data.(type) {
	case map[string]any:
		if s, ok := v["artifact"].(string); ok {
			return strings.TrimSpace(s)
		}
	case map[string]string:
		return strings.TrimSpace(v["artifact"])
	}
	return ""
}

// StartOptions configures a new run capture.
type StartOptions struct {
	StorageRoot     string
	WorkspaceID     string
	Actor           string
	Source          trajectory.Source
	CLICommand      string
	ProtocolCommand string
	JobID           string
	CorrelationID   string
	AgentRole       string
	Input           []byte
	SessionID       string // AI coding tool session ID (optional)
}

// RunCapture tracks a single run's trajectory capture.
type RunCapture struct {
	store    trajectory.Store
	request  trajectory.UserRequestCapture
	traj     trajectory.Trajectory
	todoOp   string
	taskHint *trajectory.TaskHints
}

// Start initializes trajectory capture for a single run.
func Start(ctx context.Context, opts StartOptions) (*RunCapture, error) {
	if strings.TrimSpace(opts.StorageRoot) == "" {
		return nil, fmt.Errorf("trajectorycapture: storage root required")
	}
	if strings.TrimSpace(opts.WorkspaceID) == "" {
		return nil, fmt.Errorf("trajectorycapture: workspace id required")
	}
	if strings.TrimSpace(opts.Actor) == "" {
		opts.Actor = "actor:human:cli"
	}
	if opts.Source == "" {
		opts.Source = trajectory.SourceCLI
	}
	if strings.TrimSpace(opts.CorrelationID) == "" {
		opts.CorrelationID = ulid.Make().String()
	}

	store, err := trajectory.Open(ctx, opts.StorageRoot)
	if err != nil {
		return nil, err
	}

	taskHints, todoOp := deriveTaskHints(opts.ProtocolCommand, opts.Input)

	text := secrets.Redact(strings.TrimSpace(opts.CLICommand))
	if text == "" {
		text = opts.ProtocolCommand
	}

	ur := trajectory.UserRequestCapture{
		WorkspaceID: opts.WorkspaceID,
		Actor:       opts.Actor,
		Source:      opts.Source,
		Text:        text,
		CommandContext: &trajectory.CommandContext{
			CLICommand:      secrets.Redact(opts.CLICommand),
			ProtocolCommand: opts.ProtocolCommand,
			JobID:           opts.JobID,
			TraceID:         opts.CorrelationID,
		},
		TaskHints: taskHints,
	}
	ur, err = store.InsertUserRequest(ctx, ur)
	if err != nil {
		// Cleanup on error; error is not actionable.
		_ = store.Close() //nolint:errcheck
		return nil, err
	}

	taskIDs := []string{}
	if taskHints != nil && taskHints.TaskID != "" {
		taskIDs = append(taskIDs, taskHints.TaskID)
	}

	traj := trajectory.Trajectory{
		WorkspaceID:    opts.WorkspaceID,
		RootRequestID:  ur.ID,
		TaskIDs:        taskIDs,
		EpicID:         "",
		AgentRole:      strings.TrimSpace(opts.AgentRole),
		JobID:          strings.TrimSpace(opts.JobID),
		TraceID:        opts.CorrelationID,
		Status:         trajectory.StatusPartial,
		Summary:        "",
		ArtifactDigest: "",
		SessionID:      strings.TrimSpace(opts.SessionID),
	}
	if taskHints != nil {
		traj.EpicID = strings.TrimSpace(taskHints.EpicID)
	}

	traj, err = store.InsertTrajectory(ctx, traj)
	if err != nil {
		// Cleanup on error; error is not actionable.
		_ = store.Close() //nolint:errcheck
		return nil, err
	}

	meta := &trajectory.EventMeta{
		TraceID:    opts.CorrelationID,
		JobID:      strings.TrimSpace(opts.JobID),
		CreatedBy:  "agentctl",
		TaskID:     "",
		EpicID:     "",
		CASDigest:  "",
		ActorID:    "",
		JobAttempt: 0,
	}
	if taskHints != nil {
		meta.TaskID = strings.TrimSpace(taskHints.TaskID)
		meta.EpicID = strings.TrimSpace(taskHints.EpicID)
	}

	dataInline := map[string]any{
		"summary": secrets.Redact(fmt.Sprintf("user request: %s", opts.ProtocolCommand)),
	}
	if todoOp != "" {
		dataInline["operation"] = todoOp
	}
	dataInline = secrets.RedactMap(dataInline)

	_, err = store.InsertEvent(ctx, trajectory.Event{
		TrajectoryID: traj.ID,
		Kind:         trajectory.EventKindUserRequest,
		Actor:        opts.Actor,
		Command:      opts.ProtocolCommand,
		Status:       "ok",
		DataInline:   dataInline,
		Meta:         meta,
	})
	if err != nil {
		// Cleanup on error; error is not actionable.
		_ = store.Close() //nolint:errcheck
		return nil, err
	}

	return &RunCapture{store: store, request: ur, traj: traj, todoOp: todoOp, taskHint: taskHints}, nil
}

// Close releases resources.
func (c *RunCapture) Close() error {
	if c == nil || c.store == nil {
		return nil
	}
	err := c.store.Close()
	c.store = nil
	return err
}

// SetJobID updates the associated job id.
func (c *RunCapture) SetJobID(ctx context.Context, jobID string) error {
	if c == nil {
		return nil
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil
	}
	if c.traj.JobID == jobID {
		return nil
	}
	c.traj.JobID = jobID
	return c.store.UpdateTrajectory(ctx, c.traj)
}

// CaptureHookCall persists a hook call event for hooks/* runs.
func (c *RunCapture) CaptureHookCall(ctx context.Context, command string, input []byte, jobID string, correlationID string) error {
	if c == nil {
		return nil
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	dataInline := map[string]any{
		"summary": secrets.Redact(fmt.Sprintf("hook call: %s", command)),
	}
	preview := map[string]any{}
	var m map[string]any
	if err := json.Unmarshal(input, &m); err == nil && m != nil {
		for _, k := range []string{"event", "workspace_root", "session_id", "transcript_path", "tool_name"} {
			if v, ok := m[k].(string); ok {
				v = strings.TrimSpace(v)
				if v != "" {
					preview[k] = v
				}
			}
		}
		if _, ok := m["tool_input"]; ok {
			preview["tool_input_present"] = true
		}
		if _, ok := m["tool_response"]; ok {
			preview["tool_response_present"] = true
		}
	} else if len(input) > 0 {
		preview["input_bytes"] = len(input)
	}
	if len(preview) > 0 {
		dataInline["hook_input"] = preview
	}
	dataInline = secrets.RedactMap(dataInline)

	traceID := strings.TrimSpace(correlationID)
	if traceID == "" {
		traceID = strings.TrimSpace(c.traj.TraceID)
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		jobID = strings.TrimSpace(c.traj.JobID)
	}

	meta := &trajectory.EventMeta{
		TraceID:   traceID,
		JobID:     jobID,
		CreatedBy: "agentctl",
		CASDigest: "",
	}
	if c.taskHint != nil {
		meta.TaskID = strings.TrimSpace(c.taskHint.TaskID)
		meta.EpicID = strings.TrimSpace(c.taskHint.EpicID)
	}

	_, err := c.store.InsertEvent(ctx, trajectory.Event{
		TrajectoryID: c.traj.ID,
		Kind:         trajectory.EventKindHookCall,
		Actor:        strings.TrimSpace(c.request.Actor),
		Command:      command,
		Status:       "ok",
		DataInline:   dataInline,
		DataArtifact: "",
		Meta:         meta,
	})
	return err
}

// CaptureResult persists a result envelope as an event and updates trajectory status.
func (c *RunCapture) CaptureResult(ctx context.Context, envBytes []byte, jobID string, correlationID string) error {
	if c == nil {
		return nil
	}
	var env envelope.Envelope
	if err := json.Unmarshal(envBytes, &env); err != nil {
		return fmt.Errorf("trajectorycapture: decode envelope: %w", err)
	}

	kind := deriveResultKind(env, c.todoOp)
	dataInline := buildResultInlineData(env, c.todoOp)
	artifact := resolveResultArtifact(env)
	meta := c.buildResultMeta(dataInline, jobID, correlationID, artifact)

	_, err := c.store.InsertEvent(ctx, trajectory.Event{
		TrajectoryID: c.traj.ID,
		Kind:         kind,
		Actor:        "",
		Command:      env.Command,
		Status:       env.Status,
		DataInline:   dataInline,
		DataArtifact: artifact,
		Meta:         meta,
	})
	if err != nil {
		return err
	}

	c.updateTrajectoryStatus(env, dataInline)
	return c.store.UpdateTrajectory(ctx, c.traj)
}

func deriveResultKind(env envelope.Envelope, todoOp string) trajectory.EventKind {
	kind := trajectory.EventKindToolResult
	if strings.HasPrefix(env.Command, "hooks/") {
		kind = trajectory.EventKindHookResult
	}
	if env.Command == "todo/manage" {
		switch todoOp {
		case "add", "complete", "set_active", "ensure_active", "clear_active", "get_active", "plan":
			kind = trajectory.EventKindTaskTransition
		case "review_request":
			kind = trajectory.EventKindReviewRequest
		case "review_status":
			kind = trajectory.EventKindReviewResult
		default:
			kind = trajectory.EventKindToolResult
		}
	}
	return kind
}

func buildResultInlineData(env envelope.Envelope, todoOp string) map[string]any {
	dataInline := map[string]any{}
	if m, ok := env.Data.(map[string]any); ok {
		if s, ok := m["summary"].(string); ok && strings.TrimSpace(s) != "" {
			dataInline["summary"] = s
		}
		if env.Command == "todo/manage" {
			if task, ok := m["task"].(map[string]any); ok {
				if id, ok := task["id"].(string); ok && id != "" {
					dataInline["task_id"] = id
				}
				if rid, ok := task["last_review_id"].(string); ok && rid != "" {
					dataInline["review_id"] = rid
				}
			}
			if id, ok := m["task_id"].(string); ok && id != "" {
				dataInline["task_id"] = id
			}
			if rid, ok := m["last_review_id"].(string); ok && rid != "" {
				dataInline["review_id"] = rid
			}
		}
	}
	if dataInline["summary"] == nil {
		if env.Status == envelope.StatusError {
			dataInline["summary"] = env.Error.Message
		}
	}
	if todoOp != "" {
		dataInline["operation"] = todoOp
	}
	return secrets.RedactMap(dataInline)
}

func resolveResultArtifact(env envelope.Envelope) string {
	artifact := extractArtifactDigest(env.Data)
	if artifact == "" {
		artifact = strings.TrimSpace(env.Meta.CASDigest)
	}
	return artifact
}

func (c *RunCapture) buildResultMeta(
	dataInline map[string]any,
	jobID string,
	correlationID string,
	artifact string,
) *trajectory.EventMeta {
	meta := &trajectory.EventMeta{
		TraceID:   strings.TrimSpace(correlationID),
		JobID:     strings.TrimSpace(jobID),
		CreatedBy: "agentctl",
		CASDigest: artifact,
	}
	if c.taskHint != nil {
		meta.TaskID = strings.TrimSpace(c.taskHint.TaskID)
		meta.EpicID = strings.TrimSpace(c.taskHint.EpicID)
	}
	if rid, ok := dataInline["review_id"].(string); ok && rid != "" {
		meta.ReviewID = rid
	}
	if tid, ok := dataInline["task_id"].(string); ok && tid != "" {
		meta.TaskID = tid
		if !containsString(c.traj.TaskIDs, tid) {
			c.traj.TaskIDs = append(c.traj.TaskIDs, tid)
		}
	}
	return meta
}

func (c *RunCapture) updateTrajectoryStatus(env envelope.Envelope, dataInline map[string]any) {
	if env.Status == envelope.StatusOK {
		c.traj.Status = trajectory.StatusOK
	} else {
		c.traj.Status = trajectory.StatusError
	}
	if s, ok := dataInline["summary"].(string); ok {
		c.traj.Summary = strings.TrimSpace(secrets.Redact(s))
	}
}

func deriveTaskHints(command string, input []byte) (*trajectory.TaskHints, string) {
	cmd := strings.TrimSpace(command)
	var m map[string]any
	if err := json.Unmarshal(input, &m); err != nil {
		return nil, ""
	}

	if cmd == "dspy-agent/spawn" {
		th := &trajectory.TaskHints{}
		if s, ok := m["task_id"].(string); ok {
			th.TaskID = strings.TrimSpace(s)
		}
		if s, ok := m["epic_id"].(string); ok {
			th.EpicID = strings.TrimSpace(s)
		}
		if th.TaskID == "" && th.EpicID == "" {
			return nil, ""
		}
		return th, ""
	}

	if cmd != "todo/manage" {
		return nil, ""
	}
	op, _ := m["operation"].(string)
	op = strings.ToLower(strings.TrimSpace(op))
	th := &trajectory.TaskHints{}
	switch op {
	case "complete":
		if c, ok := m["complete"].(map[string]any); ok {
			if id, ok := c["id"].(string); ok {
				th.TaskID = id
			}
		}
	case "set_active":
		if c, ok := m["set_active"].(map[string]any); ok {
			if id, ok := c["task_id"].(string); ok {
				th.TaskID = id
			}
		}
	case "review_request":
		if c, ok := m["review_request"].(map[string]any); ok {
			if id, ok := c["task_id"].(string); ok {
				th.TaskID = id
			}
		}
	case "review_status":
		if c, ok := m["review_status"].(map[string]any); ok {
			if id, ok := c["task_id"].(string); ok {
				th.TaskID = id
			}
		}
	case "plan":
		if c, ok := m["plan"].(map[string]any); ok {
			if id, ok := c["attach_to_task_id"].(string); ok {
				th.EpicID = id
			}
		}
	}
	if th.TaskID == "" && th.EpicID == "" {
		return nil, op
	}
	return th, op
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
