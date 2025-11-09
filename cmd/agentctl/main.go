// Package main is the entry point for the agentctl CLI binary.
package main

import (
	"context"
	"log"

	cmd "github.com/jkatigb/agentctl/cmd/agentctl/cmd"
)

func main() {
	if err := cmd.Execute(context.Background()); err != nil {
		log.Fatal(err)
	}
}
