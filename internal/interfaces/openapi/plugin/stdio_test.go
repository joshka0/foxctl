package plugin

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

func TestReadRequestDecodesTypedPayload(t *testing.T) {
	request := envelope.OK(CommandAuth, AuthRequestPayload{
		Request: HTTPRequest{Method: "GET", URL: "https://api.example.com"},
		Context: AuthContext{
			Credentials: map[string]any{"key": "access"},
		},
	})
	var buf bytes.Buffer
	if err := envelope.Write(&buf, request); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var payload AuthRequestPayload
	if err := ReadRequest(&buf, CommandAuth, &payload); err != nil {
		t.Fatalf("ReadRequest returned error: %v", err)
	}
	if payload.Request.Method != "GET" || payload.Context.Credentials["key"] != "access" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestReadRequestRejectsUnexpectedCommand(t *testing.T) {
	var buf bytes.Buffer
	if err := envelope.Write(&buf, envelope.OK(CommandPagination, PaginationRequestPayload{})); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var payload AuthRequestPayload
	err := ReadRequest(&buf, CommandAuth, &payload)
	if err == nil || !strings.Contains(err.Error(), "unexpected command") {
		t.Fatalf("ReadRequest error=%v, want unexpected command", err)
	}
}

func TestWriteHandshake(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHandshake(&buf, Handshake{
		Name:      "auth-hmac",
		Commands:  []string{CommandAuth},
		Protocols: []string{"core/v1"},
	}); err != nil {
		t.Fatalf("WriteHandshake returned error: %v", err)
	}

	var hs Handshake
	if err := json.Unmarshal(buf.Bytes(), &hs); err != nil {
		t.Fatalf("decode handshake: %v", err)
	}
	if hs.Name != "auth-hmac" || len(hs.Commands) != 1 || hs.Commands[0] != CommandAuth {
		t.Fatalf("unexpected handshake: %#v", hs)
	}
}

func TestWriteOKAndWriteError(t *testing.T) {
	var okBuf bytes.Buffer
	if err := WriteOK(&okBuf, CommandPagination, PaginationResult{Continue: true}); err != nil {
		t.Fatalf("WriteOK returned error: %v", err)
	}
	var okEnv envelope.Envelope
	if err := json.Unmarshal(okBuf.Bytes(), &okEnv); err != nil {
		t.Fatalf("decode ok envelope: %v", err)
	}
	if okEnv.Status != envelope.StatusOK || okEnv.Command != CommandPagination {
		t.Fatalf("unexpected ok envelope: %#v", okEnv)
	}

	var errBuf bytes.Buffer
	if err := WriteError(&errBuf, CommandAuth, "EAUTH", "missing key", map[string]any{"hint": "set key"}); err != nil {
		t.Fatalf("WriteError returned error: %v", err)
	}
	var errEnv envelope.Envelope
	if err := json.Unmarshal(errBuf.Bytes(), &errEnv); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if errEnv.Status != envelope.StatusError || errEnv.Error.Code != "EAUTH" {
		t.Fatalf("unexpected error envelope: %#v", errEnv)
	}
}
