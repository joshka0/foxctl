package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type manifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Distribution struct {
		Type string `yaml:"type"`
		WASI *struct {
			Entry string `yaml:"entry"`
		} `yaml:"wasi"`
		Exec *struct {
			Entry string `yaml:"entry"`
		} `yaml:"exec"`
	} `yaml:"distribution"`
	IO struct {
		Format         string `yaml:"format"`
		InlineOutputKB int    `yaml:"inline_output_kb"`
	} `yaml:"io"`
	Capabilities struct {
		Network string `yaml:"network"`
	} `yaml:"capabilities"`
}

type violation struct {
	Path string `json:"path"`
	Msg  string `json:"message"`
}

func main() {
	var violations []violation

	_ = filepath.Walk("skills", func(path string, info os.FileInfo, err error) error {
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

	if len(violations) > 0 {
		out := struct {
			Errors []violation `json:"errors"`
		}{Errors: violations}
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "checkmanifests: encode: %v\n", err)
		}
		os.Exit(1)
	}

	fmt.Println(`{"status":"ok","command":"policy/checkmanifests"}`)
}

func loadManifest(path string) (manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var m manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	return m, nil
}

func validateManifest(path string, m manifest) []violation {
	var out []violation
	format := strings.ToUpper(m.IO.Format)
	if format != "JSON" {
		out = append(out, violation{Path: path, Msg: "io.format must be JSON"})
	}
	if strings.EqualFold(m.Distribution.Type, "wasi") && m.Capabilities.Network != "none" {
		out = append(out, violation{Path: path, Msg: "WASI skills must set capabilities.network=none"})
	}
	if m.IO.InlineOutputKB > 256 {
		out = append(out, violation{Path: path, Msg: fmt.Sprintf("inline_output_kb too large (%d)", m.IO.InlineOutputKB)})
	}
	return out
}
