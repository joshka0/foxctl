package agentpolicy

import (
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		wantAgentctl bool
		wantSkill    string
		wantEnvVars  map[string]string
	}{
		{
			name:         "simple foxctl run",
			command:      "foxctl run code/symbols --input '{}'",
			wantAgentctl: true,
			wantSkill:    "code/symbols",
		},
		{
			name:         "foxctl with absolute path",
			command:      "/usr/local/bin/foxctl run code/semantic_search",
			wantAgentctl: true,
			wantSkill:    "code/semantic_search",
		},
		{
			name:         "foxctl with env vars",
			command:      "AGENTCTL_WORKSPACE=/foo foxctl run code/snippet_extract",
			wantAgentctl: true,
			wantSkill:    "code/snippet_extract",
			wantEnvVars:  map[string]string{"AGENTCTL_WORKSPACE": "/foo"},
		},
		{
			name:         "foxctl with multiple env vars",
			command:      "FOO=bar BAZ=qux foxctl run test/run",
			wantAgentctl: true,
			wantSkill:    "test/run",
			wantEnvVars:  map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:         "skill with single quotes",
			command:      `foxctl run 'code/symbols' --input '{}'`,
			wantAgentctl: true,
			wantSkill:    "code/symbols",
		},
		{
			name:         "skill with double quotes",
			command:      `foxctl run "code/symbols" --input '{}'`,
			wantAgentctl: true,
			wantSkill:    "code/symbols",
		},
		{
			name:         "not foxctl command",
			command:      "rm -rf /",
			wantAgentctl: false,
			wantSkill:    "",
		},
		{
			name:         "foxctl but not run",
			command:      "foxctl skills list",
			wantAgentctl: false,
			wantSkill:    "",
		},
		{
			name:         "foxctl run without skill",
			command:      "foxctl run",
			wantAgentctl: false,
			wantSkill:    "",
		},
		{
			name:         "foxctl run with flag instead of skill",
			command:      "foxctl run --help",
			wantAgentctl: false,
			wantSkill:    "",
		},
		{
			name:         "empty command",
			command:      "",
			wantAgentctl: false,
			wantSkill:    "",
		},
		{
			name:         "foxctl with complex input",
			command:      `foxctl run code/snippet_extract --input '{"question":"Where is auth?"}'`,
			wantAgentctl: true,
			wantSkill:    "code/snippet_extract",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCommand(tt.command)

			if result.IsAgentctlRun != tt.wantAgentctl {
				t.Errorf("IsAgentctlRun = %v, want %v", result.IsAgentctlRun, tt.wantAgentctl)
			}

			if result.Skill != tt.wantSkill {
				t.Errorf("Skill = %q, want %q", result.Skill, tt.wantSkill)
			}

			if tt.wantEnvVars != nil {
				for k, v := range tt.wantEnvVars {
					if got, ok := result.EnvVars[k]; !ok || got != v {
						t.Errorf("EnvVars[%q] = %q, want %q", k, got, v)
					}
				}
			}
		})
	}
}

