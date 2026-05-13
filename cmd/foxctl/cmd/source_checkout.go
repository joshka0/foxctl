package cmd

import (
	"os"
	"path/filepath"
	"strings"
)

func inFoxctlSourceCheckout() bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	_, ok := foxctlSourceRoot(cwd)
	return ok
}

func foxctlSourceRoot(cwd string) (string, bool) {
	dir := strings.TrimSpace(cwd)
	if dir == "" {
		return "", false
	}
	dir = filepath.Clean(dir)
	for {
		if isFoxctlSourceRoot(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isFoxctlSourceRoot(dir string) bool {
	body, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "module github.com/joshka0/foxctl" {
			return true
		}
		if strings.HasPrefix(line, "module ") {
			return false
		}
	}
	return false
}
