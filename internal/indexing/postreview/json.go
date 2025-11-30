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
		return nil, nil
	}
	var files []indexing.FileChange
	err := json.Unmarshal([]byte(data), &files)
	return files, err
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
		return nil, nil
	}
	var meta map[string]any
	err := json.Unmarshal([]byte(data), &meta)
	return meta, err
}
