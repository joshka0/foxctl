package runservice

import (
	"fmt"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/cache"
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
			return e.writeCacheError(protocol.ErrorCodeECacheUnavailable, "cache storage unavailable", err)
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
		return e.writeCacheError(protocol.ErrorCodeECacheUnavailable, "cache lookup failed", err)
	}
	if ok {
		hit, err := cache.AnnotateHitBytes(entry.Result, entry.CacheKey, e.options.Workspace, e.handle.Manifest.Metadata.Version)
		if err != nil {
			return false, err
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
		return e.writeCacheMissError()
	}
	return false, nil
}

// writeCacheMissError emits an ECACHE_MISS error envelope for cache-only mode.
func (e *Executor) writeCacheMissError() (bool, error) {
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
	if err := protocol.Write(e.stdout, env); err != nil {
		return true, err
	}
	return true, nil
}

// writeCacheError emits a cache error envelope for cache-only mode.
func (e *Executor) writeCacheError(code protocol.ErrorCode, message string, cause error) (bool, error) {
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
