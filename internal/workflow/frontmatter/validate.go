package frontmatter

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultLinearEndpoint      = "https://api.linear.app/graphql"
	defaultPollingIntervalMS   = 30000
	defaultHooksTimeoutMS      = 60000
	defaultMaxConcurrentAgents = 10
	defaultMaxRetryBackoffMS   = 300000
	defaultMaxTurns            = 20
	defaultCodexCommand        = "codex app-server"
	defaultTurnTimeoutMS       = 3600000
	defaultReadTimeoutMS       = 5000
	defaultStallTimeoutMS      = 300000
	defaultWorkspaceSubdir     = "symphony_workspaces"
	defaultLinearAPIKeyEnv     = "LINEAR_API_KEY"
)

var (
	errUnsupportedTrackerKind = errors.New("workflow frontmatter: unsupported tracker kind")
	errMissingTrackerAPIKey   = errors.New("workflow frontmatter: missing tracker api_key")
	errMissingProjectSlug     = errors.New("workflow frontmatter: missing tracker project_slug")
	errMissingCodexCommand    = errors.New("workflow frontmatter: missing codex command")
)

// DefaultConfig returns baseline scheduler/runtime config defaults.
func DefaultConfig() Config {
	return Config{
		Tracker: TrackerConfig{
			Endpoint:       defaultLinearEndpoint,
			ActiveStates:   []string{"Todo", "In Progress"},
			TerminalStates: []string{"Closed", "Cancelled", "Canceled", "Duplicate", "Done"},
		},
		Polling: PollingConfig{
			IntervalMS: defaultPollingIntervalMS,
		},
		Workspace: WorkspaceConfig{
			Root: filepath.Join(os.TempDir(), defaultWorkspaceSubdir),
		},
		Hooks: HooksConfig{
			TimeoutMS: defaultHooksTimeoutMS,
		},
		Agent: AgentConfig{
			MaxConcurrentAgents: defaultMaxConcurrentAgents,
			MaxRetryBackoffMS:   defaultMaxRetryBackoffMS,
			MaxTurns:            defaultMaxTurns,
		},
		Codex: CodexConfig{
			Command:        defaultCodexCommand,
			TurnTimeoutMS:  defaultTurnTimeoutMS,
			ReadTimeoutMS:  defaultReadTimeoutMS,
			StallTimeoutMS: defaultStallTimeoutMS,
		},
	}
}

