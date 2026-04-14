package plugin

import (
	"fmt"

	"github.com/jkatigb/agentctl/internal/protocol"
)

// InvocationError represents a plugin invocation failure surfaced to callers.
type InvocationError struct {
	Code    protocol.ErrorCode
	Message string
	Details map[string]any
	Cause   error
}

// Error implements the error interface.
func (e *InvocationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code.String()
	}
	return "plugin invocation failed"
}

// Unwrap exposes the underlying cause for errors.Is/As.
func (e *InvocationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newInvocationError(code protocol.ErrorCode, message string, cause error, details map[string]any) *InvocationError {
	if details == nil {
		details = map[string]any{}
	}
	if code == "" {
		code = protocol.ErrorCodeERuntime
	}
	if message == "" {
		message = fmt.Sprintf("plugin error (%s)", code)
	}
	return &InvocationError{
		Code:    code,
		Message: message,
		Details: details,
		Cause:   cause,
	}
}
