package cas

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

func decodeCASTags(raw sql.NullString) ([]string, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw.String), &tags); err != nil {
		return nil, fmt.Errorf("cas: decode tags: %w", err)
	}
	return tags, nil
}
