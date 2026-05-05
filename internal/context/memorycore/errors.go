package memorycore

import "fmt"

type ErrInvalidKind struct {
	Kind string
}

func (e ErrInvalidKind) Error() string {
	return fmt.Sprintf("memory record kind %q is not valid", e.Kind)
}

type ErrInvalidLifecycleState struct {
	State string
}

func (e ErrInvalidLifecycleState) Error() string {
	return fmt.Sprintf("memory lifecycle state %q is not valid", e.State)
}
