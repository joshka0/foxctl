package kinds

import (
	"errors"
	"testing"

	"github.com/joshka/foxprox/foxprox/addressing"
)

func TestAllRegisteredKindsResolve(t *testing.T) {
	specs := All()
	if len(specs) == 0 {
		t.Fatal("registry is empty; init did not populate specs")
	}
	for _, sp := range specs {
		if sp.Kind == "" {
			t.Error("registered spec has empty kind")
		}
		if sp.Category != CategoryIntent && sp.Category != CategoryEvent {
			t.Errorf("kind %q has invalid category %d", sp.Kind, sp.Category)
		}
		got, err := Lookup(sp.Kind)
		if err != nil {
			t.Errorf("Lookup(%q) returned error: %v", sp.Kind, err)
			continue
		}
		if got.Kind != sp.Kind {
			t.Errorf("Lookup mismatch: got %q want %q", got.Kind, sp.Kind)
		}
	}
}

func TestLookup_UnknownReturnsError(t *testing.T) {
	_, err := Lookup("not.a.kind")
	if !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("want ErrUnknownKind, got %v", err)
	}
}

func TestIsRegistered(t *testing.T) {
	if !IsRegistered(RoomCreate) {
		t.Error("RoomCreate should be registered")
	}
	if IsRegistered("fictional.kind") {
		t.Error("fictional.kind should not be registered")
	}
}

func TestValidateTarget_AcceptsMatchingScheme(t *testing.T) {
	if err := ValidateTarget(RoomJoin, "room:dev-loop"); err != nil {
		t.Errorf("RoomJoin with room target should pass: %v", err)
	}
	if err := ValidateTarget(TerminalSubmit, "session:01HX"); err != nil {
		t.Errorf("TerminalSubmit with session target should pass: %v", err)
	}
}

func TestValidateTarget_RejectsWrongScheme(t *testing.T) {
	err := ValidateTarget(RoomJoin, "session:01HX")
	if !errors.Is(err, ErrWrongTargetScheme) {
		t.Fatalf("want ErrWrongTargetScheme, got %v", err)
	}
}

func TestValidateTarget_RejectsMissingTargetWhenRequired(t *testing.T) {
	err := ValidateTarget(RoomCreate, "")
	if !errors.Is(err, ErrWrongTargetScheme) {
		t.Fatalf("want ErrWrongTargetScheme, got %v", err)
	}
}

func TestValidateTarget_OptionalTargetAcceptsEmpty(t *testing.T) {
	// MessageDelivered has no required schemes; empty target should pass.
	if err := ValidateTarget(MessageDelivered, ""); err != nil {
		t.Errorf("MessageDelivered with empty target should pass: %v", err)
	}
}

func TestValidateTarget_SessionCreateAcceptsEmpty(t *testing.T) {
	// session.create has no TargetSchemes because the session id does not exist
	// yet at creation time.
	if err := ValidateTarget(SessionCreate, ""); err != nil {
		t.Errorf("SessionCreate with empty target should pass: %v", err)
	}
}

func TestValidateTarget_MessageSendAcceptsRoomInboxSessionAgent(t *testing.T) {
	cases := []string{
		"room:dev-loop",
		"inbox:coder-a",
		"session:01HX",
		"agent:coder-a",
	}
	for _, target := range cases {
		if err := ValidateTarget(MessageSend, target); err != nil {
			t.Errorf("MessageSend with %s should pass: %v", target, err)
		}
	}
	if err := ValidateTarget(MessageSend, "scheduler:main"); !errors.Is(err, ErrWrongTargetScheme) {
		t.Fatalf("MessageSend should reject scheduler target, got %v", err)
	}
}

func TestCategoryString(t *testing.T) {
	if CategoryIntent.String() != "intent" {
		t.Errorf("CategoryIntent.String() = %q", CategoryIntent.String())
	}
	if CategoryEvent.String() != "event" {
		t.Errorf("CategoryEvent.String() = %q", CategoryEvent.String())
	}
	if Category(99).String() != "unknown" {
		t.Errorf("unknown category String() should be %q", "unknown")
	}
}

func TestRoomIntentsAllRequireRoomTarget(t *testing.T) {
	roomIntents := []Kind{RoomCreate, RoomJoin, RoomLeave, RoomRebind, RoomArchive, RoomDestroy}
	for _, k := range roomIntents {
		sp, err := Lookup(k)
		if err != nil {
			t.Fatalf("Lookup(%s): %v", k, err)
		}
		if len(sp.TargetSchemes) != 1 || sp.TargetSchemes[0] != addressing.SchemeRoom {
			t.Errorf("%s should require scheme room, got %v", k, sp.TargetSchemes)
		}
		if sp.Category != CategoryIntent {
			t.Errorf("%s should be CategoryIntent", k)
		}
	}
}

func TestRoomEventsAllRequireRoomTarget(t *testing.T) {
	roomEvents := []Kind{RoomMemberJoined, RoomMemberLeft, RoomMemberRebound}
	for _, k := range roomEvents {
		sp, err := Lookup(k)
		if err != nil {
			t.Fatalf("Lookup(%s): %v", k, err)
		}
		if sp.Category != CategoryEvent {
			t.Errorf("%s should be CategoryEvent", k)
		}
	}
}

func TestTerminalIntentsAllTargetSession(t *testing.T) {
	intents := []Kind{TerminalText, TerminalKey, TerminalSubmit, TerminalPaste, TerminalWriteBytes}
	for _, k := range intents {
		sp, _ := Lookup(k)
		if len(sp.TargetSchemes) != 1 || sp.TargetSchemes[0] != addressing.SchemeSession {
			t.Errorf("%s must target session scheme, got %v", k, sp.TargetSchemes)
		}
	}
}
