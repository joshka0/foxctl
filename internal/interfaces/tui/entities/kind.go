// Package entities declares the typed domain objects used by the TUI operator
// cockpit. All transcript and event kinds are represented as typed constants
// (EntryKind) instead of raw strings, providing compile-time safety while a
// legacy-string mapper (ParseEntryKind) preserves compatibility with existing
// wire formats.
package entities

import "strings"

// EntryKind is a typed enum representing the kind of a transcript entry,
// event row, or room message. It replaces the string-keyed sprawl used in
// earlier TUI code.
type EntryKind int

const (
	// EntryKindUnknown is the zero value and represents an unmapped or
	// unrecognised kind. It must NOT be used as a valid kind constant.
	EntryKindUnknown EntryKind = iota

	EntryKindPending   // pending — composer text queued, not yet sent
	EntryKindAsk       // ask — user message submitted to an agent
	EntryKindReply     // reply — agent or assistant response
	EntryKindEvent     // event — generic stream event
	EntryKindCmd       // cmd — command acknowledgment
	EntryKindDraft     // draft — local composer draft, not submitted
	EntryKindStatus    // status — operational status update
	EntryKindError     // error — error indicator
	EntryKindTool      // tool — tool call or tool result
	EntryKindCounts    // counts — epic/work counters
	EntryKindNext      // next — next-action hint
	EntryKindBrief     // brief — brief summary
	EntryKindEpic      // epic — epic-level context
	EntryKindInflight  // inflight — in-flight correlation indicator
	EntryKindAgent     // agent — agent lifecycle or attachment event
	EntryKindConsole   // console — console session message
	EntryKindConnected // connected — session connected event
	EntryKindHeartbeat // heartbeat — keep-alive (usually suppressed)
)

// String returns the canonical lowercase name for the EntryKind.
// Unknown kinds return "unknown".
func (k EntryKind) String() string {
	switch k {
	case EntryKindPending:
		return "pending"
	case EntryKindAsk:
		return "ask"
	case EntryKindReply:
		return "reply"
	case EntryKindEvent:
		return "event"
	case EntryKindCmd:
		return "cmd"
	case EntryKindDraft:
		return "draft"
	case EntryKindStatus:
		return "status"
	case EntryKindError:
		return "error"
	case EntryKindTool:
		return "tool"
	case EntryKindCounts:
		return "counts"
	case EntryKindNext:
		return "next"
	case EntryKindBrief:
		return "brief"
	case EntryKindEpic:
		return "epic"
	case EntryKindInflight:
		return "inflight"
	case EntryKindAgent:
		return "agent"
	case EntryKindConsole:
		return "console"
	case EntryKindConnected:
		return "connected"
	case EntryKindHeartbeat:
		return "heartbeat"
	default:
		return "unknown"
	}
}

// ParseEntryKind maps a legacy string value to the typed EntryKind enum.
// The lookup is case-insensitive. Unrecognised strings return EntryKindUnknown.
func ParseEntryKind(s string) EntryKind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pending":
		return EntryKindPending
	case "ask":
		return EntryKindAsk
	case "reply":
		return EntryKindReply
	case "event":
		return EntryKindEvent
	case "cmd":
		return EntryKindCmd
	case "draft":
		return EntryKindDraft
	case "status":
		return EntryKindStatus
	case "error":
		return EntryKindError
	case "tool":
		return EntryKindTool
	case "counts":
		return EntryKindCounts
	case "next":
		return EntryKindNext
	case "brief":
		return EntryKindBrief
	case "epic":
		return EntryKindEpic
	case "inflight":
		return EntryKindInflight
	case "agent":
		return EntryKindAgent
	case "console":
		return EntryKindConsole
	case "connected":
		return EntryKindConnected
	case "heartbeat":
		return EntryKindHeartbeat
	default:
		return EntryKindUnknown
	}
}
