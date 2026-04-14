package transcriptpipeline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
)

// SourceMeta describes one transcript source before packetization.
type SourceMeta struct {
	Provider            sourceimport.Provider `json:"provider"`
	SessionID           string                `json:"session_id"`
	SourcePath          string                `json:"source_path"`
	WorkspacePath       string                `json:"workspace_path,omitempty"`
	WorkspaceFamilyPath string                `json:"workspace_family_path,omitempty"`
	ParentSessionID     string                `json:"parent_session_id,omitempty"`
	IsSubagent          bool                  `json:"is_subagent,omitempty"`
	AgentNickname       string                `json:"agent_nickname,omitempty"`
	AgentRole           string                `json:"agent_role,omitempty"`
	StartedAt           time.Time             `json:"started_at,omitempty"`
}

// SourceBundle couples parsed transcript turns with source metadata.
type SourceBundle struct {
	Meta   SourceMeta                 `json:"meta"`
	Parsed sourceimport.ParsedSession `json:"parsed"`
}

// SourceGroup represents one lineage family with mainline and sidecar sources.
type SourceGroup struct {
	GroupID             string         `json:"group_id"`
	WorkspacePath       string         `json:"workspace_path,omitempty"`
	WorkspaceFamilyPath string         `json:"workspace_family_path,omitempty"`
	Bundles             []SourceBundle `json:"bundles"`
}

func (g SourceGroup) MainlineBundles() []SourceBundle {
	out := make([]SourceBundle, 0, len(g.Bundles))
	for _, bundle := range g.Bundles {
		if bundle.Meta.IsSubagent {
			continue
		}
		out = append(out, bundle)
	}
	if len(out) == 0 {
		return append([]SourceBundle(nil), g.Bundles...)
	}
	return out
}

func (g SourceGroup) SidecarBundles() []SourceBundle {
	out := make([]SourceBundle, 0, len(g.Bundles))
	for _, bundle := range g.Bundles {
		if bundle.Meta.IsSubagent {
			out = append(out, bundle)
		}
	}
	return out
}

func (g SourceGroup) SessionIDs() []string {
	return SessionIDsForBundles(g.Bundles)
}

func (g SourceGroup) SourceFiles() []string {
	out := make([]string, 0, len(g.Bundles))
	for _, bundle := range g.Bundles {
		out = append(out, bundle.Meta.SourcePath)
	}
	return out
}

// SessionIDsForBundles extracts session IDs from source bundles.
func SessionIDsForBundles(bundles []SourceBundle) []string {
	out := make([]string, 0, len(bundles))
	for _, bundle := range bundles {
		out = append(out, bundle.Meta.SessionID)
	}
	return out
}

// GroupSourceBundles organizes transcript bundles into lineage families.
func GroupSourceBundles(bundles []SourceBundle) []SourceGroup {
	parentByID := make(map[string]string, len(bundles))
	for _, bundle := range bundles {
		if bundle.Meta.ParentSessionID != "" {
			parentByID[bundle.Meta.SessionID] = bundle.Meta.ParentSessionID
		}
	}

	rootFor := func(id string) string {
		seen := map[string]struct{}{}
		cur := strings.TrimSpace(id)
		for cur != "" {
			if _, ok := seen[cur]; ok {
				break
			}
			seen[cur] = struct{}{}
			parent := strings.TrimSpace(parentByID[cur])
			if parent == "" {
				return cur
			}
			cur = parent
		}
		return strings.TrimSpace(id)
	}

	groupMap := make(map[string]*SourceGroup)
	for _, bundle := range bundles {
		root := rootFor(bundle.Meta.SessionID)
		groupID := string(bundle.Meta.Provider) + ":" + root
		group := groupMap[groupID]
		if group == nil {
			group = &SourceGroup{
				GroupID:             groupID,
				WorkspacePath:       bundle.Meta.WorkspacePath,
				WorkspaceFamilyPath: bundle.Meta.WorkspaceFamilyPath,
			}
			groupMap[groupID] = group
		}
		if group.WorkspacePath == "" {
			group.WorkspacePath = bundle.Meta.WorkspacePath
		}
		if group.WorkspaceFamilyPath == "" {
			group.WorkspaceFamilyPath = firstNonEmpty(bundle.Meta.WorkspaceFamilyPath, workspace.FamilyPath(bundle.Meta.WorkspacePath))
		}
		group.Bundles = append(group.Bundles, bundle)
	}

	groups := make([]SourceGroup, 0, len(groupMap))
	for _, group := range groupMap {
		sort.SliceStable(group.Bundles, func(i, j int) bool {
			return group.Bundles[i].Meta.StartedAt.Before(group.Bundles[j].Meta.StartedAt)
		})
		groups = append(groups, *group)
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].GroupID < groups[j].GroupID })
	return groups
}

