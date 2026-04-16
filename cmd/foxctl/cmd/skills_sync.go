package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

type skillsSyncOptions struct {
	sourceDir string
	targets   []string
	mode      string
	dryRun    bool
}

type skillsSyncTarget struct {
	name string
	dir  string
}

type skillsSyncChange struct {
	Target  string `json:"target"`
	Skill   string `json:"skill"`
	Source  string `json:"source"`
	Path    string `json:"path"`
	Action  string `json:"action"`
	Applied bool   `json:"applied"`
}

func newSkillsSyncCommand() *cobra.Command {
	opts := skillsSyncOptions{
		mode:    "copy",
		targets: []string{"codex", "gemini", "claude", "factory", "agents", "opencode"},
	}

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync foxctl skill-pack skills into agent homes",
		Long: `Sync foxctl-owned SKILL.md directories from configs/skills-pack into local
agent homes. This command only installs foxctl skill-pack content; it does not
remove legacy agentctl skills or migrate agentctl databases.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.MustFromContext(cmd.Context())
			if opts.sourceDir == "" {
				sourceDir, err := defaultSkillsPackDir()
				if err != nil {
					return err
				}
				opts.sourceDir = sourceDir
			}
			changes, err := runSkillsSync(cfg, opts)
			if err != nil {
				return err
			}
			data := map[string]any{
				"source":  opts.sourceDir,
				"mode":    opts.mode,
				"dry_run": opts.dryRun,
				"changes": changes,
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.skills.sync", data, protocol.WithSource("run"))
		},
	}

	cmd.Flags().StringVar(&opts.sourceDir, "source", "", "Source skills-pack directory (default: nearest configs/skills-pack)")
	cmd.Flags().StringSliceVar(&opts.targets, "targets", opts.targets, "Comma-separated targets: codex,gemini,claude,factory,agents,opencode")
	cmd.Flags().StringVar(&opts.mode, "mode", opts.mode, "Install mode: copy or symlink")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Preview changes without writing files")
	return cmd
}

func runSkillsSync(cfg config.Config, opts skillsSyncOptions) ([]skillsSyncChange, error) {
	sourceDir, err := filepath.Abs(expandHome(opts.sourceDir))
	if err != nil {
		return nil, fmt.Errorf("resolve source: %w", err)
	}
	if opts.mode != "copy" && opts.mode != "symlink" {
		return nil, fmt.Errorf("invalid mode %q: use copy or symlink", opts.mode)
	}
	packs, err := listSkillPacks(sourceDir)
	if err != nil {
		return nil, err
	}
	targets, err := resolveSkillsSyncTargets(opts.targets, cfg.Home)
	if err != nil {
		return nil, err
	}

	var changes []skillsSyncChange
	for _, target := range targets {
		for _, pack := range packs {
			dst := filepath.Join(target.dir, pack.name)
			change := skillsSyncChange{
				Target: target.name,
				Skill:  pack.name,
				Source: pack.path,
				Path:   dst,
				Action: opts.mode,
			}
			if opts.dryRun {
				changes = append(changes, change)
				continue
			}
			if err := installSkillPack(pack.path, dst, opts.mode); err != nil {
				return nil, fmt.Errorf("sync %s to %s: %w", pack.name, target.name, err)
			}
			change.Applied = true
			changes = append(changes, change)
		}
	}
	return changes, nil
}

type skillPack struct {
	name string
	path string
}

func listSkillPacks(sourceDir string) ([]skillPack, error) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("read skills source %s: %w", sourceDir, err)
	}
	packs := make([]skillPack, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(sourceDir, entry.Name())
		if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
			continue
		}
		packs = append(packs, skillPack{name: entry.Name(), path: path})
	}
	sort.Slice(packs, func(i, j int) bool {
		return packs[i].name < packs[j].name
	})
	return packs, nil
}

func resolveSkillsSyncTargets(names []string, foxctlHome string) ([]skillsSyncTarget, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home: %w", err)
	}
	all := map[string]string{
		"codex":    filepath.Join(home, ".codex", "skills"),
		"gemini":   filepath.Join(home, ".gemini", "skills"),
		"claude":   filepath.Join(home, ".claude", "skills"),
		"factory":  filepath.Join(home, ".factory", "skills"),
		"agents":   filepath.Join(home, ".agents", "skills"),
		"opencode": filepath.Join(home, ".opencode", "skills"),
	}
	if strings.TrimSpace(foxctlHome) != "" {
		all["foxctl"] = filepath.Join(foxctlHome, "skills")
	}

	seen := map[string]struct{}{}
	targets := make([]skillsSyncTarget, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		dir, ok := all[name]
		if !ok {
			return nil, fmt.Errorf("unknown target %q", name)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		targets = append(targets, skillsSyncTarget{name: name, dir: dir})
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].name < targets[j].name
	})
	return targets, nil
}

func installSkillPack(src, dst, mode string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	switch mode {
	case "copy":
		return copyDir(src, dst)
	case "symlink":
		return os.Symlink(src, dst)
	default:
		return fmt.Errorf("invalid mode %q", mode)
	}
}

func defaultSkillsPackDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(cwd, "configs", "skills-pack")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		next := filepath.Dir(cwd)
		if next == cwd {
			break
		}
		cwd = next
	}
	return "", fmt.Errorf("could not find configs/skills-pack from current directory")
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().Type() != 0 {
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}
