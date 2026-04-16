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
	flag.Parse()

	if err := tuiapp.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "foxctl_tui: %v\n", err)
		os.Exit(1)
	}
}
