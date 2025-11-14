package runservice

import (
	"fmt"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/cache"
)

// TryServeCache attempts to serve the response from cache based on the provided input.
// It returns (true, nil) when a cached response was written to stdout.
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
			return false, err
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
		return false, err
	}
	if ok {
		hit, err := cache.AnnotateHit(entry.Result, entry.CacheKey, e.options.Workspace, e.handle.Manifest.Metadata.Version)
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
		return false, fmt.Errorf("cache miss for key %s", e.cacheKey)
	}
	return false, nil
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
