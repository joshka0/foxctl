package longmemeval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/retrieval/memoryrecall"
	"github.com/joshka0/foxctl/internal/storage"
)

type retrievalDeps struct {
	Store       func(context.Context, string) (storage.MemoryStore, error)
	Search      func(context.Context, storage.MemoryStore, string, string, int) (memoryrecall.QueryResponse, error)
	Now         func() time.Time
	MemoryName  func(workspaceID, caseID, sessionID string) string
	Leakage     map[string]int
	Limit       int
	WorkspaceID string
}

type answerDeps struct {
	Run         AnswerRunner
	JudgeAnswer func(ctx context.Context, question, answer, expected string) (score float64, reason string, err error)
	Now         func() time.Time
	MemoryName  func(workspaceID, caseID, sessionID string) string
	Leakage     map[string]int
	Limit       int
	WorkspaceID string
}

func runRetrieval(ctx context.Context, deps retrievalDeps, cases []Case) ([]CaseResult, error) {
	if deps.Store == nil {
		return nil, errors.New("memory store opener is required for retrieval")
	}
	if deps.Search == nil {
		return nil, errors.New("search function is required for retrieval")
	}
	store, err := deps.Store(ctx, deps.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("open memory: %w", err)
	}
	defer func() { _ = store.Close() }()
	out := make([]CaseResult, 0, len(cases))
	for _, c := range cases {
		out = append(out, scoreCase(ctx, deps, store, c))
	}
	return out, nil
}

func scoreCase(ctx context.Context, deps retrievalDeps, store storage.MemoryStore, c Case) CaseResult {
	caseID := strings.TrimSpace(c.QuestionID)
	result := CaseResult{
		CaseID:             caseID,
		Question:           c.Question,
		ExpectedSessionIDs: append([]string(nil), c.AnswerSessionIDs...),
		ExpectedNames:      ExpectedMemoryNames(deps.MemoryName, deps.WorkspaceID, caseID, c.AnswerSessionIDs),
		Method:             "",
		RetrievedRanks:     map[string]int{},
	}
	if leakage, ok := deps.Leakage[caseID]; ok {
		result.AntiLeakageFindings = leakage
	}
	expected := caseExpectedSet(result.ExpectedNames)
	if len(expected) == 0 {
		result.Method = "skipped"
		result.Error = "no expected-evidence session IDs for case"
		return result
	}
	started := deps.Now()
	resp, err := deps.Search(ctx, store, deps.WorkspaceID, c.Question, deps.Limit)
	if err != nil {
		result.Method = resp.Method
		result.Error = err.Error()
		result.DurationMS = deps.Now().Sub(started).Milliseconds()
		return result
	}
	result.Method = resp.Method
	ranks := make(map[string]int, len(resp.Entries))
	for i, entry := range resp.Entries {
		name := strings.TrimSpace(entry.Entry.Name)
		if name == "" {
			name = strings.TrimSpace(entry.Entry.ID)
		}
		if name == "" {
			continue
		}
		result.RetrievedNames = append(result.RetrievedNames, name)
		result.RetrievedScores = append(result.RetrievedScores, entry.Score)
		ranks[name] = i + 1
		if expected[name] {
			result.MatchedNames = append(result.MatchedNames, name)
		}
	}
	result.RetrievedRanks = ranks
	bestRank := -1
	for _, name := range result.MatchedNames {
		if r, ok := ranks[name]; ok && (bestRank == -1 || r < bestRank) {
			bestRank = r
		}
	}
	if bestRank > 0 {
		result.ReciprocalRank = 1.0 / float64(bestRank)
		result.HitAt5 = bestRank <= 5
		result.HitAt10 = bestRank <= 10
		result.HitAt50 = bestRank <= 50
		result.HitAt100 = bestRank <= 100
	}
	result.DurationMS = deps.Now().Sub(started).Milliseconds()
	return result
}

func runAnswer(ctx context.Context, deps answerDeps, cases []Case) ([]CaseResult, error) {
	if deps.Run == nil {
		return nil, errors.New("answer runner is required for answer mode")
	}
	out := make([]CaseResult, 0, len(cases))
	for _, c := range cases {
		out = append(out, scoreAnswerCase(ctx, deps, c))
	}
	return out, nil
}

