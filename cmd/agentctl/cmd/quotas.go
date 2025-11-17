package cmd

import (
	"fmt"
	"os"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/quotas"
	"github.com/spf13/cobra"
)

var quotasCmd = &cobra.Command{
	Use:   "quotas",
	Short: "Manage namespace resource quotas (Agent Profile v1)",
	Long:  "Manage resource quotas for agent namespaces",
}

var quotasShowCmd = &cobra.Command{
	Use:   "show [namespace]",
	Short: "Show quotas for a namespace",
	Long:  "Display resource quotas and current consumption for a namespace",
	Args:  cobra.ExactArgs(1),
	RunE:  runQuotasShow,
}

var quotasSetCmd = &cobra.Command{
	Use:   "set [namespace]",
	Short: "Set quotas for a namespace",
	Long:  "Set or update resource quotas for a namespace",
	Args:  cobra.ExactArgs(1),
	RunE:  runQuotasSet,
}

var quotasDeleteCmd = &cobra.Command{
	Use:   "delete [namespace]",
	Short: "Delete quotas for a namespace",
	Long:  "Remove quota limits for a namespace (sets to unlimited)",
	Args:  cobra.ExactArgs(1),
	RunE:  runQuotasDelete,
}

var quotasListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all namespace quotas",
	Long:  "List resource quotas for all namespaces",
	Args:  cobra.NoArgs,
	RunE:  runQuotasList,
}

var quotasConsumptionCmd = &cobra.Command{
	Use:   "consumption [namespace]",
	Short: "Show current resource consumption",
	Long:  "Display current resource usage for a namespace",
	Args:  cobra.ExactArgs(1),
	RunE:  runQuotasConsumption,
}

// Flags for quotas set
var (
	quotasSetMaxJobs      int
	quotasSetCPU          int
	quotasSetMemMB        int
	quotasSetLLMPerMin    int
	quotasSetEgressPerMin int
)

func init() {
	// Add quotas commands to root
	rootCmd.AddCommand(quotasCmd)
	quotasCmd.AddCommand(quotasShowCmd)
	quotasCmd.AddCommand(quotasSetCmd)
	quotasCmd.AddCommand(quotasDeleteCmd)
	quotasCmd.AddCommand(quotasListCmd)
	quotasCmd.AddCommand(quotasConsumptionCmd)

	// Set flags
	quotasSetCmd.Flags().IntVar(&quotasSetMaxJobs, "max-jobs", 0, "Maximum concurrent jobs (0=unlimited)")
	quotasSetCmd.Flags().IntVar(&quotasSetCPU, "cpu", 0, "CPU limit in millicores (0=unlimited)")
	quotasSetCmd.Flags().IntVar(&quotasSetMemMB, "memory", 0, "Memory limit in MB (0=unlimited)")
	quotasSetCmd.Flags().IntVar(&quotasSetLLMPerMin, "llm-calls", 0, "LLM calls per minute (0=unlimited)")
	quotasSetCmd.Flags().IntVar(&quotasSetEgressPerMin, "egress", 0, "Egress bytes per minute (0=unlimited)")
}

func runQuotasShow(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	ns := args[0]

	// Open quotas store
	quotasStore, err := quotas.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "quotas/show", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open quotas store: %v", err))
	}
	defer quotasStore.Close()

	// Get quotas
	q, err := quotasStore.Get(ctx, ns)
	if err != nil {
		if err == quotas.ErrNotFound {
			return writeErrorEnvelope(cmd, "quotas/show", string(protocol.ErrorCodeENotFound), fmt.Sprintf("no quotas defined for namespace: %s", ns))
		}
		return writeErrorEnvelope(cmd, "quotas/show", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to get quotas: %v", err))
	}

	// Get consumption
	consumption, err := quotasStore.GetConsumption(ctx, ns)
	if err != nil {
		return writeErrorEnvelope(cmd, "quotas/show", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to get consumption: %v", err))
	}

	// Write success envelope
	data := map[string]interface{}{
		"namespace":   ns,
		"quotas":      q,
		"consumption": consumption,
	}

	env := envelope.OK("quotas/show", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runQuotasSet(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	ns := args[0]

	// Open quotas store
	quotasStore, err := quotas.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "quotas/set", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open quotas store: %v", err))
	}
	defer quotasStore.Close()

	// Create quotas
	q := agent.Quotas{
		Namespace:         ns,
		MaxConcurrentJobs: quotasSetMaxJobs,
		CPULimit:          quotasSetCPU,
		MemMBLimit:        quotasSetMemMB,
		LLMCallsPerMin:    quotasSetLLMPerMin,
		EgressBytesPerMin: quotasSetEgressPerMin,
	}

	// Try to update first, if not found then set
	err = quotasStore.Update(ctx, ns, q)
	if err != nil {
		if err == quotas.ErrNotFound {
			// Not found, create new
			if err := quotasStore.Set(ctx, ns, q); err != nil {
				return writeErrorEnvelope(cmd, "quotas/set", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to set quotas: %v", err))
			}
		} else {
			return writeErrorEnvelope(cmd, "quotas/set", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to update quotas: %v", err))
		}
	}

	// Write success envelope
	data := map[string]interface{}{
		"namespace": ns,
		"quotas":    q,
		"action":    "set",
	}

	env := envelope.OK("quotas/set", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runQuotasDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	ns := args[0]

	// Open quotas store
	quotasStore, err := quotas.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "quotas/delete", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open quotas store: %v", err))
	}
	defer quotasStore.Close()

	// Delete quotas
	if err := quotasStore.Delete(ctx, ns); err != nil {
		if err == quotas.ErrNotFound {
			return writeErrorEnvelope(cmd, "quotas/delete", string(protocol.ErrorCodeENotFound), fmt.Sprintf("no quotas found for namespace: %s", ns))
		}
		return writeErrorEnvelope(cmd, "quotas/delete", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to delete quotas: %v", err))
	}

	// Write success envelope
	data := map[string]interface{}{
		"namespace": ns,
		"deleted":   true,
	}

	env := envelope.OK("quotas/delete", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runQuotasList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	// Open quotas store
	quotasStore, err := quotas.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "quotas/list", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open quotas store: %v", err))
	}
	defer quotasStore.Close()

	// List all quotas
	allQuotas, err := quotasStore.ListAll(ctx)
	if err != nil {
		return writeErrorEnvelope(cmd, "quotas/list", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to list quotas: %v", err))
	}

	// Write success envelope
	data := map[string]interface{}{
		"quotas": allQuotas,
		"count":  len(allQuotas),
	}

	env := envelope.OK("quotas/list", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runQuotasConsumption(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	ns := args[0]

	// Open quotas store
	quotasStore, err := quotas.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "quotas/consumption", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open quotas store: %v", err))
	}
	defer quotasStore.Close()

	// Get consumption
	consumption, err := quotasStore.GetConsumption(ctx, ns)
	if err != nil {
		return writeErrorEnvelope(cmd, "quotas/consumption", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to get consumption: %v", err))
	}

	// Write success envelope
	data := map[string]interface{}{
		"namespace":   ns,
		"consumption": consumption,
	}

	env := envelope.OK("quotas/consumption", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}
