package runservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/cas"
)

// HandleResult processes the execution result, pinning artifacts, persisting to cache/memory, and emitting the output.
func (e *Executor) HandleResult(jobID string, result []byte) error {
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

	result = e.enforceOutputLimit(e.ctx, result, jobID)

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
	limitKB := e.cfg.InlineOutputKB
	if limitKB <= 0 {
		limitKB = 32
	}
	limitBytes := limitKB * 1024

	if len(result) <= limitBytes {
		return result
	}

	casStore, err := cas.OpenDefault(ctx, e.cfg.Home)
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

	wrapper := buildOutputWrapper(result, obj.Digest, len(result), limitKB)
	wrapped, err := json.Marshal(wrapper)
	if err != nil {
		if _, warnErr := fmt.Fprintf(e.stderr, "output limiting: marshal wrapper failed: %v\n", err); warnErr != nil {
			errs.Ignore(warnErr, "warn marshal wrapper")
		}
		return result
	}

	if _, warnErr := fmt.Fprintf(e.stderr, "output %d bytes exceeded %dKB limit; stored in CAS %s\n", len(result), limitKB, obj.Digest); warnErr != nil {
		errs.Ignore(warnErr, "warn output limit")
	}

	return wrapped
}

func buildOutputWrapper(original []byte, digest string, size, limitKB int) map[string]any {
	var env map[string]any
	if err := json.Unmarshal(original, &env); err != nil {
		return map[string]any{
			"version": 1,
			"status":  "ok",
			"command": "unknown",
			"data": map[string]any{
				"artifact": digest,
				"summary":  fmt.Sprintf("Output exceeded %dKB inline limit (%d bytes)", limitKB, size),
				"size":     size,
			},
			"meta": map[string]any{
				"cas_digest":      digest,
				"original_size":   size,
				"truncated":       true,
				"truncate_reason": "inline_output_kb",
			},
			"error": map[string]any{},
		}
	}

	command, _ := env["command"].(string)
	if command == "" {
		command = "unknown"
	}

	summary := extractSummary(env, limitKB, size)

	return map[string]any{
		"version": env["version"],
		"status":  env["status"],
		"command": command,
		"data": map[string]any{
			"artifact": digest,
			"summary":  summary,
			"size":     size,
		},
		"meta": mergeMeta(env["meta"], map[string]any{
			"cas_digest":      digest,
			"original_size":   size,
			"truncated":       true,
			"truncate_reason": "inline_output_kb",
		}),
		"error": normalizeError(env["error"]),
	}
}

func extractSummary(env map[string]any, limitKB, size int) string {
	data, ok := env["data"].(map[string]any)
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

	if m, ok := existing.(map[string]any); ok {
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
	if m, ok := errField.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
