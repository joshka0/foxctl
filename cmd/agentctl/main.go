// Package main is the entry point for the agentctl CLI binary.
package main

import (
	"context"
	"errors"
	"log"
	"os"

	cmd "github.com/jkatigb/agentctl/cmd/agentctl/cmd"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
)

func main() {
	// Load .env files before anything else
	// Priority: ~/.agentctl/.env → $PWD/.env (project overrides global)
	config.LoadDotEnv()

	if err := cmd.Execute(context.Background()); err != nil {
		var written *protocol.WrittenEnvelopeError
		if errors.As(err, &written) {
			os.Exit(1)
		}
		log.Fatal(err)
	}
}
