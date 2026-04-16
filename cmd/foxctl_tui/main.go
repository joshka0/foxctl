package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tuiapp "github.com/joshka0/foxctl/internal/interfaces/tui"
)

func main() {
	var opts tuiapp.Options
	flag.StringVar(&opts.Workspace, "workspace", "", "workspace path shown in the shell")
	flag.StringVar(&opts.EpicID, "epic-id", "", "epic ID to mirror from the foxctl epics store")
	flag.StringVar(&opts.EpicsDir, "epics-dir", "", "epics directory (default: ~/.foxctl/epics)")
	flag.StringVar(&opts.APIBaseURL, "api-base-url", "", "foxctl API base URL for optional read-only agent enrichment")
	flag.IntVar(&opts.AgentLimit, "agent-limit", 25, "max agents to fetch when --api-base-url is set")
	flag.Parse()

	if err := tuiapp.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "foxctl_tui: %v\n", err)
		os.Exit(1)
	}
}
