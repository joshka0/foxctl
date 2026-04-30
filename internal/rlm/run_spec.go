package rlm

import (
	"fmt"
	"strings"
)

type ToolProfile string

const (
	ToolProfileDefault             ToolProfile = "default"
	ToolProfileGatherContext       ToolProfile = "gather-context"
	ToolProfileCodeIntel           ToolProfile = "code-intel"
	ToolProfileMemoryRecall        ToolProfile = "memory-recall"
	ToolProfileLongCoTNoModelTools ToolProfile = "longcot-no-model-tools"
)

// ToolPolicy captures the effective model-visible tool policy for one run.
type ToolPolicy struct {
	Profile      ToolProfile `json:"profile"`
	AllowedTools []string    `json:"allowed_tools,omitempty"`
	Tools        []Tool      `json:"tools,omitempty"`
}

// RunSpec is the canonical resolved runtime contract for one RLM run.
type RunSpec struct {
	RouteProfile RouteProfile `json:"route_profile"`
	PlanMode     PlanMode     `json:"plan_mode"`
	Plan         Plan         `json:"plan"`
	ToolPolicy   ToolPolicy   `json:"tool_policy"`
}

// ResolveRunSpecInput describes unresolved route/mode/tool-policy inputs.
type ResolveRunSpecInput struct {
	Prompt               string
	RequestedRoute       RouteProfile
	RequestedPlanMode    PlanMode
	RequestedToolProfile string
	AvailableTools       []Tool
}

// ResolveRunSpec canonicalizes route, planning mode, plan, and tool policy.
func ResolveRunSpec(input ResolveRunSpecInput) (RunSpec, error) {
	route, err := resolveRunRouteProfile(input.Prompt, input.RequestedRoute)
	if err != nil {
		return RunSpec{}, err
	}
	mode, err := resolveRunPlanMode(input.RequestedPlanMode)
	if err != nil {
		return RunSpec{}, err
	}
	policy, err := ResolveToolPolicy(input.AvailableTools, input.RequestedToolProfile)
	if err != nil {
		return RunSpec{}, err
	}
	queryPlan := ClassifyQuery(input.Prompt)
	queryPlan.Route = profileToQueryRoute(route)
	plan := buildPlanWithQueryPlan(route, mode, queryPlan)
	return RunSpec{
		RouteProfile: route,
		PlanMode:     mode,
		Plan:         plan,
		ToolPolicy:   policy,
	}, nil
}

// ResolveToolPolicy resolves one canonical tool profile against available tools.
func ResolveToolPolicy(available []Tool, profile string) (ToolPolicy, error) {
	resolvedProfile, err := NormalizeToolProfile(profile)
	if err != nil {
		return ToolPolicy{}, err
	}

	switch resolvedProfile {
	case ToolProfileDefault:
		// Default profile: all composite retrieval tools.
		allow := map[string]struct{}{
			"retrieve_code":     {},
			"retrieve_memory":   {},
			"retrieve_context":  {},
			"retrieve_task":     {},
			"gather_context":    {},
			"retrieve_mixed":    {},
			"load_evidence_ref": {},
		}
		tools := filterToolsBySet(available, allow)
		return ToolPolicy{
			Profile:      resolvedProfile,
			AllowedTools: collectToolNames(tools),
			Tools:        tools,
		}, nil
	case ToolProfileGatherContext:
		// Gather context: force composite context gathering first, then allow
		// bounded ref inspection only for verification.
		allow := map[string]struct{}{
			"gather_context":    {},
			"load_evidence_ref": {},
		}
		tools := filterToolsBySet(available, allow)
		return ToolPolicy{
			Profile:      resolvedProfile,
			AllowedTools: collectToolNames(tools),
			Tools:        tools,
		}, nil
	case ToolProfileCodeIntel:
		// Code intel: gather_context + retrieve_code + load_evidence_ref.
		allow := map[string]struct{}{
			"gather_context":    {},
			"retrieve_code":     {},
			"load_evidence_ref": {},
		}
		tools := filterToolsBySet(available, allow)
		return ToolPolicy{
			Profile:      resolvedProfile,
			AllowedTools: collectToolNames(tools),
			Tools:        tools,
		}, nil
	case ToolProfileMemoryRecall:
		// Memory recall: gather_context + retrieve_memory + retrieve_context + load_evidence_ref.
		allow := map[string]struct{}{
			"gather_context":    {},
			"retrieve_memory":   {},
			"retrieve_context":  {},
			"load_evidence_ref": {},
		}
		tools := filterToolsBySet(available, allow)
		return ToolPolicy{
			Profile:      resolvedProfile,
			AllowedTools: collectToolNames(tools),
			Tools:        tools,
		}, nil
	case ToolProfileLongCoTNoModelTools:
		return ToolPolicy{
			Profile:      resolvedProfile,
			AllowedTools: nil,
			Tools:        []Tool{},
		}, nil
	default:
		return ToolPolicy{}, fmt.Errorf("rlm: unsupported tool profile %q", profile)
	}
}

// NormalizeToolProfile validates a tool profile against canonical profile names.
func NormalizeToolProfile(value string) (ToolProfile, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(ToolProfileDefault):
		return ToolProfileDefault, nil
	case string(ToolProfileGatherContext):
		return ToolProfileGatherContext, nil
	case string(ToolProfileCodeIntel):
		return ToolProfileCodeIntel, nil
	case string(ToolProfileMemoryRecall):
		return ToolProfileMemoryRecall, nil
	case string(ToolProfileLongCoTNoModelTools):
		return ToolProfileLongCoTNoModelTools, nil
	default:
		return "", fmt.Errorf("rlm: unsupported tool profile %q", value)
	}
}

func resolveRunRouteProfile(prompt string, requested RouteProfile) (RouteProfile, error) {
	switch strings.ToLower(strings.TrimSpace(string(requested))) {
	case "", string(RouteProfileAuto):
		return ResolveRouteProfile(prompt, RouteProfileAuto), nil
	case string(RouteProfileCodeRetrieval):
		return RouteProfileCodeRetrieval, nil
	case string(RouteProfileMemoryRecall):
		return RouteProfileMemoryRecall, nil
	case string(RouteProfileMixed):
		return RouteProfileMixed, nil
	case string(RouteProfileEvidenceAudit):
		return RouteProfileEvidenceAudit, nil
	default:
		return "", fmt.Errorf("rlm: unsupported route profile %q", requested)
	}
}

func resolveRunPlanMode(requested PlanMode) (PlanMode, error) {
	switch strings.ToLower(strings.TrimSpace(string(requested))) {
	case "", string(PlanModeFree):
		return PlanModeFree, nil
	case string(PlanModeGuided):
		return PlanModeGuided, nil
	case string(PlanModeStaged):
		return PlanModeStaged, nil
	case string(PlanModeHard):
		return PlanModeHard, nil
	case string(PlanModeLambda):
		return PlanModeLambda, nil
	default:
		return "", fmt.Errorf("rlm: unsupported plan mode %q", requested)
	}
}

func filterToolsBySet(tools []Tool, allow map[string]struct{}) []Tool {
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allow[strings.TrimSpace(tool.Name)]; ok {
			out = append(out, tool)
		}
	}
	return out
}

func collectToolNames(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}
