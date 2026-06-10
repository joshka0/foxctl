package rlm

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

func collectSurfacedToolEvidenceRefs(calls []engine.ToolCall, results []engine.ToolResult) []string {
	callNames := make(map[string]string, len(calls))
	for _, call := range calls {
		callNames[call.ID] = call.Name
	}
	out := make([]string, 0, len(results))
	for _, result := range results {
		if result.IsError {
			continue
		}
		callName := callNames[result.ToolCallID]
		if !toolResultSurfacesEvidence(callName) {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(strings.TrimSpace(result.Content)), &payload); err != nil {
			continue
		}
		if strings.TrimSpace(callName) == "evidence_ledger" {
			collectEvidenceLedgerAcceptedRefs(payload, &out)
			continue
		}
		collectEvidenceRefsRecursive(payload, &out)
	}
	out = uniqueStringsRLM(out)
	sort.Strings(out)
	return out
}

func collectAcceptedLedgerEvidenceRows(calls []engine.ToolCall, results []engine.ToolResult) []string {
	callNames := make(map[string]string, len(calls))
	for _, call := range calls {
		callNames[call.ID] = call.Name
	}
	out := []string{}
	for _, result := range results {
		if result.IsError || strings.TrimSpace(callNames[result.ToolCallID]) != "evidence_ledger" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(result.Content)), &payload); err != nil {
			continue
		}
		rows, ok := payload["accepted_rows"].([]any)
		if !ok {
			continue
		}
		for _, rowValue := range rows {
			row, ok := rowValue.(map[string]any)
			if !ok {
				continue
			}
			ref := strings.TrimSpace(fmt.Sprint(row["ref"]))
			text := strings.TrimSpace(fmt.Sprint(row["text"]))
			if text == "" || text == "<nil>" {
				text = strings.TrimSpace(fmt.Sprint(row["claim"]))
			}
			switch {
			case ref != "" && ref != "<nil>" && text != "" && text != "<nil>":
				out = uniqueStringsRLM(append(out, ref+": "+text))
			case ref != "" && ref != "<nil>":
				out = uniqueStringsRLM(append(out, ref))
			case text != "" && text != "<nil>":
				out = uniqueStringsRLM(append(out, text))
			}
			if len(out) >= 8 {
				return out
			}
		}
	}
	return out
}

func collectEvidenceLedgerAcceptedRefs(payload any, out *[]string) {
	root, ok := payload.(map[string]any)
	if !ok {
		return
	}
	for _, ref := range stringsFromAny(root["accepted_refs"]) {
		if ref = strings.TrimSpace(ref); ref != "" {
			*out = append(*out, ref)
		}
	}
	rows, ok := root["accepted_rows"].([]any)
	if !ok {
		return
	}
	for _, rowValue := range rows {
		row, ok := rowValue.(map[string]any)
		if !ok {
			continue
		}
		if ref := evidenceRefObjectString(row); ref != "" {
			*out = append(*out, ref)
			continue
		}
		if ref, ok := row["ref"].(string); ok {
			if ref = strings.TrimSpace(ref); ref != "" && strings.Contains(ref, ":") {
				*out = append(*out, ref)
			}
		}
	}
}

func collectAnswerUsedEvidenceRefs(answer string, candidates []string) []string {
	answer = strings.TrimSpace(answer)
	if answer == "" || len(candidates) == 0 {
		return nil
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if answerMentionsEvidenceRef(answer, candidate) {
			out = append(out, candidate)
		}
	}
	out = uniqueStringsRLM(out)
	sort.Strings(out)
	return out
}

func answerMentionsEvidenceRef(answer, ref string) bool {
	if answerContainsEvidenceToken(answer, ref) {
		return true
	}
	refType, refValue := splitEvidenceRefString(ref)
	switch refType {
	case "path", "note":
		return answerContainsEvidenceToken(answer, refValue)
	default:
		return false
	}
}

func answerContainsEvidenceToken(answer, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	searchFrom := 0
	for {
		idx := strings.Index(answer[searchFrom:], token)
		if idx < 0 {
			return false
		}
		idx += searchFrom
		after := idx + len(token)
		beforeOK := idx == 0 || !isEvidenceTokenByte(answer[idx-1])
		afterOK := after >= len(answer) || !isEvidenceTokenByte(answer[after]) || isSentencePeriod(answer, after)
		if beforeOK && afterOK {
			return true
		}
		searchFrom = after
	}
}

func isSentencePeriod(answer string, idx int) bool {
	return idx < len(answer) &&
		answer[idx] == '.' &&
		(idx+1 >= len(answer) || !isEvidenceTokenByte(answer[idx+1]))
}

func isEvidenceTokenByte(value byte) bool {
	switch {
	case value >= 'a' && value <= 'z':
		return true
	case value >= 'A' && value <= 'Z':
		return true
	case value >= '0' && value <= '9':
		return true
	default:
		return strings.ContainsRune("._/:@+-", rune(value))
	}
}

func splitEvidenceRefString(ref string) (string, string) {
	refType, refValue, ok := strings.Cut(strings.TrimSpace(ref), ":")
	if !ok {
		return "", ""
	}
	return strings.TrimSpace(refType), strings.TrimSpace(refValue)
}

func toolResultSurfacesEvidence(name string) bool {
	switch strings.TrimSpace(name) {
	case "gather_context", "gather_memory_context", "gather_test_context", "gather_docs_context",
		"retrieve_code", "retrieve_memory", "retrieve_context", "retrieve_task",
		"retrieve_mixed", "expand_context_graph", "load_evidence_ref", "aggregate_evidence_refs", "evidence_ledger",
		"code_search_ensemble", "memory_ensemble_retrieve":
		return true
	default:
		return false
	}
}
