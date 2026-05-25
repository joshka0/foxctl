package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/quick"
	"time"
)

func TestBoardMessageKind_Constants(t *testing.T) {
	for _, tt := range boardMessageKindConstantCases() {
		t.Run(string(tt.kind), func(t *testing.T) {
			if string(tt.kind) != tt.want {
				t.Errorf("BoardMessageKind = %q, want %q", tt.kind, tt.want)
			}
		})
	}
}

func TestNormalizeBoardMessageKindDefaultsEmptyAndAllowsDocumentedKinds(t *testing.T) {
	for _, raw := range []BoardMessageKind{"", "   "} {
		got, err := NormalizeBoardMessageKind(raw)
		if err != nil {
			t.Fatalf("NormalizeBoardMessageKind(%q) error=%v, want nil", raw, err)
		}
		if got != BoardMessageKindInfo {
			t.Fatalf("NormalizeBoardMessageKind(%q)=%q, want %q", raw, got, BoardMessageKindInfo)
		}
	}

	for _, tt := range boardMessageKindConstantCases() {
		t.Run(string(tt.kind), func(t *testing.T) {
			got, err := NormalizeBoardMessageKind(tt.kind)
			if err != nil {
				t.Fatalf("NormalizeBoardMessageKind(%q) error=%v, want nil", tt.kind, err)
			}
			if got != tt.kind {
				t.Fatalf("NormalizeBoardMessageKind(%q)=%q, want %q", tt.kind, got, tt.kind)
			}
		})
	}

	errKind, err := NormalizeBoardMessageKind("custom")
	if !errors.Is(err, ErrInvalidBoardMessageKind) {
		t.Fatalf("NormalizeBoardMessageKind(custom)=(%q,%v), want ErrInvalidBoardMessageKind", errKind, err)
	}
}

func TestNormalizeBoardMessageKindPropertyUnknownKindsFailClosed(t *testing.T) {
	valid := make(map[BoardMessageKind]bool)
	for _, tt := range boardMessageKindConstantCases() {
		valid[tt.kind] = true
	}

	prop := func(raw string) bool {
		got, err := NormalizeBoardMessageKind(BoardMessageKind(raw))
		trimmed := BoardMessageKind(strings.TrimSpace(raw))
		if trimmed == "" {
			return err == nil && got == BoardMessageKindInfo
		}
		if valid[trimmed] {
			return err == nil && got == trimmed
		}
		return errors.Is(err, ErrInvalidBoardMessageKind)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatalf("board message kind property failed: %v", err)
	}
}

func TestBoardMessageStatus_Constants(t *testing.T) {
	tests := []struct {
		status BoardMessageStatus
		want   string
	}{
		{BoardMessageStatusUnread, "unread"},
		{BoardMessageStatusSurfaced, "surfaced"},
		{BoardMessageStatusRead, "read"},
		{BoardMessageStatusAcked, "acked"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("BoardMessageStatus = %q, want %q", tt.status, tt.want)
			}
		})
	}
}

