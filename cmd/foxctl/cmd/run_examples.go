package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

type runExample struct {
	ID          string         `json:"id,omitempty"`
	Description string         `json:"description,omitempty"`
	Command     string         `json:"command"`
	Input       map[string]any `json:"input,omitempty"`
}

type runExamplesPayload struct {
	Skill    string       `json:"skill,omitempty"`
	Examples []runExample `json:"examples"`
	Hint     string       `json:"hint,omitempty"`
}

func writeRunExamples(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return writeRunCommandExamples(cmd)
	}
	return writeRunSkillExamples(cmd, args[0])
}

func writeRunCommandExamples(cmd *cobra.Command) error {
	examples := []runExample{
		{
			Description: "Run a skill with inline JSON input.",
			Command:     `foxctl run fs/ls --input '{"path":"./src"}'`,
			Input: map[string]any{
				"path": "./src",
			},
		},
		{
			Description: "Pipe JSON input from a file or stdin.",
			Command:     "cat input.json | foxctl run text/grep --input-file -",
		},
	}

	payload := runExamplesPayload{
		Examples: examples,
		Hint:     "Pass a skill name to see examples for that skill (e.g., foxctl run todo/manage --examples).",
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.run.examples", payload, protocol.WithSource("cli"))
}

func writeRunSkillExamples(cmd *cobra.Command, skillName string) error {
	cfg := config.MustFromContext(cmd.Context())
	manifest, err := findSkillManifest(cfg, skillName)
	if err != nil {
		return err
	}
	examples := buildSkillRunExamples(manifest.Metadata.Name, manifest.Signature.Help)
	payload := runExamplesPayload{
		Skill:    manifest.Metadata.Name,
		Examples: examples,
	}
	if len(examples) == 0 {
		payload.Hint = "No examples are defined in this skill's manifest."
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.run.examples", payload, protocol.WithSource("cli"))
}

func buildSkillRunExamples(skillName string, help *skill.Help) []runExample {
	if help == nil {
		return []runExample{}
	}
	workflows := help.Workflows
	if len(workflows) == 0 {
		return []runExample{}
	}
	examples := make([]runExample, 0, len(workflows))
	for _, wf := range workflows {
		if len(wf.ExampleInput) == 0 {
			continue
		}
		cmd := formatRunExampleCommand(skillName, wf.ExampleInput)
		examples = append(examples, runExample{
			ID:          wf.ID,
			Description: wf.Description,
			Command:     cmd,
			Input:       wf.ExampleInput,
		})
	}
	if len(examples) == 0 {
		return []runExample{}
	}
	return examples
}

func formatRunExampleCommand(skillName string, input map[string]any) string {
	raw, err := json.Marshal(input)
	if err != nil {
		raw = []byte("{}")
	}
	return fmt.Sprintf("foxctl run %s --input '%s'", skillName, string(raw))
}