func TestAuthorizeBash(t *testing.T) {
	tests := []struct {
		name         string
		profile      Profile
		command      string
		wantDecision Decision
		wantSkill    string
	}{
		// Unrestricted profile allows everything
		{
			name:         "unrestricted allows foxctl",
			profile:      ProfileUnrestricted,
			command:      "foxctl run code/symbols",
			wantDecision: DecisionAllow,
		},
		{
			name:         "unrestricted allows any command",
			profile:      ProfileUnrestricted,
			command:      "rm -rf /",
			wantDecision: DecisionAllow,
		},

		// Empty profile allows everything
		{
			name:         "empty profile allows foxctl",
			profile:      "",
			command:      "foxctl run code/symbols",
			wantDecision: DecisionAllow,
		},

		// Explorer profile
		{
			name:         "explorer allows code/semantic_search",
			profile:      ProfileExplorer,
			command:      "foxctl run code/semantic_search --input '{}'",
			wantDecision: DecisionAllow,
			wantSkill:    "code/semantic_search",
		},
		{
			name:         "explorer allows code/snippet_extract",
			profile:      ProfileExplorer,
			command:      "foxctl run code/snippet_extract",
			wantDecision: DecisionAllow,
			wantSkill:    "code/snippet_extract",
		},
		{
			name:         "explorer allows code/symbols",
			profile:      ProfileExplorer,
			command:      "foxctl run code/symbols",
			wantDecision: DecisionAllow,
			wantSkill:    "code/symbols",
		},
		{
			name:         "explorer blocks test/run",
			profile:      ProfileExplorer,
			command:      "foxctl run test/run",
			wantDecision: DecisionBlock,
			wantSkill:    "test/run",
		},
		{
			name:         "explorer blocks code/smart_write",
			profile:      ProfileExplorer,
			command:      "foxctl run code/smart_write",
			wantDecision: DecisionBlock,
			wantSkill:    "code/smart_write",
		},
		{
			name:         "explorer blocks rm command",
			profile:      ProfileExplorer,
			command:      "rm -rf /",
			wantDecision: DecisionBlock,
		},
		{
			name:         "explorer blocks cat command",
			profile:      ProfileExplorer,
			command:      "cat /etc/passwd",
			wantDecision: DecisionBlock,
		},

		// Reviewer profile
		{
			name:         "reviewer allows code/complexity",
			profile:      ProfileReviewer,
			command:      "foxctl run code/complexity",
			wantDecision: DecisionAllow,
			wantSkill:    "code/complexity",
		},
		{
			name:         "reviewer allows explorer skills",
			profile:      ProfileReviewer,
			command:      "foxctl run code/semantic_search",
			wantDecision: DecisionAllow,
			wantSkill:    "code/semantic_search",
		},
		{
			name:         "reviewer blocks test/run",
			profile:      ProfileReviewer,
			command:      "foxctl run test/run",
			wantDecision: DecisionBlock,
			wantSkill:    "test/run",
		},

		// Implementer profile
		{
			name:         "implementer allows test/run",
			profile:      ProfileImplementer,
			command:      "foxctl run test/run",
			wantDecision: DecisionAllow,
			wantSkill:    "test/run",
		},
		{
			name:         "implementer allows code/smart_write",
			profile:      ProfileImplementer,
			command:      "foxctl run code/smart_write",
			wantDecision: DecisionAllow,
			wantSkill:    "code/smart_write",
		},
		{
			name:         "implementer allows reviewer skills",
			profile:      ProfileImplementer,
			command:      "foxctl run code/complexity",
			wantDecision: DecisionAllow,
			wantSkill:    "code/complexity",
		},
		{
			name:         "implementer blocks unknown skill",
			profile:      ProfileImplementer,
			command:      "foxctl run unknown/skill",
			wantDecision: DecisionBlock,
			wantSkill:    "unknown/skill",
		},

		// Edge cases
		{
			name:         "with env var prefix",
			profile:      ProfileExplorer,
			command:      "AGENTCTL_WORKSPACE=/foo foxctl run code/symbols",
			wantDecision: DecisionAllow,
			wantSkill:    "code/symbols",
		},
		{
			name:         "with absolute path",
			profile:      ProfileExplorer,
			command:      "/usr/local/bin/foxctl run code/symbols",
			wantDecision: DecisionAllow,
			wantSkill:    "code/symbols",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AuthorizeBash(tt.profile, tt.command)

			if result.Decision != tt.wantDecision {
				t.Errorf("Decision = %v, want %v (reason: %s)", result.Decision, tt.wantDecision, result.Reason)
			}

			if tt.wantSkill != "" && result.ParsedSkill != tt.wantSkill {
				t.Errorf("ParsedSkill = %q, want %q", result.ParsedSkill, tt.wantSkill)
			}
		})
	}
}

func TestIsSkillAllowed(t *testing.T) {
	tests := []struct {
		name    string
		profile Profile
		skill   string
		want    bool
	}{
		{"unrestricted allows any skill", ProfileUnrestricted, "any/skill", true},
		{"empty profile allows any skill", "", "any/skill", true},
		{"explorer allows semantic_search", ProfileExplorer, "code/semantic_search", true},
		{"explorer allows snippet_extract", ProfileExplorer, "code/snippet_extract", true},
		{"explorer allows symbols", ProfileExplorer, "code/symbols", true},
		{"explorer allows fs/read", ProfileExplorer, "fs/read", true},
		{"explorer blocks test/run", ProfileExplorer, "test/run", false},
		{"explorer blocks smart_write", ProfileExplorer, "code/smart_write", false},
		{"reviewer allows complexity", ProfileReviewer, "code/complexity", true},
		{"reviewer blocks test/run", ProfileReviewer, "test/run", false},
		{"implementer allows test/run", ProfileImplementer, "test/run", true},
		{"implementer allows smart_write", ProfileImplementer, "code/smart_write", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSkillAllowed(tt.profile, tt.skill)
			if got != tt.want {
				t.Errorf("IsSkillAllowed(%q, %q) = %v, want %v", tt.profile, tt.skill, got, tt.want)
			}
		})
	}
}

func TestProfileIsValid(t *testing.T) {
	tests := []struct {
		profile Profile
		valid   bool
	}{
		{ProfileExplorer, true},
		{ProfileReviewer, true},
		{ProfileImplementer, true},
		{ProfileUnrestricted, true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.profile), func(t *testing.T) {
			if got := tt.profile.IsValid(); got != tt.valid {
				t.Errorf("IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		command string
		want    []string
	}{
		{
			command: "foxctl run code/symbols",
			want:    []string{"foxctl", "run", "code/symbols"},
		},
		{
			command: `foxctl run code/symbols --input '{"foo":"bar"}'`,
			want:    []string{"foxctl", "run", "code/symbols", "--input", `{"foo":"bar"}`},
		},
		{
			command: `FOO=bar foxctl run skill`,
			want:    []string{"FOO=bar", "foxctl", "run", "skill"},
		},
		{
			command: `foxctl run 'code/symbols'`,
			want:    []string{"foxctl", "run", "code/symbols"},
		},
		{
			command: `foxctl run "code/symbols"`,
			want:    []string{"foxctl", "run", "code/symbols"},
		},
		{
			command: `echo "hello world"`,
			want:    []string{"echo", "hello world"},
		},
		{
			command: `echo 'it'\''s fine'`,
			want:    []string{"echo", "it's fine"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := tokenize(tt.command)
			if len(got) != len(tt.want) {
				t.Errorf("tokenize() got %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tokenize()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
