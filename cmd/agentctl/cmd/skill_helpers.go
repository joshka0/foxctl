package cmd

import (
	"bytes"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func loadSkillInput(cmd *cobra.Command, inline, file string) ([]byte, error) {
	switch {
	case file == "-":
		return io.ReadAll(cmd.InOrStdin())
	case file != "":
		return os.ReadFile(file)
	case inline != "":
		return []byte(inline), nil
	default:
		return []byte("{}"), nil
	}
}

func writeEnvelope(out io.Writer, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	_, err := out.Write(data)
	return err
}
