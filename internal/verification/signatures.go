package verification

import (
	"encoding/json"
	"fmt"
	"strings"
)

// baselineSystemPrompt returns the system prompt for baseline generation.
func baselineSystemPrompt() string {
	return `You are a knowledgeable assistant. Provide a detailed, comprehensive answer to the question.
Include specific facts, data points, and claims that can be verified.
Structure your response clearly with distinct, verifiable statements.`
}

func baselineUserPrompt(question string, context map[string]any) string {
	if len(context) == 0 {
		return fmt.Sprintf("Question: %s", question)
	}
	contextJSON, err := json.Marshal(context)
	if err != nil {
		return fmt.Sprintf("Question: %s\nContext: unavailable (%v)", question, err)
	}
	return fmt.Sprintf("Question: %s\nContext: %s", question, string(contextJSON))
}

// draftVerifierSystemPrompt returns the system prompt for claim verification.
func draftVerifierSystemPrompt() string {
	return "Verify the claim. Output: Source: [reason] -> Verdict: [True/False/Uncertain]"
}

func draftVerifierUserPrompt(question, claim string) string {
	return fmt.Sprintf("Context: %s\nClaim: %s", question, claim)
}

// refinerSystemPrompt returns the system prompt for refinement.
func refinerSystemPrompt(mode CoVeMode) string {
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

If no corrections needed, state "No corrections needed - all claims verified as accurate."
Return a JSON object with keys "final_answer" and "corrections_made".`

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
- Set corrections_made to exactly: "No corrections needed."
Return a JSON object with keys "final_answer" and "corrections_made".`
	}

	return instruction
}

func refinerUserPrompt(question, baseline, verificationNotes string) string {
	return fmt.Sprintf("Question: %s\n\nBaseline:\n%s\n\nVerification Notes:\n%s", question, baseline, verificationNotes)
}

// claimExtractorSystemPrompt returns the system prompt for claim extraction.
func claimExtractorSystemPrompt() string {
	return `Extract verifiable factual claims as JSON array.
Each claim: {"id": "c1", "text": "claim statement", "category": "factual|numerical|temporal"}
Focus on facts, numbers, dates. Skip opinions.
Example: [{"id":"c1","text":"Paris is France's capital","category":"factual"}]`
}

func claimExtractorUserPrompt(text string) string {
	return fmt.Sprintf("Text: %s", strings.TrimSpace(text))
}
