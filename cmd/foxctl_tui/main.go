package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tuiapp "github.com/joshka0/foxctl/internal/interfaces/tui"
)

func main() {
	var opts tuiapp.Options
	var smokeConsole bool
	var smokeAgent bool
	var smokeAsk string
	var smokeCancel bool
	var smokeTimeout time.Duration
	var screen string

	flag.StringVar(&opts.Workspace, "workspace", "", "workspace path shown in the shell")
	flag.StringVar(&opts.EpicID, "epic-id", "", "epic ID to mirror from the foxctl epics store")
	flag.StringVar(&opts.EpicsDir, "epics-dir", "", "epics directory (default: ~/.foxctl/epics)")
	flag.StringVar(&opts.APIBaseURL, "api-base-url", "", "foxctl API base URL for optional read-only agent enrichment")
	flag.IntVar(&opts.AgentLimit, "agent-limit", 25, "max agents to fetch when --api-base-url is set")
	flag.StringVar(&opts.AgentID, "agent-id", "", "foxctl agent ID to use as the companion target when --api-base-url is set")
	flag.StringVar(&opts.ConsoleSessionID, "console-session-id", "", "existing console session ID to attach read-only transcript when --api-base-url is set")
	flag.IntVar(&opts.ConsoleStreamBuffer, "console-stream-buffer", 16, "buffer size for console stream updates")
	flag.IntVar(&opts.TranscriptLimit, "transcript-limit", 200, "max transcript rows retained for stream updates")
	flag.BoolVar(&smokeConsole, "smoke-console", false, "run non-interactive attached-console smoke validation")
	flag.BoolVar(&smokeAgent, "smoke-agent", false, "run non-interactive companion-agent smoke validation")
	flag.StringVar(&smokeAsk, "smoke-ask", "", "optional ask content to submit during --smoke-console")
	flag.BoolVar(&smokeCancel, "smoke-cancel", false, "optionally queue one cancel request during --smoke-console")
	flag.DurationVar(&smokeTimeout, "smoke-timeout", 3*time.Second, "timeout for --smoke-console")
	flag.StringVar(&screen, "screen", "", "start a specific TUI screen: agents (operator cockpit with agent inventory)")
	flag.Parse()

	if smokeConsole {
		summary, err := tuiapp.RunConsoleSmoke(context.Background(), tuiapp.SmokeConsoleOptions{
			Options: opts,
			Ask:     smokeAsk,
			Cancel:  smokeCancel,
			Timeout: smokeTimeout,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "foxctl_tui: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, summary.String())
		return
	}
	if smokeAgent {
		summary, err := tuiapp.RunAgentSmoke(context.Background(), tuiapp.SmokeAgentOptions{
			Options: opts,
			Ask:     smokeAsk,
			Timeout: smokeTimeout,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "foxctl_tui: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, summary.String())
		return
	}

	if screen != "" {
		switch screen {
		case "agents":
			apiURL := opts.APIBaseURL
			if apiURL == "" {
				apiURL = "http://localhost:8090"
			}
			if err := tuiapp.RunCockpit(apiURL); err != nil {
				fmt.Fprintf(os.Stderr, "foxctl_tui: %v\n", err)
				os.Exit(1)
			}
		default:
			fmt.Fprintf(os.Stderr, "foxctl_tui: unknown screen %q; available screens: agents\n", screen)
			os.Exit(1)
		}
		return
	}

	if err := tuiapp.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "foxctl_tui: %v\n", err)
		os.Exit(1)
	}
}
