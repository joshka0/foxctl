package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/runtime/execution/circuitbreaker"
	"github.com/spf13/cobra"
)

// Global circuit breaker manager (shared across commands)
var globalCBManager *circuitbreaker.Manager

func init() {
	// Initialize with default config
	globalCBManager = circuitbreaker.NewManager(circuitbreaker.Config{
		MaxFailures:         5,
		ResetTimeout:        30 * time.Second,
		MaxHalfOpenRequests: 3,
		SuccessThreshold:    2,
	})
}

var cbCmd = &cobra.Command{
	Use:   "cb",
	Short: "Manage circuit breakers (Agent Profile v1)",
	Long:  "Manage circuit breakers for resilient execution",
}

var cbListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all circuit breakers",
	Long:  "Display status of all registered circuit breakers",
	Args:  cobra.NoArgs,
	RunE:  runCBList,
}

var cbShowCmd = &cobra.Command{
	Use:   "show [name]",
	Short: "Show circuit breaker details",
	Long:  "Display detailed information about a specific circuit breaker",
	Args:  cobra.ExactArgs(1),
	RunE:  runCBShow,
}

var cbResetCmd = &cobra.Command{
	Use:   "reset [name]",
	Short: "Reset a circuit breaker",
	Long:  "Manually reset a circuit breaker to closed state",
	Args:  cobra.ExactArgs(1),
	RunE:  runCBReset,
}

var cbResetAllCmd = &cobra.Command{
	Use:   "reset-all",
	Short: "Reset all circuit breakers",
	Long:  "Manually reset all circuit breakers to closed state",
	Args:  cobra.NoArgs,
	RunE:  runCBResetAll,
}

func init() {
	// Add cb commands to root
	rootCmd.AddCommand(cbCmd)
	cbCmd.AddCommand(cbListCmd)
	cbCmd.AddCommand(cbShowCmd)
	cbCmd.AddCommand(cbResetCmd)
	cbCmd.AddCommand(cbResetAllCmd)
}

func runCBList(_ *cobra.Command, _ []string) error {
	stats := globalCBManager.ListAll()

	data := map[string]any{
		"breakers": stats,
		"count":    len(stats),
	}

	env := envelope.OK("cb/list", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runCBShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	breaker := globalCBManager.Get(name)
	if breaker == nil {
		return writeErrorEnvelope(cmd, "cb/show", string(protocol.ErrorCodeENotFound), fmt.Sprintf("circuit breaker not found: %s", name))
	}

	stats := breaker.Stats()

	data := map[string]any{
		"name":              stats.Name,
		"state":             stats.State,
		"failures":          stats.Failures,
		"successes":         stats.Successes,
		"consecutive_fails": stats.ConsecutiveFails,
		"last_fail_time":    formatTime(stats.LastFailTime),
		"last_state_change": formatTime(stats.LastStateChange),
	}

	env := envelope.OK("cb/show", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runCBReset(cmd *cobra.Command, args []string) error {
	name := args[0]

	success := globalCBManager.Reset(name)
	if !success {
		return writeErrorEnvelope(cmd, "cb/reset", string(protocol.ErrorCodeENotFound), fmt.Sprintf("circuit breaker not found: %s", name))
	}

	data := map[string]any{
		"name":  name,
		"reset": true,
		"state": "closed",
	}

	env := envelope.OK("cb/reset", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runCBResetAll(_ *cobra.Command, _ []string) error {
	count := globalCBManager.Count()
	globalCBManager.ResetAll()

	data := map[string]any{
		"reset_count": count,
		"state":       "all_closed",
	}

	env := envelope.OK("cb/reset-all", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
