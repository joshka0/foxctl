package runservice

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
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
	annotated := annotateRunMeta(result, e.options.Workspace, e.handle.Manifest.Metadata.Version)
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

func annotateRunMeta(result []byte, workspacePath, skillVersion string) []byte {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err != nil {
		return result
	}
	env.Meta.Source = "run"
	if workspacePath != "" {
		env.Meta.Workspace = workspacePath
	}
	if skillVersion != "" {
		env.Meta.SkillVer = skillVersion
	}
	data, err := json.Marshal(env)
	if err != nil {
		return result
	}
	return data
}
