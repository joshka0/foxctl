package llmcompat

import "testing"

func TestApplySystemPromptDefaultsForQwen(t *testing.T) {
	t.Parallel()

	got := ApplySystemPromptDefaults("qwen3.5-4b-mlx", "Return JSON only.")
	want := "/no_think\nReturn the final answer directly with no reasoning trace.\nReturn JSON only."
	if got != want {
		t.Fatalf("prompt=%q want %q", got, want)
	}
}

func TestApplySystemPromptDefaultsDoesNotDuplicatePrefix(t *testing.T) {
	t.Parallel()

	input := "/no_think\nReturn JSON only."
	got := ApplySystemPromptDefaults("qwen3.5-4b-mlx", input)
	if got != input {
		t.Fatalf("prompt=%q want %q", got, input)
	}
}

func TestApplyOpenAICompatibleRequestDefaultsForQwen(t *testing.T) {
	t.Parallel()

	body := map[string]any{"model": "qwen3.5-35b-a3b"}
	ApplyOpenAICompatibleRequestDefaults("qwen3.5-35b-a3b", body)

	kwargs, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("chat_template_kwargs missing or wrong type: %#v", body["chat_template_kwargs"])
	}
	if enabled, ok := kwargs["enable_thinking"].(bool); !ok || enabled {
		t.Fatalf("enable_thinking=%#v want false", kwargs["enable_thinking"])
	}
}
