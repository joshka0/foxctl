// Package main is the entry point for the agentctl CLI binary.
package main

import (
	"context"
	"log"

	cmd "github.com/jkatigb/agentctl/cmd/agentctl/cmd"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func main() {
	// Load .env files before anything else
	// Priority: ~/.agentctl/.env → $PWD/.env (project overrides global)
	config.LoadDotEnv()

	if err := cmd.Execute(context.Background()); err != nil {
		log.Fatal(err)
	}
}
