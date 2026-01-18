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
	default:
		return dbdriver.GetDefaultVectorDimensions()
	}
}
