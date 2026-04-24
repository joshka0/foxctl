package repl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

var (
	errYaegiSessionNotInitialized = errors.New("yaegi session is not initialized")
	errYaegiSessionClosed         = errors.New("yaegi session is closed")
	errYaegiSessionBroken         = errors.New("yaegi session is broken")
)

// YaegiOptions configure YaegiSession behavior.
type YaegiOptions struct {
	// MaxOutputBytes caps captured stdout/stderr/result/error text per execution call.
	MaxOutputBytes int
}

// YaegiSession is an in-process Yaegi-backed implementation of rlm.Sandbox.
//
// Security note: this is not a hard security sandbox. Executed code runs
// in-process with the current OS/user permissions.
type YaegiSession struct {
	mu             sync.Mutex
	maxOutputBytes int

	interpreter  *interp.Interpreter
	stdoutRouter *outputRouter
	stderrRouter *outputRouter
	state        map[string]any
	knownNames   map[string]struct{}
	closed       bool
	broken       bool
}

var _ rlm.Sandbox = (*YaegiSession)(nil)

// NewYaegiSession creates a new uninitialized in-process Yaegi session.
func NewYaegiSession(opts YaegiOptions) *YaegiSession {
	maxOutputBytes := opts.MaxOutputBytes
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultMaxOutputBytes
	}
	return &YaegiSession{
		maxOutputBytes: maxOutputBytes,
		stdoutRouter:   newOutputRouter(),
		stderrRouter:   newOutputRouter(),
		state:          map[string]any{},
		knownNames:     map[string]struct{}{},
	}
}

// Init prepares a Yaegi interpreter and binds initial state into globals.
func (s *YaegiSession) Init(ctx context.Context, state map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errYaegiSessionClosed
	}
	if s.interpreter != nil {
		return errors.New("yaegi session already initialized")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("yaegi init canceled: %w", err)
	}
	if err := validateGoInitState(state); err != nil {
		return err
	}

	engine := interp.New(interp.Options{
		Stdout: s.stdoutRouter,
		Stderr: s.stderrRouter,
	})
	if err := engine.Use(stdlib.Symbols); err != nil {
		return fmt.Errorf("load yaegi stdlib symbols: %w", err)
	}
	if _, err := engine.Eval(`import "encoding/json"`); err != nil {
		return fmt.Errorf("prepare yaegi json helpers: %w", err)
	}

	initState := state
	if initState == nil {
		initState = map[string]any{}
	}

	mirroredState := make(map[string]any, len(initState))
	knownNames := make(map[string]struct{}, len(initState))
	for key, value := range initState {
		rawValue, normalized, err := marshalAndNormalize(value)
		if err != nil {
			return fmt.Errorf("init state key %q is not JSON-serializable: %w", key, err)
		}

		bindStmt := fmt.Sprintf("var %s any = func() any { var v any; _ = json.Unmarshal([]byte(%q), &v); return v }()", key, rawValue)
		if _, err := engine.Eval(bindStmt); err != nil {
			return fmt.Errorf("bind init state key %q: %w", key, err)
		}
		mirroredState[key] = normalized
		knownNames[key] = struct{}{}
	}

	s.interpreter = engine
	s.state = mirroredState
	s.knownNames = knownNames
	s.broken = false
	return nil
}

// Execute evaluates Go code in the persistent Yaegi interpreter.
func (s *YaegiSession) Execute(ctx context.Context, code string) (rlm.ExecResult, error) {
	start := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return rlm.ExecResult{}, errYaegiSessionClosed
	}
	if s.interpreter == nil {
		return rlm.ExecResult{}, errYaegiSessionNotInitialized
	}
	if s.broken {
		return rlm.ExecResult{}, errYaegiSessionBroken
	}
	if err := ctx.Err(); err != nil {
		return rlm.ExecResult{}, fmt.Errorf("yaegi execution canceled: %w", err)
	}

	stdout := newLimitedTextBuffer(s.maxOutputBytes)
	stderr := newLimitedTextBuffer(s.maxOutputBytes)
	s.stdoutRouter.SetTarget(stdout)
	s.stderrRouter.SetTarget(stderr)

	value, evalErr := s.safeEvalLocked(ctx, code)

	s.stdoutRouter.ClearTarget()
	s.stderrRouter.ClearTarget()

	for name := range collectTrackedIdentifiers(code) {
		s.knownNames[name] = struct{}{}
	}
	s.refreshStateFromInterpreterLocked()

	resultText := ""
	errorText := ""
	truncated := map[string]bool{}
	if stdout.Truncated() {
		truncated["stdout"] = true
	}
	if stderr.Truncated() {
		truncated["stderr"] = true
	}

	if evalErr == nil {
		resultText, _ = stringifyEvalValue(value)
		var resultTruncated bool
		resultText, resultTruncated = truncateStringByBytes(resultText, s.maxOutputBytes)
		if resultTruncated {
			truncated["result"] = true
		}
	} else {
		var errTruncated bool
		errorText, errTruncated = truncateStringByBytes(evalErr.Error(), s.maxOutputBytes)
		if errTruncated {
			truncated["error"] = true
		}
	}

	execMetadata := map[string]any{
		"ok":     evalErr == nil,
		"stdout": stdout.String(),
		"stderr": stderr.String(),
		"result": resultText,
	}
	if errorText != "" {
		execMetadata["error"] = errorText
	}
	if len(truncated) > 0 {
		execMetadata["truncated"] = truncated
	}

	return rlm.ExecResult{
		Output:     formatOutput(stdout.String(), stderr.String(), resultText, errorText),
		DurationMS: time.Since(start).Milliseconds(),
		ExecutedAt: start,
		Metadata:   execMetadata,
	}, nil
}

