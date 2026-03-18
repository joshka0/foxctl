package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	sandboxopensandbox "github.com/jkatigb/agentctl/internal/sandbox/opensandbox"
)

func newSandboxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Provision and inspect execution sandboxes",
	}
	cmd.AddCommand(newSandboxOpenSandboxCommand())
	return cmd
}

func newSandboxOpenSandboxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "opensandbox",
		Short: "OpenSandbox-backed workspace provisioning",
	}
	cmd.AddCommand(
		newSandboxOpenSandboxProvisionCommand(),
		newSandboxOpenSandboxExecCommand(),
		newSandboxOpenSandboxDeleteCommand(),
	)
	return cmd
}

var (
	sandboxOpenSandboxBaseURL        string
	sandboxOpenSandboxAPIKey         string
	sandboxOpenSandboxRepoURL        string
	sandboxOpenSandboxRepoRef        string
	sandboxOpenSandboxImage          string
	sandboxOpenSandboxName           string
	sandboxOpenSandboxWorkspaceRoot  string
	sandboxOpenSandboxTimeout        string
	sandboxOpenSandboxAllowEgress    []string
	sandboxOpenSandboxUseServerProxy bool
	sandboxOpenSandboxContextPack    string
	sandboxOpenSandboxContextDest    string
	sandboxOpenSandboxExecSandboxID  string
	sandboxOpenSandboxExecCommand    string
	sandboxOpenSandboxExecCwd        string
	sandboxOpenSandboxExecTimeout    string
	sandboxOpenSandboxDeleteID       string
)

func newSandboxOpenSandboxProvisionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provision-workspace",
		Short: "Create an OpenSandbox workspace and shallow-clone a repo into it",
		RunE:  runSandboxOpenSandboxProvision,
	}

	cmd.Flags().StringVar(&sandboxOpenSandboxBaseURL, "base-url", "", "OpenSandbox lifecycle base URL (defaults to OPEN_SANDBOX_BASE_URL or SANDBOX_DOMAIN)")
	cmd.Flags().StringVar(&sandboxOpenSandboxAPIKey, "api-key", "", "OpenSandbox API key (defaults to OPEN_SANDBOX_API_KEY or SANDBOX_API_KEY)")
	cmd.Flags().StringVar(&sandboxOpenSandboxRepoURL, "repo-url", "", "Repository URL to shallow-clone into the sandbox")
	cmd.Flags().StringVar(&sandboxOpenSandboxRepoRef, "repo-ref", "main", "Repository ref (branch, tag, or SHA) to fetch")
	cmd.Flags().StringVar(&sandboxOpenSandboxImage, "image", sandboxopensandbox.DefaultSandboxImage, "Sandbox image to provision")
	cmd.Flags().StringVar(&sandboxOpenSandboxName, "name", "", "Optional sandbox metadata.name")
	cmd.Flags().StringVar(&sandboxOpenSandboxWorkspaceRoot, "workspace-root", sandboxopensandbox.DefaultWorkspaceRoot, "Workspace path inside the sandbox")
	cmd.Flags().StringVar(&sandboxOpenSandboxTimeout, "timeout", "1h", "Sandbox TTL (for example 30m, 1h)")
	cmd.Flags().StringSliceVar(&sandboxOpenSandboxAllowEgress, "allow-egress", nil, "Additional FQDN egress allow entries (repeatable)")
	cmd.Flags().BoolVar(&sandboxOpenSandboxUseServerProxy, "use-server-proxy", true, "Resolve execd through the lifecycle server proxy")
	cmd.Flags().StringVar(&sandboxOpenSandboxContextPack, "context-pack-file", "", "Optional local file to inject into the sandbox as a read-only context pack")
	cmd.Flags().StringVar(&sandboxOpenSandboxContextDest, "context-pack-dest", "", "Destination path for the injected context pack inside the sandbox")
	_ = cmd.MarkFlagRequired("repo-url")
	return cmd
}

func newSandboxOpenSandboxExecCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Run a shell command inside an OpenSandbox sandbox",
		RunE:  runSandboxOpenSandboxExec,
	}

	cmd.Flags().StringVar(&sandboxOpenSandboxExecSandboxID, "sandbox-id", "", "Sandbox ID")
	cmd.Flags().StringVar(&sandboxOpenSandboxExecCommand, "command", "", "Shell command to execute inside the sandbox")
	cmd.Flags().StringVar(&sandboxOpenSandboxExecCwd, "cwd", "", "Optional working directory inside the sandbox")
	cmd.Flags().StringVar(&sandboxOpenSandboxExecTimeout, "timeout", "30s", "Command timeout")
	cmd.Flags().StringVar(&sandboxOpenSandboxBaseURL, "base-url", "", "OpenSandbox lifecycle base URL (defaults to OPEN_SANDBOX_BASE_URL or SANDBOX_DOMAIN)")
	cmd.Flags().StringVar(&sandboxOpenSandboxAPIKey, "api-key", "", "OpenSandbox API key (defaults to OPEN_SANDBOX_API_KEY or SANDBOX_API_KEY)")
	cmd.Flags().BoolVar(&sandboxOpenSandboxUseServerProxy, "use-server-proxy", true, "Resolve execd through the lifecycle server proxy")
	_ = cmd.MarkFlagRequired("sandbox-id")
	_ = cmd.MarkFlagRequired("command")
	return cmd
}

func newSandboxOpenSandboxDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete an OpenSandbox sandbox",
		RunE:  runSandboxOpenSandboxDelete,
	}

	cmd.Flags().StringVar(&sandboxOpenSandboxDeleteID, "sandbox-id", "", "Sandbox ID")
	cmd.Flags().StringVar(&sandboxOpenSandboxBaseURL, "base-url", "", "OpenSandbox lifecycle base URL (defaults to OPEN_SANDBOX_BASE_URL or SANDBOX_DOMAIN)")
	cmd.Flags().StringVar(&sandboxOpenSandboxAPIKey, "api-key", "", "OpenSandbox API key (defaults to OPEN_SANDBOX_API_KEY or SANDBOX_API_KEY)")
	_ = cmd.MarkFlagRequired("sandbox-id")
	return cmd
}

func runSandboxOpenSandboxProvision(cmd *cobra.Command, _ []string) error {
	timeout, err := time.ParseDuration(strings.TrimSpace(sandboxOpenSandboxTimeout))
	if err != nil {
		return writeErrorEnvelope(cmd, "sandbox/opensandbox/provision-workspace", "EARG", fmt.Sprintf("invalid timeout: %v", err))
	}
	if strings.TrimSpace(sandboxOpenSandboxRepoURL) == "" {
		return writeErrorEnvelope(cmd, "sandbox/opensandbox/provision-workspace", "EARG", "repo-url is required")
	}
	if sandboxOpenSandboxContextPack != "" {
		if _, err := os.Stat(sandboxOpenSandboxContextPack); err != nil {
			return writeErrorEnvelope(cmd, "sandbox/opensandbox/provision-workspace", "EARG", fmt.Sprintf("context-pack-file: %v", err))
		}
	}

	cfg := sandboxopensandbox.ConfigFromEnv()
	if strings.TrimSpace(sandboxOpenSandboxBaseURL) != "" {
		cfg.BaseURL = strings.TrimSpace(sandboxOpenSandboxBaseURL)
	}
	if strings.TrimSpace(sandboxOpenSandboxAPIKey) != "" {
		cfg.APIKey = strings.TrimSpace(sandboxOpenSandboxAPIKey)
	}
	cfg.UseServerProxy = sandboxOpenSandboxUseServerProxy

	client := sandboxopensandbox.New(cfg)
	result, err := client.ProvisionShallowCloneWorkspace(cmd.Context(), sandboxopensandbox.ProvisionWorkspaceRequest{
		RepoURL:         sandboxOpenSandboxRepoURL,
		RepoRef:         sandboxOpenSandboxRepoRef,
		Image:           sandboxOpenSandboxImage,
		Name:            sandboxOpenSandboxName,
		WorkspaceRoot:   sandboxOpenSandboxWorkspaceRoot,
		Timeout:         timeout,
		AllowEgress:     sandboxOpenSandboxAllowEgress,
		UseServerProxy:  sandboxOpenSandboxUseServerProxy,
		ContextPackFile: sandboxOpenSandboxContextPack,
		ContextPackDest: sandboxOpenSandboxContextDest,
	})
	if err != nil {
		return writeErrorEnvelope(cmd, "sandbox/opensandbox/provision-workspace", "ERUNTIME", err.Error())
	}

	return writeOK(cmd, "sandbox/opensandbox/provision-workspace", result, "run", nil)
}

func runSandboxOpenSandboxExec(cmd *cobra.Command, _ []string) error {
	timeout, err := time.ParseDuration(strings.TrimSpace(sandboxOpenSandboxExecTimeout))
	if err != nil {
		return writeErrorEnvelope(cmd, "sandbox/opensandbox/exec", "EARG", fmt.Sprintf("invalid timeout: %v", err))
	}

	cfg := sandboxopensandbox.ConfigFromEnv()
	if strings.TrimSpace(sandboxOpenSandboxBaseURL) != "" {
		cfg.BaseURL = strings.TrimSpace(sandboxOpenSandboxBaseURL)
	}
	if strings.TrimSpace(sandboxOpenSandboxAPIKey) != "" {
		cfg.APIKey = strings.TrimSpace(sandboxOpenSandboxAPIKey)
	}
	cfg.UseServerProxy = sandboxOpenSandboxUseServerProxy

	client := sandboxopensandbox.New(cfg)
	result, err := client.RunSandboxCommand(cmd.Context(), sandboxopensandbox.RunSandboxCommandRequest{
		SandboxID: sandboxOpenSandboxExecSandboxID,
		Command:   sandboxOpenSandboxExecCommand,
		Cwd:       sandboxOpenSandboxExecCwd,
		Timeout:   timeout,
	})
	if err != nil {
		return writeErrorEnvelope(cmd, "sandbox/opensandbox/exec", "ERUNTIME", err.Error())
	}

	return writeOK(cmd, "sandbox/opensandbox/exec", result, "run", nil)
}

func runSandboxOpenSandboxDelete(cmd *cobra.Command, _ []string) error {
	cfg := sandboxopensandbox.ConfigFromEnv()
	if strings.TrimSpace(sandboxOpenSandboxBaseURL) != "" {
		cfg.BaseURL = strings.TrimSpace(sandboxOpenSandboxBaseURL)
	}
	if strings.TrimSpace(sandboxOpenSandboxAPIKey) != "" {
		cfg.APIKey = strings.TrimSpace(sandboxOpenSandboxAPIKey)
	}
	client := sandboxopensandbox.New(cfg)
	if err := client.DeleteSandbox(cmd.Context(), sandboxOpenSandboxDeleteID); err != nil {
		return writeErrorEnvelope(cmd, "sandbox/opensandbox/delete", "ERUNTIME", err.Error())
	}

	return writeOK(cmd, "sandbox/opensandbox/delete", map[string]any{
		"deleted":    true,
		"sandbox_id": sandboxOpenSandboxDeleteID,
	}, "run", nil)
}

func init() {
	rootCmd.AddCommand(newSandboxCommand())
}
