package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

type skillsDoctorIssue struct {
	Skill    string `json:"skill"`
	Path     string `json:"path,omitempty"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

type skillsDoctorReport struct {
	OK      bool                `json:"ok"`
	Checked int                 `json:"checked"`
	Issues  []skillsDoctorIssue `json:"issues"`
}

func newSkillsDoctorCommand() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "doctor [skill-name]",
		Short: "Check skill guides for manifest drift",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.MustFromContext(cmd.Context())
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			report := runSkillsDoctor(cfg, name)
			if err := protocol.WriteOK(cmd.OutOrStdout(), "foxctl.skills.doctor", report, protocol.WithSource("run")); err != nil {
				return err
			}
			if strict && !report.OK {
				return fmt.Errorf("skills doctor found %d issue(s)", len(report.Issues))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "Return a non-zero exit code when drift issues are found")
	return cmd
}

func runSkillsDoctor(cfg config.Config, name string) skillsDoctorReport {
	handles, issues := doctorSkillHandles(cfg, name)
	report := skillsDoctorReport{Checked: len(handles), Issues: issues}
	for _, handle := range handles {
		report.Issues = append(report.Issues, inspectSkillForDrift(handle)...)
	}
	report.OK = len(report.Issues) == 0
	return report
}

func doctorSkillHandles(cfg config.Config, name string) ([]skill.Handle, []skillsDoctorIssue) {
	resolver := createSkillResolver(cfg)
	if strings.TrimSpace(name) != "" {
		handle, err := resolver.Resolve(name)
		if err != nil {
			return nil, []skillsDoctorIssue{{
				Skill:    name,
				Severity: "error",
				Code:     "skill_not_found",
				Message:  err.Error(),
				Hint:     `Run "foxctl skills" to list available skills.`,
			}}
		}
		return []skill.Handle{handle}, nil
	}
	handles, err := resolver.List()
	if err != nil {
		return nil, []skillsDoctorIssue{{
			Severity: "error",
			Code:     "list_failed",
			Message:  err.Error(),
		}}
	}
	return handles, nil
}

func inspectSkillForDrift(handle skill.Handle) []skillsDoctorIssue {
	manifest, err := loadValidatedManifest(handle.ManifestPath)
	if err != nil {
		return []skillsDoctorIssue{{
			Skill:    handle.Name,
			Path:     absolutePath(handle.ManifestPath),
			Severity: "error",
			Code:     "invalid_manifest",
			Message:  err.Error(),
			Hint:     "Fix skill.yaml before updating guides.",
		}}
	}

	dir := filepath.Dir(handle.ManifestPath)
	guide, guidePath, err := readSkillGuideFileWithPath(dir)
	if err != nil {
		return []skillsDoctorIssue{{
			Skill:    manifest.Metadata.Name,
			Path:     absolutePath(dir),
			Severity: "error",
			Code:     "guide_read_failed",
			Message:  err.Error(),
		}}
	}

	issues := []skillsDoctorIssue{}
	if strings.TrimSpace(guide) == "" {
		issues = append(issues, skillsDoctorIssue{
			Skill:    manifest.Metadata.Name,
			Path:     absolutePath(dir),
			Severity: "warning",
			Code:     "missing_guide",
			Message:  "No SKILL.md or README.md guide found beside skill.yaml.",
			Hint:     "Add SKILL.md with concise agent workflows, examples, and required parameters.",
		})
		return issues
	}

	if guideOlderThanManifest(handle.ManifestPath, guidePath) {
		issues = append(issues, skillsDoctorIssue{
			Skill:    manifest.Metadata.Name,
			Path:     absolutePath(guidePath),
			Severity: "warning",
			Code:     "guide_older_than_manifest",
			Message:  "Guide file is older than skill.yaml.",
			Hint:     "Review SKILL.md whenever signature, parameters, or behavior change.",
		})
	}
	if !guideMentionsCommand(guide, manifest.Signature.Command) {
		issues = append(issues, skillsDoctorIssue{
			Skill:    manifest.Metadata.Name,
			Path:     absolutePath(guidePath),
			Severity: "warning",
			Code:     "guide_missing_command",
			Message:  fmt.Sprintf("Guide does not mention canonical command %q.", manifest.Signature.Command),
			Hint:     fmt.Sprintf("Include a copy-pasteable example such as `foxctl skills run %s ...`.", manifest.Signature.Command),
		})
	}
	for _, param := range manifest.Signature.Parameters {
		if !param.Required {
			continue
		}
		if !guideMentionsParameter(guide, param.Name) {
			issues = append(issues, skillsDoctorIssue{
				Skill:    manifest.Metadata.Name,
				Path:     absolutePath(guidePath),
				Severity: "warning",
				Code:     "guide_missing_required_parameter",
				Message:  fmt.Sprintf("Guide does not mention required parameter %q.", param.Name),
				Hint:     fmt.Sprintf("Document `%s` and include a runnable example that sets it.", param.Name),
			})
		}
	}
	return issues
}

func guideOlderThanManifest(manifestPath, guidePath string) bool {
	manifestInfo, err := os.Stat(manifestPath)
	if err != nil {
		return false
	}
	guideInfo, err := os.Stat(guidePath)
	if err != nil {
		return false
	}
	// Use a 1-second tolerance: git checkout in CI pods can set mtimes in
	// non-deterministic order within the same second, causing false positives.
	return guideInfo.ModTime().Before(manifestInfo.ModTime().Add(-time.Second))
}

func guideMentionsCommand(guide, command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return true
	}
	return strings.Contains(guide, "foxctl run "+command) ||
		strings.Contains(guide, "foxctl skills run "+command)
}

func guideMentionsParameter(guide, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	return strings.Contains(guide, "--"+name) ||
		strings.Contains(guide, "`"+name+"`")
}
