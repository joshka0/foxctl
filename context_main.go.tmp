// Package runner provides helpers for building skill runners.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cas"
)

// Context bundles dependencies skill binaries need.
type Context struct {
	Config        config.Config
	CASStore      *cas.Store
	PathValidator *policy.PathValidator
	InlineKB      int
	Stdout        io.Writer
	Now           func() time.Time
	MaxPreview    int
}

// NewContext initializes a Context from configuration.
func NewContext(cfg config.Config, stdout io.Writer) (*Context, error) {
	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return nil, fmt.Errorf("skillslib: cas store: %w", err)
	}

	workspace := strings.TrimSpace(os.Getenv("AGENTCTL_WORKSPACE"))
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("skillslib: resolve workspace: %w", err)
		}
	}

	var allowedRoots []string
	if cfg.Home != "" {
		allowedRoots = append(allowedRoots, cfg.Home)
	}
	if tmp := os.TempDir(); tmp != "" {
		allowedRoots = append(allowedRoots, tmp)
	}

	pathValidator, err := policy.NewPathValidator(workspace, allowedRoots)
	if err != nil {
		return nil, fmt.Errorf("skillslib: path validator: %w", err)
	}

	return &Context{
		Config:        cfg,
		CASStore:      store,
		PathValidator: pathValidator,
		InlineKB:      cfg.InlineOutputKB,
		Stdout:        stdout,
		Now:           time.Now,
		MaxPreview:    5,
	}, nil
}

// Close releases resources held by the runner context.
func (c *Context) Close() error {
	// CAS store currently has no Close; placeholder for future resources.
	return nil
}

// Emit OK envelope with automatic CAS wrapping for large payloads.
// The third parameter (contentType) is currently unused and reserved for future use.
func (c *Context) Emit(command string, data any, _ string, meta envelope.Meta) error {
	if meta.CASDigest == "" {
		if digest := artifactDigest(data); digest != "" {
			meta.CASDigest = digest
		}
	}
	env := envelope.OK(command, data, envelope.WithMeta(meta))
	return envelope.Write(c.Stdout, env)
}

// Artifact describes a stored CAS object.
type Artifact struct {
	Digest string
	Size   int64
	Kind   string
}

// PersistJSON marshals value to JSON and stores it in CAS.
func PersistJSON(ctx context.Context, c *Context, value any, tags ...string) (Artifact, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return Artifact{}, fmt.Errorf("skillslib: marshal json: %w", err)
	}
	return PersistBuffer(ctx, c, bytes.NewBuffer(payload), "application/json", tags...)
}

// PersistBuffer streams the provided buffer into CAS.
func PersistBuffer(ctx context.Context, c *Context, buf *bytes.Buffer, kind string, tags ...string) (Artifact, error) {
	if buf == nil {
		return Artifact{}, fmt.Errorf("skillslib: persist buffer: nil buffer")
	}
	obj, err := c.CASStore.Put(ctx, bytes.NewReader(buf.Bytes()), kind, tags)
	if err != nil {
		return Artifact{}, fmt.Errorf("skillslib: cas put: %w", err)
	}
	return Artifact{Digest: obj.Digest, Size: obj.Size, Kind: obj.Kind}, nil
}

func artifactDigest(data any) string {
	if data == nil {
		return ""
	}
	switch v := data.(type) {
	case map[string]any:
		return extractArtifact(v)
	default:
		val := reflect.ValueOf(data)
		if val.Kind() == reflect.Map && val.Type().Key().Kind() == reflect.String {
			iter := val.MapRange()
			for iter.Next() {
				if iter.Key().String() == "artifact" {
					if s, ok := iter.Value().Interface().(string); ok && strings.HasPrefix(s, "sha256:") {
						return s
					}
				}
			}
		}
	}
	return ""
}

func extractArtifact(m map[string]any) string {
	val, ok := m["artifact"]
	if !ok {
		return ""
	}
	if digest, ok := val.(string); ok && strings.HasPrefix(digest, "sha256:") {
		return digest
	}
	return ""
}
