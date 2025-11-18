package cmd

import (
	"fmt"
	"os"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/execution/scheduler"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/spf13/cobra"
)

// Global WFQ scheduler (shared across commands)
var globalScheduler *scheduler.WFQScheduler

func init() {
	// Initialize with default config
	globalScheduler = scheduler.NewWFQScheduler(scheduler.Config{
		DefaultWeight: 1,
		WorkerCount:   4,
	})
}

var schedulerCmd = &cobra.Command{
	Use:   "scheduler",
	Short: "Manage WFQ job scheduler (Agent Profile v1)",
	Long:  "Manage weighted fair queueing scheduler for namespace-based job scheduling",
}

var schedulerStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show scheduler statistics",
	Long:  "Display current scheduler state and queue statistics",
	Args:  cobra.NoArgs,
	RunE:  runSchedulerStats,
}

var schedulerSetWeightCmd = &cobra.Command{
	Use:   "set-weight [namespace]",
	Short: "Set scheduling weight for namespace",
	Long:  "Configure the scheduling weight for a namespace (higher = more capacity)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSchedulerSetWeight,
}

var schedulerGetWeightCmd = &cobra.Command{
	Use:   "get-weight [namespace]",
	Short: "Get scheduling weight for namespace",
	Long:  "Display the configured scheduling weight for a namespace",
	Args:  cobra.ExactArgs(1),
	RunE:  runSchedulerGetWeight,
}

// Flags for set-weight
var (
	schedulerWeight int
)

func init() {
	// Add scheduler commands to root
	rootCmd.AddCommand(schedulerCmd)
	schedulerCmd.AddCommand(schedulerStatsCmd)
	schedulerCmd.AddCommand(schedulerSetWeightCmd)
	schedulerCmd.AddCommand(schedulerGetWeightCmd)

	// Set-weight flags
	schedulerSetWeightCmd.Flags().IntVar(&schedulerWeight, "weight", 1, "Scheduling weight (higher = more capacity)")
	if err := schedulerSetWeightCmd.MarkFlagRequired("weight"); err != nil {
		panic(err)
	}
}

func runSchedulerStats(_ *cobra.Command, _ []string) error {
	stats := globalScheduler.Stats()

	data := map[string]interface{}{
		"virtual_time":     stats.VirtualTime,
		"queued_jobs":      stats.QueuedJobs,
		"namespace_queues": stats.NamespaceQueues,
		"queue_count":      len(stats.NamespaceQueues),
	}

	env := envelope.OK("scheduler/stats", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runSchedulerSetWeight(cmd *cobra.Command, args []string) error {
	namespace := args[0]

	if schedulerWeight <= 0 {
		return writeErrorEnvelope(cmd, "scheduler/set-weight", string(protocol.ErrorCodeEARG), "weight must be positive")
	}

	globalScheduler.SetWeight(namespace, schedulerWeight)

	data := map[string]interface{}{
		"namespace": namespace,
		"weight":    schedulerWeight,
		"action":    "set",
	}

	env := envelope.OK("scheduler/set-weight", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runSchedulerGetWeight(_ *cobra.Command, args []string) error {
	namespace := args[0]

	weight := globalScheduler.GetWeight(namespace)

	data := map[string]interface{}{
		"namespace": namespace,
		"weight":    weight,
	}

	env := envelope.OK("scheduler/get-weight", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}
