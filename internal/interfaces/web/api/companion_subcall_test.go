package api

import (
	"encoding/json"
	"testing"

	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
)

func TestDecodeCompanionSubcallResult(t *testing.T) {
	awaitResp := v2jido.AwaitResponse{
		Status: "completed",
		Result: json.RawMessage(`{"artifact":"sha256:child-artifact"}`),
	}
	stateResp := v2jido.StateResponse{
		Status: "ok",
		State: json.RawMessage(`{
			"agentctl": {
				"last_result": {
					"summary": "Child found the prior session and extracted the key travel constraints.",
					"evidence_refs": ["session:abc", "memory:def"],
					"retrieved_paths": ["notes/travel.md"],
					"artifact": "sha256:child-artifact"
				}
			}
		}`),
	}

	got := decodeCompanionSubcallResult(awaitResp, stateResp)
	if got.Summary == "" {
		t.Fatal("expected summary")
	}
	if got.ArtifactRef != "sha256:child-artifact" {
		t.Fatalf("artifact_ref=%q", got.ArtifactRef)
	}
	if len(got.EvidenceRefs) != 2 {
		t.Fatalf("evidence_refs=%v want 2", got.EvidenceRefs)
	}
	if len(got.RetrievedPaths) != 1 || got.RetrievedPaths[0] != "notes/travel.md" {
		t.Fatalf("retrieved_paths=%v", got.RetrievedPaths)
	}
}

func TestParseStructuredSubcallAnswer(t *testing.T) {
	got := parseStructuredSubcallAnswer(`{"summary":"Found the active task.","next_action":"Continue taskflow cleanup.","evidence_refs":["session:abc"],"retrieved_paths":["docs/plan.md"]}`)
	if got.Summary == "" {
		t.Fatal("expected summary")
	}
	if len(got.EvidenceRefs) != 1 || got.EvidenceRefs[0] != "session:abc" {
		t.Fatalf("evidence_refs=%v", got.EvidenceRefs)
	}
	if len(got.RetrievedPaths) != 1 || got.RetrievedPaths[0] != "docs/plan.md" {
		t.Fatalf("retrieved_paths=%v", got.RetrievedPaths)
	}
	if !hasUsableSubcallResult(got) {
		t.Fatal("expected usable subcall result")
	}
}

func TestDecodeCompanionSubcallResult_DerivesSummaryFromEnvelope(t *testing.T) {
	awaitResp := v2jido.AwaitResponse{Status: "completed"}
	stateResp := v2jido.StateResponse{
		Status: "ok",
		State: json.RawMessage(`{
			"agentctl": {
				"last_result": {
					"envelope": {
						"command": "code/smart_search",
						"data": {
							"candidates": [
								{"path":"internal/context/contextplane/dispatch.go"},
								{"path":"internal/context/contextplane/taskhistory/taskhistory.go"}
							],
							"summary": {
								"files_relevant": 23,
								"candidates_generated": 25
							}
						}
					}
				}
			}
		}`),
	}

	got := decodeCompanionSubcallResult(awaitResp, stateResp)
	if got.Summary == "" {
		t.Fatal("expected derived summary")
	}
	if len(got.RetrievedPaths) == 0 {
		t.Fatal("expected derived retrieved paths")
	}
}
