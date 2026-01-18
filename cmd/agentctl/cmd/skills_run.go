package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/cmd/agentctl/cmd/memorycmd"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func newSkillsRunCommand() *cobra.Command {
	var input string
	var inputFile string
	var workspaceFlag string
	var showParamHelp bool
	var debugSkill bool

	cmd := &cobra.Command{
		Use:   "run <skill-name> [--param value ...]",
		Short: "Run a skill with JSON input or parameter flags",
		Long: `Run a skill binary with input provided as JSON or individual parameter flags.

Arguments can be passed in three ways (in order of precedence):
  1. Individual parameter flags (e.g., --path ., --analysis-mode hotspots)
  2. Inline JSON via --input flag
  3. JSON file via --input-file flag

Parameter flags override --input values, which override --input-file values.

Examples:
  # Using parameter flags (recommended for simple cases)
  agentctl skills run code/complexity --path . --analysis-mode hotspots

  # Using JSON input
  agentctl skills run code/complexity --input '{"path":".", "analysis_mode":"hotspots"}'

  # Mixed: JSON base with flag overrides
  agentctl skills run code/complexity --input '{"path":"."}' --analysis-mode hotspots

  # Show available parameters for a skill
  agentctl skills run code/complexity --params-help
`,
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: false,
		// Allow unknown flags - we'll parse them as skill parameters
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.MustFromContext(cmd.Context())

			// Find the skill first
			handle, err := findSkill(cfg, args[0])
			if err != nil {
				return err
			}
			if debugSkill {
				fmt.Fprintf(cmd.ErrOrStderr(), "skill=%s manifest=%s artifact=%s cgo=%v\n", handle.Manifest.Metadata.Name, handle.ManifestPath, handle.ArtifactPath, buildinfo.IsCGO())
			}

			// If --params-help was requested, show parameter help and exit
			if showParamHelp {
				return showParameterHelp(cmd, handle.Manifest)
			}

			// Get remaining args after the skill name
			remainingArgs := getRemainingArgs(os.Args, args[0])

			// Create skill-specific flag set from manifest parameters
			skillFlags := skill.NewFlagSet(handle.Manifest.Metadata.Name, handle.Manifest.Signature.Parameters)

			// Parse remaining args as skill parameter flags
			if err := skillFlags.Parse(remainingArgs); err != nil {
				return fmt.Errorf("parse parameter flags: %w", err)
			}

			// Load base input from --input or --input-file
			baseInput, err := loadSkillInput(cmd, cfg, input, inputFile)
			if err != nil {
				return err
			}

			// Merge: defaults < --input JSON < explicit flags
			mergedInput, err := skillFlags.MergeWithInput(baseInput)
			if err != nil {
				return err
			}

			// Validate merged input
			var merged map[string]any
			if err := json.Unmarshal(mergedInput, &merged); err != nil {
				return fmt.Errorf("parse merged input: %w", err)
			}
			if err := skillFlags.Validate(merged); err != nil {
				return err
			}

			runCtx := resolveWorkspaceContext(cmd.Context(), workspaceFlag)
			stdout, stderr, err := executeSkill(runCtx, handle.Manifest, handle.ArtifactPath, mergedInput)
			if len(stderr) > 0 {
				if _, werr := cmd.ErrOrStderr().Write(append(stderr, '\n')); werr != nil {
					return werr
				}
			}
			if err != nil {
				return err
			}
			return memorycmd.WriteEnvelope(cmd.OutOrStdout(), stdout)
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "Inline JSON input (default: {}). Special values: 'stdin' to extract data from envelope, 'sha256:<hex>' to read from CAS")
	cmd.Flags().StringVar(&inputFile, "input-file", "", "Path to JSON input file ('-' for stdin)")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace override (default: auto-detect)")
	cmd.Flags().BoolVar(&showParamHelp, "params-help", false, "Show available parameter flags for the skill")
	cmd.Flags().BoolVar(&debugSkill, "debug-skill", false, "Print resolved skill paths to stderr")

	return cmd
}

// getRemainingArgs extracts arguments after the skill name that should be
// parsed as skill parameter flags. It filters out known command flags.
func getRemainingArgs(osArgs []string, skillName string) []string {
	// Known command flags to skip
	knownFlags := map[string]bool{
		"--input":       true,
		"--input-file":  true,
		"--workspace":   true,
		"--params-help": true,
		"--debug-skill": true,
		"--help":        true,
		"-h":            true,
	}

	// Find the skill name in args
	skillIdx := -1
	for i, arg := range osArgs {
		if arg == skillName || strings.HasSuffix(arg, "/"+skillName) {
			skillIdx = i
			break
		}
	}
	if skillIdx == -1 {
		return nil
	}

	// Collect args after skill name, skipping known flags and their values
	var remaining []string
	skipNext := false
	for i := skillIdx + 1; i < len(osArgs); i++ {
		arg := osArgs[i]

		if skipNext {
			skipNext = false
			continue
		}

		// Check if this is a known flag
		flagName := arg
		if strings.Contains(arg, "=") {
			flagName = strings.SplitN(arg, "=", 2)[0]
		}

		if knownFlags[flagName] {
			// If flag doesn't have =, skip the next arg too (its value)
			if !strings.Contains(arg, "=") && flagName != "--params-help" && flagName != "--help" && flagName != "-h" {
				skipNext = true
			}
			continue
		}

		remaining = append(remaining, arg)
	}

	return remaining
}

// showParameterHelp displays the available parameter flags for a skill.
func showParameterHelp(cmd *cobra.Command, manifest skill.Manifest) error {
	w := cmd.OutOrStdout()

	fmt.Fprintf(w, "Skill: %s (v%s)\n", manifest.Metadata.Name, manifest.Metadata.Version)
	fmt.Fprintf(w, "%s\n\n", manifest.Metadata.Description)

	if len(manifest.Signature.Parameters) == 0 {
		fmt.Fprintln(w, "This skill has no parameters.")
		return nil
	}

	fmt.Fprintln(w, "Available parameter flags:")
	fmt.Fprint(w, skill.GenerateParameterHelp(manifest.Signature.Parameters))

	// Show example if available
	if manifest.Signature.Help != nil && len(manifest.Signature.Help.Workflows) > 0 {
		wf := manifest.Signature.Help.Workflows[0]
		if wf.ExampleInput != nil {
			fmt.Fprintln(w, "Example:")
			exampleJSON, _ := json.MarshalIndent(wf.ExampleInput, "  ", "  ")
			fmt.Fprintf(w, "  agentctl skills run %s --input '%s'\n", manifest.Signature.Command, exampleJSON)
		}
	}

	return nil
}
