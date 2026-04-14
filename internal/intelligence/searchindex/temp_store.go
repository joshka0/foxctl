package searchindex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenEphemeral creates an isolated SQL-backed search index under a temporary
// root and returns a cleanup function that closes the store and removes the temp
// directory. This is intended for per-query bootstrap paths that should not
// mutate the shared workspace searchindex.db.
func OpenEphemeral(ctx context.Context, baseRoot string) (Store, func() error, error) {
	parent := strings.TrimSpace(baseRoot)
	if parent == "" {
		parent = os.TempDir()
	}
	parent = filepath.Join(parent, "tmp-searchindex")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, nil, fmt.Errorf("searchindex: create temp parent: %w", err)
	}
	root, err := os.MkdirTemp(parent, "run-*")
	if err != nil {
		return nil, nil, fmt.Errorf("searchindex: create temp root: %w", err)
	}
	store, err := Open(ctx, root)
	if err != nil {
		_ = os.RemoveAll(root)
		return nil, nil, err
	}
	cleanup := func() error {
		var closeErr error
		if store != nil {
			closeErr = store.Close()
		}
		removeErr := os.RemoveAll(root)
		if closeErr != nil {
			return closeErr
		}
		if removeErr != nil {
			return fmt.Errorf("searchindex: remove temp root: %w", removeErr)
		}
		return nil
	}
	return store, cleanup, nil
}
