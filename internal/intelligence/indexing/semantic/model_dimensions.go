package semantic

import (
	"strings"

	"github.com/joshka0/foxctl/internal/storage/dbdriver"
)

// DimensionsForModel returns the expected embedding dimensions for a model.
func DimensionsForModel(model string) int {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch normalized {
	case "gemini-embedding-001":
		return 3072
	case "text-embedding-004":
		return 768
	case "text-embedding-3-small", "text-embedding-ada-002":
		return 1536
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-qwen3-embedding-8b":
		return 4096
	case "text-embedding-embeddinggemma-300m-qat":
		return 768
	default:
		if strings.Contains(normalized, "qwen3-embedding") {
			return 4096
		}
		if strings.Contains(normalized, "embeddinggemma") {
			return 768
		}
		return dbdriver.GetDefaultVectorDimensions()
	}
}

// ResolveDimensionsForModel chooses expected dimensions for a model and optional config.
// Known non-default model dimensions take precedence over the repository default so
// OpenAI-compatible local models such as Qwen are not rejected by a stale 1024-dim config.
func ResolveDimensionsForModel(model string, configured int) int {
	modelDims := DimensionsForModel(model)
	if strings.TrimSpace(model) != "" && modelDims > 0 && modelDims != dbdriver.DefaultVectorDimensions {
		return modelDims
	}
	if configured > 0 {
		return configured
	}
	return modelDims
}
