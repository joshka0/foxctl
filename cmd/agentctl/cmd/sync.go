package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/spf13/cobra"
)

type syncStoreResult struct {
	Store   string              `json:"store"`
	Class   storage.StoreClass  `json:"class,omitempty"`
	Driver  dbdriver.DriverType `json:"driver,omitempty"`
	SyncURL string              `json:"sync_url,omitempty"`

	Synced  bool   `json:"synced,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Error   string `json:"error,omitempty"`
}

// newSyncCommand constructs the `agentctl sync` command.
//
// Index:
// - Purpose: Provide an explicit, on-demand sync for configured libSQL/Turso embedded replicas
// - Flow: resolve target stores → load dbdriver config → open DB → call Syncer.Sync → emit envelope response
// - SideEffects: may perform network I/O; may create/update embedded replica files; may block up to per-store timeout
// - FailureModes: invalid store names, auth/network errors, sync timeouts, DB open failures
// - Related: dbdriver.Syncer, dbdriver.ConfigLoader.LoadConfig, storage.CanonicalStores
// - Keywords: sync, libsql, turso, replica, cross_device
func newSyncCommand() *cobra.Command {
	var storeNames []string
	var perStoreTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync configured libSQL/Turso replicas with the remote",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, ok := config.FromContext(ctx)
			if !ok {
				return fmt.Errorf("configuration not loaded")
			}

			storageRoot := cfg.Storage.Root
			if storageRoot == "" && cfg.Home != "" {
				storageRoot = filepath.Join(cfg.Home, "storage")
			}
			if storageRoot == "" {
				return fmt.Errorf("storage root not configured")
			}

			var specs []storage.StoreSpec
			if len(storeNames) > 0 {
				seen := make(map[string]struct{}, len(storeNames))
				for _, raw := range storeNames {
					raw = strings.TrimSpace(raw)
					if raw == "" {
						continue
					}
					key := strings.ToUpper(raw)
					if _, ok := seen[key]; ok {
						continue
					}
					seen[key] = struct{}{}

					spec, ok := storage.FindStore(key)
					if !ok {
						return fmt.Errorf("unknown store: %s", raw)
					}
					specs = append(specs, spec)
				}
			} else {
				for _, spec := range storage.CanonicalStores() {
					switch spec.Class {
					case storage.StoreClassSyncCritical, storage.StoreClassSyncUseful:
						// ok
					default:
						continue
					}

					// Dynamic/external stores are not addressable via the per-store DB_PATH model.
					if strings.Contains(spec.DefaultFile, "<") {
						continue
					}
					if spec.Class == storage.StoreClassExternal || spec.Class == storage.StoreClassObservability {
						continue
					}
					specs = append(specs, spec)
				}
			}

			start := time.Now()
			loader := dbdriver.NewConfigLoader(storageRoot)

			var results []syncStoreResult
			var failures int

			for _, spec := range specs {
				name := string(spec.Name)
				dbCfg := loader.LoadConfig(name, spec.DefaultFile)

				r := syncStoreResult{
					Store:  name,
					Class:  spec.Class,
					Driver: dbCfg.Driver,
				}

				switch dbCfg.Driver {
				case dbdriver.DriverSQLite:
					r.Skipped = true
					r.Reason = "sqlite (no remote sync configured)"
					results = append(results, r)
					continue
				case dbdriver.DriverLibSQL:
					r.SyncURL = strings.TrimSpace(dbCfg.LibSQL.SyncURL)
					if r.SyncURL == "" {
						r.Skipped = true
						r.Reason = "libsql local-only (no sync_url configured)"
						results = append(results, r)
						continue
					}
				case dbdriver.DriverTurso:
					r.SyncURL = strings.TrimSpace(dbCfg.Turso.URL)
				default:
					r.Skipped = true
					r.Reason = "unknown driver"
					results = append(results, r)
					continue
				}

				func() {
					syncCtx := ctx
					if perStoreTimeout > 0 {
						var cancel func()
						syncCtx, cancel = context.WithTimeout(ctx, perStoreTimeout)
						defer cancel()
					}

					db, err := dbdriver.OpenDB(syncCtx, dbCfg, nil)
					if err != nil {
						r.Error = err.Error()
						failures++
						results = append(results, r)
						return
					}
					defer db.Close()

					syncer, ok := any(db).(dbdriver.Syncer)
					if !ok || !syncer.IsSyncEnabled() {
						r.Skipped = true
						r.Reason = "driver does not support sync or sync is disabled"
						results = append(results, r)
						return
					}

					if err := syncer.Sync(); err != nil {
						r.Error = err.Error()
						failures++
					} else {
						r.Synced = true
					}

					results = append(results, r)
				}()
			}

			data := map[string]any{
				"results": results,
				"stats": map[string]any{
					"stores":      len(results),
					"failures":    failures,
					"duration_ms": time.Since(start).Milliseconds(),
				},
				"hint": "Check store configs and network connectivity, then retry with: agentctl sync",
			}

			if failures > 0 {
				_ = protocol.WriteError(cmd.OutOrStdout(), "agentctl.sync", protocol.ErrorCodeERuntime, "sync failed for one or more stores", data, protocol.WithSource("cli"))
				cmd.SilenceErrors = true
				return fmt.Errorf("sync failed for %d store(s)", failures)
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.sync", data, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().StringSliceVar(&storeNames, "store", nil, "Store(s) to sync (default: all sync-critical + sync-useful stores)")
	cmd.Flags().DurationVar(&perStoreTimeout, "timeout", 30*time.Second, "Per-store sync timeout")
	return cmd
}

func init() {
	rootCmd.AddCommand(newSyncCommand())
}
