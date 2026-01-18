package runservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/maputil"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/cas"
)

// HandleResult processes the execution result, pinning artifacts, persisting to cache/memory, and emitting the output.
func (e *Executor) HandleResult(jobID string, result []byte) error {
	// IMPORTANT: enforceOutputLimit must run BEFORE handleArtifacts because:
	// 1. enforceOutputLimit may create a new CAS object for truncated output
	// 2. handleArtifacts extracts digests from the result and pins them
	// If we run them in the opposite order, the truncation digest won't be pinned
	// and could be garbage collected, causing a dangling reference.
	result = e.enforceOutputLimit(e.ctx, result, jobID)

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
	annotated = annotateCorrelationAndJob(annotated, jobID, e.options.CorrelationID)
	if e.trajCapture != nil {
		capErr := e.trajCapture.CaptureResult(e.ctx, annotated, jobID, e.options.CorrelationID)
		errs.Ignore(capErr, "trajectory capture result")
	}
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

func (e *Executor) enforceOutputLimit(ctx context.Context, result []byte, jobID string) []byte {
	if e.options.NoCAS || !e.cfg.CAS.Store {
		return result
	}

	limitKB := e.cfg.InlineOutputKB
	if limitKB <= 0 {
		limitKB = 32
	}
	limitBytes := limitKB * 1024

	if len(result) <= limitBytes {
		return result
	}

	casStore, err := cas.NewStore(e.cfg.Paths.CAS)
	if err != nil {
		if _, warnErr := fmt.Fprintf(e.stderr, "output limiting: cas open failed: %v\n", err); warnErr != nil {
			errs.Ignore(warnErr, "warn cas open")
		}
		return result
	}
	defer func() { errs.Ignore(casStore.Close(), "close cas store") }()

	obj, err := casStore.Put(ctx, bytes.NewReader(result), "application/json", []string{"truncated-output"})
	if err != nil {
		if _, warnErr := fmt.Fprintf(e.stderr, "output limiting: cas put failed: %v\n", err); warnErr != nil {
			errs.Ignore(warnErr, "warn cas put")
		}
		return result
	}

	if _, warnErr := fmt.Fprintf(e.stderr, "output %d bytes exceeded %dKB limit; stored in CAS %s\n", len(result), limitKB, obj.Digest); warnErr != nil {
		errs.Ignore(warnErr, "warn output limit")
	}

	// Build wrapper based on expose policy
	wrapper := buildOutputWrapperWithPolicy(result, obj.Digest, len(result), limitKB, e.cfg.CAS.Expose)
	wrapped, err := json.Marshal(wrapper)
	if err != nil {
		if _, warnErr := fmt.Fprintf(e.stderr, "output limiting: marshal wrapper failed: %v\n", err); warnErr != nil {
			errs.Ignore(warnErr, "warn marshal wrapper")
		}
		return result
	}

	return wrapped
}

// buildOutputWrapperWithPolicy builds a truncation wrapper respecting CAS expose policy.
// - ExposePolicyOff: Store in CAS but hide digest from output (debugging only)
// - ExposePolicyDigest: Include raw digest in output
// - ExposePolicyHint: Include full retrieval hints in output
func buildOutputWrapperWithPolicy(original []byte, digest string, size, limitKB int, expose config.ExposePolicy) map[string]any {
	var env map[string]any
	if err := json.Unmarshal(original, &env); err != nil {
		env = map[string]any{
			"version": 1,
			"status":  "ok",
			"command": "unknown",
		}
	}

	command, _ := env["command"].(string)
	if command == "" {
		command = "unknown"
	}

	summary := extractSummary(env, limitKB, size)

	// Build data section based on expose policy
	data := map[string]any{
		"summary":   summary,
		"size":      size,
		"truncated": true,
	}

	// Build meta section
	meta := map[string]any{
		"original_size":   size,
		"truncated":       true,
		"truncate_reason": "inline_output_kb",
	}

	// Add digest/hint based on policy
	switch expose {
	case config.ExposePolicyOff:
		// Store for debugging, but don't expose in output
		// The digest is still logged to stderr and available in wide events
		data["stored"] = true
	case config.ExposePolicyDigest:
		// Include raw digest
		data["artifact"] = digest
		meta["cas_digest"] = digest
	case config.ExposePolicyHint:
		// Include full retrieval hints
		data["artifact"] = digest
		data["hint"] = map[string]any{
			"digest":       digest,
			"read_command": fmt.Sprintf("agentctl cas read %s", digest),
			"get_command":  fmt.Sprintf("agentctl cas get %s", digest),
		}
		meta["cas_digest"] = digest
	default:
		// Default to off
		data["stored"] = true
	}

	return map[string]any{
		"version": env["version"],
		"status":  env["status"],
		"command": command,
		"data":    data,
		"meta":    mergeMeta(env["meta"], meta),
		"error":   normalizeError(env["error"]),
	}
}

func extractSummary(env map[string]any, limitKB, size int) string {
	data, ok := maputil.AsStringMap(env["data"])
	if !ok {
		return fmt.Sprintf("Output exceeded %dKB inline limit (%d bytes)", limitKB, size)
	}

	if summary, ok := data["summary"].(string); ok && summary != "" {
		runes := []rune(summary)
		if len(runes) > 500 {
			return string(runes[:497]) + "..."
		}
		return summary
	}

	if preview, ok := data["preview"].(string); ok && preview != "" {
		runes := []rune(preview)
		if len(runes) > 500 {
			return string(runes[:497]) + "..."
		}
		return preview
	}

	return fmt.Sprintf("Output exceeded %dKB inline limit (%d bytes); retrieve with: agentctl cas get <digest>", limitKB, size)
}

func mergeMeta(existing any, additions map[string]any) map[string]any {
	result := make(map[string]any)

	if m, ok := maputil.AsStringMap(existing); ok {
		for k, v := range m {
			result[k] = v
		}
	}

	for k, v := range additions {
		result[k] = v
	}

	return result
}

// normalizeError ensures the error field is always a non-nil map for stable JSON serialization.
func normalizeError(errField any) map[string]any {
	if errField == nil {
		return map[string]any{}
	}
	if m, ok := maputil.AsStringMap(errField); ok {
		return m
	}
	return map[string]any{}
}