func boardMessageKindConstantCases() []struct {
	kind BoardMessageKind
	want string
} {
	return []struct {
		kind BoardMessageKind
		want string
	}{
		{BoardMessageKindInstruction, "instruction"},
		{BoardMessageKindInfo, "info"},
		{BoardMessageKindAlert, "alert"},
		{BoardMessageKindReviewRequest, "review_request"},
		{BoardMessageKindTaskUpdate, "task_update"},
		{BoardMessageKindLeadChange, "lead_change"},
		{BoardMessageKindCoordinatorPulse, "coordinator_pulse"},
		{BoardMessageKindPlanSession, "plan_session"},
		{BoardMessageKindPlanProposal, "plan_proposal"},
		{BoardMessageKindPlanQuestion, "plan_question"},
		{BoardMessageKindPlanDecision, "plan_decision"},
		{BoardMessageKindPlanReview, "plan_review"},
		{BoardMessageKindPlanClose, "plan_close"},
		{BoardMessageKindInterviewSession, "interview_session"},
		{BoardMessageKindInterviewQuestion, "interview_question"},
		{BoardMessageKindInterviewAnswer, "interview_answer"},
		{BoardMessageKindInterviewVerify, "interview_verify"},
		{BoardMessageKindEpic, "epic"},
		{BoardMessageKindEpicQuestion, "epic_question"},
		{BoardMessageKindEpicAnswer, "epic_answer"},
		{BoardMessageKindEpicFinalize, "epic_finalize"},
		{BoardMessageKindEpicUpdate, "epic_update"},
		{BoardMessageKindEpicClose, "epic_close"},
		{BoardMessageKindEpicCheckpoint, "epic_checkpoint"},
		{BoardMessageKindMilestoneProposal, "milestone_proposal"},
		{BoardMessageKindMilestone, "milestone"},
		{BoardMessageKindMilestoneContract, "milestone_contract"},
		{BoardMessageKindStory, "story"},
		{BoardMessageKindAcceptanceCriteria, "acceptance_criteria"},
		{BoardMessageKindMilestoneReview, "milestone_review"},
		{BoardMessageKindMilestoneSummary, "milestone_summary"},
		{BoardMessageKindStoryProposal, "story_proposal"},
		{BoardMessageKindStoryState, "story_state"},
		{BoardMessageKindStoryUpdate, "story_update"},
		{BoardMessageKindStoryValidation, "story_validation"},
		{BoardMessageKindDeliveryLog, "delivery_log"},
		{BoardMessageKindGuidanceUpdate, "guidance_update"},
	}
}

func TestValidateBoardMessageStatusAllowsOnlyDocumentedStatuses(t *testing.T) {
	validStatuses := []BoardMessageStatus{
		BoardMessageStatusUnread,
		BoardMessageStatusSurfaced,
		BoardMessageStatusRead,
		BoardMessageStatusAcked,
	}
	for _, status := range validStatuses {
		t.Run(string(status), func(t *testing.T) {
			if err := ValidateBoardMessageStatus(status); err != nil {
				t.Fatalf("ValidateBoardMessageStatus(%q) = %v, want nil", status, err)
			}
		})
	}

	err := ValidateBoardMessageStatus("lost")
	if !errors.Is(err, ErrInvalidBoardMessageStatus) {
		t.Fatalf("ValidateBoardMessageStatus(lost) = %v, want ErrInvalidBoardMessageStatus", err)
	}
}

func TestValidateBoardMessageStatusPropertyUnknownStatusesFailClosed(t *testing.T) {
	valid := map[BoardMessageStatus]bool{
		BoardMessageStatusUnread:   true,
		BoardMessageStatusSurfaced: true,
		BoardMessageStatusRead:     true,
		BoardMessageStatusAcked:    true,
	}
	prop := func(raw string) bool {
		status := BoardMessageStatus(raw)
		err := ValidateBoardMessageStatus(status)
		if valid[status] {
			return err == nil
		}
		return errors.Is(err, ErrInvalidBoardMessageStatus)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatalf("board message status property failed: %v", err)
	}
}

func TestNormalizeBoardMessagePriorityDefaultsZeroAndRejectsInvalidExplicitValues(t *testing.T) {
	got, err := NormalizeBoardMessagePriority(0)
	if err != nil {
		t.Fatalf("NormalizeBoardMessagePriority(0) error=%v, want nil", err)
	}
	if got != DefaultPriority {
		t.Fatalf("NormalizeBoardMessagePriority(0)=%d, want %d", got, DefaultPriority)
	}

	for priority := 1; priority <= 5; priority++ {
		got, err := NormalizeBoardMessagePriority(priority)
		if err != nil {
			t.Fatalf("NormalizeBoardMessagePriority(%d) error=%v, want nil", priority, err)
		}
		if got != priority {
			t.Fatalf("NormalizeBoardMessagePriority(%d)=%d, want %d", priority, got, priority)
		}
	}

	for _, priority := range []int{-1, 6} {
		_, err := NormalizeBoardMessagePriority(priority)
		if !errors.Is(err, ErrInvalidBoardMessagePriority) {
			t.Fatalf("NormalizeBoardMessagePriority(%d) error=%v, want ErrInvalidBoardMessagePriority", priority, err)
		}
	}
}

