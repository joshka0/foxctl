package cmd

import (
	"fmt"

	"github.com/jkatigb/agentctl/internal/cache"
	errs "github.com/jkatigb/agentctl/internal/errors"
)

// tryServeCache attempts to respond from the cache based on the provided input.
// It returns (done=true, nil) if a cache hit was served.
// It returns (done=false, nil) if cache should be bypassed or on cache miss.
// It returns (done=false, err) if an error occurred.
func (e *runExecutor) tryServeCache(input []byte) (bool, error) {
	// Skip cache for async or when explicitly disabled
	if e.options.Async || e.options.CacheMode == cache.ModeOff {
		return false, nil
	}

	// Initialize cache store if needed
	if e.cacheStore == nil {
		store, err := cache.Open(e.ctx, e.cfg.Paths.Cache, cache.Options{
			AutoTTL: e.cfg.Memory.AutoCacheTTL,
			CASPath: e.cfg.Paths.CAS,
		})
		if err != nil {
			return false, err
		}
		e.cacheStore = store

		// Build cache key
		key, err := cache.BuildKey(e.handle.Manifest, input, nil)
		if err != nil {
			return false, err
		}
		e.cacheKey = key
	}

	// Try to get from cache
	entry, ok, err := e.cacheStore.Get(e.ctx, e.cacheKey)
	if err != nil {
		return false, err
	}

	// Handle cache hit
	if ok {
		hit, err := cache.AnnotateHit(entry.Result, entry.CacheKey, e.options.Workspace, e.handle.Manifest.Metadata.Version)
		if err != nil {
			return false, err
		}
		if _, warnErr := fmt.Fprintf(e.stderr, "cache hit %s\n", entry.CacheKey); warnErr != nil {
			errs.Ignore(warnErr, "run: warn cache hit")
		}
		if err := writeEnvelope(e.stdout, hit); err != nil {
			return true, err
		}
		return true, nil
	}

	// Handle cache-only mode with miss
	if e.options.CacheMode == cache.ModeOnly {
		return false, fmt.Errorf("cache miss for key %s", e.cacheKey)
	}

	// Cache miss, continue with execution
	return false, nil
}

// persistCache saves the execution result to the cache.
func (e *runExecutor) persistCache(result []byte) error {
	// Skip if cache is disabled or not initialized
	if e.options.CacheMode != cache.ModeAuto || e.cacheStore == nil || e.cacheKey == "" {
		return nil
	}

	// Build cache entry
	entry := cache.Entry{
		CacheKey:     e.cacheKey,
		SkillName:    e.handle.Manifest.Metadata.Name,
		SkillVersion: e.handle.Manifest.Metadata.Version,
		Workspace:    e.options.Workspace,
		Result:       result,
		Digests:      cache.CollectDigests(result),
	}

	// Save to cache
	return e.cacheStore.Put(e.ctx, entry)
}
