package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/workspace"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var input string
	var inputFile string
	var async bool
	var dedupe bool
	var cacheModeFlag string
	var workspaceFlag string
	cmd := &cobra.Command{
		Use:   "run <skill-name>",
		Short: "Run a skill and record the result as a job",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cmd.Context())
			if err != nil {
				return err
			}
			data, err := loadSkillInput(cmd, input, inputFile)
			if err != nil {
				return err
			}
			handle, err := findSkill(cfg, args[0])
			if err != nil {
				return err
			}
			skillName := handle.Manifest.Metadata.Name
			skillVersion := handle.Manifest.Metadata.Version

			ws := workspace.Normalize(workspaceFlag)
			if ws == "" && cfg.Memory.AutoLoadWorkspace {
				ws = workspace.Detect("")
			} else if ws == "" {
				if cwd, err := os.Getwd(); err == nil {
					ws = cwd
				}
			}

			cacheMode, err := parseCacheMode(cacheModeFlag, cfg.Cache.DefaultMode)
			if err != nil {
				return err
			}
			if async && cacheMode == cache.ModeOnly {
				return fmt.Errorf("--cache=only cannot be combined with --async")
			}

			var cacheStore *cache.Store
			var cacheKey string
			if !async && cacheMode != cache.ModeOff {
				cacheStore, err = cache.Open(cmd.Context(), cfg.Paths.Cache, cache.Options{
					AutoTTL: cfg.Memory.AutoCacheTTL,
					CASPath: cfg.Paths.CAS,
				})
				if err != nil {
					return err
				}
				defer cacheStore.Close()
				cacheKey, err = cache.BuildKey(handle.Manifest, data, nil)
				if err != nil {
					return err
				}
				if entry, ok, err := cacheStore.Get(cmd.Context(), cacheKey); err != nil {
					return err
				} else if ok {
					hit, err := cache.AnnotateHit(entry.Result, entry.CacheKey, ws, skillVersion)
					if err != nil {
						return err
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "cache hit %s\n", entry.CacheKey)
					return writeEnvelope(cmd.OutOrStdout(), hit)
				} else if cacheMode == cache.ModeOnly {
					return fmt.Errorf("cache miss for key %s", cacheKey)
				}
			}

			store, cleanup, err := openJobStore(cmd.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			// Check for duplicate job if --dedupe is enabled
			if dedupe {
				// Compute hash BEFORE creating a job to avoid false duplicates
				argsHash := store.ComputeSkillArgsHash(skillName, data)
				existing, dupErr := store.FindDuplicateJob(cmd.Context(), argsHash)
				if dupErr == nil {
					// Found duplicate, use existing job
					fmt.Fprintf(cmd.ErrOrStderr(), "using existing job %s (deduplicated)\n", existing.ID)
					if async {
						fmt.Fprintf(cmd.OutOrStdout(), "job %s (existing)\n", existing.ID)
						return nil
					}
					// For sync mode, return existing result if available
					if existing.ResultPath != "" {
						result, err := store.Result(cmd.Context(), existing.ID)
						if err != nil {
							return err
						}
						result = annotateRunMeta(result, ws, skillVersion)
						return writeEnvelope(cmd.OutOrStdout(), result)
					}
					// Wait for existing job to complete
					existing, err = store.WaitForCompletion(cmd.Context(), existing.ID, 0)
					if err != nil {
						return err
					}
					result, err := store.Result(cmd.Context(), existing.ID)
					if err != nil {
						return err
					}
					result = annotateRunMeta(result, ws, skillVersion)
					return writeEnvelope(cmd.OutOrStdout(), result)
				}
				// No duplicate found, create and execute new job
			}

			if async {
				job, err := store.PrepareSkillJob(cmd.Context(), skillName, data)
				if err != nil {
					return err
				}
				worker := exec.CommandContext(cmd.Context(), os.Args[0], "jobs", "exec-skill",
					"--job-id", job.ID,
					"--manifest", handle.ManifestPath,
					"--artifact", handle.ArtifactPath,
				)
				worker.Stdout = cmd.ErrOrStderr()
				worker.Stderr = cmd.ErrOrStderr()
				if err := worker.Start(); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "job %s submitted\n", job.ID)
				return nil
			}
			job, result, runErr := store.RunSkill(cmd.Context(), handle.Manifest, handle.ArtifactPath, data)
			if runErr != nil {
				return runErr
			}
			if err := handleArtifacts(cmd.Context(), cfg, job.ID, result); err != nil {
				return err
			}
			result = annotateRunMeta(result, ws, skillVersion)
			if cacheMode == cache.ModeAuto && cacheStore != nil && cacheKey != "" {
				entry := cache.Entry{
					CacheKey:     cacheKey,
					SkillName:    skillName,
					SkillVersion: skillVersion,
					Workspace:    ws,
					Result:       result,
					Digests:      cache.CollectDigests(result),
				}
				if err := cacheStore.Put(cmd.Context(), entry); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "cache put failed: %v\n", err)
				}
			}
			if err := writeEnvelope(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			logger := cmd.ErrOrStderr()
			if _, err := logger.Write([]byte("job " + job.ID + " state " + string(job.State) + "\n")); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "Inline JSON input (default: {})")
	cmd.Flags().StringVar(&inputFile, "input-file", "", "Path to JSON input file ('-' for stdin)")
	cmd.Flags().BoolVar(&async, "async", false, "Submit job and return immediately")
	cmd.Flags().BoolVar(&dedupe, "dedupe", false, "Reuse existing job with same args_hash")
	cmd.Flags().StringVar(&cacheModeFlag, "cache", "", "Cache mode: auto|off|only (default from config)")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace override (default: auto-detect)")
	return cmd
}

func init() {
	rootCmd.AddCommand(newRunCommand())
}

func parseCacheMode(flagValue, defaultValue string) (cache.Mode, error) {
	mode := strings.TrimSpace(flagValue)
	if mode == "" {
		mode = defaultValue
	}
	switch strings.ToLower(mode) {
	case "", "auto":
		return cache.ModeAuto, nil
	case "off":
		return cache.ModeOff, nil
	case "only":
		return cache.ModeOnly, nil
	default:
		return cache.ModeOff, fmt.Errorf("invalid cache mode %q (expected auto|off|only)", mode)
	}
}

func annotateRunMeta(result []byte, workspacePath, skillVersion string) []byte {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err != nil {
		return result
	}
	env.Meta.Source = "run"
	if workspacePath != "" {
		env.Meta.Workspace = workspacePath
	}
	if skillVersion != "" {
		env.Meta.SkillVer = skillVersion
	}
	data, err := json.Marshal(env)
	if err != nil {
		return result
	}
	return data
}