func TestValidateBoardMessagePriorityAllowsOnlyPersistedRange(t *testing.T) {
	for priority := 1; priority <= 5; priority++ {
		if err := ValidateBoardMessagePriority(priority); err != nil {
			t.Fatalf("ValidateBoardMessagePriority(%d) error=%v, want nil", priority, err)
		}
	}
	for _, priority := range []int{-1, 0, 6} {
		err := ValidateBoardMessagePriority(priority)
		if !errors.Is(err, ErrInvalidBoardMessagePriority) {
			t.Fatalf("ValidateBoardMessagePriority(%d) error=%v, want ErrInvalidBoardMessagePriority", priority, err)
		}
	}
}

func TestNormalizeBoardMessagePriorityProperty(t *testing.T) {
	prop := func(priority int) bool {
		got, err := NormalizeBoardMessagePriority(priority)
		switch {
		case priority == 0:
			return err == nil && got == DefaultPriority
		case priority >= 1 && priority <= 5:
			return err == nil && got == priority
		default:
			return errors.Is(err, ErrInvalidBoardMessagePriority)
		}
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatalf("board message priority property failed: %v", err)
	}
}

func TestReservationMode_Constants(t *testing.T) {
	tests := []struct {
		mode ReservationMode
		want string
	}{
		{ReservationModeExclusive, "exclusive"},
		{ReservationModeShared, "shared"},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if string(tt.mode) != tt.want {
				t.Errorf("ReservationMode = %q, want %q", tt.mode, tt.want)
			}
		})
	}
}

func TestValidateReservationModeAllowsOnlyDocumentedModes(t *testing.T) {
	validModes := []ReservationMode{ReservationModeExclusive, ReservationModeShared}
	for _, mode := range validModes {
		t.Run(string(mode), func(t *testing.T) {
			if err := ValidateReservationMode(mode); err != nil {
				t.Fatalf("ValidateReservationMode(%q) = %v, want nil", mode, err)
			}
		})
	}

	err := ValidateReservationMode("optimistic")
	if !errors.Is(err, ErrInvalidReservationMode) {
		t.Fatalf("ValidateReservationMode(optimistic) = %v, want ErrInvalidReservationMode", err)
	}
}

func TestValidateReservationModePropertyUnknownModesFailClosed(t *testing.T) {
	valid := map[ReservationMode]bool{
		ReservationModeExclusive: true,
		ReservationModeShared:    true,
	}
	prop := func(raw string) bool {
		mode := ReservationMode(raw)
		err := ValidateReservationMode(mode)
		if valid[mode] {
			return err == nil
		}
		return errors.Is(err, ErrInvalidReservationMode)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatalf("reservation mode property failed: %v", err)
	}
}

