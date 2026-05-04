package contextengine

import (
	"fmt"
	"sort"
	"strings"
)

// CoverageRequirement describes one reducer-level coverage slot that should be
// represented in a ContextBundle when matching evidence exists.
type CoverageRequirement struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind,omitempty"`
	Label          string          `json:"label,omitempty"`
	Terms          []string        `json:"terms,omitempty"`
	Required       bool            `json:"required,omitempty"`
	MinPaths       int             `json:"min_paths,omitempty"`
	Weight         float64         `json:"weight,omitempty"`
	SourceProfiles []SourceProfile `json:"source_profiles,omitempty"`
}

// Validate checks the requirement contract.
func (r CoverageRequirement) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("coverage requirement: missing id")
	}
	if strings.TrimSpace(r.Label) == "" && len(r.Terms) == 0 {
		return fmt.Errorf("coverage requirement %q: missing label or terms", r.ID)
	}
	for i, profile := range r.SourceProfiles {
		if !profile.IsValid() {
			return fmt.Errorf("coverage requirement %q: invalid source_profile[%d] %q", r.ID, i, profile)
		}
	}
	return nil
}

// PathCoverage records selected path coverage for answer surfaces and audits.
type PathCoverage struct {
	RequirementID string   `json:"requirement_id"`
	Path          string   `json:"path"`
	EvidenceIDs   []string `json:"evidence_ids,omitempty"`
	Score         float64  `json:"score,omitempty"`
}

// CoverageReport summarizes reducer coverage decisions.
type CoverageReport struct {
	Requirements []CoverageRequirement `json:"requirements,omitempty"`
	Covered      []PathCoverage        `json:"covered,omitempty"`
	Missing      []string              `json:"missing,omitempty"`
}

func normalizeCoverageRequirements(required []CoverageRequirement, legacy []string, sourceProfiles []SourceProfile) []CoverageRequirement {
	out := make([]CoverageRequirement, 0, len(required)+len(legacy))
	seen := map[string]struct{}{}
	add := func(req CoverageRequirement) {
		req.ID = strings.TrimSpace(req.ID)
		req.Kind = strings.TrimSpace(req.Kind)
		req.Label = strings.TrimSpace(req.Label)
		req.Terms = normalizeCoverageTerms(append(req.Terms, req.Label))
		req.SourceProfiles = NormalizeSourceProfiles(sourceProfilesToStrings(req.SourceProfiles))
		if req.ID == "" {
			req.ID = stableCoverageRequirementID(req.Kind, req.Label, req.Terms)
		}
		if req.Kind == "" {
			req.Kind = "concept"
		}
		if req.Label == "" && len(req.Terms) > 0 {
			req.Label = strings.Join(req.Terms, " ")
		}
		if req.MinPaths <= 0 {
			req.MinPaths = 1
		}
		if req.Weight <= 0 {
			req.Weight = 1
		}
		if len(req.SourceProfiles) == 0 && len(sourceProfiles) > 0 {
			req.SourceProfiles = append([]SourceProfile(nil), sourceProfiles...)
		}
		if err := req.Validate(); err != nil {
			return
		}
		if _, ok := seen[req.ID]; ok {
			return
		}
		seen[req.ID] = struct{}{}
		out = append(out, req)
	}
	for _, req := range required {
		add(req)
	}
	for _, item := range legacy {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		add(CoverageRequirement{
			Kind:     "concept",
			Label:    item,
			Terms:    splitEvidenceCoverageTerms(item),
			Required: true,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required
		}
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func normalizeCoverageTerms(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range splitEvidenceCoverageTerms(value) {
			for _, term := range evidenceCoverageTermVariants(part) {
				if len(term) < 4 {
					continue
				}
				if _, ok := seen[term]; ok {
					continue
				}
				seen[term] = struct{}{}
				out = append(out, term)
			}
		}
	}
	sort.Strings(out)
	return out
}

func stableCoverageRequirementID(kind string, label string, terms []string) string {
	parts := normalizeCoverageTerms([]string{kind, label, strings.Join(terms, " ")})
	if len(parts) == 0 {
		return "coverage"
	}
	if len(parts) > 4 {
		parts = parts[:4]
	}
	return strings.Join(parts, "_")
}

func evidenceCoverageTermVariants(term string) []string {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return nil
	}
	out := []string{term}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		for _, existing := range out {
			if existing == value {
				return
			}
		}
		out = append(out, value)
	}
	switch {
	case strings.HasSuffix(term, "ification") && len(term) > len("ification")+2:
		add(strings.TrimSuffix(term, "ification") + "ify")
	case strings.HasSuffix(term, "ication") && len(term) > len("ication")+2:
		add(strings.TrimSuffix(term, "ication") + "ify")
	case strings.HasSuffix(term, "ation") && len(term) > len("ation")+2:
		add(strings.TrimSuffix(term, "ation") + "ate")
	}
	if strings.HasSuffix(term, "ies") && len(term) > 4 {
		add(strings.TrimSuffix(term, "ies") + "y")
	}
	if strings.HasSuffix(term, "s") && len(term) > 5 {
		add(strings.TrimSuffix(term, "s"))
	}
	return out
}
