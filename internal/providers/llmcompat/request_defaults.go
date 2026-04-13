package llmcompat

import "strings"

const qwenNoThinkPrefix = "/no_think\nReturn the final answer directly with no reasoning trace.\n"

// IsQwenModel reports whether the model name looks like a Qwen-family model.
func IsQwenModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, "qwen")
}

// ApplySystemPromptDefaults prepends provider/model-specific system instructions.
func ApplySystemPromptDefaults(model, systemPrompt string) string {
	if !IsQwenModel(model) {
		return systemPrompt
	}
	trimmed := strings.TrimSpace(systemPrompt)
	if strings.HasPrefix(trimmed, "/no_think") {
		return systemPrompt
	}
	if trimmed == "" {
		return qwenNoThinkPrefix
	}
	return qwenNoThinkPrefix + systemPrompt
}

// ApplyOpenAICompatibleRequestDefaults mutates an OpenAI-compatible request body
// with provider/model-specific fields understood by local runtimes such as LM Studio.
func ApplyOpenAICompatibleRequestDefaults(model string, body map[string]any) {
	if !IsQwenModel(model) {
		return
	}
	kwargs := map[string]any{}
	if existing, ok := body["chat_template_kwargs"].(map[string]any); ok {
		for k, v := range existing {
			kwargs[k] = v
		}
	} else if existing, ok := body["chat_template_kwargs"].(map[string]interface{}); ok {
		for k, v := range existing {
			kwargs[k] = v
		}
	}
	kwargs["enable_thinking"] = false
	body["chat_template_kwargs"] = kwargs
}
