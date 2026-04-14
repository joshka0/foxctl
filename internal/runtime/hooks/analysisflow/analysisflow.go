package analysisflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
)

type Dependencies struct {
	RunSkill lifecycle.SkillRunner
}

func NewDependencies(deps lifecycle.Dependencies) Dependencies {
	return Dependencies{RunSkill: deps.RunSkill}
}

type Payload struct {
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
	} `json:"tool_input"`
}

type Request struct {
	Workspace string
	Payload   Payload
}

type Response struct {
	Decision string `json:"decision"`
	Context  string `json:"context,omitempty"`
	FilePath string `json:"file_path,omitempty"`
}

type complexityEnvelope struct {
	Data struct {
		Results []struct {
			Function             string `json:"function"`
			Line                 int    `json:"line"`
			CyclomaticComplexity int    `json:"cyclomatic_complexity"`
			RiskLevel            string `json:"risk_level"`
		} `json:"results"`
	} `json:"data"`
}

type impactEnvelope struct {
	Data struct {
		HookOutput hooks.Output `json:"hook_output"`
	} `json:"data"`
}

func AnalyzeEditedFile(ctx context.Context, deps Dependencies, req Request) (Response, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return Response{}, fmt.Errorf("detect workspace")
	}
	response := Response{Decision: "approve"}
	filePath := strings.TrimSpace(req.Payload.ToolInput.FilePath)
	if filePath == "" {
		return response, nil
	}
	response.FilePath = filePath
	if envEnabled("AGENTCTL_CODE_ANALYSIS_DISABLED") || !isCodeFile(filePath) || isTestFile(filePath) {
		return response, nil
	}
	if deps.RunSkill == nil {
		return response, nil
	}

	parts := []string{}

	if !envEnabled("AGENTCTL_COMPLEXITY_DISABLED") {
		var env complexityEnvelope
		if err := deps.RunSkill(ctx, "code/complexity", map[string]any{
			"path":          filePath,
			"threshold":     envInt("AGENTCTL_COMPLEXITY_THRESHOLD", 15),
			"analysis_mode": "hotspots",
			"max_results":   5,
		}, target, &env); err == nil {
			highRisk := make([]string, 0, 2)
			riskCount := 0
			for _, item := range env.Data.Results {
				if item.RiskLevel != "high" && item.RiskLevel != "medium" {
					continue
				}
				riskCount++
				if len(highRisk) < 2 {
					highRisk = append(highRisk, fmt.Sprintf("- `%s` (line %d): cyclomatic=%d", item.Function, item.Line, item.CyclomaticComplexity))
				}
			}
			if riskCount > 0 {
				msg := fmt.Sprintf("**Complexity:** %d function(s) with elevated complexity", riskCount)
				if len(highRisk) > 0 {
					msg += "\n" + strings.Join(highRisk, "\n")
				}
				parts = append(parts, msg)
			}
		}
	}

	if !envEnabled("AGENTCTL_IMPACT_DISABLED") {
		var env impactEnvelope
		if err := deps.RunSkill(ctx, "hooks/impact_analysis", hooks.Input{
			Event:         hooks.EventPostToolUse,
			WorkspaceRoot: target,
			ToolName:      "Edit",
			ToolInput:     []byte(fmt.Sprintf(`{"file_path":%q}`, filePath)),
		}, target, &env); err == nil {
			context := strings.TrimSpace(env.Data.HookOutput.Context)
			if context != "" {
				parts = append(parts, context)
			}
		}
	}

	if len(parts) > 0 {
		response.Context = strings.Join(parts, "\n\n")
	}
	return response, nil
}

func isCodeFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".c", ".cpp", ".rs":
		return true
	default:
		return false
	}
}

func isTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.js") ||
		strings.HasSuffix(lower, ".spec.ts") ||
		strings.HasSuffix(lower, ".spec.js") ||
		strings.Contains(lower, "__test__")
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
