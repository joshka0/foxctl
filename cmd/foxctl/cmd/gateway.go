package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/interfaces/gateway"
	"github.com/joshka0/foxctl/internal/platform/logging"
	"github.com/joshka0/foxctl/internal/protocol"
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the Tailscale gateway for room sandbox terminal access",
	Long: `Start the foxctl gateway daemon.

The gateway provides HTTPS (via Tailscale tsnet) or HTTP (dev mode) access
to room sandbox terminals. It supports both web terminal (xterm.js) and SSH access.

In production mode, the gateway connects to your Tailscale tailnet using tsnet,
providing auto-TLS and MagicDNS access. In dev mode, it listens on localhost
without Tailscale.

Examples:
  # Start with Tailscale auth key
  foxctl gateway --ts-authkey tskey-auth-xxx

  # Start using TS_AUTHKEY env var
  TS_AUTHKEY=tskey-auth-xxx foxctl gateway

  # Start in dev mode (no Tailscale, localhost only)
  foxctl gateway --dev

  # Custom port for dev mode
  foxctl gateway --dev --port 9000

  # Custom state directory
  foxctl gateway --state-dir /var/lib/foxctl/gateway`,
	RunE: runGateway,
}

var (
	gatewayDev      bool
	gatewayPort     int
	gatewayStateDir string
	gatewayAuthKey  string
	gatewayHostname string
	gatewayWithWeb  bool
	gatewayWebAddr  string
)

func init() {
	rootCmd.AddCommand(gatewayCmd)

	gatewayCmd.Flags().BoolVar(&gatewayDev, "dev", false, "Development mode: localhost HTTP without Tailscale")
	gatewayCmd.Flags().IntVar(&gatewayPort, "port", gateway.DefaultPort, "HTTP port for dev mode")
	gatewayCmd.Flags().StringVar(&gatewayStateDir, "state-dir", gateway.DefaultStateDir, "Directory for tsnet state persistence")
	gatewayCmd.Flags().StringVar(&gatewayAuthKey, "ts-authkey", "", "Tailscale auth key (or set TS_AUTHKEY env var)")
	gatewayCmd.Flags().StringVar(&gatewayHostname, "hostname", gateway.HostnamePrefix, "tsnet hostname")
	gatewayCmd.Flags().BoolVar(&gatewayWithWeb, "with-web", false, "Mount the foxctl web API handler in the gateway")
	gatewayCmd.Flags().StringVar(&gatewayWebAddr, "web-addr", "127.0.0.1:8090", "Address of the foxctl web server to proxy to (used with --with-web)")
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

	// Mount the foxctl web API handler if requested
	if gatewayWithWeb {
		webHandler, err := newWebProxyHandler(ctx, gatewayWebAddr, log)
		if err != nil {
			return fmt.Errorf("gateway --with-web: %w", err)
		}
		opts.WebHandler = webHandler
		log.Info().Str("web_addr", gatewayWebAddr).Msg("Web API proxy enabled")
	}

	err := gateway.Run(ctx, opts, log)
	if err != nil {
		// Check if it's an auth key error for structured envelope output
		var authErr *gateway.AuthKeyError
		if errors.As(err, &authErr) {
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

// newWebProxyHandler creates a reverse proxy handler that forwards requests to
// the foxctl web server at the given address. This enables the gateway to
// expose the full web API (agents, SSE, companion, etc.) over tailnet without
// embedding the web server directly.
//
// The proxy forwards Tailscale identity headers when available, allowing the
// web server to enforce identity-aware access control.
func newWebProxyHandler(_ context.Context, addr string, _ zerolog.Logger) (http.Handler, error) {
	target, err := url.Parse("http://" + addr)
	if err != nil {
		return nil, fmt.Errorf("parse web address: %w", err)
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = r.In.Host
			r.SetXForwarded()

			// Inject Tailscale identity headers from the gateway context. This
			// allows the downstream web server to know who is calling.
			info := gateway.IdentityFromRequest(r.In)
			if info == nil {
				return
			}
			r.Out.Header.Set("X-Tailscale-User", info.UserLogin)
			if info.UserName != "" {
				r.Out.Header.Set("X-Tailscale-User-Name", info.UserName)
			}
			if info.NodeName != "" {
				r.Out.Header.Set("X-Tailscale-Node", info.NodeName)
			}
			if info.NodeID != "" {
				r.Out.Header.Set("X-Tailscale-Node-ID", info.NodeID)
			}
		},
	}

	return proxy, nil
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
