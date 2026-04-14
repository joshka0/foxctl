package semantic

import (
	"strings"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
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
	case "text-embedding-embeddinggemma-300m-qat":
		return 768
	default:
		if strings.Contains(normalized, "embeddinggemma") {
			return 768
		}
		return dbdriver.GetDefaultVectorDimensions()
	}
}
