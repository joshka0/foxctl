package contextengine

import "strings"

// SourceProfile describes an explicit evidence source preference for context
// gathering. Profiles are ranking and shaping signals, not hidden query routes.
type SourceProfile string

const (
	SourceProfileRepoCode        SourceProfile = "repo_code"
	SourceProfileRepoDocs        SourceProfile = "repo_docs"
	SourceProfileVaultDocs       SourceProfile = "vault_docs"
	SourceProfileMemory          SourceProfile = "memory"
	SourceProfileTask            SourceProfile = "task"
	SourceProfileSession         SourceProfile = "session"
	SourceProfileTrajectory      SourceProfile = "trajectory"
	SourceProfileCodemaps        SourceProfile = "codemaps"
	SourceProfileCochangeHistory SourceProfile = "cochange_history"
)

// IsValid reports whether p is a known source profile.
func (p SourceProfile) IsValid() bool {
	switch p {
	case SourceProfileRepoCode, SourceProfileRepoDocs, SourceProfileVaultDocs,
		SourceProfileMemory, SourceProfileTask, SourceProfileSession,
		SourceProfileTrajectory, SourceProfileCodemaps, SourceProfileCochangeHistory:
		return true
	default:
		return false
	}
}

// NormalizeSourceProfiles parses and de-duplicates source profile names.
func NormalizeSourceProfiles(raw []string) []SourceProfile {
	out := make([]SourceProfile, 0, len(raw))
	seen := map[SourceProfile]struct{}{}
	for _, item := range raw {
		profile := SourceProfile(strings.TrimSpace(strings.ToLower(item)))
		if !profile.IsValid() {
			continue
		}
		if _, ok := seen[profile]; ok {
			continue
		}
		seen[profile] = struct{}{}
		out = append(out, profile)
	}
	return out
}

func sourceProfilesToStrings(profiles []SourceProfile) []string {
	out := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if profile.IsValid() {
			out = append(out, string(profile))
		}
	}
	return out
}
