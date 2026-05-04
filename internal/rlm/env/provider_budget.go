package env

import (
	"context"
	"errors"
	"time"
)

var errLocalProviderBudgetCapped = errors.New("local provider budget capped")

type localProviderBudget struct {
	MaxFiles int
	MaxBytes int64
	MaxHits  int

	files    int
	bytes    int64
	hits     int
	capped   bool
	reason   string
	deadline time.Time
}

func newLocalProviderBudget(ctx context.Context, limit int) *localProviderBudget {
	if limit <= 0 {
		limit = 8
	}
	b := &localProviderBudget{
		MaxFiles: minInt(maxInt(limit*500, 1000), 5000),
		MaxBytes: int64(minInt(maxInt(limit*1_000_000, 4_000_000), 20_000_000)),
		MaxHits:  minInt(maxInt(limit*16, 64), 512),
	}
	if deadline, ok := ctx.Deadline(); ok {
		b.deadline = deadline
	}
	return b
}

func (b *localProviderBudget) beforeFile(ctx context.Context) error {
	if b == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return b.cap("context_cancelled")
	}
	if !b.deadline.IsZero() && time.Until(b.deadline) <= 0 {
		return b.cap("deadline_exceeded")
	}
	if b.MaxFiles > 0 && b.files >= b.MaxFiles {
		return b.cap("max_files")
	}
	return nil
}

func (b *localProviderBudget) recordFile(size int64) error {
	if b == nil {
		return nil
	}
	b.files++
	if size > 0 {
		if b.MaxBytes > 0 && b.bytes+size > b.MaxBytes {
			return b.cap("max_bytes")
		}
		b.bytes += size
	}
	return nil
}

func (b *localProviderBudget) recordHit() error {
	if b == nil {
		return nil
	}
	b.hits++
	if b.MaxHits > 0 && b.hits >= b.MaxHits {
		return b.cap("max_hits")
	}
	return nil
}

func (b *localProviderBudget) cap(reason string) error {
	if b == nil {
		return nil
	}
	if !b.capped {
		b.capped = true
		b.reason = reason
	}
	return errLocalProviderBudgetCapped
}

func (b *localProviderBudget) cappedError(err error) error {
	if errors.Is(err, errLocalProviderBudgetCapped) {
		return nil
	}
	return err
}

func (b *localProviderBudget) isCapped() bool {
	return b != nil && b.capped
}

func (b *localProviderBudget) snapshot() map[string]any {
	if b == nil {
		return nil
	}
	out := map[string]any{
		"max_files": b.MaxFiles,
		"max_bytes": b.MaxBytes,
		"max_hits":  b.MaxHits,
		"files":     b.files,
		"bytes":     b.bytes,
		"hits":      b.hits,
	}
	if b.capped {
		out["capped"] = true
		out["skip_reason"] = b.reason
	}
	if !b.deadline.IsZero() {
		out["deadline_unix_ms"] = b.deadline.UnixMilli()
	}
	return out
}