func scoreAnswerCase(ctx context.Context, deps answerDeps, c Case) CaseResult {
	caseID := strings.TrimSpace(c.QuestionID)
	expectedNames := ExpectedMemoryNames(deps.MemoryName, deps.WorkspaceID, caseID, c.AnswerSessionIDs)
	result := CaseResult{
		CaseID:             caseID,
		Question:           c.Question,
		Method:             "answer",
		ExpectedSessionIDs: append([]string(nil), c.AnswerSessionIDs...),
		ExpectedNames:      expectedNames,
		ExpectedAnswer:     c.Answer,
		RetrievedRanks:     map[string]int{},
	}
	if leakage, ok := deps.Leakage[caseID]; ok {
		result.AntiLeakageFindings = leakage
	}
	expected := caseExpectedSet(expectedNames)
	if len(expected) == 0 {
		result.Error = "no expected-evidence session IDs for case"
		return result
	}
	started := deps.Now()
	resp, err := deps.Run(ctx, AnswerRequest{
		WorkspaceID: deps.WorkspaceID,
		CaseID:      caseID,
		Question:    c.Question,
		Limit:       deps.Limit,
	})
	durationMS := deps.Now().Sub(started).Milliseconds()
	if resp.DurationMS > 0 {
		durationMS = resp.DurationMS
	}
	result.AnswerDurationMS = durationMS
	result.DurationMS = durationMS
	if err != nil {
		result.Error = err.Error()
		return result
	}
	method := strings.TrimSpace(resp.Method)
	if method == "" {
		method = "answer"
	}
	result.Method = method
	result.AnswerMethod = method
	result.Answer = strings.TrimSpace(resp.Answer)
	result.AnswerToolNames = append([]string(nil), resp.ToolNames...)
	result.AnswerEvidenceRefs = append([]string(nil), resp.EvidenceRefs...)
	result.AnswerIterations = resp.Iterations
	result.AnswerScore = answerMatchScore(result.Answer, c.Answer)
	result.AnswerMatched = result.AnswerScore > 0
	result.AnswerMethod = method

	// LLM judge: always record the raw judge verdict when configured.
	// The judge is used as a lenient secondary metric: it can upgrade a
	// deterministic FAIL to a PASS (paraphrase), but it never downgrades a
	// deterministic PASS.
	if deps.JudgeAnswer != nil {
		judgeScore, judgeReason, err := deps.JudgeAnswer(ctx, c.Question, result.Answer, c.Answer)
		if err == nil {
			result.AnswerJudgeScore = judgeScore
			result.AnswerJudgeReason = judgeReason
			if !result.AnswerMatched && judgeScore > 0 {
				result.AnswerScore = judgeScore
				result.AnswerMatched = true
				result.AnswerMethod = method + "-judge"
			}
		}
	}

	ranks := make(map[string]int, len(resp.EvidenceNames))
	for i, raw := range resp.EvidenceNames {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, seen := ranks[name]; seen {
			continue
		}
		rank := i + 1
		ranks[name] = rank
		result.RetrievedNames = append(result.RetrievedNames, name)
		result.AnswerEvidenceNames = append(result.AnswerEvidenceNames, name)
		if expected[name] {
			result.MatchedNames = append(result.MatchedNames, name)
			result.AnswerMatchedEvidence = append(result.AnswerMatchedEvidence, name)
		}
	}
	result.RetrievedRanks = ranks
	bestRank := -1
	for _, name := range result.MatchedNames {
		if r, ok := ranks[name]; ok && (bestRank == -1 || r < bestRank) {
			bestRank = r
		}
	}
	if bestRank > 0 {
		result.ReciprocalRank = 1.0 / float64(bestRank)
		result.HitAt5 = bestRank <= 5
		result.HitAt10 = bestRank <= 10
		result.HitAt50 = bestRank <= 50
		result.HitAt100 = bestRank <= 100
	}
	return result
}
