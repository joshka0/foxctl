package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func readPayload(cmd *cobra.Command, file, data string) ([]byte, error) {
	switch {
	case file == "-":
		return io.ReadAll(cmd.InOrStdin())
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		return b, nil
	case data != "":
		return []byte(data), nil
	default:
		return io.ReadAll(cmd.InOrStdin())
	}
}

func requireValidJSON(b []byte) error {
	if !json.Valid(b) {
		return fmt.Errorf("invalid JSON")
	}
	return nil
}
