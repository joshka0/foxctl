package deviceid

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	// FileName is the default filename for the persisted device identity.
	FileName = "device.json"

	// fileMode restricts the device identity file to the current user.
	fileMode = 0o600
)

type record struct {
	Version   int    `json:"version"`
	DeviceID  string `json:"device_id"`
	CreatedAt string `json:"created_at"`
}

// Path returns the device identity file path under rootDir.
func Path(rootDir string) string {
	return filepath.Join(rootDir, FileName)
}

// LoadOrCreate returns a stable per-machine device ID, creating it if missing.
//
// The ID is stored at "<rootDir>/device.json" and is intended to be stable across
// foxctl restarts on the same machine. It must not contain secrets.
//
// Index:
//   Purpose: Provide a stable machine identifier for cross-device coordination (leader leases, debugging, provenance)
//   Flow: read existing → if missing, atomically create → read back → return device_id
//   Related: Path
//   Keywords: device_id, coordination, leader_lease
//
// [[invariant:stable-device-id]]
// [[domain:machine-identity]]
func LoadOrCreate(rootDir string) (string, error) {
	if rootDir == "" {
		return "", fmt.Errorf("deviceid: root dir is required")
	}
	path := Path(rootDir)

	// Fast path: read existing.
	if id, ok, err := load(path); err != nil {
		return "", err
	} else if ok {
		return id, nil
	}

	// Ensure root dir exists before attempting to create the file.
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return "", fmt.Errorf("deviceid: mkdir root: %w", err)
	}

	// Attempt atomic create.
	id := ulid.Make().String()
	rec := record{
		Version:   1,
		DeviceID:  id,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("deviceid: marshal: %w", err)
	}
	b = append(b, '\n')

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err == nil {
		defer f.Close()
		if _, werr := f.Write(b); werr != nil {
			return "", fmt.Errorf("deviceid: write: %w", werr)
		}
		return id, nil
	}

	// If another process created it first, read it back.
	if errors.Is(err, os.ErrExist) {
		if id, ok, rerr := load(path); rerr != nil {
			return "", rerr
		} else if ok {
			return id, nil
		}
		return "", fmt.Errorf("deviceid: file exists but could not be read: %s", path)
	}

	return "", fmt.Errorf("deviceid: create: %w", err)
}

func load(path string) (deviceID string, ok bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("deviceid: read: %w", err)
	}

	var rec record
	if err := json.Unmarshal(data, &rec); err != nil {
		return "", false, fmt.Errorf("deviceid: parse json: %w", err)
	}
	if rec.DeviceID == "" {
		return "", false, fmt.Errorf("deviceid: missing device_id in %s", path)
	}

	return rec.DeviceID, true, nil
}
