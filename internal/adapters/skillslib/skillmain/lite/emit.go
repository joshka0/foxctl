package lite

import (
	"fmt"
	"io"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// Emit writes a success envelope to stdout with standard metadata.
func Emit(rc *RunContext, command string, data any) error {
	return EmitWithMeta(rc, command, data, envelope.Meta{})
}

// EmitWithMeta writes a success envelope with custom metadata.
func EmitWithMeta(rc *RunContext, command string, data any, meta envelope.Meta) error {
	env := envelope.OK(command, data, envelope.WithMeta(meta))
	return envelope.Write(rc.Stdout, env)
}

// Fatal emits an error envelope. Use this for skills with custom main().
func Fatal(w io.Writer, command string, err *skillerr.Error) error {
	env := envelope.Error(command, err.Code, err.Message, err.ToEnvelopeData())
	return envelope.Write(w, env)
}

// emitError writes an error envelope to w for the given command and skill error.
func emitError(w io.Writer, command string, err *skillerr.Error) {
	appendUsageHint(command, err)
	env := envelope.Error(command, err.Code, err.Message, err.ToEnvelopeData())
	_ = envelope.Write(w, env)
}

func appendUsageHint(command string, err *skillerr.Error) {
	if err == nil {
		return
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	usage := fmt.Sprintf("For examples, run: foxctl run %s --examples", command)
	if err.Hint == "" {
		err.Hint = usage
		return
	}
	if !strings.Contains(err.Hint, "--examples") {
		err.Hint = strings.TrimSpace(err.Hint + " " + usage)
	}
}
