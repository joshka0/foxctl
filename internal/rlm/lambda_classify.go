package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

const classifyPromptTemplate = `Based on the query below, select the most appropriate retrieval task type.

Query: %s

Reply with ONLY a single digit:
1. code_locate - find specific files, functions, or symbols in the repository
2. code_understand - explain how code works, trace execution, or understand architecture
3. memory_recall - recover past decisions, sessions, context, or timeline
4. evidence_audit - cross-check claims across multiple sources for consistency
5. general - mixed or other retrieval

Single digit:`

var taskDigitMap = map[string]TaskType{
	"1": TaskTypeCodeLocate,
	"2": TaskTypeCodeUnderstand,
	"3": TaskTypeMemoryRecall,
	"4": TaskTypeEvidenceAudit,
	"5": TaskTypeGeneral,
}

func classifyTaskWithUsage(ctx context.Context, cfg LLMConfig, prompt string) (TaskType, engine.TokenUsage, error) {
	llmCfg := lambdaLLMChatConfig(cfg)
	llmCfg.MaxIterations = 1
	llmCfg.MaxTokens = 16
	classifyPrompt := fmt.Sprintf(classifyPromptTemplate, truncateRLMText(prompt, 500))
	estimatedUsage := estimateLambdaTokenUsage("You are a task classifier. Reply with exactly one digit and nothing else.\n"+classifyPrompt, "")

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return TaskTypeGeneral, estimatedUsage, fmt.Errorf("lambda classify: init LLM: %w", err)
	}

	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: "You are a task classifier. Reply with exactly one digit and nothing else.",
		Messages:     []engine.Message{engine.NewUserMessage(classifyPrompt)},
	})
	if err != nil {
		return TaskTypeGeneral, fillMissingLambdaUsage(output.Tokens, estimatedUsage, output.AssistantText), fmt.Errorf("lambda classify: LLM call: %w", err)
	}

	resp := strings.TrimSpace(output.AssistantText)
	return parseTaskType(resp), fillMissingLambdaUsage(output.Tokens, estimatedUsage, resp), nil
}

func lambdaLLMChatConfig(cfg LLMConfig) engine.LLMChatConfig {
	llmCfg := engine.DefaultLLMChatConfig()
	if strings.TrimSpace(cfg.Provider) != "" {
		llmCfg.Provider = strings.TrimSpace(cfg.Provider)
	}
	if strings.TrimSpace(cfg.APIKey) != "" {
		llmCfg.APIKey = strings.TrimSpace(cfg.APIKey)
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		llmCfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	}
	if strings.TrimSpace(cfg.AuthMode) != "" {
		llmCfg.AuthMode = strings.TrimSpace(cfg.AuthMode)
	}
	if strings.TrimSpace(cfg.AuthHeader) != "" {
		llmCfg.AuthHeader = strings.TrimSpace(cfg.AuthHeader)
	}
	if cfg.AuthPrefix != "" {
		llmCfg.AuthPrefix = cfg.AuthPrefix
	}
	if strings.TrimSpace(cfg.Model) != "" {
		llmCfg.Model = strings.TrimSpace(cfg.Model)
	}
	if cfg.Timeout > 0 {
		llmCfg.Timeout = cfg.Timeout
	}
	if cfg.MaxTokens > 0 {
		llmCfg.MaxTokens = cfg.MaxTokens
	}
	if cfg.Temperature != 0 {
		llmCfg.Temperature = cfg.Temperature
	}
	if cfg.MaxIterations > 0 {
		llmCfg.MaxIterations = cfg.MaxIterations
	}
	return llmCfg
}

// parseTaskType extracts the first digit from the response and maps it.
func parseTaskType(response string) TaskType {
	for _, ch := range response {
		if ch >= '1' && ch <= '5' {
			if tt, ok := taskDigitMap[string(ch)]; ok {
				return tt
			}
		}
	}
	return TaskTypeGeneral
}