func TestFileReservation_IsExpired(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		reservation FileReservation
		want        bool
	}{
		{
			name: "not expired",
			reservation: FileReservation{
				ExpiresAt: now.Add(1 * time.Hour),
			},
			want: false,
		},
		{
			name: "expired",
			reservation: FileReservation{
				ExpiresAt: now.Add(-1 * time.Hour),
			},
			want: true,
		},
		{
			name: "just expired",
			reservation: FileReservation{
				ExpiresAt: now.Add(-1 * time.Second),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.reservation.IsExpired()
			if got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAdminSender(t *testing.T) {
	tests := []struct {
		sender string
		want   bool
	}{
		{"admin", true},
		{"actor:admin:user", true},
		{"actor:admin:test123", true},
		{"user", false},
		{"actor:user:test", false},
		{"admin_user", false},
		{"", false},
		{"actor:admin", false}, // Too short to have prefix "actor:admin:"
	}

	for _, tt := range tests {
		t.Run(tt.sender, func(t *testing.T) {
			got := IsAdminSender(tt.sender)
			if got != tt.want {
				t.Errorf("IsAdminSender(%q) = %v, want %v", tt.sender, got, tt.want)
			}
		})
	}
}

func TestIsOverseerSender(t *testing.T) {
	tests := []struct {
		sender string
		want   bool
	}{
		{"actor:system:overseer", true},
		{"overseer", false},
		{"actor:overseer", false},
		{"actor:system:other", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.sender, func(t *testing.T) {
			got := IsOverseerSender(tt.sender)
			if got != tt.want {
				t.Errorf("IsOverseerSender(%q) = %v, want %v", tt.sender, got, tt.want)
			}
		})
	}
}

func TestRoomStreamHelpers(t *testing.T) {
	if got := RoomStreamName("alpha"); got != "room:alpha" {
		t.Fatalf("RoomStreamName(alpha)=%q want room:alpha", got)
	}
	if got := RoomIDFromStream("room:alpha"); got != "alpha" {
		t.Fatalf("RoomIDFromStream(room:alpha)=%q want alpha", got)
	}
	if got := RoomIDFromStream("coordination"); got != "" {
		t.Fatalf("RoomIDFromStream(coordination)=%q want empty", got)
	}
}

func TestBoardMessage_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	msg := BoardMessage{
		ID:            "msg-123",
		WorkspaceID:   "ws-456",
		TaskID:        "task-789",
		Stream:        "coordination",
		Sender:        "actor:system:overseer",
		Recipient:     "*",
		Kind:          BoardMessageKindInstruction,
		Priority:      1,
		AckRequired:   true,
		ReplyExpected: true,
		Status:        BoardMessageStatusUnread,
		Subject:       "Task Assignment",
		Body:          "Please review PR #42",
		CreatedAt:     now,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got BoardMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.ID != msg.ID {
		t.Errorf("ID = %q, want %q", got.ID, msg.ID)
	}
	if got.Kind != msg.Kind {
		t.Errorf("Kind = %v, want %v", got.Kind, msg.Kind)
	}
	if got.Priority != msg.Priority {
		t.Errorf("Priority = %d, want %d", got.Priority, msg.Priority)
	}
	if got.AckRequired != msg.AckRequired {
		t.Errorf("AckRequired = %v, want %v", got.AckRequired, msg.AckRequired)
	}
	if got.ReplyExpected != msg.ReplyExpected {
		t.Errorf("ReplyExpected = %v, want %v", got.ReplyExpected, msg.ReplyExpected)
	}
}

func TestFileReservation_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	res := FileReservation{
		ID:          "res-123",
		WorkspaceID: "ws-456",
		TaskID:      "task-789",
		Path:        "src/main.go",
		Holder:      "agent-abc",
		Mode:        ReservationModeExclusive,
		Reason:      "Refactoring main function",
		ExpiresAt:   now.Add(10 * time.Minute),
		CreatedAt:   now,
	}

	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got FileReservation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.ID != res.ID {
		t.Errorf("ID = %q, want %q", got.ID, res.ID)
	}
	if got.Mode != res.Mode {
		t.Errorf("Mode = %v, want %v", got.Mode, res.Mode)
	}
	if got.Path != res.Path {
		t.Errorf("Path = %q, want %q", got.Path, res.Path)
	}
}

func TestReservationConflict_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	conflict := ReservationConflict{
		Path:      "src/utils.go",
		Holder:    "agent-xyz",
		Mode:      "exclusive",
		TaskID:    "task-101",
		Reason:    "Adding new utility functions",
		ExpiresAt: now.Add(5 * time.Minute),
	}

	data, err := json.Marshal(conflict)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got ReservationConflict
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Path != conflict.Path {
		t.Errorf("Path = %q, want %q", got.Path, conflict.Path)
	}
	if got.Holder != conflict.Holder {
		t.Errorf("Holder = %q, want %q", got.Holder, conflict.Holder)
	}
}

func TestInboxFilter_JSONSerialization(t *testing.T) {
	filter := InboxFilter{
		WorkspaceID: "ws-123",
		ActorID:     "agent-456",
		TaskID:      "task-789",
		Stream:      "coordination",
		OnlyUnread:  true,
		Limit:       50,
	}

	data, err := json.Marshal(filter)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got InboxFilter
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.WorkspaceID != filter.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", got.WorkspaceID, filter.WorkspaceID)
	}
	if got.OnlyUnread != filter.OnlyUnread {
		t.Errorf("OnlyUnread = %v, want %v", got.OnlyUnread, filter.OnlyUnread)
	}
	if got.Limit != filter.Limit {
		t.Errorf("Limit = %d, want %d", got.Limit, filter.Limit)
	}
}

