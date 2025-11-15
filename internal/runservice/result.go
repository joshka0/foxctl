package runservice

import (
	"errors"
	"fmt"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/protocol"
)

func (e *Executor) HandleResult(jobID string, result []byte) error {
	// Downgrade artifact handling failures to warnings - we still pin in CAS,
	// but if metadata write fails, log to stderr and continue with the result envelope
	if err := e.handleArtifacts(jobID, result); err != nil {
		var metaErr artifactMetadataError
		if errors.As(err, &metaErr) {
			if _, warnErr := fmt.Fprintf(e.stderr, "artifact metadata write failed: %v\n", err); warnErr != nil {
				errs.Ignore(warnErr, "runservice: warn artifact metadata failure")
			}
		} else {
			return err
		}
	}
	annotated := protocol.AnnotateRunBytes(result, e.options.Workspace, e.handle.Manifest.Metadata.Version)
	if err := e.PersistCache(annotated); err != nil {
		if _, warnErr := fmt.Fprintf(e.stderr, "cache put failed: %v\n", err); warnErr != nil {
			errs.Ignore(warnErr, "runservice: warn cache persist failure")
		}
	}
	if err := e.remember(annotated); err != nil {
		if _, warnErr := fmt.Fprintf(e.stderr, "remember failed: %v\n", err); warnErr != nil {
			errs.Ignore(warnErr, "runservice: warn remember failure")
		}
	}
	return writeEnvelope(e.stdout, annotated)
}
