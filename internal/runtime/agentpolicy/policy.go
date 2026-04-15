package agentpolicy

// Profile represents an agent capability profile.
// Profiles restrict which foxctl skills a subagent can run via bash.
type Profile string

const (
	// ProfileExplorer allows read-only code exploration skills.
	ProfileExplorer Profile = "explorer"

	// ProfileReviewer allows explorer skills plus analysis skills.
	ProfileReviewer Profile = "reviewer"

	// ProfileImplementer allows reviewer skills plus write/test skills.
	ProfileImplementer Profile = "implementer"

	// ProfileUnrestricted allows all skills (no restrictions).
	ProfileUnrestricted Profile = "unrestricted"
)

// IsValid returns true if the profile is a recognized profile.
func (p Profile) IsValid() bool {
	switch p {
	case ProfileExplorer, ProfileReviewer, ProfileImplementer, ProfileUnrestricted:
		return true
	default:
		return false
	}
}

// IsRestricted returns true if the profile has skill restrictions.
func (p Profile) IsRestricted() bool {
	return p != ProfileUnrestricted && p != ""
}

// String returns the string representation of the profile.
func (p Profile) String() string {
	return string(p)
}

// Decision represents the authorization decision for a bash command.
type Decision string

const (
	// DecisionAllow allows the command to proceed.
	DecisionAllow Decision = "allow"

	// DecisionBlock blocks the command from running.
	DecisionBlock Decision = "block"
)

// IsBlocking returns true if the decision blocks the operation.
func (d Decision) IsBlocking() bool {
	return d == DecisionBlock
}

// AuthorizationResult contains the result of authorizing a bash command.
type AuthorizationResult struct {
	// Decision is the authorization decision (allow or block).
	Decision Decision `json:"decision"`

	// Reason explains why the decision was made.
	Reason string `json:"reason"`

	// ParsedSkill is the skill name extracted from an foxctl run command.
	// Empty if the command is not an foxctl run command.
	ParsedSkill string `json:"parsed_skill,omitempty"`

	// Profile is the profile that was used for authorization.
	Profile Profile `json:"profile,omitempty"`
}

// ParsedCommand represents a parsed bash command.
type ParsedCommand struct {
	// IsFoxctlRun is true if this is an "foxctl run <skill>" command.
	IsFoxctlRun bool

	// Skill is the skill name (e.g., "code/symbols") if IsFoxctlRun is true.
	Skill string

	// RawCommand is the original command string.
	RawCommand string

	// EnvVars are environment variable assignments before the command.
	EnvVars map[string]string
}
