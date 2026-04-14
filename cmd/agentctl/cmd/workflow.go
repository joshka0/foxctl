package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/runtime/orchestration/workflow"
	"github.com/spf13/cobra"
)

func newWorkflowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Execute and manage workflows",
		Long: `Workflows chain multiple agentctl skills into automated pipelines.

Workflows are defined in YAML files and can include:
- Multiple steps with dependencies
- Parallel execution
- Conditional logic
- Loops over arrays
- Template expressions for data flow between steps

Example workflow file:
  apiVersion: agentctl/v1
  kind: Workflow
  metadata:
    name: analyze-code
  steps:
    - id: find
      skill: fs/find
      input:
        pattern: "*.go"
    - id: analyze
      skill: code/symbols
      loop:
        over: "{{.find.data.files}}"
        as: file
      input:
        path: "{{.file}}"`,
	}

	cmd.AddCommand(
		newWorkflowRunCommand(),
		newWorkflowListCommand(),
		newWorkflowValidateCommand(),
		newWorkflowShowCommand(),
	)

	return cmd
}

func init() {
	rootCmd.AddCommand(newWorkflowCommand())
}

// workflowRunFlags holds flags for the workflow run command.
type workflowRunFlags struct {
	Input      string
	InputFile  string
	MaxWorkers int
	DryRun     bool
	Verbose    bool
}

