package cas

import (
	"context"
	"time"

	"github.com/jkatigb/agentctl/internal/storage"
)

// GCOptions aliases the shared CAS GC options to preserve the public API.
type GCOptions = storage.CASGCOptions

// GCResult aliases the shared CAS GC result type.
type GCResult = storage.CASGCResult

// GC scans the store and removes objects that match the provided policy.
func (s *Store) GC(ctx context.Context, opts GCOptions) (GCResult, error) {
	if err := ctx.Err(); err != nil {
		return GCResult{}, err
	}
	objects, err := s.List(ctx)
	if err != nil {
		return GCResult{}, err
	}

	cutoff := time.Now().Add(-opts.OlderThan)
	var result GCResult

	for _, obj := range objects {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		if opts.KeepPinned && obj.Pinned {
			result.ObjectsSkipped++
			continue
		}

		if opts.OlderThan > 0 && obj.CreatedAt.After(cutoff) {
			result.ObjectsSkipped++
			continue
		}

		if opts.DryRun {
			result.ObjectsDeleted++
			result.BytesFreed += obj.Size
		} else {
			if err := s.Remove(ctx, obj.Digest); err != nil {
				if err == ErrPinned {
					result.ObjectsSkipped++
					continue
				}
				result.Errors++
				continue
			}
			result.ObjectsDeleted++
			result.BytesFreed += obj.Size
		}

		if opts.MaxDelete > 0 && result.ObjectsDeleted >= opts.MaxDelete {
			break
		}
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}