// DecodeConfig decodes frontmatter map values into typed config with defaults.
func DecodeConfig(front map[string]any, opts DecodeOptions) (Config, error) {
	cfg := DefaultConfig()
	if front == nil {
		return cfg, nil
	}

	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	tracker := getMap(front, "tracker")
	cfg.Tracker.Kind = strings.TrimSpace(resolveExactEnvToken(stringValue(tracker, "kind"), getenv))
	cfg.Tracker.Endpoint = strings.TrimSpace(resolveExactEnvToken(stringValueDefault(tracker, "endpoint", cfg.Tracker.Endpoint), getenv))
	cfg.Tracker.APIKey = strings.TrimSpace(resolveExactEnvToken(stringValue(tracker, "api_key"), getenv))
	cfg.Tracker.ProjectSlug = strings.TrimSpace(resolveExactEnvToken(stringValue(tracker, "project_slug"), getenv))
	if cfg.Tracker.Kind == "linear" && cfg.Tracker.APIKey == "" {
		cfg.Tracker.APIKey = strings.TrimSpace(getenv(defaultLinearAPIKeyEnv))
	}
	if states, ok := stringSliceValue(tracker, "active_states"); ok {
		cfg.Tracker.ActiveStates = states
	}
	if states, ok := stringSliceValue(tracker, "terminal_states"); ok {
		cfg.Tracker.TerminalStates = states
	}

	polling := getMap(front, "polling")
	if v, present, err := intValueStrict(polling, "interval_ms"); err != nil {
		return cfg, fieldDecodeError("polling.interval_ms", err)
	} else if present {
		cfg.Polling.IntervalMS = v
	}
	if cfg.Polling.IntervalMS <= 0 {
		cfg.Polling.IntervalMS = defaultPollingIntervalMS
	}

	workspace := getMap(front, "workspace")
	wsRoot := cfg.Workspace.Root
	if root, present := stringValuePresent(workspace, "root"); present {
		wsRoot = resolveExactEnvToken(root, getenv)
		if strings.TrimSpace(wsRoot) == "" {
			return cfg, fieldDecodeError("workspace.root", errors.New("resolved empty value"))
		}
	}
	expandedWorkspace, err := expandPath(wsRoot, opts.BaseDir, getenv)
	if err != nil {
		return cfg, fieldDecodeError("workspace.root", err)
	}
	cfg.Workspace.Root = expandedWorkspace

	hooks := getMap(front, "hooks")
	cfg.Hooks.AfterCreate = stringValue(hooks, "after_create")
	cfg.Hooks.BeforeRun = stringValue(hooks, "before_run")
	cfg.Hooks.AfterRun = stringValue(hooks, "after_run")
	cfg.Hooks.BeforeRemove = stringValue(hooks, "before_remove")
	if v, present, err := intValueStrict(hooks, "timeout_ms"); err != nil {
		return cfg, fieldDecodeError("hooks.timeout_ms", err)
	} else if present {
		cfg.Hooks.TimeoutMS = v
	}
	if cfg.Hooks.TimeoutMS <= 0 {
		cfg.Hooks.TimeoutMS = defaultHooksTimeoutMS
	}

	agent := getMap(front, "agent")
	if v, present, err := intValueStrict(agent, "max_concurrent_agents"); err != nil {
		return cfg, fieldDecodeError("agent.max_concurrent_agents", err)
	} else if present {
		cfg.Agent.MaxConcurrentAgents = v
	}
	if cfg.Agent.MaxConcurrentAgents <= 0 {
		cfg.Agent.MaxConcurrentAgents = defaultMaxConcurrentAgents
	}
	if v, present, err := intValueStrict(agent, "max_retry_backoff_ms"); err != nil {
		return cfg, fieldDecodeError("agent.max_retry_backoff_ms", err)
	} else if present {
		cfg.Agent.MaxRetryBackoffMS = v
	}
	if cfg.Agent.MaxRetryBackoffMS <= 0 {
		cfg.Agent.MaxRetryBackoffMS = defaultMaxRetryBackoffMS
	}
	if v, present, err := intValueStrict(agent, "max_turns"); err != nil {
		return cfg, fieldDecodeError("agent.max_turns", err)
	} else if present {
		cfg.Agent.MaxTurns = v
	}
	if cfg.Agent.MaxTurns <= 0 {
		cfg.Agent.MaxTurns = defaultMaxTurns
	}
	cfg.Agent.MaxConcurrentAgentsByState = parsePositiveIntMap(agent["max_concurrent_agents_by_state"])

	codex := getMap(front, "codex")
	cfg.Codex.Command = strings.TrimSpace(resolveExactEnvToken(stringValueDefault(codex, "command", cfg.Codex.Command), getenv))
	cfg.Codex.ApprovalPolicy = stringValue(codex, "approval_policy")
	cfg.Codex.ThreadSandbox = stringValue(codex, "thread_sandbox")
	cfg.Codex.TurnSandboxPolicy = stringValue(codex, "turn_sandbox_policy")
	if v, present, err := intValueStrict(codex, "turn_timeout_ms"); err != nil {
		return cfg, fieldDecodeError("codex.turn_timeout_ms", err)
	} else if present {
		cfg.Codex.TurnTimeoutMS = v
	}
	if cfg.Codex.TurnTimeoutMS <= 0 {
		cfg.Codex.TurnTimeoutMS = defaultTurnTimeoutMS
	}
	if v, present, err := intValueStrict(codex, "read_timeout_ms"); err != nil {
		return cfg, fieldDecodeError("codex.read_timeout_ms", err)
	} else if present {
		cfg.Codex.ReadTimeoutMS = v
	}
	if cfg.Codex.ReadTimeoutMS <= 0 {
		cfg.Codex.ReadTimeoutMS = defaultReadTimeoutMS
	}
	if v, present, err := intValueStrict(codex, "stall_timeout_ms"); err != nil {
		return cfg, fieldDecodeError("codex.stall_timeout_ms", err)
	} else if present {
		cfg.Codex.StallTimeoutMS = v
	}
	// Intentional: <=0 disables stall detection.

	server := getMap(front, "server")
	if port, present, err := intValueStrict(server, "port"); err != nil {
		return cfg, fieldDecodeError("server.port", err)
	} else if present {
		cfg.Server.Port = &port
	}

	if err := ValidateConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// ValidateConfig validates broad configuration invariants.
func ValidateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.Workspace.Root) == "" {
		return errors.New("workflow frontmatter: workspace.root must not be empty")
	}
	if cfg.Polling.IntervalMS <= 0 {
		return errors.New("workflow frontmatter: polling.interval_ms must be > 0")
	}
	if cfg.Hooks.TimeoutMS <= 0 {
		return errors.New("workflow frontmatter: hooks.timeout_ms must be > 0")
	}
	if cfg.Agent.MaxConcurrentAgents <= 0 {
		return errors.New("workflow frontmatter: agent.max_concurrent_agents must be > 0")
	}
	if cfg.Agent.MaxRetryBackoffMS <= 0 {
		return errors.New("workflow frontmatter: agent.max_retry_backoff_ms must be > 0")
	}
	if cfg.Agent.MaxTurns <= 0 {
		return errors.New("workflow frontmatter: agent.max_turns must be > 0")
	}
	if cfg.Codex.TurnTimeoutMS <= 0 {
		return errors.New("workflow frontmatter: codex.turn_timeout_ms must be > 0")
	}
	if cfg.Codex.ReadTimeoutMS <= 0 {
		return errors.New("workflow frontmatter: codex.read_timeout_ms must be > 0")
	}
	if cfg.Server.Port != nil {
		if *cfg.Server.Port < 0 || *cfg.Server.Port > 65535 {
			return errors.New("workflow frontmatter: server.port must be between 0 and 65535")
		}
	}
	return nil
}

