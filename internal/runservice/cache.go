package runservice

import (
	"encoding/json"
	"fmt"
	"strings"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/cache"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/jkatigb/agentctl/internal/trajectorycapture"
)

// TryServeCache attempts to serve the response from cache based on the provided input.
// It returns (true, nil) when a cached response was written to stdout (hit or error envelope).
// In auto mode, cache errors are non-fatal: they're logged and treated as misses.
func (e *Executor) TryServeCache(input []byte) (bool, error) {
	if e.options.Async || e.options.CacheMode == cache.ModeOff {
		return false, nil
	}
	if e.cacheStore == nil {
		store, err := cache.Open(e.ctx, e.cfg.Paths.Cache, cache.Options{
			AutoTTL: e.cfg.Memory.AutoCacheTTL,
			CASPath: e.cfg.Paths.CAS,
		})
		if err != nil {
			// In auto mode, cache errors are non-fatal - log and treat as miss
			if e.options.CacheMode == cache.ModeAuto {
				if _, warnErr := fmt.Fprintf(e.stderr, "cache unavailable: %v\n", err); warnErr != nil {
					errs.Ignore(warnErr, "runservice: warn cache unavailable")
				}
				return false, nil
			}
			// In only mode, emit error envelope
			return e.writeCacheError(input, protocol.ErrorCodeECacheUnavailable, "cache storage unavailable", err)
		}
		key, err := cache.BuildKey(e.handle.Manifest, input, nil)
		if err != nil {
			errs.Ignore(store.Close(), "close cache store after key failure")
			return false, err
		}
		e.cacheStore = store
		e.cacheKey = key
	}
	entry, ok, err := e.cacheStore.Get(e.ctx, e.cacheKey)
	if err != nil {
		// In auto mode, cache errors are non-fatal - log and treat as miss
		if e.options.CacheMode == cache.ModeAuto {
			if _, warnErr := fmt.Fprintf(e.stderr, "cache get error: %v\n", err); warnErr != nil {
				errs.Ignore(warnErr, "runservice: warn cache get error")
			}
			return false, nil
		}
		// In only mode, emit error envelope
		return e.writeCacheError(input, protocol.ErrorCodeECacheUnavailable, "cache lookup failed", err)
	}
	if ok {
		hit, err := cache.AnnotateHitBytes(entry.Result, entry.CacheKey, e.options.Workspace, e.handle.Manifest.Metadata.Version)
		if err != nil {
			return false, err
		}
		hit = annotateCorrelationAndJob(hit, "", e.options.CorrelationID)
		if e.trajCapture == nil && e.cfg.Storage.Root != "" && strings.TrimSpace(e.options.Workspace) != "" {
			capture, capErr := trajectorycapture.Start(e.ctx, trajectorycapture.StartOptions{
				StorageRoot:     e.cfg.Storage.Root,
				WorkspaceID:     e.options.Workspace,
				Actor:           "actor:human:cli",
				Source:          trajectory.SourceCLI,
				CLICommand:      e.options.CLICommand,
				ProtocolCommand: e.handle.Manifest.Metadata.Name,
				JobID:           "",
				CorrelationID:   e.options.CorrelationID,
				Input:           input,
			})
			if capErr == nil {
				e.trajCapture = capture
			} else {
				errs.Ignore(capErr, "trajectory capture start")
			}
		}
		if e.trajCapture != nil {
			if strings.HasPrefix(e.handle.Manifest.Metadata.Name, "hooks/") {
				capErr := e.trajCapture.CaptureHookCall(e.ctx, e.handle.Manifest.Metadata.Name, input, "", e.options.CorrelationID)
				errs.Ignore(capErr, "trajectory capture hook call")
			}
			capErr := e.trajCapture.CaptureResult(e.ctx, hit, "", e.options.CorrelationID)
			errs.Ignore(capErr, "trajectory capture cache hit")
		}
		if _, warnErr := fmt.Fprintf(e.stderr, "cache hit %s\n", entry.CacheKey); warnErr != nil {
			errs.Ignore(warnErr, "runservice: warn cache hit")
		}
		if err := writeEnvelope(e.stdout, hit); err != nil {
			return true, err
		}
		return true, nil
	}
	if e.options.CacheMode == cache.ModeOnly {
		return e.writeCacheMissError(input)
	}
	return false, nil
}

