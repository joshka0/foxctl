package codexjsonl

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LocateSessionJSONL tries to find a Codex JSONL file for a session ID.
// It searches under ~/.codex/sessions/YYYY/MM/DD/rollout-*-<session_id>.jsonl.
// Returns empty string if no file is found.
func LocateSessionJSONL(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}

	homeDir, _ := os.UserHomeDir()
	patterns := []string{
		filepath.Join(homeDir, ".codex", "sessions", "*", "*", "*", "*-"+sessionID+".jsonl"),
		filepath.Join(homeDir, ".codex", "sessions", "*", "*", "*", "*"+sessionID+".jsonl"),
	}

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}

	return ""
}

// LocateMostRecentSessionJSONL finds the most recently modified Codex JSONL file.
// Returns empty values if none are found.
func LocateMostRecentSessionJSONL() (string, string) {
	homeDir, _ := os.UserHomeDir()
	pattern := filepath.Join(homeDir, ".codex", "sessions", "*", "*", "*", "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "", ""
	}

	var newestPath string
	var newestMod time.Time
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if newestPath == "" || info.ModTime().After(newestMod) {
			newestMod = info.ModTime()
			newestPath = path
		}
	}
	if newestPath == "" {
		return "", ""
	}
	return newestPath, sessionIDFromFilename(newestPath)
}

func sessionIDFromFilename(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".jsonl")
	if len(base) >= 36 {
		return base[len(base)-36:]
	}
	return base
}

// SessionIDFromFilename extracts the session ID from a Codex JSONL filename.
func SessionIDFromFilename(path string) string {
	return sessionIDFromFilename(path)
}

// SessionFile describes a Codex session JSONL file on disk.
type SessionFile struct {
	Path    string
	ID      string
	ModTime time.Time
	Size    int64
}

// SessionMetadata captures workspace-related metadata from a Codex JSONL file.
type SessionMetadata struct {
	CWD           string
	RepositoryURL string
}

// ListSessionFiles finds Codex session JSONL files under the provided home directory.
func ListSessionFiles(home string) ([]SessionFile, error) {
	if strings.TrimSpace(home) == "" {
		return nil, nil
	}

	pattern := filepath.Join(home, "sessions", "*", "*", "*", "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	sessions := make([]SessionFile, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		sessions = append(sessions, SessionFile{
			Path:    path,
			ID:      sessionIDFromFilename(path),
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	return sessions, nil
}

// ExtractMetadata scans a Codex JSONL file for workspace metadata (cwd/repository_url).
func ExtractMetadata(path string) (SessionMetadata, error) {
	var meta SessionMetadata

	file, err := os.Open(path)
	if err != nil {
		return meta, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, DefaultBufferSize), MaxLineSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var value any
		if err := json.Unmarshal(line, &value); err != nil {
			continue
		}
		scanForMetadata(value, &meta)
		if meta.CWD != "" && meta.RepositoryURL != "" {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return meta, err
	}

	return meta, nil
}

func scanForMetadata(value any, meta *SessionMetadata) {
	if meta.CWD != "" && meta.RepositoryURL != "" {
		return
	}

	switch typed := value.(type) {
	case map[string]any:
		for key, val := range typed {
			if meta.CWD == "" || meta.RepositoryURL == "" {
				if s, ok := val.(string); ok {
					field := strings.ToLower(strings.TrimSpace(key))
					switch field {
					case "cwd":
						if meta.CWD == "" {
							meta.CWD = strings.TrimSpace(s)
						}
					case "repository_url":
						if meta.RepositoryURL == "" {
							meta.RepositoryURL = strings.TrimSpace(s)
						}
					case "text":
						if meta.CWD == "" {
							if cwd := extractCWDFromText(s); cwd != "" {
								meta.CWD = cwd
							}
						}
					}
				}
			}
			scanForMetadata(val, meta)
			if meta.CWD != "" && meta.RepositoryURL != "" {
				return
			}
		}
	case []any:
		for _, val := range typed {
			scanForMetadata(val, meta)
			if meta.CWD != "" && meta.RepositoryURL != "" {
				return
			}
		}
	}
}

func extractCWDFromText(text string) string {
	const marker = "Current working directory:"
	idx := strings.Index(text, marker)
	if idx == -1 {
		return ""
	}
	rest := text[idx+len(marker):]
	if newline := strings.Index(rest, "\n"); newline != -1 {
		rest = rest[:newline]
	}
	return strings.TrimSpace(rest)
}
