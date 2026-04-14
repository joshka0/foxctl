package verification

import (
	"encoding/json"
	"fmt"
	"strings"
)

func parseClaimsJSON(jsonStr string) ([]Claim, error) {
	jsonStr = strings.TrimSpace(jsonStr)

	if idx := strings.Index(jsonStr, "["); idx >= 0 {
		if endIdx := strings.LastIndex(jsonStr, "]"); endIdx > idx {
			jsonStr = jsonStr[idx : endIdx+1]
		}
	}

	var rawClaims []struct {
		ID       string `json:"id"`
		Text     string `json:"text"`
		Category string `json:"category"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &rawClaims); err != nil {
		return extractClaimsFallback(jsonStr), nil
	}

	claims := make([]Claim, 0, len(rawClaims))
	for i, rc := range rawClaims {
		id := rc.ID
		if id == "" {
			id = fmt.Sprintf("c%d", i+1)
		}
		claims = append(claims, Claim{
			ID:       id,
			Text:     rc.Text,
			Category: rc.Category,
		})
	}

	return claims, nil
}

func extractClaimsFallback(text string) []Claim {
	var claims []Claim
	lines := strings.Split(text, "\n")

	claimNum := 1
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "[" || line == "]" {
			continue
		}

		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")

		if len(line) > 10 {
			claims = append(claims, Claim{
				ID:   fmt.Sprintf("c%d", claimNum),
				Text: line,
			})
			claimNum++
		}
	}

	return claims
}
