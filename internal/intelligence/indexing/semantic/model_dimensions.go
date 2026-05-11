package semantic

import (
	"strings"

	"github.com/joshka0/foxctl/internal/storage/dbdriver"
)

// DimensionsForModel returns the expected embedding dimensions for a model.
func DimensionsForModel(model string) int {
	if dims, ok := knownDimensionsForModel(model); ok {
		return dims
	}
	return dbdriver.GetDefaultVectorDimensions()
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
	return dbdriver.GetDefaultVectorDimensions()
}