func newWorkflowRunCommand() *cobra.Command {
	var flags workflowRunFlags

	cmd := &cobra.Command{
		Use:   "run <workflow-name>",
		Short: "Execute a workflow",
		Long: `Execute a workflow by name or path.

The workflow will be loaded from:
1. AGENTCTL_WORKFLOW_PATH environment variable
2. ~/.agentctl/workflows/
3. ./workflows/

Examples:
  # Run a workflow by name
  agentctl workflow run analyze-code --input '{"path": "./src"}'

  # Run a workflow from a file
  agentctl workflow run ./my-workflow.yaml --input '{"path": "."}'

  # Run with input from file
  agentctl workflow run analyze-code --input-file params.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflow(cmd, args[0], flags)
		},
	}

	cmd.Flags().StringVarP(&flags.Input, "input", "i", "{}", "Workflow inputs as JSON")
	cmd.Flags().StringVar(&flags.InputFile, "input-file", "", "Read inputs from JSON file")
	cmd.Flags().IntVar(&flags.MaxWorkers, "workers", 10, "Maximum parallel workers")
	cmd.Flags().BoolVar(&flags.DryRun, "dry-run", false, "Validate workflow without executing")
	cmd.Flags().BoolVarP(&flags.Verbose, "verbose", "v", false, "Show detailed execution progress")

	return cmd
}

func runWorkflow(cmd *cobra.Command, nameOrPath string, flags workflowRunFlags) error {
	// Parse inputs
	inputs, err := parseWorkflowInputs(flags.Input, flags.InputFile)
	if err != nil {
		return writeWorkflowError(cmd, "workflow/run", "input_error", err)
	}

	// Create engine
	engineOpts := []workflow.EngineOption{
		workflow.WithEngineMaxWorkers(flags.MaxWorkers),
	}

	engine := workflow.NewEngine(engineOpts...)

	// Dry run - just validate
	if flags.DryRun {
		if err := engine.Validate(nameOrPath); err != nil {
			return writeWorkflowError(cmd, "workflow/run", "validation_error", err)
		}
		env := envelope.OK("workflow/run", map[string]any{
			"status":  "valid",
			"message": "Workflow validation passed",
		})
		return envelope.Write(cmd.OutOrStdout(), env)
	}

	// Execute workflow
	result, err := engine.Run(cmd.Context(), nameOrPath, inputs)
	if err != nil {
		// Still output the partial result
		if result != nil {
			env := envelope.Error("workflow/run", "execution_error", err.Error(), result)
			return envelope.Write(cmd.OutOrStdout(), env)
		}
		return writeWorkflowError(cmd, "workflow/run", "execution_error", err)
	}

	// Output result
	env := envelope.OK("workflow/run", result)
	return envelope.Write(cmd.OutOrStdout(), env)
}

func newWorkflowListCommand() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine := workflow.NewEngine()
			handles, err := engine.List()
			if err != nil {
				return writeWorkflowError(cmd, "workflow/list", "list_error", err)
			}

			if outputJSON {
				data := make([]map[string]any, len(handles))
				for i, h := range handles {
					data[i] = map[string]any{
						"name":        h.Name,
						"path":        h.Path,
						"description": h.Workflow.Metadata.Description,
						"steps":       len(h.Workflow.Steps),
					}
				}
				env := envelope.OK("workflow/list", map[string]any{
					"workflows": data,
					"count":     len(handles),
				})
				return envelope.Write(cmd.OutOrStdout(), env)
			}

			// Table output
			if len(handles) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No workflows found.")
				fmt.Fprintln(cmd.OutOrStdout(), "\nSearch paths:")
				for _, p := range workflow.NewLoader().SearchPaths() {
					fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", p)
				}
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTEPS\tDESCRIPTION\tPATH")
			for _, h := range handles {
				desc := h.Workflow.Metadata.Description
				if len(desc) > 50 {
					desc = desc[:47] + "..."
				}
				fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", h.Name, len(h.Workflow.Steps), desc, h.Path)
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON envelope")
	return cmd
}

func newWorkflowValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <workflow-name>",
		Short: "Validate a workflow without executing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine := workflow.NewEngine()
			if err := engine.Validate(args[0]); err != nil {
				return writeWorkflowError(cmd, "workflow/validate", "validation_error", err)
			}

			env := envelope.OK("workflow/validate", map[string]any{
				"status":   "valid",
				"workflow": args[0],
				"message":  "Workflow is valid",
			})
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}
	return cmd
}

func newWorkflowShowCommand() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "show <workflow-name>",
		Short: "Show workflow details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			loader := workflow.NewLoader()
			handle, err := loader.Load(args[0])
			if err != nil {
				return writeWorkflowError(cmd, "workflow/show", "load_error", err)
			}

			wf := handle.Workflow

			if outputJSON {
				env := envelope.OK("workflow/show", wf)
				return envelope.Write(cmd.OutOrStdout(), env)
			}

			// Human-readable output
			fmt.Fprintf(cmd.OutOrStdout(), "Workflow: %s\n", wf.Metadata.Name)
			if wf.Metadata.Description != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Description: %s\n", wf.Metadata.Description)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Path: %s\n", handle.Path)
			fmt.Fprintln(cmd.OutOrStdout())

			// Inputs
			if len(wf.Inputs) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Inputs:")
				for _, in := range wf.Inputs {
					req := ""
					if in.Required {
						req = " (required)"
					}
					def := ""
					if in.Default != nil {
						def = fmt.Sprintf(" [default: %v]", in.Default)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  - %s%s%s\n", in.Name, req, def)
					if in.Description != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", in.Description)
					}
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}

			// Steps
			fmt.Fprintln(cmd.OutOrStdout(), "Steps:")
			for _, step := range wf.Steps {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s:\n", step.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "    skill: %s\n", step.Skill)
				if len(step.DependsOn) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "    depends_on: %s\n", strings.Join(step.DependsOn, ", "))
				}
				if step.If != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "    if: %s\n", step.If)
				}
				if step.Loop != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "    loop: over=%s, as=%s\n", step.Loop.Over, step.Loop.As)
				}
			}

			// Outputs
			if len(wf.Outputs) > 0 {
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), "Outputs:")
				for _, out := range wf.Outputs {
					fmt.Fprintf(cmd.OutOrStdout(), "  - %s: %s\n", out.Name, out.Value)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")
	return cmd
}

// parseWorkflowInputs parses inputs from flags.
func parseWorkflowInputs(inputJSON, inputFile string) (map[string]any, error) {
	var data []byte

	if inputFile != "" {
		var err error
		data, err = os.ReadFile(inputFile)
		if err != nil {
			return nil, fmt.Errorf("read input file: %w", err)
		}
	} else {
		data = []byte(inputJSON)
	}

	var inputs map[string]any
	if err := json.Unmarshal(data, &inputs); err != nil {
		return nil, fmt.Errorf("parse inputs: %w", err)
	}

	return inputs, nil
}

// writeWorkflowError writes an error envelope.
func writeWorkflowError(cmd *cobra.Command, command, code string, err error) error {
	env := envelope.Error(command, code, err.Error(), nil)
	if writeErr := envelope.Write(cmd.OutOrStdout(), env); writeErr != nil {
		return fmt.Errorf("write error: %w", writeErr)
	}
	return err
}
