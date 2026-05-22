package observability

import platformobs "github.com/joshka0/foxctl/internal/platform/observability"

// Event is the canonical foxctl observability event.
type Event = platformobs.Event

// Status represents the outcome of an operation.
type Status = platformobs.Status

const (
	// StatusOK indicates the operation completed successfully.
	StatusOK = platformobs.StatusOK
	// StatusError indicates the operation failed.
	StatusError = platformobs.StatusError
	// StatusCanceled indicates the operation was canceled (e.g., context canceled).
	StatusCanceled = platformobs.StatusCanceled
)

// Component constants for consistent tagging.
const (
	ComponentCLI   = platformobs.ComponentCLI
	ComponentWeb   = platformobs.ComponentWeb
	ComponentHook  = platformobs.ComponentHook
	ComponentSkill = platformobs.ComponentSkill
	ComponentJob   = platformobs.ComponentJob
	ComponentAgent = platformobs.ComponentAgent
	// ComponentContextBuilder identifies layered context assembly and retrieval operations.
	ComponentContextBuilder = platformobs.ComponentContextBuilder
)

// Operation constants for common operations.
const (
	OpSkillRun     = platformobs.OpSkillRun
	OpSkillCache   = platformobs.OpSkillCache
	OpHookExecute  = platformobs.OpHookExecute
	OpJobSubmit    = platformobs.OpJobSubmit
	OpJobComplete  = platformobs.OpJobComplete
	OpHTTPRequest  = platformobs.OpHTTPRequest
	OpSessionStart = platformobs.OpSessionStart
	OpSessionEnd   = platformobs.OpSessionEnd

	// OpAgentSpawn is the operation name for agent spawn.
	OpAgentSpawn     = platformobs.OpAgentSpawn
	OpAgentWait      = platformobs.OpAgentWait
	OpAgentComplete  = platformobs.OpAgentComplete
	OpAgentKill      = platformobs.OpAgentKill
	OpAgentIteration = platformobs.OpAgentIteration
	OpAgentTool      = platformobs.OpAgentTool

	// OpContextSemanticArtifactSearch is emitted for optional semantic artifact retrieval.
	OpContextSemanticArtifactSearch = platformobs.OpContextSemanticArtifactSearch
	// OpContextLayeredBundle is emitted when layered context assembly completes with stable refs.
	OpContextLayeredBundle = platformobs.OpContextLayeredBundle

	// OpMemoryQuery is emitted when canonical memory records are retrieved.
	OpMemoryQuery = platformobs.OpMemoryQuery
	// OpMemoryCuratorReport is emitted when the memory curator plans or applies lifecycle actions.
	OpMemoryCuratorReport = platformobs.OpMemoryCuratorReport
	// OpMemorySessionRestore is emitted when session restore selects memory records for context.
	OpMemorySessionRestore = platformobs.OpMemorySessionRestore
)

// Canonical event data keys for foxctl metadata carried inside foxcular.Event.Data.
const (
	DataKeyService     = platformobs.DataKeyService
	DataKeyVersion     = platformobs.DataKeyVersion
	DataKeyComponent   = platformobs.DataKeyComponent
	DataKeyCommand     = platformobs.DataKeyCommand
	DataKeySubtype     = platformobs.DataKeySubtype
	DataKeySessionID   = platformobs.DataKeySessionID
	DataKeyAgentID     = platformobs.DataKeyAgentID
	DataKeyWorkspaceID = platformobs.DataKeyWorkspaceID
	DataKeyJobID       = platformobs.DataKeyJobID
	DataKeyRetriable   = platformobs.DataKeyRetriable
)

// EventDataString reads a string metadata value from Event.Data.
func EventDataString(event *Event, key string) string {
	return platformobs.EventDataString(event, key)
}

// EventDataBoolPtr reads a boolean metadata value from Event.Data.
func EventDataBoolPtr(event *Event, key string) *bool {
	return platformobs.EventDataBoolPtr(event, key)
}

func eventDataString(event *Event, key string) string {
	return platformobs.EventDataString(event, key)
}

func eventDataBoolPtr(event *Event, key string) *bool {
	return platformobs.EventDataBoolPtr(event, key)
}
