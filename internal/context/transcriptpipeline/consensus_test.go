package transcriptpipeline

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

func TestDeriveConsensusClaims_UsesSidecarSummarySupport(t *testing.T) {
	sidecars := []SidecarPacket{
		{
			SessionID:   "side-1",
			SummaryText: "Implement auto-memory as a second-pass consolidator over the existing hybrid companion runtime.",
		},
		{
			SessionID:   "side-2",
			SummaryText: "The design should keep auto-memory as a second-pass consolidator on top of the hybrid companion runtime.",
		},
	}

	claims := DeriveConsensusClaims(sidecars, sourceimport.ParsedSession{}, nil, nil, []ConsensusClaim{{
		Text: "Implement auto-memory as a second-pass consolidator over the existing hybrid companion runtime.",
	}})
	if len(claims) != 1 {
		t.Fatalf("claims=%d want 1", len(claims))
	}
	if claims[0].SupportCount != 2 {
		t.Fatalf("support_count=%d want 2", claims[0].SupportCount)
	}
}
