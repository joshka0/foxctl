package cmd

import "github.com/joshka0/foxctl/internal/storage/testwatch"

func loadTestWatchConfig(workspaceDir string) (*testwatch.Config, bool, error) {
	if !testwatch.ConfigExists(workspaceDir) {
		return nil, false, nil
	}
	cfg, err := testwatch.LoadConfig(workspaceDir)
	if err != nil {
		return nil, true, err
	}
	return cfg, true, nil
}
