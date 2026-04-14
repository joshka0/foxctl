// Package services defines the newer v2 command and orchestration services.
//
// Package-topology note:
// internal/v2/services is reserved for the agent/runtime/orchestration lane.
// It replaces specific legacy runtime-management surfaces such as
// internal/runtime/execution/agentmanager and parts of internal/agent/runtime; it is
// not the default destination for new context, retrieval, storage, or interface
// packages.
package services
