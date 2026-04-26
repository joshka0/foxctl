package addressing

import (
	"errors"
	"testing"
)

func TestParse_KnownSchemes(t *testing.T) {
	cases := []struct {
		raw    string
		scheme Scheme
		id     string
	}{
		{"room:dev-loop", SchemeRoom, "dev-loop"},
		{"session:01HX8Z0000000000000000000", SchemeSession, "01HX8Z0000000000000000000"},
		{"agent:coder-a", SchemeAgent, "coder-a"},
		{"inbox:dev-loop/coder-a", SchemeInbox, "dev-loop/coder-a"},
		{"scheduler:reminders", SchemeScheduler, "reminders"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			got, err := Parse(tc.raw)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tc.raw, err)
			}
			if got.Scheme != tc.scheme {
				t.Errorf("Scheme = %s, want %s", got.Scheme, tc.scheme)
			}
			if got.ID != tc.id {
				t.Errorf("ID = %q, want %q", got.ID, tc.id)
			}
			if got.String() != tc.raw {
				t.Errorf("String() = %q, want %q", got.String(), tc.raw)
			}
		})
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []struct {
		raw    string
		wantIs error
	}{
		{"", ErrEmptyAddress},
		{"   ", ErrEmptyAddress},
		{"room", ErrMissingScheme},
		{":id", ErrEmptyScheme},
		{"room:", ErrEmptyID},
		{"unknown:abc", ErrUnknownScheme},
		{"room:bad id", ErrInvalidIDChars},
		{"room:bad\nid", ErrInvalidIDChars},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			_, err := Parse(tc.raw)
			if err == nil {
				t.Fatalf("Parse(%q) want error, got nil", tc.raw)
			}
			if !errors.Is(err, tc.wantIs) {
				t.Fatalf("Parse(%q) error = %v, want to wrap %v", tc.raw, err, tc.wantIs)
			}
		})
	}
}

func TestParseExpect(t *testing.T) {
	if _, err := ParseExpect("room:dev-loop", SchemeRoom); err != nil {
		t.Fatalf("ParseExpect room ok: unexpected err %v", err)
	}
	_, err := ParseExpect("session:abc", SchemeRoom)
	if !errors.Is(err, ErrWrongScheme) {
		t.Fatalf("want ErrWrongScheme, got %v", err)
	}
}

func TestParse_IDMayContainSlashAndDash(t *testing.T) {
	a, err := Parse("inbox:workspace/room/agent-42")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if a.ID != "workspace/room/agent-42" {
		t.Errorf("ID lost characters: %q", a.ID)
	}
}

func TestConstructors(t *testing.T) {
	cases := []struct {
		got  Address
		want string
	}{
		{Room("r1"), "room:r1"},
		{Session("s1"), "session:s1"},
		{Agent("a1"), "agent:a1"},
		{Inbox("i1"), "inbox:i1"},
		{Scheduler("sc1"), "scheduler:sc1"},
	}
	for _, tc := range cases {
		if got := tc.got.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

func TestString_EmptyAddressReturnsEmpty(t *testing.T) {
	var a Address
	if a.String() != "" {
		t.Errorf("zero Address should stringify to empty, got %q", a.String())
	}
}

func TestIsKnown(t *testing.T) {
	for _, s := range KnownSchemes() {
		if !IsKnown(s) {
			t.Errorf("IsKnown(%q) = false, want true", s)
		}
	}
	if IsKnown(Scheme("pants")) {
		t.Error("IsKnown returned true for bogus scheme")
	}
}

func TestMustParse(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustParse should panic on bad input")
		}
	}()
	_ = MustParse("not-an-address")
}
