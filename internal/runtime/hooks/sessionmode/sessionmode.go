package sessionmode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultTTL = 6 * time.Hour

type Flags struct {
	Todo       bool
	AnchorGoal string
}

func EnableTodo(sessionID string, at time.Time) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	path := todoPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"updated_at": at.UnixMilli(),
	})
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}

func SetAnchor(sessionID, goal string, at time.Time) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	path := anchorPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"goal":       strings.TrimSpace(goal),
		"updated_at": at.UnixMilli(),
	})
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}

func Read(sessionID string, now time.Time) (Flags, error) {
	flags := Flags{}
	if strings.TrimSpace(sessionID) == "" {
		return flags, nil
	}
	nowMS := now.UnixMilli()
	ttlMS := DefaultTTL.Milliseconds()

	if updatedAt, ok := readModeTimestamp(todoPath(sessionID)); ok {
		if nowMS-updatedAt <= ttlMS {
			flags.Todo = true
		} else {
			_ = os.Remove(todoPath(sessionID))
		}
	}

	if updatedAt, goal, ok := readAnchor(anchorPath(sessionID)); ok {
		if nowMS-updatedAt <= ttlMS {
			flags.AnchorGoal = strings.TrimSpace(goal)
		} else {
			_ = os.Remove(anchorPath(sessionID))
		}
	}
	return flags, nil
}

func todoPath(sessionID string) string {
	return filepath.Join(modeDir(), "todo-"+shortHash("todo:"+sessionID)+".json")
}

func anchorPath(sessionID string) string {
	return filepath.Join(modeDir(), "anchor-"+shortHash("anchor:"+sessionID)+".json")
}

func modeDir() string {
	agentctlHome := strings.TrimSpace(os.Getenv("AGENTCTL_HOME"))
	if agentctlHome == "" {
		home, err := userHomeDir()
		switch {
		case err == nil && strings.TrimSpace(home) != "":
			agentctlHome = filepath.Join(home, ".agentctl")
		default:
			agentctlHome = filepath.Join(os.TempDir(), "agentctl")
		}
	}
	return filepath.Join(agentctlHome, "cache", "session-modes")
}

func readModeTimestamp(path string) (int64, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var payload struct {
		UpdatedAt int64 `json:"updated_at"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, false
	}
	return payload.UpdatedAt, payload.UpdatedAt > 0
}

func readAnchor(path string) (int64, string, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, "", false
	}
	var payload struct {
		Goal      string `json:"goal"`
		UpdatedAt int64  `json:"updated_at"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, "", false
	}
	return payload.UpdatedAt, payload.Goal, payload.UpdatedAt > 0
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

var userHomeDir = os.UserHomeDir