// estimateProblemSize estimates the candidate search space size for planning.
//
// For code retrieval tasks, this approximates the number of potentially relevant
// files/symbols. For memory tasks, it estimates the number of memory entries.
// Returns a rough integer used by PlanLambda to compute k* and depth.
func estimateProblemSize(task Task, env Environment) int {
	// Use repo handle count and tool surface as a proxy for problem space.
	n := len(env.RepoHandles) + len(env.VaultHandles) + len(env.SceneHandles) + len(env.ArtifactHandles)
	if n == 0 {
		// Default heuristic: assume a moderate-sized search problem.
		n = 50
	}
	// Scale by prompt length as a rough complexity proxy.
	words := len(strings.Fields(task.Prompt))
	if words > 20 {
		n = n * 2
	}
	if n < 5 {
		n = 5
	}
	return n
}

// queryVariants produces k varied search formulations from the original query.
// Each variant targets a different search strategy.
func queryVariants(query string, k int) []string {
	if k <= 1 {
		return []string{query}
	}
	variants := make([]string, 0, k)
	variants = append(variants, query) // original

	// Variant 2: content-bearing terms. This is more useful than the prompt tail
	// for instruction-shaped queries whose final words are often output format.
	keywords := filterStopWords(query)
	if keywords != "" && keywords != query && !containsString(variants, keywords) {
		variants = append(variants, keywords)
	}

	// Variant 3: extract CamelCase / underscore tokens for symbol search.
	symbols := extractSymbolTokens(query)
	if symbols != "" && symbols != query && !containsString(variants, symbols) {
		variants = append(variants, symbols)
	}

	words := strings.Fields(query)
	if len(words) > 2 {
		// Variant 4: last N words as a fallback for short question-shaped prompts.
		tail := strings.Join(words[maxInt(0, len(words)-4):], " ")
		if tail != query && !containsString(variants, tail) {
			variants = append(variants, tail)
		}
	}

	// Pad or trim to exactly k.
	if len(variants) > k {
		variants = variants[:k]
	}
	for len(variants) < k {
		variants = append(variants, query)
	}
	return variants
}

func extractSymbolTokens(query string) string {
	var parts []string
	var current strings.Builder
	prevLower := false
	for _, ch := range query {
		if ch >= 'A' && ch <= 'Z' && prevLower {
			if current.Len() > 1 {
				parts = append(parts, current.String())
			}
			current.Reset()
		}
		if isIdentChar(ch) {
			current.WriteRune(ch)
			prevLower = ch >= 'a' && ch <= 'z'
		} else {
			if current.Len() > 1 {
				parts = append(parts, current.String())
			}
			current.Reset()
			prevLower = false
		}
	}
	if current.Len() > 1 {
		parts = append(parts, current.String())
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func isIdentChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

var stopWords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "have": {}, "has": {}, "had": {},
	"do": {}, "does": {}, "did": {}, "will": {}, "would": {}, "could": {},
	"should": {}, "may": {}, "might": {}, "can": {}, "shall": {},
	"what": {}, "where": {}, "when": {}, "how": {}, "which": {}, "who": {},
	"that": {}, "this": {}, "these": {}, "those": {}, "it": {},
	"in": {}, "on": {}, "at": {}, "to": {}, "for": {}, "of": {}, "with": {},
	"by": {}, "from": {}, "up": {}, "about": {}, "into": {}, "through": {},
	"and": {}, "or": {}, "but": {}, "not": {}, "no": {},
	// Instruction-tail words: common output format directives that are not
	// content-bearing for search.
	"return": {}, "give": {}, "provide": {}, "list": {}, "describe": {},
	"explain": {}, "show": {}, "tell": {}, "summarize": {},
	"key": {}, "files": {}, "concise": {}, "grounded": {}, "summary": {},
	"brief": {}, "detailed": {}, "short": {}, "long": {}, "report": {},
}

func filterStopWords(query string) string {
	words := strings.Fields(query)
	var filtered []string
	for _, w := range words {
		if _, ok := stopWords[strings.ToLower(w)]; !ok && len(w) > 1 {
			filtered = append(filtered, w)
		}
	}
	return strings.Join(filtered, " ")
}

// jsonArgs builds a json.RawMessage from key-value pairs.
func jsonArgs(kv map[string]any) json.RawMessage {
	b, _ := json.Marshal(kv)
	return b
}