// ValidateDispatch validates fields required to dispatch orchestration runs.
func ValidateDispatch(cfg Config) error {
	if err := ValidateConfig(cfg); err != nil {
		return err
	}
	kind := strings.TrimSpace(strings.ToLower(cfg.Tracker.Kind))
	if kind == "" {
		return fmt.Errorf("%w: tracker.kind is required", errUnsupportedTrackerKind)
	}
	if kind != "linear" {
		return fmt.Errorf("%w: %s", errUnsupportedTrackerKind, kind)
	}
	if strings.TrimSpace(cfg.Tracker.APIKey) == "" {
		return errMissingTrackerAPIKey
	}
	if strings.TrimSpace(cfg.Tracker.ProjectSlug) == "" {
		return errMissingProjectSlug
	}
	if strings.TrimSpace(cfg.Codex.Command) == "" {
		return errMissingCodexCommand
	}
	return nil
}

func fieldDecodeError(field string, err error) error {
	return fmt.Errorf("workflow frontmatter: invalid %s: %w", field, err)
}

func resolveExactEnvToken(v string, getenv func(string) string) string {
	v = strings.TrimSpace(v)
	if v == "" || getenv == nil {
		return v
	}

	key := ""
	switch {
	case strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}"):
		key = strings.TrimSuffix(strings.TrimPrefix(v, "${"), "}")
	case strings.HasPrefix(v, "$"):
		key = strings.TrimPrefix(v, "$")
	}
	if key == "" || !isEnvName(key) {
		return v
	}
	return strings.TrimSpace(getenv(key))
}

func isEnvName(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			continue
		case i > 0 && r >= '0' && r <= '9':
			continue
		default:
			return false
		}
	}
	return true
}

var pathEnvPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func expandPath(pathValue string, baseDir string, getenv func(string) string) (string, error) {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		return pathValue, nil
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	expanded, err := expandPathEnvStrict(pathValue, getenv)
	if err != nil {
		return "", err
	}
	pathValue = expanded
	if strings.HasPrefix(pathValue, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			switch pathValue {
			case "~":
				pathValue = home
			default:
				if strings.HasPrefix(pathValue, "~/") {
					pathValue = filepath.Join(home, strings.TrimPrefix(pathValue, "~/"))
				}
			}
		}
	}

	pathValue = filepath.Clean(pathValue)
	if !filepath.IsAbs(pathValue) {
		if strings.TrimSpace(baseDir) == "" {
			return "", errors.New("relative path requires BaseDir")
		}
		pathValue = filepath.Join(baseDir, pathValue)
	}
	if abs, err := filepath.Abs(pathValue); err == nil {
		pathValue = abs
	}
	return filepath.Clean(pathValue), nil
}

func getMap(root map[string]any, key string) map[string]any {
	if root == nil {
		return nil
	}
	raw, ok := root[key]
	if !ok || raw == nil {
		return nil
	}
	if out, ok := raw.(map[string]any); ok {
		return out
	}
	return nil
}

func stringValue(root map[string]any, key string) string {
	v, _ := stringValuePresent(root, key)
	return v
}

func stringValuePresent(root map[string]any, key string) (string, bool) {
	if root == nil {
		return "", false
	}
	raw, ok := root[key]
	if !ok || raw == nil {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), true
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v)), true
	}
}

func stringValueDefault(root map[string]any, key, fallback string) string {
	if v, ok := stringValuePresent(root, key); ok && v != "" {
		return v
	}
	return fallback
}

func intValueStrict(root map[string]any, key string) (value int, present bool, err error) {
	if root == nil {
		return 0, false, nil
	}
	raw, ok := root[key]
	if !ok || raw == nil {
		return 0, false, nil
	}
	switch v := raw.(type) {
	case int:
		return v, true, nil
	case int64:
		return int(v), true, nil
	case float64:
		if math.Trunc(v) != v {
			return 0, true, fmt.Errorf("must be an integer, got %v", v)
		}
		return int(v), true, nil
	case string:
		n, parseErr := strconv.Atoi(strings.TrimSpace(v))
		if parseErr != nil {
			return 0, true, fmt.Errorf("must be an integer, got %q", v)
		}
		return n, true, nil
	default:
		return 0, true, fmt.Errorf("unsupported numeric type %T", raw)
	}
}

func stringSliceValue(root map[string]any, key string) ([]string, bool) {
	if root == nil {
		return nil, false
	}
	raw, ok := root[key]
	if !ok || raw == nil {
		return nil, false
	}
	return normalizeStringSlice(raw)
}

func normalizeStringSlice(raw any) ([]string, bool) {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out, true
	case string:
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, item := range parts {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func parsePositiveIntMap(raw any) map[string]int {
	root, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]int)
	for state, value := range root {
		key := strings.ToLower(strings.TrimSpace(state))
		if key == "" {
			continue
		}
		n, valid := strictPositiveInt(value)
		if !valid {
			continue
		}
		out[key] = n
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func strictPositiveInt(raw any) (int, bool) {
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return v, true
		}
	case int64:
		if v > 0 {
			return int(v), true
		}
	case float64:
		if math.Trunc(v) == v && v > 0 {
			return int(v), true
		}
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

func expandPathEnvStrict(pathValue string, getenv func(string) string) (string, error) {
	missing := make(map[string]struct{})
	out := pathEnvPattern.ReplaceAllStringFunc(pathValue, func(token string) string {
		key := ""
		if strings.HasPrefix(token, "${") && strings.HasSuffix(token, "}") {
			key = strings.TrimSuffix(strings.TrimPrefix(token, "${"), "}")
		} else {
			key = strings.TrimPrefix(token, "$")
		}
		if key == "" {
			return token
		}
		value := getenv(key)
		if strings.TrimSpace(value) == "" {
			missing[key] = struct{}{}
			return token
		}
		return value
	})
	if len(missing) == 0 {
		return out, nil
	}
	names := make([]string, 0, len(missing))
	for k := range missing {
		names = append(names, k)
	}
	sort.Strings(names)
	return "", fmt.Errorf("unresolved env var(s): %s", strings.Join(names, ", "))
}
