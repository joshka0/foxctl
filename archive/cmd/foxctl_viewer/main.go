//go:build archived

package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	help := flag.Bool("help", false, "Show help")
	version := flag.Bool("version", false, "Show version")
	robotHelp := flag.Bool("robot-help", false, "Show AI agent help")
	robotJobs := flag.Bool("robot-jobs", false, "Output jobs list as JSON for AI agents")
	robotJob := flag.String("robot-job", "", "Output single job detail as JSON")
	robotInsights := flag.Bool("robot-insights", false, "Output task graph insights as JSON for AI agents")
	robotPlan := flag.Bool("robot-plan", false, "Output execution plan as JSON for AI agents")
	robotTasks := flag.Bool("robot-tasks", false, "Output task list as JSON for AI agents")
	robotGraph := flag.Bool("robot-graph", false, "Output task dependency graph as JSON for AI agents")
	robotPriority := flag.Bool("robot-priority", false, "Output priority recommendations with reasoning as JSON")
	robotMailbox := flag.String("robot-mailbox", "", "Output mailbox messages for actor as JSON")
	robotReservations := flag.Bool("robot-reservations", false, "Output active file reservations as JSON")
	robotSQLite := flag.Bool("robot-sqlite", false, "Output SQLite database info as JSON")
	robotSearch := flag.String("robot-search", "", "Semantic search query (JSON output for AI agents)")
	dbFlag := flag.String("db", "", "SQLite database name (for --robot-sqlite)")
	tableFlag := flag.String("table", "", "SQLite table name (for --robot-sqlite)")
	workspaceFlag := flag.String("workspace", "", "Filter by workspace path")
	limitFlag := flag.Int("limit", 50, "Limit number of results")
	stateFlag := flag.String("state", "", "Filter by job state (ok, error, running, queued)")
	recipeFlag := flag.String("recipe", "", "Apply recipe filter (actionable, blocked, recent, errors)")
	tasksFlag := flag.Bool("tasks", false, "Show tasks view instead of jobs")
	insightsFlag := flag.Bool("insights", false, "Show insights dashboard")
	mailboxFlag := flag.String("mailbox", "", "Show mailbox for actor (e.g., actor:coder:agent1)")
	reservationsFlag := flag.Bool("reservations", false, "Show file reservations view")
	sqliteFlag := flag.Bool("sqlite", false, "Show SQLite database browser")
	searchFlag := flag.String("search", "", "Start in search mode with query")
	rerankFlag := flag.Bool("rerank", false, "Enable Voyage rerank-2.5 for search")
	scopeFlag := flag.String("scope", "", "Search scopes: symbols,sessions,memories,tasks (comma-separated)")
	flag.Parse()

	if *help {
		printHelp()
		os.Exit(0)
	}

	if *robotHelp {
		printRobotHelp()
		os.Exit(0)
	}

	if *version {
		fmt.Println("foxctl-viewer v0.1.0")
		os.Exit(0)
	}

	if *robotJobs {
		handleRobotJobs(*workspaceFlag, *stateFlag, *limitFlag)
		os.Exit(0)
	}

	if *robotJob != "" {
		handleRobotJobDetail(*robotJob)
		os.Exit(0)
	}

	if *robotInsights {
		handleRobotInsights(*workspaceFlag)
		os.Exit(0)
	}

	if *robotPlan {
		handleRobotPlan(*workspaceFlag, *limitFlag)
		os.Exit(0)
	}

	if *robotTasks {
		handleRobotTasks(*workspaceFlag, *limitFlag)
		os.Exit(0)
	}

	if *robotGraph {
		handleRobotGraph(*workspaceFlag)
		os.Exit(0)
	}

	if *robotPriority {
		handleRobotPriority(*workspaceFlag, *limitFlag)
		os.Exit(0)
	}

	if *robotMailbox != "" {
		handleRobotMailbox(*workspaceFlag, *robotMailbox, *limitFlag)
		os.Exit(0)
	}

	if *robotReservations {
		handleRobotReservations(*workspaceFlag)
		os.Exit(0)
	}

	if *robotSQLite {
		handleRobotSQLite(*dbFlag, *tableFlag, *limitFlag)
		os.Exit(0)
	}

	if *robotSearch != "" {
		scopes := parseScopes(*scopeFlag)
		handleRobotSearch(*workspaceFlag, *robotSearch, *limitFlag, *rerankFlag, scopes)
		os.Exit(0)
	}

	if *insightsFlag {
		runInsightsView(*workspaceFlag)
		os.Exit(0)
	}

	if *tasksFlag {
		runTasksView(*workspaceFlag, *limitFlag)
		os.Exit(0)
	}

	if *mailboxFlag != "" {
		runMailboxView(*workspaceFlag, *mailboxFlag, *limitFlag)
		os.Exit(0)
	}

	if *reservationsFlag {
		runReservationsView(*workspaceFlag)
		os.Exit(0)
	}

	if *recipeFlag != "" {
		applyRecipeFilter(recipeFlag, stateFlag)
	}

	// Handle --sqlite flag: start TUI directly in SQLite browser mode
	if *sqliteFlag {
		// For SQLite mode, we don't require jobs
		p := tea.NewProgram(newModelWithMode(nil, *workspaceFlag, "", viewSQLite), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Handle --search flag: start TUI in search mode
	if *searchFlag != "" {
		scopes := parseScopes(*scopeFlag)
		p := tea.NewProgram(newSearchModel(*workspaceFlag, *searchFlag, *limitFlag, *rerankFlag, scopes), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	jobs, err := loadJobs(*limitFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading jobs: %v\n", err)
		os.Exit(1)
	}

	if len(jobs) == 0 {
		fmt.Println("No jobs found. Run some skills with 'foxctl run <skill>'!")
		os.Exit(0)
	}

	p := tea.NewProgram(newModel(jobs, *workspaceFlag), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
