package skillout

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// DefaultCASHintLines is the default pagination size (lines per page) for CAS hints.
const DefaultCASHintLines = 50

// DefaultCASHint builds a CAS hint with the default pagination size.
func DefaultCASHint(artifact skillmain.Artifact) envelope.CASHint {
	return BuildCASHint(artifact, DefaultCASHintLines)
}

// FormatCASHint returns a consistent hint for retrieving large outputs from CAS.
func FormatCASHint(label, digest string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Sprintf("Full output stored in CAS; fetch via: foxctl cas get %s", digest)
	}
	return fmt.Sprintf("Full %s stored in CAS; fetch via: foxctl cas get %s", label, digest)
}

// PersistJSONWithHint stores JSON in CAS and returns a retrieval hint.
func PersistJSONWithHint(
	ctx context.Context,
	rc *skillmain.RunContext,
	payload any,
	tag string,
	hintLines int,
) (skillmain.Artifact, *envelope.CASHint, error) {
	artifact, err := skillmain.PersistJSON(ctx, rc, payload, tag)
	if err != nil {
		return skillmain.Artifact{}, nil, err
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
	artifact, err := skillmain.PersistBuffer(ctx, rc, buf, kind, tag)
	if err != nil {
		return skillmain.Artifact{}, nil, err
	}
	hint := BuildCASHint(artifact, hintLines)
	return artifact, &hint, nil
}
