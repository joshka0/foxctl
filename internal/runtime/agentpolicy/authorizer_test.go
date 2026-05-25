package agentpolicy

import (
	"fmt"
	"testing"
	"testing/quick"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		wantFoxctl  bool
		wantSkill   string
		wantEnvVars map[string]string
	}{
		{
			name:       "simple foxctl run",
			command:    "foxctl run code/symbols --input '{}'",
			wantFoxctl: true,
			wantSkill:  "code/symbols",
		},
		{
			name:       "foxctl with absolute path",
			command:    "/usr/local/bin/foxctl run code/semantic_search",
			wantFoxctl: true,
			wantSkill:  "code/semantic_search",
		},
		{
			name:       "relative foxctl path is not trusted",
			command:    "./foxctl run code/symbols",
			wantFoxctl: false,
			wantSkill:  "",
		},
		{
			name:       "workspace foxctl path is not trusted",
			command:    "tools/foxctl run code/symbols",
			wantFoxctl: false,
			wantSkill:  "",
		},
		{
			name:        "foxctl with env vars",
			command:     "FOXCTL_WORKSPACE=/foo foxctl run code/snippet_extract",
			wantFoxctl:  true,
			wantSkill:   "code/snippet_extract",
			wantEnvVars: map[string]string{"FOXCTL_WORKSPACE": "/foo"},
		},
		{
			name:        "foxctl with multiple env vars",
			command:     "FOO=bar BAZ=qux foxctl run test/run",
			wantFoxctl:  true,
			wantSkill:   "test/run",
			wantEnvVars: map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:       "skill with single quotes",
			command:    `foxctl run 'code/symbols' --input '{}'`,
			wantFoxctl: true,
			wantSkill:  "code/symbols",
		},
		{
			name:       "skill with double quotes",
			command:    `foxctl run "code/symbols" --input '{}'`,
			wantFoxctl: true,
			wantSkill:  "code/symbols",
		},
		{
			name:       "not foxctl command",
			command:    "rm -rf /",
			wantFoxctl: false,
			wantSkill:  "",
		},
		{
			name:       "foxctl but not run",
			command:    "foxctl skills list",
			wantFoxctl: false,
			wantSkill:  "",
		},
		{
			name:       "foxctl run without skill",
			command:    "foxctl run",
			wantFoxctl: false,
			wantSkill:  "",
		},
		{
			name:       "foxctl run with flag instead of skill",
			command:    "foxctl run --help",
			wantFoxctl: false,
			wantSkill:  "",
		},
		{
			name:       "empty command",
			command:    "",
			wantFoxctl: false,
			wantSkill:  "",
		},
		{
			name:       "foxctl with complex input",
			command:    `foxctl run code/snippet_extract --input '{"question":"Where is auth?"}'`,
			wantFoxctl: true,
			wantSkill:  "code/snippet_extract",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCommand(tt.command)

			if result.IsFoxctlRun != tt.wantFoxctl {
				t.Errorf("IsFoxctlRun = %v, want %v", result.IsFoxctlRun, tt.wantFoxctl)
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
			command:      "FOXCTL_WORKSPACE=/foo foxctl run code/symbols",
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

func TestAuthorizeBashRestrictedProfilesBlockShellControlOperators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
	}{
		{
			name:    "semicolon chain",
			command: "foxctl run code/symbols ; rm -rf /tmp/workspace",
		},
		{
			name:    "and chain",
			command: "foxctl run code/symbols && rm -rf /tmp/workspace",
		},
		{
			name:    "or chain",
			command: "foxctl run code/symbols || rm -rf /tmp/workspace",
		},
		{
			name:    "pipe",
			command: "foxctl run code/symbols | sh",
		},
		{
			name:    "background command",
			command: "foxctl run code/symbols & rm -rf /tmp/workspace",
		},
		{
			name:    "newline chain",
			command: "foxctl run code/symbols\nrm -rf /tmp/workspace",
		},
		{
			name:    "stdout redirect",
			command: "foxctl run code/symbols > /tmp/out",
		},
		{
			name:    "stdin redirect",
			command: "foxctl run code/symbols < /etc/passwd",
		},
		{
			name:    "command substitution",
			command: "FOXCTL_WORKSPACE=$(rm -rf /tmp/workspace) foxctl run code/symbols",
		},
		{
			name:    "backtick substitution",
			command: "FOXCTL_WORKSPACE=`rm -rf /tmp/workspace` foxctl run code/symbols",
		},
		{
			name:    "command substitution inside double quotes",
			command: `foxctl run code/symbols --input "$(rm -rf /tmp/workspace)"`,
		},
	}

	for _, profile := range []Profile{ProfileExplorer, ProfileReviewer, ProfileImplementer, Profile("unknown")} {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s/%s", profile, tt.name), func(t *testing.T) {
				result := AuthorizeBash(profile, tt.command)
				if result.Decision != DecisionBlock {
					t.Fatalf("AuthorizeBash(%q, %q) = %s, want block", profile, tt.command, result.Decision)
				}
			})
		}
	}
}

