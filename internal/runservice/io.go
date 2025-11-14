package runservice

import (
	"bytes"
	"io"
)

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
