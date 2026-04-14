package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/deviceid"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	"github.com/spf13/cobra"
)

func newDoctorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate foxctl configuration and environment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, ok := config.FromContext(cmd.Context())
			if !ok {
				return fmt.Errorf("configuration not loaded")
			}

			// Workspace identity is derived from the current working directory.
			wsInfo := workspace.DetectWithIdentity("")
			wsID := workspace.ID(wsInfo.Path)

			// Device identity is stored at "<home>/device.json".
			device := ""
			if cfg.Home != "" {
				if id, err := deviceid.LoadOrCreate(cfg.Home); err == nil {
					device = id
				}
			}

			// Summarize per-store DB configuration (best-effort; does not open DBs).
			storageRoot := cfg.Storage.Root
			if storageRoot == "" && cfg.Home != "" {
				// Keep doctor useful even if config is partially materialized in tests.
				storageRoot = filepath.Join(cfg.Home, "storage")
			}
			storeDB := make(map[string]any)
			if storageRoot != "" {
				loader := dbdriver.NewConfigLoader(storageRoot)
				for _, spec := range storage.CanonicalStores() {
					name := string(spec.Name)

					// Some stores are dynamic or external; summarize without attempting driver load.
					if spec.Class == storage.StoreClassExternal || spec.Class == storage.StoreClassObservability {
						storeDB[name] = map[string]any{
							"class":        spec.Class,
							"default_file": spec.DefaultFile,
							"notes":        spec.Notes,
						}
						continue
					}
					if strings.Contains(spec.DefaultFile, "<") {
						storeDB[name] = map[string]any{
							"class":        spec.Class,
							"default_file": spec.DefaultFile,
							"driver":       "sqlite",
							"summary":      "dynamic db name (directory-based)",
							"notes":        spec.Notes,
						}
						continue
					}

					dbCfg := loader.LoadConfig(name, spec.DefaultFile)
					storeDB[name] = map[string]any{
						"class":        spec.Class,
						"default_file": spec.DefaultFile,
						"driver":       dbCfg.Driver,
						"summary":      dbdriver.GetConfigSummary(dbCfg),
						"notes":        spec.Notes,
					}
				}
			}

			data := map[string]any{
				"config": cfg,
				"device": map[string]any{
					"id": device,
				},
				"workspace": map[string]any{
					"path":          wsInfo.Path,
					"family_path":   wsInfo.FamilyPath,
					"id":            wsID,
					"repo_identity": wsInfo.RepoIdentity,
				},
				"stores": map[string]any{
					"db": storeDB,
				},
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.doctor", data, protocol.WithSource("run"))
		},
	}

	return cmd
}

func init() {
	rootCmd.AddCommand(newDoctorCommand())
}