func TestAuthorizeBashRestrictedProfilesBlockUnsafeEnvAssignments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
	}{
		{
			name:    "path can change command resolution",
			command: "PATH=/tmp/malicious foxctl run code/symbols",
		},
		{
			name:    "bash_env can source arbitrary shell code",
			command: "BASH_ENV=/tmp/payload foxctl run code/symbols",
		},
		{
			name:    "ld_preload can inject code",
			command: "LD_PRELOAD=/tmp/libpayload.so foxctl run code/symbols",
		},
		{
			name:    "arbitrary env var is outside the restricted contract",
			command: "FOO=bar foxctl run code/symbols",
		},
		{
			name:    "mixed allowed and unsafe env vars",
			command: "FOXCTL_WORKSPACE=/repo PATH=/tmp/malicious foxctl run code/symbols",
		},
	}

	for _, profile := range []Profile{ProfileExplorer, ProfileReviewer, ProfileImplementer, Profile("unknown")} {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s/%s", profile, tt.name), func(t *testing.T) {
				result := AuthorizeBash(profile, tt.command)
				if result.Decision != DecisionBlock {
					t.Fatalf("AuthorizeBash(%q, %q) = %s, want block", profile, tt.command, result.Decision)
				}
			})
		}
	}
}

func TestAuthorizeBashRestrictedProfilesAllowFoxctlWorkspaceEnvAssignment(t *testing.T) {
	t.Parallel()

	command := "FOXCTL_WORKSPACE=/repo foxctl run code/symbols"
	result := AuthorizeBash(ProfileExplorer, command)
	if result.Decision != DecisionAllow {
		t.Fatalf("AuthorizeBash(%q) = %s (%s), want allow", command, result.Decision, result.Reason)
	}
	if result.ParsedSkill != "code/symbols" {
		t.Fatalf("ParsedSkill = %q, want %q", result.ParsedSkill, "code/symbols")
	}
}

func TestAuthorizeBashRestrictedProfilesBlockWorkspaceLocalFoxctlPath(t *testing.T) {
	t.Parallel()

	for _, command := range []string{
		"./foxctl run code/symbols",
		"tools/foxctl run code/symbols",
		"../bin/foxctl run code/symbols",
	} {
		result := AuthorizeBash(ProfileExplorer, command)
		if result.Decision != DecisionBlock {
			t.Fatalf("AuthorizeBash(%q) = %s, want block for workspace-local foxctl path", command, result.Decision)
		}
	}
}

func TestAuthorizeBashPropertyRestrictedProfilesBlockNonAllowlistedEnv(t *testing.T) {
	t.Parallel()

	profiles := []Profile{ProfileExplorer, ProfileReviewer, ProfileImplementer, Profile("unknown")}
	cfg := &quick.Config{MaxCount: 200}
	err := quick.Check(func(raw string, profileSeed uint8) bool {
		envName := nonAllowlistedEnvName(raw)
		profile := profiles[int(profileSeed)%len(profiles)]
		command := fmt.Sprintf("%s=value foxctl run code/symbols", envName)
		result := AuthorizeBash(profile, command)
		if result.Decision != DecisionBlock {
			t.Logf("AuthorizeBash(%q, %q) = %s, want block", profile, command, result.Decision)
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("restricted env allowlist property failed: %v", err)
	}
}

func TestAuthorizeBashRestrictedProfilesAllowShellPunctuationInsideSingleQuotedInput(t *testing.T) {
	t.Parallel()

	command := `foxctl run code/symbols --input '{"pattern":"a;b && c || d | e > f < g $(literal) ` + "`literal`" + `"}'`
	result := AuthorizeBash(ProfileExplorer, command)
	if result.Decision != DecisionAllow {
		t.Fatalf("AuthorizeBash(%q) = %s (%s), want allow", command, result.Decision, result.Reason)
	}
	if result.ParsedSkill != "code/symbols" {
		t.Fatalf("ParsedSkill = %q, want %q", result.ParsedSkill, "code/symbols")
	}
}

func nonAllowlistedEnvName(raw string) string {
	cleaned := make([]rune, 0, len(raw))
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z':
			cleaned = append(cleaned, r)
		case r >= 'a' && r <= 'z':
			cleaned = append(cleaned, r-'a'+'A')
		case r >= '0' && r <= '9':
			cleaned = append(cleaned, r)
		case r == '_':
			cleaned = append(cleaned, r)
		}
	}
	if len(cleaned) == 0 || (cleaned[0] >= '0' && cleaned[0] <= '9') {
		cleaned = append([]rune{'F'}, cleaned...)
	}
	name := string(cleaned)
	if name == "FOXCTL_WORKSPACE" {
		return "FOXCTL_WORKSPACE_"
	}
	return name
}

