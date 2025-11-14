package metrics

import (
	"testing"
	"time"
)

func TestCollectorSnapshot(t *testing.T) {
	c := &Collector{}
	c.RecordSkillExecution()
	c.RecordSkillExecution()
	c.RecordCacheHit()
	c.RecordCacheMiss()
	c.RecordCASOperation()
	c.RecordExecutionTime(100 * time.Millisecond)
	c.RecordExecutionTime(200 * time.Millisecond)

	snap := c.Snapshot()
	if snap.SkillExecutions != 2 {
		t.Fatalf("expected 2 executions, got %d", snap.SkillExecutions)
	}
	if snap.CacheHits != 1 || snap.CacheMisses != 1 {
		t.Fatalf("unexpected cache counts: %+v", snap)
	}
	if snap.CacheHitRate != 0.5 {
		t.Fatalf("expected hit rate 0.5, got %f", snap.CacheHitRate)
	}
	if snap.CASOperations != 1 {
		t.Fatalf("expected 1 cas op, got %d", snap.CASOperations)
	}
	if snap.AvgExecutionTimeMS == 0 {
		t.Fatalf("expected avg execution time to be tracked")
	}
}

func TestCollectorReset(t *testing.T) {
	c := &Collector{}
	c.RecordSkillExecution()
	c.RecordCacheHit()
	c.RecordExecutionTime(time.Second)
	c.Reset()
	snap := c.Snapshot()
	if snap.SkillExecutions != 0 || snap.CacheHits != 0 {
		t.Fatalf("expected reset snapshot, got %+v", snap)
	}
	if snap.AvgExecutionTimeMS != 0 {
		t.Fatalf("expected avg execution to reset")
	}
}
