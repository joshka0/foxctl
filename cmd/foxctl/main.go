// Package main is the entry point for the foxctl CLI binary.
package main

import (
	"context"
	"errors"
	"log"
	"os"

	cmd "github.com/joshka0/foxctl/cmd/foxctl/cmd"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
)

func main() {
	// Load .env files before anything else
	// Priority: ~/.foxctl/.env → $PWD/.env (project overrides global)
	config.LoadDotEnv()

	if err := cmd.Execute(context.Background()); err != nil {
		var written *protocol.WrittenEnvelopeError
		if errors.As(err, &written) {
			os.Exit(1)
		}
		log.Fatal(err)
	}
}
