package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/companion"
	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
)

func makeCompanionJidoSubcallProvider(log zerolog.Logger) func(context.Context, companion.CompanionSubcallRequest) (companion.CompanionSubcallResult, error) {
	return func(ctx context.Context, req companion.CompanionSubcallRequest) (companion.CompanionSubcallResult, error) {
		parentAgentID := strings.TrimSpace(req.ParentAgentID)
		if parentAgentID == "" {
			return companion.CompanionSubcallResult{}, fmt.Errorf("parent_agent_id is required for subcall")
		}

		client, err := v2jido.NewJSONRPCClient(v2jido.JSONRPCClientConfig{
			SocketPath: strings.TrimSpace(os.Getenv("AGENTCTL_JIDO_SOCKET")),
			RPCPath:    strings.TrimSpace(os.Getenv("AGENTCTL_JIDO_RPC_PATH")),
			Timeout:    20 * time.Second,
		})
		if err != nil {
			return companion.CompanionSubcallResult{}, err
		}

		spawner, err := v2jido.NewChildSpawner(v2jido.ChildSpawnerConfig{
			Client:       client,
			SignalSource: v2jido.DefaultSignalSource,
			Timeout:      20 * time.Second,
		})
		if err != nil {
			return companion.CompanionSubcallResult{}, err
		}

		role := companion.DefaultSubcallWorkerRole
		if resolved := strings.TrimSpace(req.Role); resolved != "" {
			role = resolved
		}

		spawnResp, err := spawner.SpawnChild(ctx, spawn.Request{
			RequestID:     req.ConversationID + ":subcall",
			Role:          role,
			Prompt:        strings.TrimSpace(req.Prompt),
			ExecMode:      "autonomous",
			ParentAgentID: parentAgentID,
			MaxIterations: req.MaxIterations,
			MaxAutoTurns:  1,
			Metadata: map[string]any{
				"source":          "companion_subcall",
				"workspace":       strings.TrimSpace(req.Workspace),
				"conversation_id": strings.TrimSpace(req.ConversationID),
			},
		})
		if err != nil {
			return companion.CompanionSubcallResult{}, err
		}

		agentID := strings.TrimSpace(spawnResp.AgentID)
		if agentID == "" {
			return companion.CompanionSubcallResult{}, fmt.Errorf("spawned child agent id is empty")
		}

		raw, err := json.Marshal(map[string]any{
			"prompt":          strings.TrimSpace(req.Prompt),
			"harness_state":   strings.TrimSpace(req.HarnessState),
			"request_id":      req.ConversationID + ":subcall:signal",
			"conversation_id": strings.TrimSpace(req.ConversationID),
			"role":            role,
			"llm_provider":    strings.TrimSpace(req.LLMProvider),
			"llm_model":       strings.TrimSpace(req.LLMModel),
			"context": map[string]any{
				"agent_workspace": strings.TrimSpace(req.Workspace),
				"parent_agent_id": parentAgentID,
			},
		})
		if err != nil {
			return companion.CompanionSubcallResult{}, err
		}
		if _, err := client.Signal(ctx, v2jido.SignalRequest{
			RequestID: req.ConversationID + ":subcall:signal",
			AgentID:   agentID,
			Signal: v2jido.Signal{
				ID:            req.ConversationID + ":subcall:signal",
				Type:          "agentctl.subcall",
				Source:        v2jido.DefaultSignalSource,
				CorrelationID: req.ConversationID + ":subcall:signal",
				CausationID:   req.ConversationID + ":subcall",
				Data:          raw,
			},
			Mode:      v2jido.SignalModeCall,
			TimeoutMS: 20_000,
		}); err != nil {
			return companion.CompanionSubcallResult{}, err
		}

		awaitResp, err := client.Await(ctx, v2jido.AwaitRequest{
			AgentID:   agentID,
			TimeoutMS: 20_000,
		})
		if err != nil {
			return companion.CompanionSubcallResult{}, err
		}

		stateResp, err := client.State(ctx, v2jido.StateRequest{AgentID: agentID})
		if err != nil {
			return companion.CompanionSubcallResult{}, err
		}

		result := decodeCompanionSubcallResult(awaitResp, stateResp)
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		result.Metadata["spawn_status"] = spawnResp.Status
		result.Metadata["await_status"] = awaitResp.Status
		result.Metadata["agent_id"] = agentID
		if !hasUsableSubcallResult(result) {
			return companion.CompanionSubcallResult{}, fmt.Errorf("subcall child did not produce a bounded result (await_status=%s)", strings.TrimSpace(awaitResp.Status))
		}
		log.Debug().
			Str("parent_agent_id", parentAgentID).
			Str("child_agent_id", agentID).
			Str("subcall_prompt", strings.TrimSpace(req.Prompt)).
			Msg("companion jido subcall completed")
		return result, nil
	}
}

