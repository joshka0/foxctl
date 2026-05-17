package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/terminal/herdrbridge"
	"github.com/spf13/cobra"
)

func newHerdrCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "herdr",
		Short: "Call a local Herdr socket and wrap responses in foxctl envelopes",
	}
	cmd.AddCommand(newHerdrAPICommand())
	return cmd
}

func newHerdrAPICommand() *cobra.Command {
	var (
		session    string
		socketPath string
		requestID  string
		params     string
		paramsFile string
	)
	cmd := &cobra.Command{
		Use:   "api <method>",
		Short: "Call a raw Herdr socket API method",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			method := strings.TrimSpace(args[0])
			payload, err := loadHerdrParams(cmd, params, paramsFile)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.herdr.api", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"hint": "Pass --params '{}', --params-file <path>, --params-file -, or --params stdin to read data from a foxctl envelope.",
				}, protocol.WithSource("cli"))
			}
			client := herdrbridge.NewWithOptions(herdrbridge.Options{
				Session:    session,
				SocketPath: socketPath,
			})
			resp, err := client.Raw(cmd.Context(), method, payload, requestID)
			if err != nil {
				var herdrErr *herdrbridge.HerdrError
				if errors.As(err, &herdrErr) {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.herdr.api", herdrProtocolErrorCode(herdrErr), herdrErr.Message, map[string]any{
						"socket_path": client.SocketPath(),
						"method":      method,
						"herdr_error": herdrErr,
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.herdr.api", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"socket_path": client.SocketPath(),
					"method":      method,
				}, protocol.WithSource("cli"))
			}
			data := map[string]any{
				"socket_path": client.SocketPath(),
				"method":      method,
				"id":          resp.ID,
			}
			if len(resp.Result) > 0 {
				data["result"] = json.RawMessage(resp.Result)
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.herdr.api", data, protocol.WithSource("cli"))
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Herdr named session namespace (overrides inherited HERDR_SOCKET_PATH)")
	cmd.Flags().StringVar(&socketPath, "socket", "", "Herdr Unix socket path override")
	cmd.Flags().StringVar(&requestID, "id", "", "Herdr request id (defaults to a foxctl-generated id)")
	cmd.Flags().StringVar(&params, "params", "{}", "Raw JSON params, or 'stdin' to read the data field from a foxctl envelope on stdin")
	cmd.Flags().StringVar(&paramsFile, "params-file", "", "Read raw JSON params from file ('-' for stdin)")
	return cmd
}

func loadHerdrParams(cmd *cobra.Command, inline, file string) (json.RawMessage, error) {
	var data []byte
	switch {
	case file == "-":
		in := cmd.InOrStdin()
		if isTerminalReader(in) {
			return nil, fmt.Errorf("stdin is a terminal")
		}
		b, err := io.ReadAll(in)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		data = b
	case strings.TrimSpace(file) != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		data = b
	case strings.EqualFold(strings.TrimSpace(inline), "stdin"):
		in := cmd.InOrStdin()
		if isTerminalReader(in) {
			return nil, fmt.Errorf("stdin is a terminal")
		}
		b, err := extractEnvelopeData(in)
		if err != nil {
			return nil, fmt.Errorf("read stdin envelope: %w", err)
		}
		data = b
	case strings.TrimSpace(inline) != "":
		data = []byte(inline)
	default:
		data = []byte("{}")
	}
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		data = []byte("{}")
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("params must be valid JSON")
	}
	return json.RawMessage(data), nil
}

func herdrProtocolErrorCode(err *herdrbridge.HerdrError) protocol.ErrorCode {
	if err == nil {
		return protocol.ErrorCodeERuntime
	}
	switch strings.TrimSpace(err.Code) {
	case "pane_not_found", "workspace_not_found", "tab_not_found":
		return protocol.ErrorCodeENotFound
	case "invalid_key", "invalid_request", "invalid_params":
		return protocol.ErrorCodeEARG
	case "timeout":
		return protocol.ErrorCodeETimeout
	default:
		return protocol.ErrorCodeERuntime
	}
}

func init() {
	rootCmd.AddCommand(newHerdrCommand())
}