// CombineMainline merges only non-sidecar transcripts into one parsed session.
func CombineMainline(group SourceGroup) sourceimport.ParsedSession {
	mainline := group.MainlineBundles()
	combined := sourceimport.ParsedSession{
		Provider:      mainline[0].Parsed.Provider,
		SessionID:     group.GroupID,
		SourcePath:    strings.Join(group.SourceFiles(), ","),
		WorkspacePath: group.WorkspacePath,
	}
	for _, bundle := range mainline {
		for _, turn := range bundle.Parsed.Turns {
			combined.Turns = append(combined.Turns, turn.Clone())
		}
	}
	sort.SliceStable(combined.Turns, func(i, j int) bool {
		if combined.Turns[i].CreatedAt.Equal(combined.Turns[j].CreatedAt) {
			return combined.Turns[i].TurnIndex < combined.Turns[j].TurnIndex
		}
		return combined.Turns[i].CreatedAt.Before(combined.Turns[j].CreatedAt)
	})
	return combined
}

// InspectSource derives transcript metadata from a parsed session and raw source file.
func InspectSource(path string, parsed sourceimport.ParsedSession, workspaceHint string) (SourceMeta, error) {
	meta := SourceMeta{
		Provider:            parsed.Provider,
		SessionID:           parsed.SessionID,
		SourcePath:          path,
		WorkspacePath:       strings.TrimSpace(parsed.WorkspacePath),
		WorkspaceFamilyPath: workspace.FamilyPath(strings.TrimSpace(parsed.WorkspacePath)),
	}
	if len(parsed.Turns) > 0 {
		meta.StartedAt = parsed.Turns[0].CreatedAt
	}

	switch parsed.Provider {
	case sourceimport.ProviderCodex:
		meta, err := inspectCodexSource(path, meta)
		if err != nil {
			return meta, err
		}
		if meta.WorkspacePath == "" {
			meta.WorkspacePath = strings.TrimSpace(workspaceHint)
		}
		if meta.WorkspaceFamilyPath == "" {
			meta.WorkspaceFamilyPath = workspace.FamilyPath(firstNonEmpty(meta.WorkspacePath, workspaceHint))
		}
		return meta, nil
	default:
		if meta.WorkspacePath == "" {
			meta.WorkspacePath = strings.TrimSpace(workspaceHint)
		}
		if meta.WorkspaceFamilyPath == "" {
			meta.WorkspaceFamilyPath = workspace.FamilyPath(firstNonEmpty(meta.WorkspacePath, workspaceHint))
		}
		return meta, nil
	}
}

func inspectCodexSource(path string, meta SourceMeta) (SourceMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return meta, fmt.Errorf("transcriptpipeline: inspect codex transcript open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*32), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var envelope struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp,omitempty"`
			Payload   json.RawMessage `json:"payload,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}
		if envelope.Type != "session_meta" {
			continue
		}
		var payload struct {
			ID            string          `json:"id"`
			ForkedFromID  string          `json:"forked_from_id,omitempty"`
			Cwd           string          `json:"cwd,omitempty"`
			Timestamp     string          `json:"timestamp,omitempty"`
			AgentNickname string          `json:"agent_nickname,omitempty"`
			AgentRole     string          `json:"agent_role,omitempty"`
			Source        json.RawMessage `json:"source,omitempty"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return meta, fmt.Errorf("transcriptpipeline: inspect codex payload: %w", err)
		}
		if strings.TrimSpace(payload.ID) != "" {
			meta.SessionID = strings.TrimSpace(payload.ID)
		}
		meta.ParentSessionID = strings.TrimSpace(payload.ForkedFromID)
		meta.AgentNickname = strings.TrimSpace(payload.AgentNickname)
		meta.AgentRole = strings.TrimSpace(payload.AgentRole)
		meta.IsSubagent = codexSessionMetaHasSubagent(payload.Source) || meta.AgentRole != ""
		if strings.TrimSpace(meta.WorkspacePath) == "" && strings.TrimSpace(payload.Cwd) != "" {
			meta.WorkspacePath = strings.TrimSpace(payload.Cwd)
		}
		if strings.TrimSpace(meta.WorkspaceFamilyPath) == "" {
			meta.WorkspaceFamilyPath = workspace.FamilyPath(firstNonEmpty(meta.WorkspacePath, payload.Cwd))
		}
		if ts := strings.TrimSpace(firstNonEmpty(payload.Timestamp, envelope.Timestamp)); ts != "" {
			if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
				meta.StartedAt = parsed.UTC()
			} else if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				meta.StartedAt = parsed.UTC()
			}
		}
		return meta, nil
	}
	if err := scanner.Err(); err != nil {
		return meta, fmt.Errorf("transcriptpipeline: inspect codex scan: %w", err)
	}
	return meta, nil
}

func codexSessionMetaHasSubagent(raw json.RawMessage) bool {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	_, ok := obj["subagent"]
	return ok
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
