// Package skillrun provides generic skill resolution, execution, and envelope
// decoding helpers. It belongs to the reusable tooling family rather than to
// runtime-facing agent tooling.
package skillrun

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/runtime/execution/runner"
	"github.com/jkatigb/agentctl/internal/protocol"
)

// Resolver abstracts skill resolution for callers that don't need the full Resolver type.
type Resolver interface {
	Resolve(nameOrPath string) (skill.Handle, error)
}

// Options configure a skill run.
type Options struct {
	PreferCGO bool
	EntryRoot string
}

// Result captures a decoded skill response plus execution context.
type Result struct {
	Handle       skill.Handle
	Manifest     skill.Manifest
	ArtifactPath string
	Stdout       []byte
	Stderr       []byte
	Data         map[string]any
}

// RunError captures a skill execution error with stderr for debugging.
type RunError struct {
	Err    error
	Stderr []byte
}

func (e RunError) Error() string {
	return e.Err.Error()
}

func (e RunError) Unwrap() error {
	return e.Err
}

// RunAndDecode resolves, executes, and decodes a skill envelope response.
func RunAndDecode(ctx context.Context, resolver Resolver, skillName string, input any, opts Options) (Result, error) {
	var data map[string]any
	result, err := RunAndDecodeInto(ctx, resolver, skillName, input, opts, &data)
	if err != nil {
		return result, err
	}
	if data == nil {
		data = map[string]any{}
	}
	result.Data = data
	return result, nil
}

func marshalInput(input any) ([]byte, error) {
	if inputBytes, ok := input.([]byte); ok {
		return inputBytes, nil
	}
	return json.Marshal(input)
}

// RunAndDecodeInto resolves, executes, and decodes a skill envelope response into dst.
func RunAndDecodeInto(ctx context.Context, resolver Resolver, skillName string, input any, opts Options, dst any) (Result, error) {
	if resolver == nil {
		return Result{}, fmt.Errorf("resolver is required")
	}
	if dst == nil {
		return Result{}, fmt.Errorf("destination is required")
	}
	handle, err := resolver.Resolve(skillName)
	if err != nil {
		return Result{}, fmt.Errorf("resolve skill %s: %w", skillName, err)
	}

	inputBytes, err := marshalInput(input)
	if err != nil {
		return Result{Handle: handle}, fmt.Errorf("marshal input: %w", err)
	}

	manifest, artifactPath, err := skill.LoadManifestAndArtifact(handle.ManifestPath, skill.ArtifactOptions{
		PreferCGO: opts.PreferCGO,
		EntryRoot: opts.EntryRoot,
	})
	if err != nil {
		return Result{Handle: handle}, err
	}

	stdout, stderr, err := runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        inputBytes,
	})
	result := Result{
		Handle:       handle,
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Stdout:       stdout,
		Stderr:       stderr,
	}
	if err != nil {
		return result, RunError{Err: fmt.Errorf("run skill: %w", err), Stderr: stderr}
	}

	if err := protocol.DecodeEnvelopeInto(stdout, dst); err != nil {
		return result, fmt.Errorf("decode envelope: %w", err)
	}

	return result, nil
}
