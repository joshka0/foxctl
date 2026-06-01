package skillout

import (
	"bytes"
	"context"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillcas"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// DefaultCASHintLines is the default pagination size (lines per page) for CAS hints.
const DefaultCASHintLines = 50

// DefaultCASHint builds a CAS hint with the default pagination size.
func DefaultCASHint(artifact skillcas.Artifact) envelope.CASHint {
	return BuildCASHint(artifact, DefaultCASHintLines)
}

// FormatCASHint returns a consistent hint for retrieving large outputs from CAS.
func FormatCASHint(label, digest string) string {
	return skillcas.FormatCASHint(label, digest)
}

// PersistJSONWithHint stores JSON in CAS and returns a retrieval hint.
func PersistJSONWithHint(
	ctx context.Context,
	rc *skillmain.RunContext,
	payload any,
	tag string,
	hintLines int,
) (skillmain.Artifact, *envelope.CASHint, error) {
	return PersistJSONWithHintContext(ctx, rc, payload, tag, hintLines)
}

// PersistJSONWithHintContext stores JSON through a CAS writer and returns a retrieval hint.
func PersistJSONWithHintContext(
	ctx context.Context,
	writer skillcas.Writer,
	payload any,
	tag string,
	hintLines int,
) (skillcas.Artifact, *envelope.CASHint, error) {
	artifact, err := skillcas.PersistJSON(ctx, writer, payload, tag)
	if err != nil {
		return skillcas.Artifact{}, nil, err
	}
	hint := BuildCASHint(artifact, hintLines)
	return artifact, &hint, nil
}

// PersistBufferWithHint stores a buffer in CAS and returns a retrieval hint.
func PersistBufferWithHint(
	ctx context.Context,
	rc *skillmain.RunContext,
	buf *bytes.Buffer,
	kind string,
	tag string,
	hintLines int,
) (skillmain.Artifact, *envelope.CASHint, error) {
	return PersistBufferWithHintContext(ctx, rc, buf, kind, tag, hintLines)
}

// PersistBufferWithHintContext stores a buffer through a CAS writer and returns a retrieval hint.
func PersistBufferWithHintContext(
	ctx context.Context,
	writer skillcas.Writer,
	buf *bytes.Buffer,
	kind string,
	tag string,
	hintLines int,
) (skillcas.Artifact, *envelope.CASHint, error) {
	artifact, err := skillcas.PersistBuffer(ctx, writer, buf, kind, tag)
	if err != nil {
		return skillcas.Artifact{}, nil, err
	}
	hint := BuildCASHint(artifact, hintLines)
	return artifact, &hint, nil
}
