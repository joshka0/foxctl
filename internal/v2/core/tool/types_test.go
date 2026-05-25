package tool

import (
	"strings"
	"testing"
	"testing/quick"
)

func TestEffectReplayPolicyAllowsOnlySafeIncompleteRetries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy EffectReplayPolicy
		want   bool
	}{
		{
			name:   "read-only tool can retry an incomplete effect",
			policy: EffectReplayReadOnly,
			want:   true,
		},
		{
			name:   "idempotent tool can retry an incomplete effect",
			policy: EffectReplayIdempotent,
			want:   true,
		},
		{
			name:   "fail-closed tool cannot retry an incomplete effect",
			policy: EffectReplayFailClosed,
			want:   false,
		},
		{
			name:   "missing policy fails closed",
			policy: "",
			want:   false,
		},
		{
			name:   "unknown policy fails closed",
			policy: EffectReplayPolicy("eventually_consistent"),
			want:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.policy.AllowsIncompleteEffectRetry(); got != tt.want {
				t.Fatalf("AllowsIncompleteEffectRetry()=%v want %v", got, tt.want)
			}
		})
	}
}

func TestToolDefAllowedForPolicyInvariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		def     ToolDef
		profile ProcessProfile
		want    bool
	}{
		{
			name:    "open policy allows any profile",
			def:     ToolDef{},
			profile: ProfileWorker,
			want:    true,
		},
		{
			name: "deny-by-default without explicit profiles denies listed actors",
			def: ToolDef{
				Policy: ToolPolicy{
					DenyByDefault: true,
				},
			},
			profile: ProfileWorker,
			want:    false,
		},
		{
			name: "explicit allowlist admits listed profile even when deny-by-default",
			def: ToolDef{
				Policy: ToolPolicy{
					DenyByDefault: true,
					AllowProfiles: []ProcessProfile{
						ProfileWorker,
					},
				},
			},
			profile: ProfileWorker,
			want:    true,
		},
		{
			name: "explicit allowlist denies unlisted profile",
			def: ToolDef{
				Policy: ToolPolicy{
					AllowProfiles: []ProcessProfile{
						ProfileWorker,
					},
				},
			},
			profile: ProfileCompanion,
			want:    false,
		},
		{
			name: "explicit allowlist matching is case-insensitive",
			def: ToolDef{
				Policy: ToolPolicy{
					AllowProfiles: []ProcessProfile{
						ProcessProfile("WoRkEr"),
					},
				},
			},
			profile: ProfileWorker,
			want:    true,
		},
		{
			name: "unknown profile is denied when explicit allowlist is present",
			def: ToolDef{
				Policy: ToolPolicy{
					AllowProfiles: []ProcessProfile{
						ProfileWorker,
						ProfileOverseer,
					},
				},
			},
			profile: ProcessProfile("operator"),
			want:    false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.def.AllowedFor(tt.profile); got != tt.want {
				t.Fatalf("AllowedFor(%q)=%v want %v", tt.profile, got, tt.want)
			}
		})
	}
}

func TestToolDefAllowedForExplicitProfilesRestrictsByCaseFoldedProfile(t *testing.T) {
	t.Parallel()

	property := func(seed uint64, denyByDefault bool) bool {
		name := generatedProfileName(seed)
		def := ToolDef{
			Policy: ToolPolicy{
				DenyByDefault: denyByDefault,
				AllowProfiles: []ProcessProfile{
					ProcessProfile(strings.ToUpper(name)),
				},
			},
		}

		return def.AllowedFor(ProcessProfile(strings.ToLower(name))) &&
			!def.AllowedFor(ProcessProfile(name+"_other"))
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 128}); err != nil {
		t.Fatalf("explicit profile allowlist property failed: %v", err)
	}
}

func generatedProfileName(seed uint64) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789_-"
	if seed == 0 {
		return string(ProfileWorker)
	}

	var b strings.Builder
	for seed > 0 && b.Len() < 24 {
		b.WriteByte(alphabet[seed%uint64(len(alphabet))])
		seed /= uint64(len(alphabet))
	}
	return b.String()
}
