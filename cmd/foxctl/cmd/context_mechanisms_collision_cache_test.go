package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
)

func TestContextMechanismsCollisionCacheListCommandLoadsTypedRecords(t *testing.T) {
	vaultRoot := t.TempDir()
	note, err := contextplane.PlanMemoryCollisionCacheNote(contextplane.MemoryCollisionCacheInput{
		WorkspaceID: "workspace-foxctl",
		Query: contextplane.MechanismQuery{
			ID:             "query:one",
			Domain:         "go:internal/context/contextplane",
			Text:           "func BuildRepoMotifArtifacts()",
			AbstractSchema: "build motif artifacts from local graph structure",
			MechanismTags:  []string{"graph_shape"},
		},
		Syntheses: []contextplane.MemoryCollisionSynthesis{{
			AgentProvider:     "openrouter",
			AgentModel:        "example/model",
			BisociationMode:   contextplane.MemoryCollisionAgentModeFarAlien,
			SelectionMode:     "far",
			PromptAbstraction: "alien",
			Collision: contextplane.MemoryCollisionCell{
				CollisionID:          "memory_collision:one",
				MemoryID:             "memory-one",
				MemoryDomain:         "go:cmd/foxctl/cmd/sessionscmd",
				MemorySummary:        "session fanout shape",
				AbstractSchema:       "distributed session event routing",
				StructuralSimilarity: 0.91,
				LiteralSimilarity:    0.12,
				CollisionScore:       0.84,
			},
			Output: contextplane.MemoryCollisionAgentOutput{
				BridgeSchema:      "Move fanout behind an intent boundary.",
				NewCollision:      "Preserve the facade and route internal intent handlers.",
				TransferSteps:     []string{"name the facade", "move fanout"},
				Confidence:        0.82,
				NoveltyConfidence: 0.74,
			},
			Validation: contextplane.MemoryCollisionAgentValidation{Valid: true},
		}},
		CreatedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanMemoryCollisionCacheNote: %v", err)
	}
	if err := contextplane.WriteMemoryCollisionCacheNote(context.Background(), vaultRoot, note); err != nil {
		t.Fatalf("WriteMemoryCollisionCacheNote: %v", err)
	}

	cmd := newContextMechanismsCollisionCacheListCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--vault-path", vaultRoot, "--mode", "far-alien", "--include-syntheses"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("collision-cache list: %v", err)
	}

	var got struct {
		Status string `json:"status"`
		Data   struct {
			RecordCount int `json:"record_count"`
			Records     []struct {
				NotePath         string   `json:"note_path"`
				BisociationModes []string `json:"bisociation_modes"`
				MemoryDomains    []string `json:"memory_domains"`
				Syntheses        []struct {
					BisociationMode string `json:"bisociation_mode"`
				} `json:"syntheses"`
			} `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode command output: %v\n%s", err, out.String())
	}
	if got.Status != "ok" || got.Data.RecordCount != 1 || len(got.Data.Records) != 1 {
		t.Fatalf("unexpected command output: %#v", got)
	}
	record := got.Data.Records[0]
	if record.NotePath != note.NotePath {
		t.Fatalf("unexpected note path: %s", record.NotePath)
	}
	if len(record.BisociationModes) != 1 || record.BisociationModes[0] != "far-alien" {
		t.Fatalf("unexpected modes: %#v", record.BisociationModes)
	}
	if len(record.MemoryDomains) != 1 || record.MemoryDomains[0] != "go:cmd/foxctl/cmd/sessionscmd" {
		t.Fatalf("unexpected domains: %#v", record.MemoryDomains)
	}
	if len(record.Syntheses) != 1 || record.Syntheses[0].BisociationMode != "far-alien" {
		t.Fatalf("expected full syntheses in output: %#v", record.Syntheses)
	}
}
