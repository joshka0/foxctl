package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

type skillSearchResult struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Score       int      `json:"score"`
	Reasons     []string `json:"reasons"`
	NextSteps   []string `json:"next_steps"`
}

func newSkillsSearchCommand() *cobra.Command {
	var compact bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search for skills by name, description, or tags",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.MustFromContext(cmd.Context())
			query := args[0]
			manifests, err := skill.Discover(cfg.Paths.Skills)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			var matches []skillSearchResult
			for _, m := range manifests {
				if result, ok := scoreSkillSearchResult(m, query); ok {
					matches = append(matches, result)
				}
			}
			sort.Slice(matches, func(i, j int) bool {
				if matches[i].Score == matches[j].Score {
					return matches[i].Name < matches[j].Name
				}
				return matches[i].Score > matches[j].Score
			})
			if compact {
				for _, result := range matches {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%d\t%s\n", result.Name, result.Score, result.NextSteps[0]); err != nil {
						return err
					}
				}
				return nil
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.skills.search", map[string]any{
				"query":      query,
				"skills":     matches,
				"next_steps": searchNextSteps(matches),
			}, protocol.WithSource("run"))
		},
	}
	cmd.Flags().BoolVar(&compact, "compact", false, "Print compact ranked lines instead of a JSON envelope")
	return cmd
}

func scoreSkillSearchResult(m skill.Manifest, query string) (skillSearchResult, bool) {
	tokens := queryTokens(query)
	if len(tokens) == 0 {
		return skillSearchResult{}, false
	}
	name := m.Metadata.Name
	base := strings.ToLower(filepathBase(name))
	nameLower := strings.ToLower(name)
	descLower := strings.ToLower(m.Metadata.Description)
	tagText := strings.ToLower(strings.Join(m.Metadata.Tags, " "))

	score := 0
	reasons := []string{}
	for _, token := range tokens {
		switch {
		case nameLower == token || base == token:
			score += 100
			reasons = append(reasons, "name")
		case strings.Contains(nameLower, token):
			score += 60
			reasons = append(reasons, "name_contains")
		case strings.Contains(tagText, token):
			score += 40
			reasons = append(reasons, "tag")
		case strings.Contains(descLower, token):
			score += 20
			reasons = append(reasons, "description")
		}
	}
	if score == 0 {
		return skillSearchResult{}, false
	}
	return skillSearchResult{
		Name:        name,
		Version:     m.Metadata.Version,
		Description: m.Metadata.Description,
		Score:       score,
		Reasons:     uniqueSkillSearchReasons(reasons),
		NextSteps: []string{
			fmt.Sprintf("foxctl skills get %s", name),
			fmt.Sprintf("foxctl skills path %s", name),
			fmt.Sprintf("foxctl skills run %s --params-help", name),
		},
	}, true
}

func queryTokens(query string) []string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " \t\r\n,.;:()[]{}'\"")
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func filepathBase(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func searchNextSteps(results []skillSearchResult) []string {
	if len(results) == 0 {
		return []string{"foxctl skills --compact", "foxctl skills get foxctl"}
	}
	return results[0].NextSteps
}

func uniqueSkillSearchReasons(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
