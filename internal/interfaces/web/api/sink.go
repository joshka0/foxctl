package api

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
)

// SinkRequest is the expected JSON body for POST /api/sink.
type SinkRequest struct {
	// VaultPath is deprecated. The sink root is server-controlled via
	// FOXCTL_ACA_VAULT_PATH or FOXCTL_OBSIDIAN_VAULT_PATH.
	VaultPath string `json:"vaultPath"`
	FilePath  string `json:"filePath"`
	Content   string `json:"content"`
}

// SinkResponse is the JSON response for a successful sink write.
type SinkResponse struct {
	Success     bool   `json:"success"`
	OperationID string `json:"operationId"`
	FilePath    string `json:"filePath"`
	Timestamp   int64  `json:"timestamp"`
	Message     string `json:"message"`
}

// SinkHandler returns a handler for POST /api/sink.
// It writes note content to the configured vault path with containment checks.
func SinkHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var req SinkRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}

		filePath := strings.TrimSpace(req.FilePath)
		if strings.TrimSpace(req.VaultPath) != "" {
			httpError(w, http.StatusBadRequest, "vaultPath is server-controlled")
			return
		}
		if filePath == "" {
			httpError(w, http.StatusBadRequest, "filePath is required")
			return
		}

		vaultPath := resolveContextVaultPath("")
		target, err := prepareSinkTarget(vaultPath, filePath)
		if err != nil {
			log.Warn().Str("vault", vaultPath).Str("file", filePath).Err(err).Msg("sink path validation failed")
			status := http.StatusForbidden
			if strings.TrimSpace(vaultPath) == "" {
				status = http.StatusServiceUnavailable
			}
			httpError(w, status, err.Error())
			return
		}

		if err := os.MkdirAll(filepath.Dir(target.FullPath), 0o755); err != nil {
			log.Error().Str("path", target.FullPath).Err(err).Msg("sink mkdir failed")
			httpError(w, http.StatusInternalServerError, "failed to create directory: "+err.Error())
			return
		}
		if err := validateSinkWritePath(target.RootPath, target.FullPath); err != nil {
			log.Warn().Str("path", target.FullPath).Err(err).Msg("sink write path rejected")
			httpError(w, http.StatusForbidden, err.Error())
			return
		}

		if err := os.WriteFile(target.FullPath, []byte(req.Content), 0o644); err != nil {
			log.Error().Str("path", target.FullPath).Err(err).Msg("sink write failed")
			httpError(w, http.StatusInternalServerError, "failed to write file: "+err.Error())
			return
		}

		log.Info().Str("path", target.FullPath).Str("vault", target.RootPath).Msg("sink write succeeded")

		operationID := r.Header.Get("X-Operation-Id")
		if operationID == "" {
			operationID = fmt.Sprintf("sink_%d", time.Now().UnixNano())
		}

		writeJSON(w, http.StatusOK, envelope.OK("sink.write", SinkResponse{
			Success:     true,
			OperationID: operationID,
			FilePath:    target.RelativePath,
			Timestamp:   time.Now().UnixMilli(),
			Message:     "Sink write succeeded",
		}))
	}
}

type sinkTarget struct {
	RootPath     string
	FullPath     string
	RelativePath string
}

func prepareSinkTarget(vaultPath, filePath string) (sinkTarget, error) {
	relPath, err := normalizeSinkPath(filePath)
	if err != nil {
		return sinkTarget{}, err
	}
	rootPath, err := resolveSinkRoot(vaultPath)
	if err != nil {
		return sinkTarget{}, err
	}
	fullPath := filepath.Join(rootPath, filepath.FromSlash(relPath))
	if !pathInside(rootPath, fullPath) {
		return sinkTarget{}, fmt.Errorf("resolved path escapes vault root")
	}
	if err := rejectSymlinkComponents(rootPath, filepath.Dir(fullPath)); err != nil {
		return sinkTarget{}, err
	}
	if info, err := os.Lstat(fullPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return sinkTarget{}, fmt.Errorf("target path is a symlink")
		}
	} else if !os.IsNotExist(err) {
		return sinkTarget{}, fmt.Errorf("inspect target path: %w", err)
	}
	return sinkTarget{
		RootPath:     rootPath,
		FullPath:     fullPath,
		RelativePath: relPath,
	}, nil
}

func resolveSinkRoot(vaultPath string) (string, error) {
	vaultPath = strings.TrimSpace(vaultPath)
	if vaultPath == "" {
		return "", fmt.Errorf("vault path is not configured")
	}
	rootAbs, err := filepath.Abs(vaultPath)
	if err != nil {
		return "", fmt.Errorf("resolve vault root: %w", err)
	}
	info, err := os.Stat(rootAbs)
	if err != nil {
		return "", fmt.Errorf("vault path is not accessible: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("vault path is not a directory")
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve vault root symlinks: %w", err)
	}
	return filepath.Clean(rootReal), nil
}

func normalizeSinkPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", fmt.Errorf("filePath is required")
	}
	if strings.Contains(p, "\\") {
		return "", fmt.Errorf("backslash path separators are not allowed")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("absolute file paths are not allowed")
	}
	if len(p) >= 2 && p[1] == ':' {
		return "", fmt.Errorf("drive-qualified file paths are not allowed")
	}

	slashed := filepath.ToSlash(p)
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." {
			return "", fmt.Errorf("path traversal is not allowed")
		}
	}

	clean := path.Clean(slashed)
	if clean == "." || clean == "/" {
		return "", fmt.Errorf("filePath is required")
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.Contains(part, "~") {
			return "", fmt.Errorf("tilde path segments are not allowed")
		}
	}
	return clean, nil
}

func validateSinkWritePath(rootPath, fullPath string) error {
	if !pathInside(rootPath, fullPath) {
		return fmt.Errorf("resolved path escapes vault root")
	}
	if err := rejectSymlinkComponents(rootPath, filepath.Dir(fullPath)); err != nil {
		return err
	}
	if info, err := os.Lstat(fullPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("target path is a symlink")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect target path: %w", err)
	}
	parentReal, err := filepath.EvalSymlinks(filepath.Dir(fullPath))
	if err != nil {
		return fmt.Errorf("resolve parent path symlinks: %w", err)
	}
	if !pathInside(rootPath, parentReal) {
		return fmt.Errorf("parent path escapes vault root")
	}
	return nil
}

func rejectSymlinkComponents(rootPath, targetPath string) error {
	if !pathInside(rootPath, targetPath) {
		return fmt.Errorf("resolved path escapes vault root")
	}
	rel, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return fmt.Errorf("resolve relative path: %w", err)
	}
	if rel == "." {
		return nil
	}
	current := rootPath
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect path component: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %q is a symlink", part)
		}
	}
	return nil
}

func pathInside(rootPath, targetPath string) bool {
	rootPath = filepath.Clean(rootPath)
	targetPath = filepath.Clean(targetPath)
	rel, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return false
	}
	return rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