func TestConstants(t *testing.T) {
	if DefaultStream != "coordination" {
		t.Errorf("DefaultStream = %q, want %q", DefaultStream, "coordination")
	}
	if DefaultPriority != 3 {
		t.Errorf("DefaultPriority = %d, want %d", DefaultPriority, 3)
	}
	if DefaultReservationTTL != 10*time.Minute {
		t.Errorf("DefaultReservationTTL = %v, want %v", DefaultReservationTTL, 10*time.Minute)
	}
	if BroadcastRecipient != "*" {
		t.Errorf("BroadcastRecipient = %q, want %q", BroadcastRecipient, "*")
	}
}

func TestCompactRoomSummaryForInboxOmitsBulkLists(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	s := RoomSummary{
		ID:               "r1",
		WorkspaceID:      "/ws",
		Stream:           "room:r1",
		Title:            "T",
		DispatchPolicy:   "all_subtree",
		CreatedAt:        now,
		UpdatedAt:        now,
		LatestSubject:    "subj",
		LatestPreview:    "prev",
		LatestSender:     "human-a",
		LatestMessageAt:  now,
		MessageCount:     9,
		UnreadCount:      3,
		Participants:     []string{"a", "b"},
		TaskIDs:          []string{"t1", "t2"},
		Members:          []RoomMember{{ActorID: "a", JoinedAt: now}},
		Description:      "long description",
		DispatchAgentIDs: []string{"x"},
	}
	got := CompactRoomSummaryForInbox(s)
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal compact summary: %v", err)
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatalf("unmarshal compact summary: %v", err)
	}
	for _, key := range []string{"task_ids", "participants", "members", "description", "dispatch_agent_ids"} {
		if _, ok := encoded[key]; ok {
			t.Fatalf("unexpected key %q in compact summary", key)
		}
	}
	if got.ID != "r1" || got.MessageCount != 9 {
		t.Fatalf("got=%v", got)
	}
}

