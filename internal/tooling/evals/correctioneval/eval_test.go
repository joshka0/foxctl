package correctioneval

import (
	"path/filepath"
	"testing"
)

func TestLoadSuite(t *testing.T) {
	suite, err := LoadSuite(filepath.Join("..", "..", "..", "..", "testdata", "evals", "corrections", "agentctl-inspectors.yaml"))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if suite.Name != "agentctl-inspectors" {
		t.Fatalf("suite.Name=%q", suite.Name)
	}
	if len(suite.Cases) == 0 {
		t.Fatal("expected cases")
	}
}

func TestSummarize(t *testing.T) {
	summaries := Summarize([]CaseResult{
		{Method: "aca_retrieve", ClassificationMatch: true},
		{Method: "aca_retrieve", ClassificationMatch: false, FixChecked: true, FixMatch: true},
		{Method: "repoindex_dag", ClassificationMatch: true, FixChecked: true, FixMatch: false},
	})
	if len(summaries) != 2 {
		t.Fatalf("summaries=%d want 2", len(summaries))
	}
	if summaries[0].Method != "aca_retrieve" || summaries[0].ClassificationAccuracy != 0.5 || summaries[0].FixAccuracy != 1.0 || summaries[0].FixChecks != 1 {
		t.Fatalf("aca summary=%+v", summaries[0])
	}
	if summaries[1].Method != "repoindex_dag" || summaries[1].ClassificationAccuracy != 1.0 || summaries[1].FixAccuracy != 0.0 || summaries[1].FixChecks != 1 {
		t.Fatalf("dag summary=%+v", summaries[1])
	}
}
