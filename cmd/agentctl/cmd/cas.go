package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/cas"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
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
		newCASListCommand(),
		newCASPinCommand(),
		newCASUnpinCommand(),
		newCASRemoveCommand(),
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
				_ = r.Close()
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
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.cas.put", obj))
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
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.cas.head", obj))
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
				closeErr := file.Close()
				_ = rc.Close()
				if closeErr != nil {
					return closeErr
				}
				return fmt.Errorf("cas: copy: %w", err)
			}
			if err := file.Close(); err != nil {
				_ = rc.Close()
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
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.cas.get", data))
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "File to write (default: <digest hex>)")
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
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.cas.list", map[string]any{"objects": objects}))
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
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.cas.pin", map[string]string{"digest": args[0]}))
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
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.cas.unpin", map[string]string{"digest": args[0]}))
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
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("agentctl.cas.rm", map[string]string{"digest": args[0]}))
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Allow deletion of pinned objects")
	return cmd
}

func storeFromContext(ctx context.Context) (*cas.Store, error) {
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
