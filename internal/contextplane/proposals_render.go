package contextplane

import "strings"

func RenderHookContextForProposalPacket(packet ProposalWorkPacket) string {
	if strings.TrimSpace(packet.ProposalID) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Proposal Work\n\n")
	b.WriteString("**Proposal:** `")
	b.WriteString(strings.TrimSpace(packet.ProposalID))
	b.WriteString("`")
	if kind := strings.TrimSpace(packet.ProposalKind); kind != "" {
		b.WriteString(" (`")
		b.WriteString(kind)
		b.WriteString("`)")
	}
	b.WriteString("\n")
	if action := strings.TrimSpace(packet.Action); action != "" {
		b.WriteString("**Action:** `")
		b.WriteString(action)
		b.WriteString("`\n")
	}
	if status := strings.TrimSpace(packet.Status); status != "" {
		b.WriteString("**Status:** `")
		b.WriteString(status)
		b.WriteString("`\n")
	}
	if draft := strings.TrimSpace(packet.DraftPath); draft != "" {
		b.WriteString("**Draft:** `")
		b.WriteString(draft)
		b.WriteString("`\n")
	}
	if target := strings.TrimSpace(packet.TargetPath); target != "" {
		b.WriteString("**Target:** `")
		b.WriteString(target)
		b.WriteString("`\n")
	}
	if heading := strings.TrimSpace(packet.Heading); heading != "" {
		b.WriteString("**Heading:** `")
		b.WriteString(heading)
		b.WriteString("`\n")
	}
	if policyPath := strings.TrimSpace(packet.PolicyPath); policyPath != "" {
		b.WriteString("**Policy path:** `")
		b.WriteString(policyPath)
		b.WriteString("`\n")
	}
	if next := strings.TrimSpace(packet.NextCommand); next != "" {
		b.WriteString("\n**Next command:** `")
		b.WriteString(next)
		b.WriteString("`\n")
	}
	return strings.TrimSpace(b.String())
}
