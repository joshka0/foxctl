// Package main implements manifest policy checking for skill.yaml files.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/policy"
	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/errors"
)

type violation struct {
	Path string `json:"path"`
	Msg  string `json:"message"`
}

func main() {
	var violations []violation

	walkErr := filepath.Walk("skills", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			violations = append(violations, violation{Path: path, Msg: err.Error()})
			return nil
		}
		if info.IsDir() || filepath.Base(path) != "skill.yaml" {
			return nil
		}
		m, loadErr := loadManifest(path)
		if loadErr != nil {
			violations = append(violations, violation{Path: path, Msg: loadErr.Error()})
			return nil
		}
		violations = append(violations, validateManifest(path, m)...)
		return nil
	})
	if walkErr != nil {
		violations = append(violations, violation{Path: "skills", Msg: walkErr.Error()})
	}

	if len(violations) > 0 {
		out := struct {
			Errors []violation `json:"errors"`
		}{Errors: violations}
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			if _, writeErr := fmt.Fprintf(os.Stderr, "checkmanifests: encode: %v\n", err); writeErr != nil {
				errors.Ignore(writeErr, "stderr logging failed")
			}
		}
		os.Exit(1)
	}

	fmt.Println(`{"status":"ok","command":"policy/checkmanifests"}`)
}

func loadManifest(path string) (skill.Manifest, error) {
	return skill.LoadManifest(path)
}

func validateManifest(path string, m skill.Manifest) []violation {
	var out []violation

	// Validate I/O format
	format := strings.ToUpper(m.IO.Format)
	if format != "JSON" {
		out = append(out, violation{Path: path, Msg: "io.format must be JSON"})
	}

	// Validate WASI network policy using centralized validation
	if err := policy.ValidateWASIPolicy(m); err != nil {
		out = append(out, violation{Path: path, Msg: err.Error()})
	}

	// Validate filesystem capabilities
	if err := policy.ValidateFilesystemCapabilities(m); err != nil {
		out = append(out, violation{Path: path, Msg: err.Error()})
	}

	// Validate inline output size
	if m.IO.InlineOutputKB > 256 {
		out = append(out, violation{Path: path, Msg: fmt.Sprintf("inline_output_kb too large (%d)", m.IO.InlineOutputKB)})
	}

	return out
}
