package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newProtoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proto",
		Short: "Protocol v1 utilities",
	}
	cmd.AddCommand(newProtoValidateCommand())
	return cmd
}

func newProtoValidateCommand() *cobra.Command {
	var inputPath string
	var strict bool
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate an envelope against Protocol v1",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			opts := func() []protocol.Option {
				duration := time.Since(start).Milliseconds()
				options := []protocol.Option{protocol.WithSource("proto")}
				if duration > 0 {
					options = append(options, protocol.WithDuration(duration))
				}
				return options
			}

			inputDesc := "stdin"
			var reader io.Reader
			if inputPath == "-" {
				reader = cmd.InOrStdin()
			} else {
				file, err := os.Open(filepath.Clean(inputPath))
				if err != nil {
					payload := map[string]any{"input": inputPath, "hint": "Provide a readable JSON envelope"}
					if writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.proto.validate", protocol.ErrorCodeEEnvelope, fmt.Sprintf("open input: %v", err), payload, opts()...); writeErr != nil {
						return fmt.Errorf("write envelope: %w", writeErr)
					}
					return fmt.Errorf("open input: %w", err)
				}
				defer func() {
					if err := file.Close(); err != nil {
						cmd.PrintErrf("close input %s: %v\n", inputPath, err)
					}
				}()
				reader = file
				inputDesc = inputPath
			}

			raw, err := io.ReadAll(reader)
			if err != nil {
				payload := map[string]any{"input": inputDesc}
				if writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.proto.validate", protocol.ErrorCodeEEnvelope, fmt.Sprintf("read input: %v", err), payload, opts()...); writeErr != nil {
					return fmt.Errorf("write envelope: %w", writeErr)
				}
				return fmt.Errorf("read input: %w", err)
			}
			if len(bytes.TrimSpace(raw)) == 0 {
				payload := map[string]any{"input": inputDesc, "hint": "Envelope input cannot be empty"}
				if writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.proto.validate", protocol.ErrorCodeEEnvelope, "no input provided", payload, opts()...); writeErr != nil {
					return fmt.Errorf("write envelope: %w", writeErr)
				}
				return fmt.Errorf("no input provided")
			}

			dec := json.NewDecoder(bytes.NewReader(raw))
			if strict {
				dec.DisallowUnknownFields()
			}
			var env envelope.Envelope
			if err := dec.Decode(&env); err != nil {
				payload := map[string]any{"input": inputDesc, "hint": "Ensure the payload is valid JSON"}
				if writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.proto.validate", protocol.ErrorCodeEEnvelope, fmt.Sprintf("decode envelope: %v", err), payload, opts()...); writeErr != nil {
					return fmt.Errorf("write envelope: %w", writeErr)
				}
				return fmt.Errorf("decode envelope: %w", err)
			}
			if strict {
				consumed := int(dec.InputOffset())
				if consumed < len(raw) {
					trailing := bytes.TrimSpace(raw[consumed:])
					if len(trailing) > 0 {
						payload := map[string]any{
							"input":    inputDesc,
							"hint":     "Remove trailing data when using --strict",
							"trailing": string(trailing),
						}
						if writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.proto.validate", protocol.ErrorCodeEEnvelope, "unexpected trailing data after envelope", payload, opts()...); writeErr != nil {
							return fmt.Errorf("write envelope: %w", writeErr)
						}
						return fmt.Errorf("unexpected trailing data after envelope")
					}
				}
			}

			if err := protocol.Validate(env); err != nil {
				payload := map[string]any{"input": inputDesc, "error": err.Error()}
				if strict {
					payload["strict"] = true
				}
				if writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.proto.validate", protocol.ErrorCodeEEnvelope, "protocol validation failed", payload, opts()...); writeErr != nil {
					return fmt.Errorf("write envelope: %w", writeErr)
				}
				return fmt.Errorf("protocol validation failed: %w", err)
			}

			payload := map[string]any{
				"valid":   true,
				"input":   inputDesc,
				"command": env.Command,
				"status":  env.Status,
			}
			if strict {
				payload["strict"] = true
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.proto.validate", payload, opts()...)
		},
	}
	cmd.Flags().StringVar(&inputPath, "input", "-", "Input file (- for stdin)")
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail on unknown fields and trailing data")
	return cmd
}

func init() {
	rootCmd.AddCommand(newProtoCommand())
}