func TestSandboxConfig_IsSandbox(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		sc   *SandboxConfig
		want bool
	}{
		{"nil", nil, false},
		{"empty", &SandboxConfig{}, false},
		{"with worktree path", &SandboxConfig{WorktreePath: "/tmp/worktree"}, true},
		{"no worktree path", &SandboxConfig{TmuxSession: "s1"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sc.IsSandbox(); got != tt.want {
				t.Errorf("IsSandbox() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSandboxConfig_EffectiveRuntime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		sc   *SandboxConfig
		want string
	}{
		{"nil defaults to worktree", nil, "worktree"},
		{"empty defaults to worktree", &SandboxConfig{}, "worktree"},
		{"explicit worktree", &SandboxConfig{Runtime: "worktree"}, "worktree"},
		{"opensandbox", &SandboxConfig{Runtime: "opensandbox"}, "opensandbox"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sc.EffectiveRuntime(); got != tt.want {
				t.Errorf("EffectiveRuntime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSandboxConfig_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	sc := &SandboxConfig{
		WorktreePath:   "/tmp/worktrees/sandbox/room-test-room",
		WorktreeBranch: "sandbox/room-test-room",
		TmuxSession:    "foxctl-sandbox-test-room",
		TerminalURL:    "/terminal/test-room",
		Runtime:        "worktree",
		BaseRef:        "HEAD",
	}

	data, err := json.Marshal(sc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got SandboxConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.WorktreePath != sc.WorktreePath {
		t.Errorf("WorktreePath = %q, want %q", got.WorktreePath, sc.WorktreePath)
	}
	if got.WorktreeBranch != sc.WorktreeBranch {
		t.Errorf("WorktreeBranch = %q, want %q", got.WorktreeBranch, sc.WorktreeBranch)
	}
	if got.TmuxSession != sc.TmuxSession {
		t.Errorf("TmuxSession = %q, want %q", got.TmuxSession, sc.TmuxSession)
	}
	if got.TerminalURL != sc.TerminalURL {
		t.Errorf("TerminalURL = %q, want %q", got.TerminalURL, sc.TerminalURL)
	}
	if got.Runtime != sc.Runtime {
		t.Errorf("Runtime = %q, want %q", got.Runtime, sc.Runtime)
	}
	if got.BaseRef != sc.BaseRef {
		t.Errorf("BaseRef = %q, want %q", got.BaseRef, sc.BaseRef)
	}
}

func TestRoom_JSONRoundTrip_WithSandbox(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	room := Room{
		ID:          "test-room",
		WorkspaceID: "/workspace",
		Stream:      "room:test-room",
		Title:       "Test Room",
		CreatedAt:   now,
		UpdatedAt:   now,
		SandboxConfig: &SandboxConfig{
			WorktreePath:   "/tmp/worktrees/sandbox/room-test-room",
			WorktreeBranch: "sandbox/room-test-room",
			TmuxSession:    "foxctl-sandbox-test-room",
			TerminalURL:    "/terminal/test-room",
			Runtime:        "worktree",
			BaseRef:        "HEAD",
		},
	}

	data, err := json.Marshal(room)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got Room
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.SandboxConfig == nil {
		t.Fatal("SandboxConfig is nil after round-trip")
	}
	if got.SandboxConfig.WorktreePath != room.SandboxConfig.WorktreePath {
		t.Errorf("WorktreePath = %q, want %q", got.SandboxConfig.WorktreePath, room.SandboxConfig.WorktreePath)
	}
}

func TestRoom_JSONRoundTrip_WithoutSandbox(t *testing.T) {
	t.Parallel()
	room := Room{
		ID:          "plain-room",
		WorkspaceID: "/workspace",
		Stream:      "room:plain-room",
		Title:       "Plain Room",
	}

	data, err := json.Marshal(room)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got Room
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.SandboxConfig != nil {
		t.Errorf("SandboxConfig should be nil for non-sandbox room, got %+v", got.SandboxConfig)
	}
}

func TestRoomSummary_WithSandboxConfig(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	summary := RoomSummary{
		ID:              "test-room",
		WorkspaceID:     "/workspace",
		Stream:          "room:test-room",
		Title:           "Test Room",
		CreatedAt:       now,
		UpdatedAt:       now,
		LatestMessageAt: now,
		SandboxConfig: &SandboxConfig{
			WorktreePath: "/tmp/wt",
			Runtime:      "worktree",
		},
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got RoomSummary
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.SandboxConfig == nil {
		t.Fatal("SandboxConfig is nil after round-trip")
	}
	if got.SandboxConfig.WorktreePath != "/tmp/wt" {
		t.Errorf("WorktreePath = %q, want %q", got.SandboxConfig.WorktreePath, "/tmp/wt")
	}
}
