package skillcas

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type fakeCommandRunner struct {
	name     string
	args     []string
	stdin    []byte
	stdout   string
	stderr   string
	runError error
}

func (r *fakeCommandRunner) Run(_ context.Context, name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	r.name = name
	r.args = append([]string(nil), args...)
	if stdin != nil {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		r.stdin = data
	}
	if r.stdout != "" {
		if _, err := io.WriteString(stdout, r.stdout); err != nil {
			return err
		}
	}
	if r.stderr != "" {
		if _, err := io.WriteString(stderr, r.stderr); err != nil {
			return err
		}
	}
	return r.runError
}

func TestCLIWriterPutArtifactUsesFoxctlCASPutContract(t *testing.T) {
	runner := &fakeCommandRunner{
		stdout: `{"version":1,"status":"ok","command":"foxctl.cas.put","data":{"digest":"sha256:abc","size_bytes":12,"kind":"text/plain"},"meta":{"ts":"2026-01-01T00:00:00Z"},"error":{}}`,
	}
	writer := &CLIWriter{Binary: "foxctl-dev", Runner: runner}

	artifact, err := writer.PutArtifact(context.Background(), strings.NewReader("hello world!"), "text/plain", []string{"one", "", "two"})
	if err != nil {
		t.Fatalf("PutArtifact returned error: %v", err)
	}

	if runner.name != "foxctl-dev" {
		t.Fatalf("binary = %q, want foxctl-dev", runner.name)
	}
	wantArgs := "cas put --kind text/plain --tag one --tag two -"
	if got := strings.Join(runner.args, " "); got != wantArgs {
		t.Fatalf("args = %q, want %q", got, wantArgs)
	}
	if string(runner.stdin) != "hello world!" {
		t.Fatalf("stdin = %q, want payload", runner.stdin)
	}
	if artifact != (Artifact{Digest: "sha256:abc", Size: 12, Kind: "text/plain"}) {
		t.Fatalf("artifact = %+v, want decoded artifact", artifact)
	}
}

func TestCLIWriterDefaultsBinaryAndRunner(t *testing.T) {
	writer := NewCLIWriter("")
	if writer.Binary != "foxctl" {
		t.Fatalf("Binary = %q, want foxctl", writer.Binary)
	}
	if writer.Runner == nil {
		t.Fatal("Runner is nil")
	}
}

func TestDecodePutEnvelopeReportsErrorEnvelope(t *testing.T) {
	_, err := decodePutEnvelope([]byte(`{"version":1,"status":"error","command":"foxctl.cas.put","error":{"code":"ERUNTIME","message":"disk full"},"meta":{"ts":"2026-01-01T00:00:00Z"}}`))
	if err == nil {
		t.Fatal("decodePutEnvelope returned nil error")
	}
	if !strings.Contains(err.Error(), "ERUNTIME") || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %q, want status code and message", err)
	}
}

func TestDecodePutEnvelopeRequiresDigest(t *testing.T) {
	_, err := decodePutEnvelope([]byte(`{"version":1,"status":"ok","command":"foxctl.cas.put","data":{"size_bytes":12},"meta":{"ts":"2026-01-01T00:00:00Z"},"error":{}}`))
	if err == nil {
		t.Fatal("decodePutEnvelope returned nil error")
	}
	if !strings.Contains(err.Error(), "missing digest") {
		t.Fatalf("error = %q, want missing digest", err)
	}
}

func TestCLIWriterIncludesStderrOnRunnerError(t *testing.T) {
	runner := &fakeCommandRunner{
		stderr:   "permission denied",
		runError: errors.New("exit status 1"),
	}
	writer := &CLIWriter{Binary: "foxctl", Runner: runner}

	_, err := writer.PutArtifact(context.Background(), bytes.NewBufferString("payload"), "text/plain", nil)
	if err == nil {
		t.Fatal("PutArtifact returned nil error")
	}
	if !strings.Contains(fmt.Sprint(err), "exit status 1") || !strings.Contains(fmt.Sprint(err), "permission denied") {
		t.Fatalf("error = %q, want runner error and stderr", err)
	}
}
