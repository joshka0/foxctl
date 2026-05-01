package main

import "example.com/polyglot/internal/goapp/commands"

func main() {
	registry := commands.NewRegistry()
	commands.RegisterDeployCommand(registry)
	registry.Dispatch("deploy")
}