// writeCacheMissError emits an ECACHE_MISS error envelope for cache-only mode.
func (e *Executor) writeCacheMissError(input []byte) (bool, error) {
	data := map[string]any{
		"cache_key": e.cacheKey,
		"hint":      "No cached result exists. Run with --cache=auto to execute the skill and populate the cache.",
	}
	env := protocol.Error(
		e.handle.Manifest.Metadata.Name,
		protocol.ErrorCodeECacheMiss,
		fmt.Sprintf("cache miss for key %s", e.cacheKey),
		data,
		protocol.WithWorkspace(e.options.Workspace),
		protocol.WithSkillVersion(e.handle.Manifest.Metadata.Version),
		protocol.WithCacheKey(e.cacheKey),
	)
	env.Meta.CorrelID = e.options.CorrelationID
	if e.trajCapture == nil && e.cfg.Storage.Root != "" && strings.TrimSpace(e.options.Workspace) != "" {
		capture, capErr := trajectorycapture.Start(e.ctx, trajectorycapture.StartOptions{
			StorageRoot:     e.cfg.Storage.Root,
			WorkspaceID:     e.options.Workspace,
			Actor:           "actor:human:cli",
			Source:          trajectory.SourceCLI,
			CLICommand:      e.options.CLICommand,
			ProtocolCommand: e.handle.Manifest.Metadata.Name,
			JobID:           "",
			CorrelationID:   e.options.CorrelationID,
			Input:           e.options.Input,
		})
		if capErr == nil {
			e.trajCapture = capture
		} else {
			errs.Ignore(capErr, "trajectory capture start")
		}
	}
	if e.trajCapture != nil {
		if strings.HasPrefix(e.handle.Manifest.Metadata.Name, "hooks/") {
			rawIn := input
			if len(rawIn) == 0 {
				rawIn = e.options.Input
			}
			capErr := e.trajCapture.CaptureHookCall(e.ctx, e.handle.Manifest.Metadata.Name, rawIn, "", e.options.CorrelationID)
			errs.Ignore(capErr, "trajectory capture hook call")
		}
		// Envelope marshalling; error is nil for valid envelope structs.
		raw, _ := json.Marshal(env) //nolint:errcheck
		capErr := e.trajCapture.CaptureResult(e.ctx, raw, "", e.options.CorrelationID)
		errs.Ignore(capErr, "trajectory capture cache miss")
	}
	if err := protocol.Write(e.stdout, env); err != nil {
		return true, err
	}
	return true, nil
}

// writeCacheError emits a cache error envelope for cache-only mode.
func (e *Executor) writeCacheError(input []byte, code protocol.ErrorCode, message string, cause error) (bool, error) {
	data := map[string]any{
		"hint": "Cache storage is unavailable. Check cache configuration and storage path.",
	}
	if cause != nil {
		data["cause"] = cause.Error()
	}
	env := protocol.Error(
		e.handle.Manifest.Metadata.Name,
		code,
		message,
		data,
		protocol.WithWorkspace(e.options.Workspace),
		protocol.WithSkillVersion(e.handle.Manifest.Metadata.Version),
	)
	env.Meta.CorrelID = e.options.CorrelationID
	if e.trajCapture == nil && e.cfg.Storage.Root != "" && strings.TrimSpace(e.options.Workspace) != "" {
		capture, capErr := trajectorycapture.Start(e.ctx, trajectorycapture.StartOptions{
			StorageRoot:     e.cfg.Storage.Root,
			WorkspaceID:     e.options.Workspace,
			Actor:           "actor:human:cli",
			Source:          trajectory.SourceCLI,
			CLICommand:      e.options.CLICommand,
			ProtocolCommand: e.handle.Manifest.Metadata.Name,
			JobID:           "",
			CorrelationID:   e.options.CorrelationID,
			Input:           e.options.Input,
		})
		if capErr == nil {
			e.trajCapture = capture
		} else {
			errs.Ignore(capErr, "trajectory capture start")
		}
	}
	if e.trajCapture != nil {
		if strings.HasPrefix(e.handle.Manifest.Metadata.Name, "hooks/") {
			rawIn := input
			if len(rawIn) == 0 {
				rawIn = e.options.Input
			}
			capErr := e.trajCapture.CaptureHookCall(e.ctx, e.handle.Manifest.Metadata.Name, rawIn, "", e.options.CorrelationID)
			errs.Ignore(capErr, "trajectory capture hook call")
		}
		// Envelope marshalling; error is nil for valid envelope structs.
		raw, _ := json.Marshal(env) //nolint:errcheck
		capErr := e.trajCapture.CaptureResult(e.ctx, raw, "", e.options.CorrelationID)
		errs.Ignore(capErr, "trajectory capture cache error")
	}
	if err := protocol.Write(e.stdout, env); err != nil {
		return true, err
	}
	return true, nil
}

// PersistCache saves the execution result to the cache when cache mode allows it.
func (e *Executor) PersistCache(result []byte) error {
	if e.options.CacheMode != cache.ModeAuto || e.cacheStore == nil || e.cacheKey == "" {
		return nil
	}
	entry := cache.Entry{
		CacheKey:     e.cacheKey,
		SkillName:    e.handle.Manifest.Metadata.Name,
		SkillVersion: e.handle.Manifest.Metadata.Version,
		Workspace:    e.options.Workspace,
		Result:       result,
		Digests:      cache.CollectDigests(result),
	}
	return e.cacheStore.Put(e.ctx, entry)
}
