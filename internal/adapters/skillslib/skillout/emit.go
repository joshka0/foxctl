package skillout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

// Emit writes a success envelope to stdout with standard metadata.
func Emit(rc *skillmain.RunContext, command string, data any) error {
	return EmitWithMeta(rc, command, data, envelope.Meta{})
}

// EmitWithMeta writes a success envelope with custom metadata.
func EmitWithMeta(rc *skillmain.RunContext, command string, data any, meta envelope.Meta) error {
	env := envelope.OK(command, data, envelope.WithMeta(meta))
	return envelope.Write(rc.Stdout, env)
}

// Fatal emits an error envelope. Use this for skills with custom main().
func Fatal(w io.Writer, command string, err *skillerr.Error) error {
	env := envelope.Error(command, err.Code, err.Message, err.ToEnvelopeData())
	return envelope.Write(w, env)
}

// Artifact is an alias for skillmain.Artifact.
// Deprecated: Use skillmain.Artifact directly.
type Artifact = skillmain.Artifact

// PersistJSON is an alias for skillmain.PersistJSON.
// Deprecated: Use skillmain.PersistJSON directly.
var PersistJSON = skillmain.PersistJSON

// PersistBuffer is an alias for skillmain.PersistBuffer.
// Deprecated: Use skillmain.PersistBuffer directly.
var PersistBuffer = skillmain.PersistBuffer

// BuildCASHint creates a user-friendly CAS hint for the given artifact.
func BuildCASHint(artifact Artifact, linesPerPage int) envelope.CASHint {
	hint := envelope.CASHint{
		Digest:      artifact.Digest,
		TotalBytes:  artifact.Size,
		ContentType: artifact.Kind,
		ReadCommand: fmt.Sprintf("agentctl cas read %s", artifact.Digest),
		GetCommand:  fmt.Sprintf("agentctl cas get %s", artifact.Digest),
	}

	// Calculate pagination if applicable (~80 bytes per line heuristic)
	if linesPerPage > 0 && artifact.Size > 0 {
		bytesPerPage := linesPerPage * 80
		if int(artifact.Size) > bytesPerPage {
			hint.PageCount = (int(artifact.Size) + bytesPerPage - 1) / bytesPerPage
			hint.PageSize = bytesPerPage
			hint.ReadCommand = fmt.Sprintf("agentctl cas read %s --page-size %d", artifact.Digest, bytesPerPage)
		}
	}

	// Detect binary content
	if artifact.Kind != "" && !strings.HasPrefix(artifact.Kind, "text/") && artifact.Kind != "application/json" {
		hint.IsBinary = true
	}

	return hint
}

// EmitWithCAS emits data, automatically storing large outputs in CAS.
// If data exceeds the inline limit, it's stored in CAS and exposed based on policy.
// - ExposePolicyOff: Store but don't include digest/hint in output
// - ExposePolicyDigest: Include raw digest in output
// - ExposePolicyHint: Include full retrieval hints in output
func EmitWithCAS(ctx context.Context, rc *skillmain.RunContext, command string, data any) error {
	// Marshal to check size
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal data: %w", err)
	}

	// Check if truncation needed
	if !rc.ShouldTruncate(len(payload)) {
		return Emit(rc, command, data)
	}

	// Check if CAS storage is enabled
	if !rc.ShouldStoreCAS() {
		return Emit(rc, command, data)
	}

	// Store in CAS
	artifact, err := PersistJSON(ctx, rc, data, command)
	if err != nil {
		return fmt.Errorf("persist to cas: %w", err)
	}

	// Build result based on expose policy
	result := BuildCASResult(artifact, rc.ExposePolicy())

	return Emit(rc, command, result)
}

// BuildCASResult builds the result payload based on CAS expose policy.
func BuildCASResult(artifact Artifact, expose config.ExposePolicy) map[string]any {
	result := map[string]any{
		"size":      artifact.Size,
		"truncated": true,
	}

	switch expose {
	case config.ExposePolicyOff:
		// Store for debugging, but don't expose in output
		result["stored"] = true
	case config.ExposePolicyDigest:
		// Include raw digest
		result["artifact"] = artifact.Digest
	case config.ExposePolicyHint:
		// Include full retrieval hints
		result["artifact"] = artifact.Digest
		result["hint"] = BuildCASHint(artifact, 50)
	default:
		// Default to off
		result["stored"] = true
	}

	return result
}
