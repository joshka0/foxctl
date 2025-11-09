package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

func resolveSkillBinary(name string) (string, error) {
	bin := strings.ReplaceAll(name, "/", "_")
	candidates := []string{
		filepath.Join("dist", "skills", bin, bin),
		filepath.Join("skills", bin, bin),
	}
	for _, cand := range candidates {
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("skill binary not found for %s (expected %v)", name, candidates)
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
