package verification

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/modules"
)

// Spawner manages parallel verification agents using a worker pool pattern.
// It implements fan-out/fan-in concurrency for high-throughput claim verification.
type Spawner struct {
	llm    core.LLM
	config SpawnerConfig

	mu         sync.RWMutex
	activeJobs int
}

// NewSpawner creates a new verification spawner with the given LLM and config.
func NewSpawner(llm core.LLM, config SpawnerConfig) *Spawner {
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = 10
	}
	if config.DefaultTimeout <= 0 {
		config.DefaultTimeout = 30 * time.Second
	}
	if config.QueueSize <= 0 {
		config.QueueSize = config.MaxWorkers * 2
	}

	return &Spawner{
		llm:    llm,
		config: config,
	}
}

type verificationJob struct {
	claim   Claim
	context string
}

type verificationResult struct {
	claimID string
	result  VerificationResult
}

// SpawnVerifiers verifies multiple claims in parallel using a worker pool.
// This is the core "Swarm" implementation that enables high-throughput verification.
//
// Index:
// - Purpose: Verify a batch of claims concurrently and aggregate results
// - Flow: size worker pool → enqueue jobs → collect results → tally verdict counts
// - SideEffects: spawns goroutines; LLM calls per claim
// - FailureModes: none (per-claim errors captured in results)
// - Related: Spawner.worker, Spawner.verifyClaim
// - Keywords: verification, claims, parallelism, max_workers, queue_size, verdict
func (s *Spawner) SpawnVerifiers(ctx context.Context, question string, claims []Claim) (*BatchVerificationResult, error) {
	if len(claims) == 0 {
		return &BatchVerificationResult{
			Results:     make(map[string]VerificationResult),
			TotalClaims: 0,
			Parallelism: s.config.MaxWorkers,
		}, nil
	}

	startTime := time.Now()

	workers := s.config.MaxWorkers
	if len(claims) < workers {
		workers = len(claims)
	}

	jobChan := make(chan verificationJob, s.config.QueueSize)
	resultChan := make(chan verificationResult, len(claims))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			s.worker(ctx, workerID, jobChan, resultChan)
		}(i)
	}

	for _, claim := range claims {
		jobChan <- verificationJob{
			claim:   claim,
			context: question,
		}
	}
	close(jobChan)

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	batch := &BatchVerificationResult{
		Results:     make(map[string]VerificationResult),
		TotalClaims: len(claims),
		Parallelism: workers,
	}

	for vr := range resultChan {
		batch.Results[vr.claimID] = vr.result
		batch.VerifiedCount++

		switch vr.result.Verdict {
		case VerdictTrue:
			batch.TrueCount++
		case VerdictFalse:
			batch.FalseCount++
		case VerdictUncertain:
			batch.UncertainCount++
		}

		if vr.result.Error != "" {
			batch.ErrorCount++
		}
	}

	batch.TotalDuration = time.Since(startTime)
	return batch, nil
}

func (s *Spawner) worker(ctx context.Context, workerID int, jobs <-chan verificationJob, results chan<- verificationResult) {
	for job := range jobs {
		select {
		case <-ctx.Done():
			results <- verificationResult{
				claimID: job.claim.ID,
				result: VerificationResult{
					ClaimID: job.claim.ID,
					Claim:   job.claim.Text,
					Error:   ctx.Err().Error(),
				},
			}
			continue
		default:
		}

		result := s.verifyClaim(ctx, job)
		results <- verificationResult{
			claimID: job.claim.ID,
			result:  result,
		}
	}
}

func (s *Spawner) verifyClaim(ctx context.Context, job verificationJob) VerificationResult {
	startTime := time.Now()

	verifierCtx, cancel := context.WithTimeout(ctx, s.config.DefaultTimeout)
	defer cancel()

	sig := BuildDraftVerifierSignature()
	predict := modules.NewPredict(*sig).WithTextOutput()
	predict.SetLLM(s.llm)

	// Combine context and claim into single input (dspy-go Predict limitation with multi-input)
	verificationQuery := fmt.Sprintf("Context: %s\nClaim: %s", job.context, job.claim.Text)
	input := map[string]any{
		"verification_query": verificationQuery,
	}

	resultMap, err := predict.Process(verifierCtx, input)
	if err != nil {
		return VerificationResult{
			ClaimID:  job.claim.ID,
			Claim:    job.claim.Text,
			Error:    fmt.Sprintf("verification failed: %v", err),
			Duration: time.Since(startTime),
		}
	}

	rawOutput := extractDraftOutput(resultMap)
	verdict, evidence := parseDraftOutput(rawOutput)

	return VerificationResult{
		ClaimID:   job.claim.ID,
		Claim:     job.claim.Text,
		Verdict:   verdict,
		Evidence:  evidence,
		RawOutput: rawOutput,
		Duration:  time.Since(startTime),
	}
}

func extractDraftOutput(resultMap map[string]any) string {
	if resultMap == nil {
		return ""
	}

	if v, ok := resultMap["draft_verdict"].(string); ok && v != "" {
		return v
	}

	for _, key := range []string{"result", "output", "answer", "thought"} {
		if v, ok := resultMap[key].(string); ok && v != "" {
			return v
		}
	}

	return fmt.Sprintf("%v", resultMap)
}

var draftOutputRegex = regexp.MustCompile(`(?i)Source:\s*(.+?)\s*->\s*Verdict:\s*(True|False|Uncertain)`)

func parseDraftOutput(raw string) (Verdict, string) {
	matches := draftOutputRegex.FindStringSubmatch(raw)
	if len(matches) >= 3 {
		evidence := strings.TrimSpace(matches[1])
		verdictStr := strings.TrimSpace(matches[2])

		var verdict Verdict
		switch strings.ToLower(verdictStr) {
		case "true":
			verdict = VerdictTrue
		case "false":
			verdict = VerdictFalse
		default:
			verdict = VerdictUncertain
		}

		return verdict, evidence
	}

	rawLower := strings.ToLower(raw)
	if strings.Contains(rawLower, "true") && !strings.Contains(rawLower, "false") {
		return VerdictTrue, raw
	}
	if strings.Contains(rawLower, "false") {
		return VerdictFalse, raw
	}

	return VerdictUncertain, raw
}

// VerifySingle verifies a single claim (convenience method).
func (s *Spawner) VerifySingle(ctx context.Context, question string, claim Claim) VerificationResult {
	job := verificationJob{
		claim:   claim,
		context: question,
	}
	return s.verifyClaim(ctx, job)
}

// ActiveJobs returns the current number of active verification jobs.
func (s *Spawner) ActiveJobs() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeJobs
}
