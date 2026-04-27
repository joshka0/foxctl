// Package profiles owns built-in Foxprox adapter profile defaults.
//
// The broker consumes this registry but does not define the profiles itself.
// Keeping the table as embedded data makes profile additions reviewable as
// configuration changes instead of broker control-flow changes.
package profiles

import (
	_ "embed"
	"encoding/json"
	"strings"
	"time"

	"github.com/joshka/foxprox/foxprox/broker/session"
)

//go:embed profiles.json
var builtinsJSON []byte

const (
	defaultThresholdBPS = 32
	defaultDebounceMS   = 500
)

type profileFileEntry struct {
	Name      string            `json:"name"`
	Aliases   []string          `json:"aliases,omitempty"`
	Readiness readinessFileSpec `json:"readiness"`
}

type readinessFileSpec struct {
	ScreenRegex         string  `json:"screen_regex,omitempty"`
	ThresholdBPS        float64 `json:"threshold_bps,omitempty"`
	DebounceMS          int64   `json:"debounce_ms,omitempty"`
	RequireNotAltScreen bool    `json:"require_not_alt_screen,omitempty"`
}

var builtins = mustLoadBuiltins(builtinsJSON)

// DefaultReadiness returns the readiness defaults for profile. Unknown
// profiles still get conservative byte-idle defaults so callers can rely on
// threshold/debounce fields being populated.
func DefaultReadiness(profile string) session.ReadinessProfile {
	base := session.ReadinessProfile{
		ThresholdBPS: defaultThresholdBPS,
		Debounce:     time.Duration(defaultDebounceMS) * time.Millisecond,
	}
	if p, ok := builtins[normalize(profile)]; ok {
		return merge(base, p.Readiness.toSessionProfile())
	}
	return base
}

// Names returns canonical built-in profile names.
func Names() []string {
	out := make([]string, 0, len(builtins))
	seen := map[string]struct{}{}
	var entries []profileFileEntry
	_ = json.Unmarshal(builtinsJSON, &entries)
	for _, entry := range entries {
		name := normalize(entry.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func mustLoadBuiltins(raw []byte) map[string]profileFileEntry {
	var entries []profileFileEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		panic("foxprox adapter profiles: invalid built-in profiles: " + err.Error())
	}
	out := make(map[string]profileFileEntry, len(entries))
	for _, entry := range entries {
		name := normalize(entry.Name)
		if name == "" {
			panic("foxprox adapter profiles: built-in profile has empty name")
		}
		entry.Name = name
		out[name] = entry
		for _, alias := range entry.Aliases {
			alias = normalize(alias)
			if alias == "" {
				continue
			}
			out[alias] = entry
		}
	}
	return out
}

func (r readinessFileSpec) toSessionProfile() session.ReadinessProfile {
	return session.ReadinessProfile{
		ScreenRegex:         r.ScreenRegex,
		ThresholdBPS:        r.ThresholdBPS,
		Debounce:            time.Duration(r.DebounceMS) * time.Millisecond,
		RequireNotAltScreen: r.RequireNotAltScreen,
	}
}

func merge(base, override session.ReadinessProfile) session.ReadinessProfile {
	if override.ScreenRegex != "" {
		base.ScreenRegex = override.ScreenRegex
	}
	if override.ThresholdBPS > 0 {
		base.ThresholdBPS = override.ThresholdBPS
	}
	if override.Debounce > 0 {
		base.Debounce = override.Debounce
	}
	if override.RequireNotAltScreen {
		base.RequireNotAltScreen = true
	}
	return base
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
