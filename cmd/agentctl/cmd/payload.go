package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// readPayload reads payload data from the given sources and returns its bytes.
// It reads from stdin when file == "-" or when neither file nor data are provided, reads from the file path when file != "", or returns the bytes of data when data != "".
// If stdin is a terminal, it returns an error instructing the caller to provide --data, --payload-file <path>, or pipe input; file read failures are wrapped with "read <path>: <err>".
func readPayload(cmd *cobra.Command, file, data string) ([]byte, error) {
	switch {
	case file == "-":
		in := cmd.InOrStdin()
		if isTerminalReader(in) {
			return nil, fmt.Errorf("stdin is a terminal; provide --data, --payload-file <path>, or pipe input into --payload-file -")
		}
		return io.ReadAll(in)
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		return b, nil
	case data != "":
		return []byte(data), nil
	default:
		in := cmd.InOrStdin()
		if isTerminalReader(in) {
			return nil, fmt.Errorf("stdin is a terminal; provide --data, --payload-file <path>, or pipe input into stdin")
		}
		return io.ReadAll(in)
	}
}

func requireValidJSON(b []byte) error {
	if !json.Valid(b) {
		return fmt.Errorf("invalid JSON")
	}
	return nil
}