package lite

import "context"

// Well-known circuit breaker names for external service calls.
// These are string constants only; the lite package does not implement
// actual circuit breaker logic. Skills that migrate from skillmain to
// lite can keep using the same breaker names, and GuardCall is a
// pass-through that can later be wired to a real circuit breaker.
const (
	// BreakerEmbeddingAPI guards calls to embedding providers.
	BreakerEmbeddingAPI = "embedding_api"

	// BreakerLLMProvider guards calls to LLM chat/completion providers.
	BreakerLLMProvider = "llm_provider"

	// BreakerHTTP guards outbound HTTP requests to external services.
	BreakerHTTP = "http_external"

	// BreakerGitCLI guards shell-out calls to git and other CLI tools.
	BreakerGitCLI = "git_cli"
)

// GuardCall wraps an external service call with a no-op guard in the lite
// package. In the full skillmain package this routes through the circuit
// breaker manager; in lite it simply invokes fn directly.
//
// Usage:
//
//	var result string
//	err := lite.GuardCall(ctx, lite.BreakerLLMProvider, func(ctx context.Context) error {
//	    var e error
//	    result, e = callLLM(ctx, provider, prompt)
//	    return e
//	})
func GuardCall(ctx context.Context, breakerName string, fn func(context.Context) error) error {
	_ = breakerName
	return fn(ctx)
}
