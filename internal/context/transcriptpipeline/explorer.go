package transcriptpipeline

import (
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/context/companion"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

// BuildExplorerPrompt renders a compact comparison prompt for free-form transcript memoryization.
func BuildExplorerPrompt(parsed sourceimport.ParsedSession, derivations []companion.AnchoredMemoryDerivation, turnLimit int) string {
	var b strings.Builder
	b.WriteString("You are reviewing one imported transcript and producing a free-form memoryization summary.\n")
	b.WriteString("Compare your output mentally against the structured anchored derivation results below.\n\n")
	b.WriteString("Focus on:\n")
	b.WriteString("- durable preferences, decisions, technical context, and gotchas\n")
	b.WriteString("- unresolved issues or follow-up-needed items\n")
	b.WriteString("- correction, frustration, confusion, or other memorable user reactions\n")
	b.WriteString("- what should be session-only vs durable memory\n\n")
	b.WriteString("Transcript:\n")

	start := 0
	if turnLimit > 0 && len(parsed.Turns) > turnLimit {
		start = len(parsed.Turns) - turnLimit
	}
	for _, turn := range parsed.Turns[start:] {
		prompt := companion.NormalizeTranscriptTurnText(turn.Prompt)
		out := companion.NormalizeTranscriptTurnText(turn.FinalOutput.Text)
		if prompt == "" && out == "" {
			continue
		}
		if prompt != "" {
			b.WriteString("- user: ")
			b.WriteString(truncatePacketInline(prompt, 240))
			b.WriteString("\n")
		}
		if out != "" {
			b.WriteString("- assistant: ")
			b.WriteString(truncatePacketInline(out, 240))
			b.WriteString("\n")
		}
	}

	b.WriteString("\nStructured anchored derivations:\n")
	for _, item := range derivations {
		b.WriteString(fmt.Sprintf("- frame %d [%s/%s]: %s\n", item.FrameIndex, item.Resolution, item.Reaction.Outcome, truncatePacketInline(item.InteractionSummary, 260)))
		for _, candidate := range item.Candidates {
			b.WriteString(fmt.Sprintf("  candidate: %s (%s, %.2f) %s\n", candidate.Type, candidate.Scope, candidate.Confidence, truncatePacketInline(candidate.Text, 180)))
		}
	}
	return b.String()
}
