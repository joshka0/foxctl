package frontmatter

// Document is the parsed WORKFLOW.md payload.
//
// Config contains the decoded YAML frontmatter key/value pairs.
// PromptTemplate contains the markdown body (trimmed).
type Document struct {
	Config         map[string]any
	PromptTemplate string
	HasFrontMatter bool
}

// Config is the typed orchestration configuration derived from frontmatter.
type Config struct {
	Tracker   TrackerConfig   `json:"tracker"`
	Polling   PollingConfig   `json:"polling"`
	Workspace WorkspaceConfig `json:"workspace"`
	Hooks     HooksConfig     `json:"hooks"`
	Agent     AgentConfig     `json:"agent"`
	Codex     CodexConfig     `json:"codex"`
	Server    ServerConfig    `json:"server,omitempty"`
}

type TrackerConfig struct {
	Kind           string            `json:"kind"`
	Endpoint       string            `json:"endpoint"`
	APIKey         string            `json:"api_key"`
	ProjectSlug    string            `json:"project_slug"`
	ActiveStates   []string          `json:"active_states,omitempty"`
	TerminalStates []string          `json:"terminal_states,omitempty"`
	Extra          map[string]string `json:"-"`
}

type PollingConfig struct {
	IntervalMS int `json:"interval_ms"`
}

type WorkspaceConfig struct {
	Root string `json:"root"`
}

type HooksConfig struct {
	AfterCreate  string `json:"after_create,omitempty"`
	BeforeRun    string `json:"before_run,omitempty"`
	AfterRun     string `json:"after_run,omitempty"`
	BeforeRemove string `json:"before_remove,omitempty"`
	TimeoutMS    int    `json:"timeout_ms"`
}

type AgentConfig struct {
	MaxConcurrentAgents        int            `json:"max_concurrent_agents"`
	MaxRetryBackoffMS          int            `json:"max_retry_backoff_ms"`
	MaxTurns                   int            `json:"max_turns"`
	MaxConcurrentAgentsByState map[string]int `json:"max_concurrent_agents_by_state,omitempty"`
}

type CodexConfig struct {
	Command           string `json:"command"`
	ApprovalPolicy    string `json:"approval_policy,omitempty"`
	ThreadSandbox     string `json:"thread_sandbox,omitempty"`
	TurnSandboxPolicy string `json:"turn_sandbox_policy,omitempty"`
	TurnTimeoutMS     int    `json:"turn_timeout_ms"`
	ReadTimeoutMS     int    `json:"read_timeout_ms"`
	StallTimeoutMS    int    `json:"stall_timeout_ms"`
}

type ServerConfig struct {
	Port *int `json:"port,omitempty"`
}

// DecodeOptions controls frontmatter decode behavior.
type DecodeOptions struct {
	Getenv  func(string) string
	BaseDir string
}
