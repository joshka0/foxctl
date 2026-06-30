package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/rlm"
)

type longmemAnswerToolRecorder struct {
	next      rlm.ToolExecutor
	seenNames map[string]struct{}
	seenRefs  map[string]struct{}
	names     []string
	refs      []string
}

func (r *longmemAnswerToolRecorder) Execute(ctx context.Context, name string, args json.RawMessage) (map[string]any, error) {
	if r == nil || r.next == nil {
		return nil, fmt.Errorf("longmem answer tool recorder is not configured")
	}
	out, err := r.next.Execute(ctx, name, args)
	if err != nil {
		return out, err
	}
	r.recordMemoryEvidence(name, out)
	return out, nil
}

func (r *longmemAnswerToolRecorder) recordMemoryEvidence(toolName string, payload map[string]any) {
	if strings.TrimSpace(toolName) == "evidence_ledger" {
		r.recordAcceptedLedgerEvidence(payload)
		return
	}
	collectLongmemMemoryEvidence(payload, r.addName, r.addRef)
	for _, node := range longmemEvidenceNodes(payload["nodes"]) {
		refType := strings.TrimSpace(fmt.Sprint(node["ref_type"]))
		if refType != "" && !longmemMemoryEvidenceRefType(refType) {
			continue
		}
		refValue := strings.TrimSpace(fmt.Sprint(node["ref_value"]))
		if refValue != "" {
			r.addName(refValue)
		}
		ref := strings.TrimSpace(fmt.Sprint(node["ref"]))
		if ref != "" {
			r.addRef(ref)
		}
	}
}

func (r *longmemAnswerToolRecorder) recordAcceptedLedgerEvidence(payload map[string]any) {
	for _, ref := range longmemStringSlice(payload["accepted_refs"]) {
		recordLongmemMemoryRefString(ref, r.addName, r.addRef)
	}
	for _, row := range longmemMapSlice(payload["accepted_rows"]) {
		collectLongmemMemoryEvidence(row, r.addName, r.addRef)
	}
}

func collectLongmemMemoryEvidence(value any, addName func(string), addRef func(string)) {
	switch v := value.(type) {
	case string:
		recordLongmemMemoryRefString(v, addName, addRef)
	case []any:
		for _, item := range v {
			collectLongmemMemoryEvidence(item, addName, addRef)
		}
	case []map[string]any:
		for _, item := range v {
			collectLongmemMemoryEvidence(item, addName, addRef)
		}
	case map[string]any:
		refType := strings.TrimSpace(fmt.Sprint(firstMapValue(v, "ref_type", "type")))
		refValue := strings.TrimSpace(fmt.Sprint(firstMapValue(v, "ref_value", "ref")))
		if longmemMemoryEvidenceRefType(refType) && refValue != "" {
			addName(refValue)
			addRef(refType + ":" + refValue)
		}
		for _, child := range v {
			collectLongmemMemoryEvidence(child, addName, addRef)
		}
	}
}

func longmemStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" && value != "<nil>" {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func longmemMapSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func firstMapValue(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			return value
		}
	}
	return nil
}

func recordLongmemMemoryRefString(value string, addName func(string), addRef func(string)) {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"memory_claim:", "named_memory:"} {
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(value, prefix))
		if name == "" {
			return
		}
		name = normalizeLongmemMemoryName(name)
		addName(name)
		addRef(prefix + name)
		return
	}
}

func longmemMemoryEvidenceRefType(refType string) bool {
	switch strings.TrimSpace(refType) {
	case "memory_claim", "named_memory":
		return true
	default:
		return false
	}
}

func normalizeLongmemMemoryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "longmem://") {
		return name
	}
	if slash := strings.LastIndex(name, "/"); slash >= 0 && slash+1 < len(name) {
		name = name[slash+1:]
	}
	if looksLikeLongmemMemoryDigest(name) {
		return "longmem://" + name
	}
	return name
}

func looksLikeLongmemMemoryDigest(value string) bool {
	if len(value) != 24 {
		return false
	}
	for _, ch := range value {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return false
	}
	return true
}

func longmemEvidenceNodes(value any) []map[string]any {
	switch nodes := value.(type) {
	case []map[string]any:
		return nodes
	case []any:
		out := make([]map[string]any, 0, len(nodes))
		for _, node := range nodes {
			if m, ok := node.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func (r *longmemAnswerToolRecorder) addName(name string) {
	name = normalizeLongmemMemoryName(name)
	if name == "" {
		return
	}
	if r.seenNames == nil {
		r.seenNames = make(map[string]struct{})
	}
	if _, ok := r.seenNames[name]; ok {
		return
	}
	r.seenNames[name] = struct{}{}
	r.names = append(r.names, name)
}

func (r *longmemAnswerToolRecorder) addRef(ref string) {
	ref = normalizeLongmemMemoryEvidenceRef(ref)
	if ref == "" {
		return
	}
	if r.seenRefs == nil {
		r.seenRefs = make(map[string]struct{})
	}
	if _, ok := r.seenRefs[ref]; ok {
		return
	}
	r.seenRefs[ref] = struct{}{}
	r.refs = append(r.refs, ref)
}

func normalizeLongmemMemoryEvidenceRef(ref string) string {
	ref = strings.TrimSpace(ref)
	for _, prefix := range []string{"memory_claim:", "named_memory:"} {
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		name := normalizeLongmemMemoryName(strings.TrimPrefix(ref, prefix))
		if name == "" {
			return ""
		}
		return prefix + name
	}
	return ref
}

func (r *longmemAnswerToolRecorder) evidenceNames() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.names...)
}

func (r *longmemAnswerToolRecorder) evidenceRefs() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.refs...)
}

func uniqueEvalStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
