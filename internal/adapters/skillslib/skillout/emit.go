package skillout

import (
	"context"
	"io"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillcas"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// Emit writes a success envelope to stdout with standard metadata.
func Emit(rc *skillmain.RunContext, command string, data any) error {
	return EmitWithMeta(rc, command, data, envelope.Meta{})
}

// EmitContext writes a success envelope using a backend-neutral output context.
func EmitContext(rc interface{ OutputWriter() io.Writer }, command string, data any) error {
	return skillcas.EmitOK(rc, command, data)
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

// Artifact is an alias for skillcas.Artifact.
// Deprecated: Use skillcas.Artifact directly.
type Artifact = skillcas.Artifact

// PersistJSON is an alias for skillmain.PersistJSON.
// Deprecated: Use skillmain.PersistJSON directly.
var PersistJSON = skillmain.PersistJSON

// PersistBuffer is an alias for skillmain.PersistBuffer.
// Deprecated: Use skillmain.PersistBuffer directly.
var PersistBuffer = skillmain.PersistBuffer

// BuildCASHint creates a user-friendly CAS hint for the given artifact.
func BuildCASHint(artifact Artifact, linesPerPage int) envelope.CASHint {
	return skillcas.BuildCASHint(artifact, linesPerPage)
}

// EmitWithCAS emits data, automatically storing large outputs in CAS.
// If data exceeds the inline limit, it's stored in CAS and exposed based on policy.
// - ExposePolicyOff: Store but don't include digest/hint in output
// - ExposePolicyDigest: Include raw digest in output
// - ExposePolicyHint: Include full retrieval hints in output
func EmitWithCAS(ctx context.Context, rc *skillmain.RunContext, command string, data any) error {
	return EmitWithCASContext(ctx, rc, command, data)
}

// EmitWithCASContext emits data through a backend-neutral CAS output context.
func EmitWithCASContext(ctx context.Context, rc skillcas.OutputContext, command string, data any) error {
	return skillcas.EmitWithCAS(ctx, rc, command, data)
}

// BuildCASResult builds the result payload based on CAS expose policy.
func BuildCASResult(artifact Artifact, expose skillcas.ExposePolicy) map[string]any {
	return skillcas.BuildCASResult(artifact, expose)
}
