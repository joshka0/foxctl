package cmd

import (
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/terminal/herdrbridge"
	"github.com/spf13/cobra"
)

func TestLoadHerdrParamsReadsEnvelopeDataFromStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(`{"version":1,"status":"ok","command":"x","data":{"pane_id":"w1-1","source":"recent"},"meta":{"ts":"2026-05-16T00:00:00Z"},"error":{}}`))

	got, err := loadHerdrParams(cmd, "stdin", "")
	if err != nil {
		t.Fatalf("loadHerdrParams() error = %v", err)
	}
	want := `{"pane_id":"w1-1","source":"recent"}`
	if string(got) != want {
		t.Fatalf("params=%s want %s", got, want)
	}
}

func TestHerdrProtocolErrorCode(t *testing.T) {
	tests := []struct {
		code string
		want protocol.ErrorCode
	}{
		{code: "pane_not_found", want: protocol.ErrorCodeENotFound},
		{code: "invalid_key", want: protocol.ErrorCodeEARG},
		{code: "timeout", want: protocol.ErrorCodeETimeout},
		{code: "socket_closed", want: protocol.ErrorCodeERuntime},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := herdrProtocolErrorCode(&herdrbridge.HerdrError{Code: tt.code})
			if got != tt.want {
				t.Fatalf("herdrProtocolErrorCode(%q)=%q want %q", tt.code, got, tt.want)
			}
		})
	}
}