func decodeCompanionSubcallResult(awaitResp v2jido.AwaitResponse, stateResp v2jido.StateResponse) companion.CompanionSubcallResult {
	result := companion.CompanionSubcallResult{
		Metadata: map[string]any{
			"await_status": strings.TrimSpace(awaitResp.Status),
			"state_status": strings.TrimSpace(stateResp.Status),
		},
	}

	if len(awaitResp.Result) > 0 && string(awaitResp.Result) != "null" {
		result.ArtifactRef = strings.TrimSpace(extractStringFromJSON(awaitResp.Result, "artifact"))
	}

	var root map[string]any
	if err := json.Unmarshal(stateResp.State, &root); err != nil {
		return result
	}

	agentctlState := mapAt(root, "agentctl")
	lastResult := mapAt(agentctlState, "last_result")
	if len(lastResult) == 0 {
		return result
	}

	if summary := strings.TrimSpace(stringValue(lastResult["summary"])); summary != "" {
		result.Summary = summary
	}
	if answer := strings.TrimSpace(stringValue(lastResult["answer"])); answer != "" && result.Summary == "" {
		if parsed := parseStructuredSubcallAnswer(answer); hasUsableSubcallResult(parsed) {
			if result.Summary == "" {
				result.Summary = parsed.Summary
			}
			if len(result.EvidenceRefs) == 0 {
				result.EvidenceRefs = parsed.EvidenceRefs
			}
			if len(result.RetrievedPaths) == 0 {
				result.RetrievedPaths = parsed.RetrievedPaths
			}
			if result.ArtifactRef == "" {
				result.ArtifactRef = parsed.ArtifactRef
			}
		} else {
			result.Summary = answer
		}
	}
	if refs := stringSlice(lastResult["evidence_refs"]); len(refs) > 0 {
		result.EvidenceRefs = refs
	}
	if paths := stringSlice(lastResult["retrieved_paths"]); len(paths) > 0 {
		result.RetrievedPaths = paths
	}
	if artifact := strings.TrimSpace(stringValue(lastResult["artifact"])); artifact != "" && result.ArtifactRef == "" {
		result.ArtifactRef = artifact
	}
	if envelope := mapAt(lastResult, "envelope"); len(envelope) > 0 {
		mergeEnvelopeDerivedSubcallResult(&result, envelope)
		if summary := strings.TrimSpace(stringValue(envelope["summary"])); summary != "" && result.Summary == "" {
			result.Summary = summary
		}
	}
	return result
}

func mergeEnvelopeDerivedSubcallResult(result *companion.CompanionSubcallResult, envelope map[string]any) {
	if result == nil || len(envelope) == 0 {
		return
	}
	data := mapAt(envelope, "data")
	if len(data) == 0 {
		return
	}
	if len(result.RetrievedPaths) == 0 {
		result.RetrievedPaths = append(result.RetrievedPaths, extractCandidatePaths(data)...)
	}
	if strings.TrimSpace(result.Summary) == "" {
		if summary := summarizeEnvelopeData(data); summary != "" {
			result.Summary = summary
		}
	}
}

func extractCandidatePaths(data map[string]any) []string {
	out := make([]string, 0, 4)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		for _, existing := range out {
			if existing == path {
				return
			}
		}
		out = append(out, path)
	}
	if candidates, ok := data["candidates"].([]any); ok {
		for _, item := range candidates {
			if entry, ok := item.(map[string]any); ok {
				add(stringValue(entry["path"]))
			}
			if len(out) >= 3 {
				break
			}
		}
	}
	if snippets, ok := data["snippets_inline"].([]any); ok {
		for _, item := range snippets {
			if entry, ok := item.(map[string]any); ok {
				add(stringValue(entry["file"]))
			}
			if len(out) >= 3 {
				break
			}
		}
	}
	return out
}

func summarizeEnvelopeData(data map[string]any) string {
	if len(data) == 0 {
		return ""
	}
	summaryMap := mapAt(data, "summary")
	filesRelevant := intValue(summaryMap["files_relevant"])
	candidatesGenerated := intValue(summaryMap["candidates_generated"])
	topPaths := extractCandidatePaths(data)

	var parts []string
	if filesRelevant > 0 {
		parts = append(parts, fmt.Sprintf("Subcall found %d relevant files", filesRelevant))
	} else if candidatesGenerated > 0 {
		parts = append(parts, fmt.Sprintf("Subcall generated %d candidate matches", candidatesGenerated))
	}
	if len(topPaths) > 0 {
		parts = append(parts, "top paths: "+strings.Join(topPaths, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(stringValue(item))
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func extractStringFromJSON(raw json.RawMessage, key string) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(stringValue(payload[key]))
}

func intValue(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func parseStructuredSubcallAnswer(raw string) companion.CompanionSubcallResult {
	var payload map[string]any
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return companion.CompanionSubcallResult{}
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start == -1 || end <= start {
			return companion.CompanionSubcallResult{}
		}
		if err := json.Unmarshal([]byte(raw[start:end+1]), &payload); err != nil {
			return companion.CompanionSubcallResult{}
		}
	}
	result := companion.CompanionSubcallResult{
		Summary:        strings.TrimSpace(stringValue(payload["summary"])),
		EvidenceRefs:   stringSlice(payload["evidence_refs"]),
		RetrievedPaths: stringSlice(payload["retrieved_paths"]),
		ArtifactRef:    strings.TrimSpace(stringValue(payload["artifact"])),
	}
	if nextAction := strings.TrimSpace(stringValue(payload["next_action"])); nextAction != "" {
		if result.Summary == "" {
			result.Summary = nextAction
		} else {
			result.Summary = result.Summary + " Next action: " + nextAction
		}
	}
	return result
}

func hasUsableSubcallResult(result companion.CompanionSubcallResult) bool {
	return strings.TrimSpace(result.Summary) != "" ||
		strings.TrimSpace(result.ArtifactRef) != "" ||
		len(result.EvidenceRefs) > 0 ||
		len(result.RetrievedPaths) > 0
}

func mapAt(input map[string]any, key string) map[string]any {
	if len(input) == 0 {
		return nil
	}
	raw, ok := input[key]
	if !ok || raw == nil {
		return nil
	}
	out, _ := raw.(map[string]any)
	return out
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case json.RawMessage:
		return string(value)
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("%v", value)
	}
}
