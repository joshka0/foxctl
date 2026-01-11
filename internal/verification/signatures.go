package verification

import (
	"github.com/XiaoConstantine/dspy-go/pkg/core"
)

// BuildBaselineSignature creates the signature for the baseline (drafter) agent.
// This agent generates an initial detailed response that will be verified.
func BuildBaselineSignature() *core.Signature {
	sig := core.NewSignature(
		[]core.InputField{
			{Field: core.NewField("question", core.WithDescription("User query to answer"))},
		},
		[]core.OutputField{
			{Field: core.NewField("baseline_response", core.WithDescription("Initial detailed response to the question"))},
		},
	).WithInstruction(`You are a knowledgeable assistant. Provide a detailed, comprehensive answer to the question.
Include specific facts, data points, and claims that can be verified.
Structure your response clearly with distinct, verifiable statements.`)

	return &sig
}

// BuildDraftVerifierSignature creates the signature for verification agents.
// Uses single combined input due to dspy-go Predict module limitation with multi-input fields.
func BuildDraftVerifierSignature() *core.Signature {
	sig := core.NewSignature(
		[]core.InputField{
			{Field: core.NewField("verification_query", core.WithDescription("Context and claim to verify"))},
		},
		[]core.OutputField{
			{Field: core.NewField("draft_verdict", core.WithDescription("Source: [reason] -> Verdict: [True/False/Uncertain]"))},
		},
	).WithInstruction("Verify the claim. Output: Source: [reason] -> Verdict: [True/False/Uncertain]")

	return &sig
}

// BuildRefinerSignature creates the signature for the refinement agent.
// This agent takes the baseline and verification notes to produce a corrected final answer.
func BuildRefinerSignature(mode CoVeMode) *core.Signature {
	instruction := `You are a Refiner. Your job is to produce an accurate final answer by incorporating verification feedback.

PROCESS:
1. Review the baseline response
2. Check each verification note for False or Uncertain verdicts
3. Correct any inaccurate claims in your final answer
4. Preserve accurate claims (True verdicts) unchanged
5. Note what corrections you made

OUTPUT:
- final_answer: The corrected, accurate response
- corrections_made: List each correction as "Original: [wrong claim] -> Corrected: [fixed claim] (reason: [why])"

If no corrections needed, state "No corrections needed - all claims verified as accurate."`

	if mode == CoVeModeGate {
		instruction = `You are a Refiner acting as a strict completion gate.
Use the verification notes to decide whether the Definition of Done is met.

Return final_answer in EXACTLY this format (no extra headings):
STATUS: DONE|NOT DONE
BLOCKERS:
- <bullet(s)>
EVIDENCE:
- <bullet(s)>

Rules:
- DONE only if all relevant claims are verified True (no False/Uncertain/Error).
- Treat Uncertain as NOT DONE.
- BLOCKERS: 0-3 bullets; if DONE, write "- none".
- EVIDENCE: 0-3 bullets; cite claim IDs and verifier evidence from verification_notes; if none, write "- none".
- Keep bullets short (<= 120 chars). 
- Set corrections_made to exactly: "No corrections needed."`
	}

	sig := core.NewSignature(
		[]core.InputField{
			{Field: core.NewField("question", core.WithDescription("The original question"))},
			{Field: core.NewField("baseline", core.WithDescription("The original response to be refined"))},
			{Field: core.NewField("verification_notes", core.WithDescription("Aggregated verification results from Draft verifiers"))},
		},
		[]core.OutputField{
			{Field: core.NewField("final_answer", core.WithDescription("Corrected answer incorporating verification feedback"))},
			{Field: core.NewField("corrections_made", core.WithDescription("List of corrections: 'Original: X -> Corrected: Y (reason)'"))},
		},
	).WithInstruction(instruction)

	return &sig
}

// BuildClaimExtractorSignature creates the signature for extracting claims from a baseline.
func BuildClaimExtractorSignature() *core.Signature {
	sig := core.NewSignature(
		[]core.InputField{
			{Field: core.NewField("text", core.WithDescription("Text to extract claims from"))},
		},
		[]core.OutputField{
			{Field: core.NewField("claims", core.WithDescription("JSON array: [{\"id\":\"c1\",\"text\":\"claim\",\"category\":\"factual\"}]"))},
		},
	).WithInstruction(`Extract verifiable factual claims as JSON array.
Each claim: {"id": "c1", "text": "claim statement", "category": "factual|numerical|temporal"}
Focus on facts, numbers, dates. Skip opinions.
Example: [{"id":"c1","text":"Paris is France's capital","category":"factual"}]`)

	return &sig
}