// Snapshot returns a best-effort JSON-serializable snapshot of known globals.
func (s *YaegiSession) Snapshot(ctx context.Context) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, errYaegiSessionClosed
	}
	if s.interpreter == nil {
		return nil, errYaegiSessionNotInitialized
	}
	if s.broken {
		return nil, errYaegiSessionBroken
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("yaegi snapshot canceled: %w", err)
	}

	s.refreshStateFromInterpreterLocked()
	snapshot := make(map[string]any, len(s.state))
	for key, value := range s.state {
		snapshot[key] = value
	}
	return snapshot, nil
}

// Close marks the session closed and releases interpreter references.
func (s *YaegiSession) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("yaegi close canceled: %w", err)
	}

	s.closed = true
	s.interpreter = nil
	s.stdoutRouter.ClearTarget()
	s.stderrRouter.ClearTarget()
	s.state = nil
	s.knownNames = nil
	return nil
}

func (s *YaegiSession) safeEvalLocked(ctx context.Context, code string) (value reflect.Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.broken = true
			err = fmt.Errorf("yaegi evaluation panic: %v", recovered)
		}
	}()
	return s.interpreter.EvalWithContext(ctx, code)
}

func (s *YaegiSession) refreshStateFromInterpreterLocked() {
	if s.interpreter == nil {
		return
	}
	for name := range s.knownNames {
		value, err := s.interpreter.Eval(name)
		if err != nil {
			continue
		}
		serialized, ok := reflectValueToJSON(value)
		if !ok {
			continue
		}
		s.state[name] = serialized
	}
}

func validateGoInitState(state map[string]any) error {
	for key := range state {
		if strings.TrimSpace(key) == "" {
			return errors.New("init state contains empty key")
		}
		if !token.IsIdentifier(key) || token.Lookup(key).IsKeyword() {
			return fmt.Errorf("init state key %q is not a valid Go identifier", key)
		}
	}
	return nil
}

func marshalAndNormalize(value any) (raw string, normalized any, err error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return "", nil, err
	}
	return string(encoded), normalized, nil
}

func stringifyEvalValue(value reflect.Value) (string, bool) {
	if !value.IsValid() {
		return "", false
	}
	defer func() {
		_ = recover()
	}()
	typed := value.Interface()
	if typed == nil {
		return "", false
	}
	return fmt.Sprintf("%#v", typed), true
}

func reflectValueToJSON(value reflect.Value) (any, bool) {
	if !value.IsValid() {
		return nil, false
	}
	var converted any
	func() {
		defer func() {
			_ = recover()
		}()
		converted = value.Interface()
	}()
	if converted == nil {
		return nil, true
	}
	encoded, err := json.Marshal(converted)
	if err != nil {
		return nil, false
	}
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, false
	}
	return normalized, true
}

func truncateStringByBytes(text string, maxBytes int) (string, bool) {
	if maxBytes <= 0 {
		return text, false
	}
	raw := []byte(text)
	if len(raw) <= maxBytes {
		return text, false
	}
	return string(raw[:maxBytes]), true
}

func collectTrackedIdentifiers(code string) map[string]struct{} {
	identifiers := make(map[string]struct{})
	if strings.TrimSpace(code) == "" {
		return identifiers
	}

	parse := func(src string) (*ast.File, error) {
		return parser.ParseFile(token.NewFileSet(), "yaegi-snippet.go", src, 0)
	}

	file, err := parse("package repl\nfunc __foxctl_capture(){\n" + code + "\n}")
	if err != nil {
		file, err = parse("package repl\n" + code + "\n")
		if err != nil {
			return identifiers
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			for _, expr := range typed.Lhs {
				identifier, ok := expr.(*ast.Ident)
				if !ok {
					continue
				}
				maybeTrackIdentifier(identifier.Name, identifiers)
			}
		case *ast.RangeStmt:
			if typed.Key != nil {
				if keyIdent, ok := typed.Key.(*ast.Ident); ok {
					maybeTrackIdentifier(keyIdent.Name, identifiers)
				}
			}
			if typed.Value != nil {
				if valueIdent, ok := typed.Value.(*ast.Ident); ok {
					maybeTrackIdentifier(valueIdent.Name, identifiers)
				}
			}
		case *ast.ValueSpec:
			for _, name := range typed.Names {
				maybeTrackIdentifier(name.Name, identifiers)
			}
		}
		return true
	})

	return identifiers
}

func maybeTrackIdentifier(name string, identifiers map[string]struct{}) {
	if name == "" || name == "_" || strings.HasPrefix(name, "__foxctl_") {
		return
	}
	identifiers[name] = struct{}{}
}

type outputRouter struct {
	mu     sync.Mutex
	target *limitedTextBuffer
}

func newOutputRouter() *outputRouter {
	return &outputRouter{}
}

func (r *outputRouter) SetTarget(target *limitedTextBuffer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.target = target
}

func (r *outputRouter) ClearTarget() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.target = nil
}

func (r *outputRouter) Write(p []byte) (int, error) {
	r.mu.Lock()
	target := r.target
	r.mu.Unlock()
	if target == nil {
		return len(p), nil
	}
	return target.Write(p)
}

type limitedTextBuffer struct {
	mu        sync.Mutex
	max       int
	buf       []byte
	truncated bool
}

func newLimitedTextBuffer(max int) *limitedTextBuffer {
	return &limitedTextBuffer{max: max}
}

func (b *limitedTextBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.max <= 0 {
		b.buf = append(b.buf, p...)
		return len(p), nil
	}
	remaining := b.max - len(b.buf)
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) <= remaining {
		b.buf = append(b.buf, p...)
		return len(p), nil
	}
	b.buf = append(b.buf, p[:remaining]...)
	b.truncated = true
	return len(p), nil
}

func (b *limitedTextBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func (b *limitedTextBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
