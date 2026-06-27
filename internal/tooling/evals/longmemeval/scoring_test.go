package longmemeval

import "testing"

func TestAnswerMatchScoreRejectsQuotedExpectedValueInRefusal(t *testing.T) {
	t.Parallel()

	answer := `I cannot provide a verified answer. The evidence ledger rejected all candidate memories; one rejected claim mentioned 45 minutes each way, but it was not accepted.`
	if got := answerMatchScore(answer, "45 minutes each way"); got != 0 {
		t.Fatalf("answerMatchScore()=%v want 0 for refused/rejected quoted value", got)
	}
	if got := answerMatchScore("Your commute is 45 minutes each way.", "45 minutes each way"); got != 1 {
		t.Fatalf("answerMatchScore()=%v want 1 for direct supported wording", got)
	}
	if got := answerMatchScore("Target", "Target checkout last Sunday"); got != 0 {
		t.Fatalf("answerMatchScore()=%v want 0 for partial substring answer", got)
	}
}

func TestKeyFactOverlapScoreCatchesParaphrasedAnswer(t *testing.T) {
	t.Parallel()

	// Video-editing case: answer correctly identifies Premiere Pro preference
	// but phrases it differently from the expected answer.
	answer := "I found a strong match in memory from a past conversation where you were exploring advanced settings in Adobe Premiere Pro. Since you already enjoy using Premiere Pro, here are the resources we discussed."
	expected := "The user would prefer responses that suggest resources specifically tailored to Adobe Premiere Pro, especially those that delve into its advanced settings."
	score := answerMatchScore(answer, expected)
	if score == 0 {
		t.Fatalf("paraphrased correct answer should score > 0 via key-fact overlap")
	}
	if score < 0.3 {
		t.Fatalf("overlap score %f should be >= 0.3 for this case", score)
	}
}

func TestKeyFactOverlapScoreRejectsWrongAnswer(t *testing.T) {
	t.Parallel()

	// Clothing case: expected "3", answer says "2 items" — genuinely wrong.
	score := answerMatchScore("you need to attend to 2 items of clothing", "3")
	if score > 0 {
		t.Fatalf("wrong numeric answer should score 0, got %f", score)
	}
}

func TestKeyFactOverlapScoreShortExpectedUsesStrictOnly(t *testing.T) {
	t.Parallel()

	// Short expected answer ("yes") should not trigger overlap scoring.
	score := answerMatchScore("yeah absolutely that is correct", "yes")
	if score > 0 {
		t.Fatalf("short expected answer should use strict matching only, got %f", score)
	}
}

func TestBidirectionalContainsScore(t *testing.T) {
	t.Parallel()

	// Answer "business administration" is contained in the longer expected
	// answer. The answer is a significant fraction of the expected, so this
	// should score via the key-fact overlap path even if the bidirectional
	// length check doesn't fire.
	score := answerMatchScore("business administration", "business administration. you mentioned it has been helpful.")
	if score == 0 {
		t.Fatalf("answer containing expected key facts should score > 0")
	}
}

func TestNumericFactMatchScoreCatchesVerboseCorrectAnswer(t *testing.T) {
	t.Parallel()

	// MoMA case: answer is verbose but contains "7 days"
	answer := "7 days passed between your two museum visits. MoMA visit: January 8, 2023. Met visit: January 15, 2023."
	expected := "7 days. 8 days (including the last day) is also acceptable."
	score := answerMatchScore(answer, expected)
	if score == 0 {
		t.Fatalf("verbose answer containing '7 days' should match expected '7 days. ...'")
	}
}

func TestNumericFactMatchScoreRejectsWrongNumber(t *testing.T) {
	t.Parallel()

	// Wrong number: answer says 3 days, expected 7 days
	score := answerMatchScore("3 days passed between visits", "7 days. 8 days (including the last day) is also acceptable.")
	if score > 0 {
		t.Fatalf("wrong numeric answer should score 0, got %f", score)
	}
}

func TestNumericFactMatchScoreMarkdownFormatting(t *testing.T) {
	t.Parallel()

	// Answer has markdown bold formatting: "**7 days**"
	score := answerMatchScore("**7 days** passed between the visits", "7 days")
	if score == 0 {
		t.Fatalf("markdown-formatted '7 days' should match expected '7 days'")
	}
}
