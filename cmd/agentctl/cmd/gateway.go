package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/gateway"
	"github.com/jkatigb/agentctl/internal/platform/logging"
	"github.com/jkatigb/agentctl/internal/protocol"
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the Tailscale gateway for room sandbox terminal access",
	Long: `Start the agentctl gateway daemon.

The gateway provides HTTPS (via Tailscale tsnet) or HTTP (dev mode) access
to room sandbox terminals. It supports both web terminal (xterm.js) and SSH access.

In production mode, the gateway connects to your Tailscale tailnet using tsnet,
providing auto-TLS and MagicDNS access. In dev mode, it listens on localhost
without Tailscale.

Examples:
  # Start with Tailscale auth key
  agentctl gateway --ts-authkey tskey-auth-xxx

  # Start using TS_AUTHKEY env var
  TS_AUTHKEY=tskey-auth-xxx agentctl gateway

  # Start in dev mode (no Tailscale, localhost only)
  agentctl gateway --dev

  # Custom port for dev mode
  agentctl gateway --dev --port 9000

  # Custom state directory
  agentctl gateway --state-dir /var/lib/agentctl/gateway`,
	RunE: runGateway,
}

var (
	gatewayDev      bool
	gatewayPort     int
	gatewayStateDir string
	gatewayAuthKey  string
	gatewayHostname string
)

func init() {
	rootCmd.AddCommand(gatewayCmd)

	gatewayCmd.Flags().BoolVar(&gatewayDev, "dev", false, "Development mode: localhost HTTP without Tailscale")
	gatewayCmd.Flags().IntVar(&gatewayPort, "port", gateway.DefaultPort, "HTTP port for dev mode")
	gatewayCmd.Flags().StringVar(&gatewayStateDir, "state-dir", gateway.DefaultStateDir, "Directory for tsnet state persistence")
	gatewayCmd.Flags().StringVar(&gatewayAuthKey, "ts-authkey", "", "Tailscale auth key (or set TS_AUTHKEY env var)")
	gatewayCmd.Flags().StringVar(&gatewayHostname, "hostname", gateway.HostnamePrefix, "tsnet hostname")
}

func runGateway(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	// Get logger from context (set by PersistentPreRunE in root.go)
	log := logging.FromContext(ctx).With().
		Str("component", "gateway").
		Bool("dev", gatewayDev).
		Logger()

	// If no logger in context, create a default one writing to stderr
	if log.GetLevel() == zerolog.Disabled {
		log = logging.New(logging.Config{
			Level:  logging.ParseLevel("info"),
			Format: logging.ParseFormat("json"),
		}).With().
			Str("component", "gateway").
			Bool("dev", gatewayDev).
			Logger()
	}

	opts := gateway.Options{
		Dev:      gatewayDev,
		Port:     gatewayPort,
		StateDir: gatewayStateDir,
		AuthKey:  gatewayAuthKey,
		Hostname: gatewayHostname,
	}

	err := gateway.Run(ctx, opts, log)
	if err != nil {
		// Check if it's an auth key error for structured envelope output
		var authErr *gateway.AuthKeyError
		if asErr(err, &authErr) {
			env := envelope.Error(
				"gateway",
				string(protocol.ErrorCodeEARG),
				authErr.Error(),
				authErr.Envelope()["data"],
			)
			return writeEnvelope(env)
		}
		return fmt.Errorf("gateway: %w", err)
	}
	return nil
}

// asErr is a helper that works like errors.As for typed error checking.
func asErr(err error, target **gateway.AuthKeyError) bool {
	if e, ok := err.(*gateway.AuthKeyError); ok {
		*target = e
		return true
	}
	return false
}

// writeEnvelope writes a JSON envelope to stdout and returns a
// WrittenEnvelopeError so the caller exits non-zero.
func writeEnvelope(env envelope.Envelope) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		return err
	}
	return &protocol.WrittenEnvelopeError{
		Command: env.Command,
		Code:    protocol.ErrorCode(env.Error.Code),
		Message: env.Error.Message,
	}
}
