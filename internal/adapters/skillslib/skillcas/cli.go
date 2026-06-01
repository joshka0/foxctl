package skillcas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// CommandRunner executes a command with explicit stdio wiring.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error
}

// ExecRunner executes commands through os/exec.
type ExecRunner struct{}

// Run executes name with args and waits for it to finish.
func (ExecRunner) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// CLIWriter stores artifacts through the foxctl CAS CLI contract.
type CLIWriter struct {
	Binary string
	Runner CommandRunner
}

// NewCLIWriter builds a CAS writer backed by `foxctl cas put`.
func NewCLIWriter(binary string) *CLIWriter {
	if strings.TrimSpace(binary) == "" {
		binary = "foxctl"
	}
	return &CLIWriter{Binary: binary, Runner: ExecRunner{}}
}

// PutArtifact writes content through `foxctl cas put -`.
func (w *CLIWriter) PutArtifact(ctx context.Context, r io.Reader, kind string, tags []string) (Artifact, error) {
	if w == nil {
		return Artifact{}, fmt.Errorf("skillcas: nil CLI writer")
	}
	runner := w.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	binary := strings.TrimSpace(w.Binary)
	if binary == "" {
		binary = "foxctl"
	}

	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "application/octet-stream"
	}
	args := []string{"cas", "put", "--kind", kind}
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			args = append(args, "--tag", tag)
		}
	}
	args = append(args, "-")

	var stdout, stderr bytes.Buffer
	if err := runner.Run(ctx, binary, args, r, &stdout, &stderr); err != nil {
		return Artifact{}, fmt.Errorf("skillcas: foxctl cas put: %w%s", err, stderrSuffix(stderr.String()))
	}
	return decodePutEnvelope(stdout.Bytes())
}

func stderrSuffix(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}

type cliEnvelope struct {
	Status  string          `json:"status"`
	Command string          `json:"command"`
	Data    json.RawMessage `json:"data"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type cliPutData struct {
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
	Size      int64  `json:"size"`
	Kind      string `json:"kind"`
}

func decodePutEnvelope(data []byte) (Artifact, error) {
	var env cliEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Artifact{}, fmt.Errorf("skillcas: decode foxctl cas put envelope: %w", err)
	}
	if env.Status != envelope.StatusOK {
		msg := strings.TrimSpace(env.Error.Message)
		if msg == "" {
			msg = "foxctl cas put returned non-ok status"
		}
		if env.Error.Code != "" {
			return Artifact{}, fmt.Errorf("skillcas: %s: %s", env.Error.Code, msg)
		}
		return Artifact{}, fmt.Errorf("skillcas: %s", msg)
	}

	var payload cliPutData
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		return Artifact{}, fmt.Errorf("skillcas: decode foxctl cas put data: %w", err)
	}
	if strings.TrimSpace(payload.Digest) == "" {
		return Artifact{}, fmt.Errorf("skillcas: foxctl cas put response missing digest")
	}
	size := payload.SizeBytes
	if size == 0 {
		size = payload.Size
	}
	return Artifact{Digest: payload.Digest, Size: size, Kind: payload.Kind}, nil
}
