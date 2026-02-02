package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Collector aggregates runtime metrics for monitoring.
type Collector struct {
	mu              sync.RWMutex
	skillExecutions atomic.Uint64
	cacheHits       atomic.Uint64
	cacheMisses     atomic.Uint64
	casOps          atomic.Uint64
	executionTimes  []time.Duration
}

var global = &Collector{}

// Global returns the shared metrics collector.
func Global() *Collector {
	return global
}

// RecordSkillExecution increments the skill execution counter.
func (c *Collector) RecordSkillExecution() {
	c.skillExecutions.Add(1)
}

// RecordCacheHit increments cache hit counter.
func (c *Collector) RecordCacheHit() {
	c.cacheHits.Add(1)
}

// RecordCacheMiss increments cache miss counter.
func (c *Collector) RecordCacheMiss() {
	c.cacheMisses.Add(1)
}

// RecordCASOperation increments CAS operation counter.
func (c *Collector) RecordCASOperation() {
	c.casOps.Add(1)
}

// RecordExecutionTime adds a duration sample capped at 1000 entries.
func (c *Collector) RecordExecutionTime(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.executionTimes = append(c.executionTimes, d)
	if len(c.executionTimes) > 1000 {
		c.executionTimes = c.executionTimes[len(c.executionTimes)-1000:]
	}
}

// Reset clears all tracked metrics (useful for testing).
func (c *Collector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.skillExecutions.Store(0)
	c.cacheHits.Store(0)
	c.cacheMisses.Store(0)
	c.casOps.Store(0)
	c.executionTimes = nil
}

// Snapshot returns a copy of current metrics.
func (c *Collector) Snapshot() Metrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hits := c.cacheHits.Load()
	misses := c.cacheMisses.Load()
	var hitRate float64
	if hits+misses > 0 {
		hitRate = float64(hits) / float64(hits+misses)
	}

	var total time.Duration
	for _, d := range c.executionTimes {
		total += d
	}
	var avgMS int64
	if n := len(c.executionTimes); n > 0 {
		avg := total / time.Duration(n)
		avgMS = int64(avg / time.Millisecond)
	}

	return Metrics{
		SkillExecutions:    c.skillExecutions.Load(),
		CacheHits:          hits,
		CacheMisses:        misses,
		CacheHitRate:       hitRate,
		CASOperations:      c.casOps.Load(),
		AvgExecutionTimeMS: avgMS,
	}
}

// Metrics represents a snapshot of runtime metrics.
type Metrics struct {
	SkillExecutions    uint64  `json:"skill_executions"`
	CacheHits          uint64  `json:"cache_hits"`
	CacheMisses        uint64  `json:"cache_misses"`
	CacheHitRate       float64 `json:"cache_hit_rate"`
	CASOperations      uint64  `json:"cas_operations"`
	AvgExecutionTimeMS int64   `json:"avg_execution_time_ms"`
}
