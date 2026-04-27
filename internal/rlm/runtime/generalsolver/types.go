package generalsolver

const (
	ArchetypeExplicitDAG     ProblemArchetype = "explicit_dag"
	ArchetypeStateTransition ProblemArchetype = "state_transition"
	ArchetypeSymbolicTrace   ProblemArchetype = "symbolic_trace"
	ArchetypeGraphSearch     ProblemArchetype = "graph_search"
	ArchetypeTableRecurrence ProblemArchetype = "table_recurrence"
	ArchetypeConstraintSolve ProblemArchetype = "constraint_solve"
	ArchetypeCandidateVerify ProblemArchetype = "candidate_verify"
	ArchetypeMixed           ProblemArchetype = "mixed"
)

type ProblemArchetype string

const (
	StatusPending  WorkItemStatus = "pending"
	StatusReady    WorkItemStatus = "ready"
	StatusSolving  WorkItemStatus = "solving"
	StatusSolved   WorkItemStatus = "solved"
	StatusBlocked  WorkItemStatus = "blocked"
	StatusFailed   WorkItemStatus = "failed"
)

type WorkItemStatus string

type WorkItem struct {
	ID              string         `json:"id"`
	Goal            string         `json:"goal"`
	Archetype       ProblemArchetype `json:"archetype"`
	DependsOn       []string       `json:"depends_on,omitempty"`
	Status          WorkItemStatus `json:"status"`
	Attempts        int            `json:"attempts"`
	MaxAttempts     int            `json:"max_attempts,omitempty"`
	Priority        float64        `json:"priority"`
	Risk            float64        `json:"risk"`
	MaxSummaryChars int            `json:"max_summary_chars,omitempty"`
	Payload         map[string]any `json:"payload,omitempty"`
}

type WorkArtifact struct {
	WorkItemID      string         `json:"work_item_id"`
	Status          string         `json:"status"`
	Answer          any            `json:"answer,omitempty"`
	Code            string         `json:"code,omitempty"`
	Derived         map[string]any `json:"derived,omitempty"`
	Evidence        map[string]any `json:"evidence,omitempty"`
	Checks          []string       `json:"checks,omitempty"`
	Counterexamples []map[string]any `json:"counterexamples,omitempty"`
	Confidence      float64        `json:"confidence"`
}

type WorkVerdict struct {
	Accept     bool           `json:"accept"`
	Repairable bool           `json:"repairable"`
	Confidence float64        `json:"confidence"`
	Feedback   map[string]any `json:"feedback,omitempty"`
}

func ValidProblemArchetype(a ProblemArchetype) bool {
	switch a {
	case ArchetypeExplicitDAG, ArchetypeStateTransition, ArchetypeSymbolicTrace,
		ArchetypeGraphSearch, ArchetypeTableRecurrence, ArchetypeConstraintSolve,
		ArchetypeCandidateVerify, ArchetypeMixed:
		return true
	}
	return false
}

func ValidWorkItemStatus(s WorkItemStatus) bool {
	switch s {
	case StatusPending, StatusReady, StatusSolving, StatusSolved, StatusBlocked, StatusFailed:
		return true
	}
	return false
}