func TestAuthorizeBashRestrictedProfilesBlockIncompleteShellSyntax(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
	}{
		{
			name:    "unclosed double quote hides chain operator",
			command: `foxctl run code/symbols " ; rm -rf /tmp/workspace`,
		},
		{
			name:    "unclosed single quote hides chain operator",
			command: `foxctl run code/symbols ' ; rm -rf /tmp/workspace`,
		},
		{
			name:    "unclosed quoted allowed skill",
			command: `foxctl run "code/symbols`,
		},
		{
			name:    "unclosed env assignment",
			command: `FOXCTL_WORKSPACE="/tmp foxctl run code/symbols`,
		},
		{
			name:    "trailing escape",
			command: "foxctl run code/symbols \\",
		},
	}

	for _, profile := range []Profile{ProfileExplorer, ProfileReviewer, ProfileImplementer, Profile("unknown")} {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%s/%s", profile, tt.name), func(t *testing.T) {
				result := AuthorizeBash(profile, tt.command)
				if result.Decision != DecisionBlock {
					t.Fatalf("AuthorizeBash(%q, %q) = %s, want block", profile, tt.command, result.Decision)
				}
			})
		}
	}
}

func TestAuthorizeBashPropertyRestrictedProfilesNeverAllowUnquotedShellControl(t *testing.T) {
	t.Parallel()

	operators := []string{";", "&&", "||", "|", "&", "\n", "\r", ">", "<", "$(echo x)", "`echo x`"}
	profiles := []Profile{ProfileExplorer, ProfileReviewer, ProfileImplementer, Profile("unknown")}
	skills := []string{"code/symbols", "code/semantic_search", "test/run"}

	cfg := &quick.Config{MaxCount: 200}
	err := quick.Check(func(profileSeed, skillSeed, operatorSeed uint8) bool {
		profile := profiles[int(profileSeed)%len(profiles)]
		skill := skills[int(skillSeed)%len(skills)]
		operator := operators[int(operatorSeed)%len(operators)]

		command := fmt.Sprintf("foxctl run %s %s echo escaped", skill, operator)
		return AuthorizeBash(profile, command).Decision == DecisionBlock
	}, cfg)
	if err != nil {
		t.Fatalf("restricted shell-control property failed: %v", err)
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

func TestIsSkillAllowedDomainProfileHierarchy(t *testing.T) {
	t.Parallel()

	explorerSkills := GetAllowedSkillNames(ProfileExplorer)
	reviewerSkills := GetAllowedSkillNames(ProfileReviewer)
	implementerSkills := GetAllowedSkillNames(ProfileImplementer)

	for _, skill := range explorerSkills {
		if !IsSkillAllowed(ProfileReviewer, skill) {
			t.Fatalf("reviewer must include explorer skill %q", skill)
		}
		if !IsSkillAllowed(ProfileImplementer, skill) {
			t.Fatalf("implementer must include explorer skill %q", skill)
		}
	}

	for _, skill := range reviewerSkills {
		if !IsSkillAllowed(ProfileImplementer, skill) {
			t.Fatalf("implementer must include reviewer skill %q", skill)
		}
	}

	for _, skill := range implementerSkills {
		if IsSkillAllowed(Profile("unknown"), skill) {
			t.Fatalf("unknown restricted profile must not allow known skill %q", skill)
		}
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
