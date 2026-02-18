package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const envV2Commands = "AGENTCTL_V2_COMMANDS"
const envV2ShadowCommands = "AGENTCTL_V2_SHADOW_COMMANDS"

var supportedCommands = map[string]struct{}{
	"spawn": {},
	"ask":   {},
	"run":   {},
	"list":  {},
	"kill":  {},
}

// V2Flags stores the normalized v2-enabled command set.
type V2Flags struct {
	enabled map[string]struct{}
}

// ParseV2Commands parses AGENTCTL_V2_COMMANDS-like comma-separated command sets.
func ParseV2Commands(raw string) (V2Flags, error) {
	flags := V2Flags{enabled: map[string]struct{}{}}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return flags, nil
	}

	parts := strings.Split(trimmed, ",")
	for _, part := range parts {
		cmd := strings.ToLower(strings.TrimSpace(part))
		if cmd == "" {
			continue
		}
		if _, ok := supportedCommands[cmd]; !ok {
			return V2Flags{}, fmt.Errorf("unknown v2 command %q", cmd)
		}
		flags.enabled[cmd] = struct{}{}
	}
	return flags, nil
}

// ParseV2CommandsFromEnv parses AGENTCTL_V2_COMMANDS from the environment.
func ParseV2CommandsFromEnv() (V2Flags, error) {
	return ParseV2Commands(os.Getenv(envV2Commands))
}

// ParseV2ShadowCommands parses AGENTCTL_V2_SHADOW_COMMANDS-like comma-separated command sets.
func ParseV2ShadowCommands(raw string) (V2Flags, error) {
	return ParseV2Commands(raw)
}

// ParseV2ShadowCommandsFromEnv parses AGENTCTL_V2_SHADOW_COMMANDS from the environment.
func ParseV2ShadowCommandsFromEnv() (V2Flags, error) {
	return ParseV2ShadowCommands(os.Getenv(envV2ShadowCommands))
}

// Enabled reports whether command is routed to v2.
func (f V2Flags) Enabled(command string) bool {
	_, ok := f.enabled[strings.ToLower(strings.TrimSpace(command))]
	return ok
}

// Commands returns enabled commands in deterministic order.
func (f V2Flags) Commands() []string {
	out := make([]string, 0, len(f.enabled))
	for cmd := range f.enabled {
		out = append(out, cmd)
	}
	sort.Strings(out)
	return out
}

// Empty reports whether no commands are routed to v2.
func (f V2Flags) Empty() bool {
	return len(f.enabled) == 0
}

// SupportedCommands returns the known v2 command keys in deterministic order.
func SupportedCommands() []string {
	out := make([]string, 0, len(supportedCommands))
	for cmd := range supportedCommands {
		out = append(out, cmd)
	}
	sort.Strings(out)
	return out
}
