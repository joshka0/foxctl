package skillcas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// ExposePolicy controls how CAS-backed outputs are disclosed in skill results.
type ExposePolicy string

const (
	// ExposePolicyOff stores outputs without exposing the digest.
	ExposePolicyOff ExposePolicy = "off"
	// ExposePolicyDigest exposes only the digest.
	ExposePolicyDigest ExposePolicy = "digest"
	// ExposePolicyHint exposes the digest and retrieval commands.
	ExposePolicyHint ExposePolicy = "hint"
)

const defaultHintLines = 50

// Artifact describes a stored CAS object without exposing the storage backend.
type Artifact struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	Kind   string `json:"kind"`
}

// Writer stores content in CAS and returns backend-neutral artifact metadata.
type Writer interface {
	PutArtifact(ctx context.Context, r io.Reader, kind string, tags []string) (Artifact, error)
}

// OutputContext is the minimal capability set needed for CAS-backed output.
type OutputContext interface {
	Writer
	OutputWriter() io.Writer
	ShouldTruncate(dataSize int) bool
	ShouldStoreCAS() bool
	CASExposePolicy() ExposePolicy
}

// PersistJSON marshals value to JSON and stores it with the provided tags.
func PersistJSON(ctx context.Context, writer Writer, value any, tags ...string) (Artifact, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return Artifact{}, fmt.Errorf("skillcas: marshal json: %w", err)
	}
	return PersistBuffer(ctx, writer, bytes.NewBuffer(payload), "application/json", tags...)
}

// PersistBuffer stores the provided buffer without consuming the caller's buffer.
func PersistBuffer(ctx context.Context, writer Writer, buf *bytes.Buffer, kind string, tags ...string) (Artifact, error) {
	if writer == nil {
		return Artifact{}, fmt.Errorf("skillcas: writer not configured")
	}
	if buf == nil {
		return Artifact{}, fmt.Errorf("skillcas: persist buffer: nil buffer")
	}
	artifact, err := writer.PutArtifact(ctx, bytes.NewReader(buf.Bytes()), kind, tags)
	if err != nil {
		return Artifact{}, fmt.Errorf("skillcas: put artifact: %w", err)
	}
	return artifact, nil
}

// BuildCASHint creates a user-friendly CAS hint for the given artifact.
func BuildCASHint(artifact Artifact, linesPerPage int) envelope.CASHint {
	hint := envelope.CASHint{
		Digest:      artifact.Digest,
		TotalBytes:  artifact.Size,
		ContentType: artifact.Kind,
		ReadCommand: fmt.Sprintf("foxctl cas read %s", artifact.Digest),
		GetCommand:  fmt.Sprintf("foxctl cas get %s", artifact.Digest),
	}

	if linesPerPage > 0 && artifact.Size > 0 {
		bytesPerPage := linesPerPage * 80
		if int(artifact.Size) > bytesPerPage {
			hint.PageCount = (int(artifact.Size) + bytesPerPage - 1) / bytesPerPage
			hint.PageSize = bytesPerPage
			hint.ReadCommand = fmt.Sprintf("foxctl cas read %s --page-size %d", artifact.Digest, bytesPerPage)
		}
	}

	if artifact.Kind != "" && !strings.HasPrefix(artifact.Kind, "text/") && artifact.Kind != "application/json" {
		hint.IsBinary = true
	}

	return hint
}

// FormatCASHint returns a concise retrieval hint for CAS-backed outputs.
func FormatCASHint(label, digest string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Sprintf("Full output stored in CAS; fetch via: foxctl cas get %s", digest)
	}
	return fmt.Sprintf("Full %s stored in CAS; fetch via: foxctl cas get %s", label, digest)
}

// BuildCASResult builds a truncation payload from the configured expose policy.
func BuildCASResult(artifact Artifact, expose ExposePolicy) map[string]any {
	result := map[string]any{
		"size":      artifact.Size,
		"truncated": true,
	}

	switch expose {
	case ExposePolicyOff:
		result["stored"] = true
	case ExposePolicyDigest:
		result["artifact"] = artifact.Digest
	case ExposePolicyHint:
		result["artifact"] = artifact.Digest
		result["hint"] = BuildCASHint(artifact, defaultHintLines)
	default:
		result["stored"] = true
	}

	return result
}
