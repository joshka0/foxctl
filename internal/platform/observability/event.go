package observability

import "github.com/joshka0/foxcular"

// Event is the canonical foxctl observability event.
type Event = foxcular.Event

// Status represents the outcome of an operation.
type Status = foxcular.Status

const (
	// StatusOK indicates the operation completed successfully.
	StatusOK = foxcular.StatusOK
	// StatusError indicates the operation failed.
	StatusError = foxcular.StatusError
	// StatusCanceled indicates the operation was canceled.
	StatusCanceled = foxcular.StatusCanceled
)

const (
	ComponentCLI   = "cli"
	ComponentWeb   = "web"
	ComponentHook  = "hook"
	ComponentSkill = "skill"
	ComponentJob   = "job"
	ComponentAgent = "agent"
	// ComponentContextBuilder identifies layered context assembly and retrieval operations.
	ComponentContextBuilder = "contextbuilder"
)

const (
	OpSkillRun     = "skill.run"
	OpSkillCache   = "skill.cache"
	OpHookExecute  = "hook.execute"
	OpJobSubmit    = "job.submit"
	OpJobComplete  = "job.complete"
	OpHTTPRequest  = "http.request"
	OpSessionStart = "session.start"
	OpSessionEnd   = "session.end"

	OpAgentSpawn     = "agent.spawn"
	OpAgentWait      = "agent.wait"
	OpAgentComplete  = "agent.complete"
	OpAgentKill      = "agent.kill"
	OpAgentIteration = "agent.iteration"
	OpAgentTool      = "agent.tool"

	OpContextSemanticArtifactSearch = "context.semantic_artifact_search"
	OpContextLayeredBundle          = "context.layered_bundle"

	OpMemoryQuery          = "memory.query"
	OpMemoryCuratorReport  = "memory.curator_report"
	OpMemorySessionRestore = "memory.session_restore"
)

const (
	DataKeyService     = "service"
	DataKeyVersion     = "version"
	DataKeyComponent   = "component"
	DataKeyCommand     = "command"
	DataKeySubtype     = "subtype"
	DataKeySessionID   = "session_id"
	DataKeyAgentID     = "agent_id"
	DataKeyWorkspaceID = "workspace_id"
	DataKeyJobID       = "job_id"
	DataKeyRetriable   = "retriable"
)

// EventDataString reads a string metadata value from Event.Data.
func EventDataString(event *Event, key string) string {
	if event == nil || event.Data == nil {
		return ""
	}
	value, _ := event.Data[key].(string)
	return value
}

// EventDataBoolPtr reads a boolean metadata value from Event.Data.
func EventDataBoolPtr(event *Event, key string) *bool {
	if event == nil || event.Data == nil {
		return nil
	}
	value, ok := event.Data[key].(bool)
	if !ok {
		return nil
	}
	return &value
}
