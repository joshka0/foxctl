package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/spf13/cobra"
)

func newCASCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cas",
		Short: "Content-addressable storage helpers",
	}
	cmd.AddCommand(
		newCASPutCommand(),
		newCASHeadCommand(),
		newCASGetCommand(),
		newCASReadCommand(),
		newCASListCommand(),
		newCASPinCommand(),
		newCASUnpinCommand(),
		newCASRemoveCommand(),
		newCASGCCommand(),
	)
	return cmd
}

func init() {
	rootCmd.AddCommand(newCASCommand())
}

func newCASPutCommand() *cobra.Command {
	var kind string
	var tags []string
	var pin bool
	cmd := &cobra.Command{
		Use:   "put <file|->",
		Short: "Write bytes into the CAS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := commandConfig(cmd.Context())
			if err != nil {
				return err
			}
			store, err := cas.NewStore(cfg.Paths.CAS)
			if err != nil {
				return err
			}
			r, err := openInput(args[0], cmd.InOrStdin())
			if err != nil {
				return err
			}
			defer func() {
				errs.Ignore(r.Close(), "close cas put input")
			}()
			obj, err := store.Put(cmd.Context(), r, kind, tags)
			if err != nil {
				return err
			}
			if pin {
				if err := store.Pin(cmd.Context(), obj.Digest); err != nil {
					return err
				}
				obj, err = store.Head(cmd.Context(), obj.Digest)
				if err != nil {
					return err
				}
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.cas.put", obj, protocol.WithSource("run"))
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "application/octet-stream", "MIME type for the stored object")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "Tags to associate with the object")
	cmd.Flags().BoolVar(&pin, "pin", false, "Pin the object immediately after storage")
	return cmd
}

func newCASHeadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "head <digest>",
		Short: "Show metadata for a digest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := storeFromContext(cmd.Context())
			if err != nil {
				return err
			}
			obj, err := store.Head(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.cas.head", obj, protocol.WithSource("run"))
		},
	}
	return cmd
}

func newCASGetCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "get <digest>",
		Short: "Materialize a digest to a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := commandConfig(cmd.Context())
			if err != nil {
				return err
			}
			store, err := cas.NewStore(cfg.Paths.CAS)
			if err != nil {
				return err
			}
			rc, meta, err := store.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			dest := output
			if dest == "" {
				dest = filepath.Base(meta.Digest)
			}
			file, err := os.Create(dest)
			if err != nil {
				return fmt.Errorf("cas: create output: %w", err)
			}
			if _, err := io.Copy(file, rc); err != nil {
				errs.Ignore(file.Close(), "close output file after copy failure")
				errs.Ignore(rc.Close(), "close cas reader after copy failure")
				return fmt.Errorf("cas: copy: %w", err)
			}
			if err := file.Close(); err != nil {
				errs.Ignore(rc.Close(), "close cas reader after file close failure")
				return err
			}
			if err := rc.Close(); err != nil {
				return err
			}
			data := map[string]any{
				"digest":     meta.Digest,
				"size_bytes": meta.Size,
				"output":     dest,
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.cas.get", data, protocol.WithSource("run"))
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "File to write (default: <digest hex>)")
	return cmd
}

func newCASReadCommand() *cobra.Command {
	var (
		page       int
		pageSize   int
		allowLarge bool
	)

	const maxReadableBytes int64 = 10 * 1024 * 1024 // 10 MiB safety guard
	cmd := &cobra.Command{
		Use:   "read <digest>",
		Short: "Read text content from CAS with pagination",
		Long: `Read content from a CAS object with automatic pagination.

Use --page to retrieve specific pages of large content.
Default page size is 2048 bytes (2KB) to stay within context limits.

Examples:
  agentctl cas read sha256:abc123              # Read first page
  agentctl cas read sha256:abc123 --page 2     # Read second page
  agentctl cas read sha256:abc123 --page-size 4096  # Larger pages`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if pageSize <= 0 {
				return fmt.Errorf("page-size must be > 0")
			}

			cfg, err := commandConfig(cmd.Context())
			if err != nil {
				return err
			}
			store, err := cas.NewStore(cfg.Paths.CAS)
			if err != nil {
				return err
			}

			// Get metadata first for size info
			obj, err := store.Head(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if obj.Size > maxReadableBytes && !allowLarge {
				return fmt.Errorf("cas: object is %d bytes (> %d) — re-run with --allow-large to proceed", obj.Size, maxReadableBytes)
			}
			kind := strings.ToLower(obj.Kind)
			if kind != "" && !(strings.HasPrefix(kind, "text/") || strings.Contains(kind, "utf-8") || strings.Contains(kind, "json")) {
				return fmt.Errorf("cas: object kind %q is not text; use `agentctl cas get` to download binary content", obj.Kind)
			}

			// Read content
			rc, _, err := store.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			defer func() {
				errs.Ignore(rc.Close(), "close cas reader")
			}()

			// Calculate pagination from metadata size
			totalSize := obj.Size
			totalPages := int((totalSize + int64(pageSize) - 1) / int64(pageSize))
			if totalPages == 0 {
				totalPages = 1
			}

			// Validate page number
			if page < 1 {
				page = 1
			}
			if page > totalPages {
				page = totalPages
			}

			// Seek to page offset by discarding bytes up to the page start
			offset := int64(page-1) * int64(pageSize)
			if offset > 0 {
				if _, err := io.CopyN(io.Discard, rc, offset); err != nil && !errors.Is(err, io.EOF) {
					return fmt.Errorf("cas: seek to page offset: %w", err)
				}
			}

			remaining := int64(pageSize)
			if offset+remaining > totalSize {
				remaining = totalSize - offset
			}

			pageContent := ""
			if remaining > 0 {
				buf := make([]byte, remaining)
				n, readErr := io.ReadFull(rc, buf)
				if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
					return fmt.Errorf("cas: read page: %w", readErr)
				}
				buf = buf[:n]
				if !utf8.Valid(buf) {
					return fmt.Errorf("cas: content is not valid UTF-8; use `agentctl cas get` for binary data")
				}
				pageContent = string(buf)
			}

			// Build response
			data := map[string]any{
				"digest":       args[0],
				"content":      pageContent,
				"page":         page,
				"total_pages":  totalPages,
				"page_size":    pageSize,
				"total_bytes":  totalSize,
				"content_type": obj.Kind,
			}

			// Add navigation hints
			if page < totalPages {
				data["next_page"] = page + 1
				data["next_command"] = fmt.Sprintf("agentctl cas read %s --page %d", args[0], page+1)
			}
			if page > 1 {
				data["prev_page"] = page - 1
			}

			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.cas.read", data, protocol.WithSource("run"))
		},
	}
	cmd.Flags().IntVar(&page, "page", 1, "Page number to retrieve (1-indexed)")
	cmd.Flags().IntVar(&pageSize, "page-size", 2048, "Bytes per page (default: 2048)")
	cmd.Flags().BoolVar(&allowLarge, "allow-large", false, "Allow reading objects larger than 10MB (may be slow)")
	return cmd
}

func newCASListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored digests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := storeFromContext(cmd.Context())
			if err != nil {
				return err
			}
			objects, err := store.List(cmd.Context())
			if err != nil {
				return err
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.cas.list", map[string]any{"objects": objects}, protocol.WithSource("run"))
		},
	}
	return cmd
}

func newCASPinCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pin <digest>",
		Short: "Pin a digest to prevent GC",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := storeFromContext(cmd.Context())
			if err != nil {
				return err
			}
			if err := store.Pin(cmd.Context(), args[0]); err != nil {
				return err
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.cas.pin", map[string]string{"digest": args[0]}, protocol.WithSource("run"))
		},
	}
	return cmd
}

func newCASUnpinCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unpin <digest>",
		Short: "Remove a pin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := storeFromContext(cmd.Context())
			if err != nil {
				return err
			}
			if err := store.Unpin(cmd.Context(), args[0]); err != nil {
				return err
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.cas.unpin", map[string]string{"digest": args[0]}, protocol.WithSource("run"))
		},
	}
	return cmd
}

func newCASRemoveCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <digest>",
		Short: "Remove an object from the store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := storeFromContext(cmd.Context())
			if err != nil {
				return err
			}
			err = store.Remove(cmd.Context(), args[0])
			if errors.Is(err, cas.ErrPinned) && force {
				if unpinErr := store.Unpin(cmd.Context(), args[0]); unpinErr != nil && !errors.Is(unpinErr, cas.ErrNotFound) {
					return unpinErr
				}
				err = store.Remove(cmd.Context(), args[0])
			}
			if err != nil {
				return err
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.cas.rm", map[string]string{"digest": args[0]}, protocol.WithSource("run"))
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Allow deletion of pinned objects")
	return cmd
}

func newCASGCCommand() *cobra.Command {
	var (
		olderThan  time.Duration
		dryRun     bool
		keepPinned = true
		maxDelete  int
	)
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Garbage collect CAS objects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := storeFromContext(cmd.Context())
			if err != nil {
				return err
			}
			opts := cas.GCOptions{
				DryRun:     dryRun,
				OlderThan:  olderThan,
				KeepPinned: keepPinned,
				MaxDelete:  maxDelete,
			}
			result, err := store.GC(cmd.Context(), opts)
			if err != nil {
				return err
			}
			data := map[string]any{
				"dry_run":         dryRun,
				"older_than":      olderThan.String(),
				"keep_pinned":     keepPinned,
				"max_delete":      maxDelete,
				"objects_deleted": result.ObjectsDeleted,
				"objects_skipped": result.ObjectsSkipped,
				"bytes_freed":     result.BytesFreed,
				"errors":          result.Errors,
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.cas.gc", data, protocol.WithSource("run"))
		},
	}
	cmd.Flags().DurationVar(&olderThan, "older-than", 72*time.Hour, "Only delete objects older than this duration (e.g. 24h)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be deleted without removing files")
	cmd.Flags().BoolVar(&keepPinned, "keep-pinned", true, "Skip pinned digests during GC")
	cmd.Flags().IntVar(&maxDelete, "max-delete", 0, "Maximum number of objects to delete (0 = unlimited)")
	return cmd
}

func storeFromContext(ctx context.Context) (storage.CASStore, error) {
	cfg, err := commandConfig(ctx)
	if err != nil {
		return nil, err
	}
	return cas.NewStore(cfg.Paths.CAS)
}

func commandConfig(ctx context.Context) (config.Config, error) {
	cfg, ok := config.FromContext(ctx)
	if !ok {
		return config.Config{}, fmt.Errorf("configuration not loaded")
	}
	return cfg, nil
}

func openInput(path string, stdin io.Reader) (io.ReadCloser, error) {
	if path == "-" {
		return io.NopCloser(stdin), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return file, nil
}
