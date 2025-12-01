package postreview

import (
	"encoding/json"

	"github.com/jkatigb/agentctl/internal/indexing"
)

func marshalFiles(files []indexing.FileChange) (string, error) {
	if files == nil {
		return "[]", nil
	}
	b, err := json.Marshal(files)
	return string(b), err
}

func unmarshalFiles(data string) ([]indexing.FileChange, error) {
	if data == "" || data == "[]" {
		return []indexing.FileChange{}, nil
	}
	var files []indexing.FileChange
	err := json.Unmarshal([]byte(data), &files)
	if err != nil {
		return nil, err
	}
	if files == nil {
		return []indexing.FileChange{}, nil
	}
	return files, nil
}

func marshalMetadata(meta map[string]any) (string, error) {
	if meta == nil {
		return "{}", nil
	}
	b, err := json.Marshal(meta)
	return string(b), err
}

func unmarshalMetadata(data string) (map[string]any, error) {
	if data == "" || data == "{}" {
		return map[string]any{}, nil
	}
	var meta map[string]any
	err := json.Unmarshal([]byte(data), &meta)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return map[string]any{}, nil
	}
	return meta, nil
}
