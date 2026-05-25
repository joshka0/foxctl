package smolvm

import (
	"errors"
	"path"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

func TestNormalizeReadableID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "normalizes run id with spaces",
			in:   "Run 2026-04-21 Main",
			want: "run-2026-04-21-main",
		},
		{
			name: "flattens slash in scalar ids",
			in:   "Researcher/Child#1",
			want: "researcher-child-1",
		},
		{
			name: "keeps allowed separators",
			in:   "alpha_beta-01",
			want: "alpha_beta-01",
		},
		{
			name: "empty after normalization",
			in:   "___",
			want: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeReadableID(tc.in); got != tc.want {
				t.Fatalf("NormalizeReadableID(%q)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeReadableAgentIDPreservesHierarchy(t *testing.T) {
	t.Parallel()

	if got := NormalizeReadableAgentID("Agent Root/RLM 0001/Child#2"); got != "agent-root/rlm-0001/child-2" {
		t.Fatalf("NormalizeReadableAgentID()=%q", got)
	}
}

func TestNormalizeReadableAgentIDRemovesTraversalSegments(t *testing.T) {
	t.Parallel()

	got := NormalizeReadableAgentID(`Agent Root/../Escape/.\Child`)
	if got != "agent-root/escape/child" {
		t.Fatalf("NormalizeReadableAgentID()=%q", got)
	}
}

func TestNormalizeReadableAgentIDPropertyProducesSafeRelativeSegments(t *testing.T) {
	t.Parallel()

	property := func(raw string) bool {
		got := NormalizeReadableAgentID(raw)
		if got == "" {
			return true
		}
		if strings.HasPrefix(got, "/") || strings.HasSuffix(got, "/") || strings.Contains(got, "\\") {
			return false
		}
		for _, segment := range strings.Split(got, "/") {
			if segment == "" || segment == "." || segment == ".." {
				return false
			}
		}
		return path.Clean(got) == got
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestPlanOutputLayout(t *testing.T) {
	t.Parallel()

	plan, err := PlanOutputLayout(
		"/mnt/out",
		"Run 2026-04-21 Main",
		[]string{"Researcher/Child#1", "Researcher/Child#1", "Planner"},
	)
	if err != nil {
		t.Fatalf("PlanOutputLayout() error = %v", err)
	}

	if plan.RootDir != "/mnt/out" {
		t.Fatalf("RootDir=%q want /mnt/out", plan.RootDir)
	}
	if plan.Run.ID != "run-2026-04-21-main" {
		t.Fatalf("Run.ID=%q", plan.Run.ID)
	}

	gotIDs := make([]string, 0, len(plan.Run.Agents))
	for _, agent := range plan.Run.Agents {
		gotIDs = append(gotIDs, agent.ID)
	}
	wantIDs := []string{"researcher/child-1", "researcher/child-1-2", "planner"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("agent ids=%v want %v", gotIDs, wantIDs)
	}

	first := plan.Run.Agents[0]
	if first.TrajectoryPath != "/mnt/out/runs/run-2026-04-21-main/agents/researcher/child-1/trajectory.jsonl" {
		t.Fatalf("first trajectory path=%q", first.TrajectoryPath)
	}
	if first.ArtifactsDir != "/mnt/out/runs/run-2026-04-21-main/agents/researcher/child-1/artifacts" {
		t.Fatalf("first artifacts dir=%q", first.ArtifactsDir)
	}
	if first.ScratchDir != "/mnt/out/runs/run-2026-04-21-main/agents/researcher/child-1/scratch" {
		t.Fatalf("first scratch dir=%q", first.ScratchDir)
	}

	sharedJoined := strings.Join(plan.SharedReadPaths, "\n")
	if strings.Contains(sharedJoined, "/scratch") {
		t.Fatalf("shared paths should not include scratch: %v", plan.SharedReadPaths)
	}
	if !strings.Contains(sharedJoined, "/blackboard.jsonl") {
		t.Fatalf("shared paths should include blackboard: %v", plan.SharedReadPaths)
	}
}

func TestPlanOutputLayoutPropertyAgentPathsStayUnderAgentsRoot(t *testing.T) {
	t.Parallel()

	property := func(rawAgentID string) bool {
		normalizedAgentID := NormalizeReadableAgentID(rawAgentID)
		plan, err := PlanOutputLayout("/mnt/out", "run-1", []string{rawAgentID})
		if normalizedAgentID == "" {
			return errors.Is(err, ErrInvalidAgentID)
		}
		if err != nil || len(plan.Run.Agents) != 1 {
			return false
		}

		agentsRoot := "/mnt/out/runs/run-1/agents"
		agent := plan.Run.Agents[0]
		for _, candidate := range []string{
			agent.Dir,
			agent.TrajectoryPath,
			agent.ArtifactsDir,
			agent.ScratchDir,
		} {
			cleaned := path.Clean(candidate)
			if cleaned != candidate {
				return false
			}
			if cleaned != agentsRoot && !strings.HasPrefix(cleaned, agentsRoot+"/") {
				return false
			}
		}

		for _, shared := range plan.SharedReadPaths {
			if strings.Contains(shared, "/scratch") {
				return false
			}
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestPlanOutputLayoutErrors(t *testing.T) {
	t.Parallel()

	_, err := PlanOutputLayout("", "run-1", nil)
	if !errors.Is(err, ErrInvalidOutputRoot) {
		t.Fatalf("empty root error=%v", err)
	}

	_, err = PlanOutputLayout("/mnt/out", "!!!", nil)
	if !errors.Is(err, ErrInvalidRunID) {
		t.Fatalf("invalid run id error=%v", err)
	}

	_, err = PlanOutputLayout("/mnt/out", "run-1", []string{"###"})
	if !errors.Is(err, ErrInvalidAgentID) {
		t.Fatalf("invalid agent id error=%v", err)
	}
}
