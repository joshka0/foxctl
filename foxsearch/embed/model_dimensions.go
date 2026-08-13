package embed

import (
	"strings"
	"sync/atomic"
)

// BuiltInDefaultDimensions is the fallback embedding dimension when no
// model-specific dimension is known and no default has been configured.
// Common sizes: 384 (MiniLM), 768 (BERT), 1536/3072 (OpenAI), 3072 (Gemini), 4096 (Qwen).
const BuiltInDefaultDimensions = 4096

var defaultDimensionsFunc atomic.Value // holds func() int

// SetDefaultDimensionsFunc installs a resolver for the default vector
// dimensions (e.g. a host config or environment lookup). Passing nil restores
// the built-in default.
func SetDefaultDimensionsFunc(f func() int) {
	if f == nil {
		defaultDimensionsFunc.Store(func() int { return BuiltInDefaultDimensions })
		return
	}
	defaultDimensionsFunc.Store(f)
}

// SetDefaultDimensions sets a fixed default vector dimension.
func SetDefaultDimensions(n int) {
	if n <= 0 {
		n = BuiltInDefaultDimensions
	}
	SetDefaultDimensionsFunc(func() int { return n })
}

// DefaultDimensions returns the configured default vector dimensions.
func DefaultDimensions() int {
	if f, ok := defaultDimensionsFunc.Load().(func() int); ok && f != nil {
		if dims := f(); dims > 0 {
			return dims
		}
	}
	return BuiltInDefaultDimensions
}

// DimensionsForModel returns the expected embedding dimensions for a model.
func DimensionsForModel(model string) int {
	if dims, ok := knownDimensionsForModel(model); ok {
		return dims
	}
	return DefaultDimensions()
}

func knownDimensionsForModel(model string) (int, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch normalized {
	case "gemini-embedding-001":
		return 3072, true
	case "text-embedding-004":
		return 768, true
	case "text-embedding-3-small", "text-embedding-ada-002":
		return 1536, true
	case "text-embedding-3-large":
		return 3072, true
	case "qwen/qwen3-embedding-0.6b", "qwen3-embedding-0.6b", "text-embedding-qwen3-embedding-0.6b":
		return 1024, true
	case "qwen/qwen3-embedding-4b", "qwen3-embedding-4b", "text-embedding-qwen3-embedding-4b":
		return 2560, true
	case "text-embedding-qwen3-embedding-8b":
		return 4096, true
	case "text-embedding-embeddinggemma-300m-qat":
		return 768, true
	default:
		if strings.Contains(normalized, "qwen3-embedding-0.6b") {
			return 1024, true
		}
		if strings.Contains(normalized, "qwen3-embedding-4b") {
			return 2560, true
		}
		if strings.Contains(normalized, "qwen3-embedding") {
			return 4096, true
		}
		if strings.Contains(normalized, "embeddinggemma") {
			return 768, true
		}
		return 0, false
	}
}

// ResolveDimensionsForModel chooses expected dimensions for a model and optional config.
// Known non-default model dimensions take precedence over the repository default so
// OpenAI-compatible local models such as Qwen are not rejected by a stale 1024-dim config.
func ResolveDimensionsForModel(model string, configured int) int {
	if modelDims, ok := knownDimensionsForModel(model); ok && strings.TrimSpace(model) != "" {
		return modelDims
	}
	if configured > 0 {
		return configured
	}
	return DefaultDimensions()
}
