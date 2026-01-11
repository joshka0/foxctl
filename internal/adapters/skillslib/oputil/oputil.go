// Package oputil provides operation dispatch helpers for multi-operation skills.
// Many skills support multiple operations (list, get, add, remove, etc.) and
// this package provides utilities to normalize, validate, and dispatch operations.
package oputil

import (
	"fmt"
	"sort"
	"strings"
)

// Op normalizes an operation string (trims whitespace, lowercases).
func Op(op string) string {
	return strings.ToLower(strings.TrimSpace(op))
}

// Validate checks if an operation is in the allowed set.
// Returns an error with allowed operations if invalid.
//
// Example:
//
//	if err := oputil.Validate(in.Operation, "list", "get", "add", "remove"); err != nil {
//	    return err
//	}
func Validate(op string, allowed ...string) error {
	normalized := Op(op)
	for _, a := range allowed {
		if normalized == a {
			return nil
		}
	}
	return &InvalidOpError{
		Operation: op,
		Allowed:   allowed,
	}
}

// InvalidOpError is returned when an operation is not in the allowed set.
type InvalidOpError struct {
	Operation string
	Allowed   []string
}

func (e *InvalidOpError) Error() string {
	if len(e.Allowed) == 0 {
		return fmt.Sprintf("invalid operation: %q", e.Operation)
	}
	sorted := make([]string, len(e.Allowed))
	copy(sorted, e.Allowed)
	sort.Strings(sorted)
	return fmt.Sprintf("invalid operation: %q (must be one of: %s)", e.Operation, strings.Join(sorted, ", "))
}

// Require returns a non-empty error if the string is empty.
// Useful for validating required fields based on operation.
//
// Example:
//
//	if err := oputil.Require(in.Path, "path"); err != nil {
//	    return err
//	}
func Require(value, fieldName string) error {
	if strings.TrimSpace(value) == "" {
		return &MissingFieldError{Field: fieldName}
	}
	return nil
}

// RequireInt returns an error if the int is zero.
func RequireInt(value int, fieldName string) error {
	if value == 0 {
		return &MissingFieldError{Field: fieldName}
	}
	return nil
}

// RequirePtr returns an error if the pointer is nil.
func RequirePtr[T any](ptr *T, fieldName string) error {
	if ptr == nil {
		return &MissingFieldError{Field: fieldName}
	}
	return nil
}

// MissingFieldError is returned when a required field is empty.
type MissingFieldError struct {
	Field string
}

func (e *MissingFieldError) Error() string {
	return fmt.Sprintf("missing required field: %s", e.Field)
}

// Handler is a function that handles an operation and returns result data.
type Handler func() (map[string]any, error)

// HandlerWithContext is a handler that receives arbitrary context.
type HandlerWithContext[C any] func(ctx C) (map[string]any, error)

// Dispatcher manages operation routing.
type Dispatcher struct {
	handlers map[string]Handler
	aliases  map[string]string
}

// NewDispatcher creates a new operation dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		handlers: make(map[string]Handler),
		aliases:  make(map[string]string),
	}
}

// On registers a handler for an operation.
func (d *Dispatcher) On(op string, handler Handler) *Dispatcher {
	d.handlers[Op(op)] = handler
	return d
}

// Alias registers an alias for an operation (e.g., "ls" -> "list").
func (d *Dispatcher) Alias(alias, target string) *Dispatcher {
	d.aliases[Op(alias)] = Op(target)
	return d
}

// Dispatch executes the handler for the given operation.
// Returns the result or an error if the operation is invalid.
func (d *Dispatcher) Dispatch(op string) (map[string]any, error) {
	normalized := Op(op)

	// Check for alias
	if target, ok := d.aliases[normalized]; ok {
		normalized = target
	}

	handler, ok := d.handlers[normalized]
	if !ok {
		return nil, &InvalidOpError{
			Operation: op,
			Allowed:   d.AllowedOps(),
		}
	}

	return handler()
}

// AllowedOps returns the list of registered operations.
func (d *Dispatcher) AllowedOps() []string {
	ops := make([]string, 0, len(d.handlers))
	for op := range d.handlers {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	return ops
}

// DefaultOp returns the operation to use if none is provided.
// If op is empty, returns defaultOp; otherwise returns op.
func DefaultOp(op, defaultOp string) string {
	if strings.TrimSpace(op) == "" {
		return defaultOp
	}
	return op
}

// RequireForOp validates that a field is set for specific operations.
// Returns nil if the operation doesn't require the field.
//
// Example:
//
//	// "path" is required for "add" and "remove" operations
//	if err := oputil.RequireForOp(in.Operation, in.Path, "path", "add", "remove"); err != nil {
//	    return err
//	}
func RequireForOp(op, value, fieldName string, requiredOps ...string) error {
	normalized := Op(op)
	for _, reqOp := range requiredOps {
		if normalized == reqOp {
			return Require(value, fieldName)
		}
	}
	return nil
}

// RequireIntForOp validates that an int field is set for specific operations.
func RequireIntForOp(op string, value int, fieldName string, requiredOps ...string) error {
	normalized := Op(op)
	for _, reqOp := range requiredOps {
		if normalized == reqOp {
			return RequireInt(value, fieldName)
		}
	}
	return nil
}

// Switch is a fluent helper for simple operation dispatch.
// Returns the result of the matching case, or an error for unknown operations.
//
// Example:
//
//	return oputil.Switch(in.Operation).
//	    Case("list", func() (map[string]any, error) { return listItems() }).
//	    Case("add", func() (map[string]any, error) { return addItem(in) }).
//	    Default("list").
//	    Run()
type Switch struct {
	op          string
	cases       map[string]Handler
	defaultOp   string
	aliases     map[string]string
}

// NewSwitch creates a new switch for the given operation.
func NewSwitch(op string) *Switch {
	return &Switch{
		op:      Op(op),
		cases:   make(map[string]Handler),
		aliases: make(map[string]string),
	}
}

// Case registers a handler for an operation.
func (s *Switch) Case(op string, handler Handler) *Switch {
	s.cases[Op(op)] = handler
	return s
}

// Alias registers an alias (e.g., "ls" -> "list").
func (s *Switch) Alias(alias, target string) *Switch {
	s.aliases[Op(alias)] = Op(target)
	return s
}

// Default sets the operation to use if none is provided.
func (s *Switch) Default(op string) *Switch {
	s.defaultOp = Op(op)
	return s
}

// Run executes the matching handler.
func (s *Switch) Run() (map[string]any, error) {
	op := s.op
	if op == "" && s.defaultOp != "" {
		op = s.defaultOp
	}

	// Check for alias
	if target, ok := s.aliases[op]; ok {
		op = target
	}

	handler, ok := s.cases[op]
	if !ok {
		allowed := make([]string, 0, len(s.cases))
		for k := range s.cases {
			allowed = append(allowed, k)
		}
		return nil, &InvalidOpError{Operation: s.op, Allowed: allowed}
	}

	return handler()
}
