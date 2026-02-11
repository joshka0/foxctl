package identity

import "strings"

// Principal represents the authenticated identity making a request.
// It unifies platform user identity, agent actor identity, and tenant context
// into a single struct that flows through context.Context.
type Principal struct {
	// TenantID identifies the organizational tenant (e.g., Teams tenant GUID).
	// Empty for single-tenant deployments.
	TenantID string `json:"tenant_id,omitempty"`

	// UserID is the platform-specific user identifier.
	// Discord snowflake, Telegram user ID, Teams AAD object ID, etc.
	UserID string `json:"user_id,omitempty"`

	// Username is the display name (best-effort, not unique).
	Username string `json:"username,omitempty"`

	// ActorID identifies the agent actor processing this request.
	// Format: "actor:agent:<name>" or empty for direct user requests.
	ActorID string `json:"actor_id,omitempty"`

	// Platform identifies the origin: "discord", "telegram", "teams", "web", "cli".
	Platform string `json:"platform,omitempty"`

	// WorkspaceID is the stable workspace identifier (hashed).
	WorkspaceID string `json:"workspace_id,omitempty"`

	// WorkspaceRoot is the absolute filesystem path of the workspace.
	WorkspaceRoot string `json:"workspace_root,omitempty"`

	// SessionID is the durable session identifier.
	SessionID string `json:"session_id,omitempty"`

	// Roles are the principal's authorization roles (populated by auth middleware).
	Roles []string `json:"roles,omitempty"`
}

// IsAnonymous returns true if no user or actor identity is set.
func (p Principal) IsAnonymous() bool {
	return p.UserID == "" && p.ActorID == ""
}

// Subject returns a stable identity subject string.
// Format:
// - "user:<platform>:<userID>"
// - "actor:<actorID>"
// Returns empty string if the principal is anonymous.
func (p Principal) Subject() string {
	if p.UserID != "" {
		return "user:" + p.Platform + ":" + p.UserID
	}
	if p.ActorID != "" {
		// Preserve existing "actor:*" IDs (common in the codebase), while still
		// providing a stable subject prefix for raw actor IDs.
		if strings.HasPrefix(p.ActorID, "actor:") {
			return p.ActorID
		}
		return "actor:" + p.ActorID
	}
	return ""
}

// ConversationKey returns a tenant-scoped conversation key.
// Format: "{platform}:{tenantID}:{rawKey}" or "{platform}::{rawKey}" if no tenant.
func (p Principal) ConversationKey(rawKey string) string {
	if p.TenantID != "" {
		return p.Platform + ":" + p.TenantID + ":" + rawKey
	}
	return p.Platform + "::" + rawKey
}
