// Package cas implements the content-addressable storage used by agentctl.
package cas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNotFound is returned when the requested object is missing.
	ErrNotFound = errors.New("cas: object not found")
	// ErrDigestMismatch indicates the stored content does not match the expected digest.
	ErrDigestMismatch = errors.New("cas: digest mismatch")
	// ErrPinned indicates an object cannot be removed because it is pinned.
	ErrPinned = errors.New("cas: object pinned")
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Store manages content-addressable objects rooted at a filesystem path.
type Store struct {
	root string
	mu   sync.Mutex
}

// Metadata describes a stored object.
type Metadata struct {
	Digest    string    `json:"digest"`
	Size      int64     `json:"size_bytes"`
	Kind      string    `json:"kind,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Object augments metadata with pinning info.
type Object struct {
	Metadata
	Pinned bool `json:"pinned"`
}

// NewStore initializes a Store rooted at the provided directory.
func NewStore(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, "sha256"), 0o755); err != nil {
		return nil, fmt.Errorf("cas: create store: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pins"), 0o755); err != nil {
		return nil, fmt.Errorf("cas: create pins: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o755); err != nil {
		return nil, fmt.Errorf("cas: create tmp: %w", err)
	}
	return &Store{root: root}, nil
}

// Put streams data into the store, returning the metadata for the resulting object.
func (s *Store) Put(ctx context.Context, r io.Reader, kind string, tags []string) (Object, error) {
	if kind == "" {
		kind = "application/octet-stream"
	}
	h := sha256.New()
	tmp, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "cas-*")
	if err != nil {
		return Object{}, fmt.Errorf("cas: temp file: %w", err)
	}
	defer func() {
		_ = os.Remove(tmp.Name())
	}()
	var size int64
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return Object{}, err
		}
		n, readErr := r.Read(buf)
		if n > 0 {
			if _, err := h.Write(buf[:n]); err != nil {
				return Object{}, fmt.Errorf("cas: hash: %w", err)
			}
			if _, err := tmp.Write(buf[:n]); err != nil {
				return Object{}, fmt.Errorf("cas: write: %w", err)
			}
			size += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return Object{}, fmt.Errorf("cas: read input: %w", readErr)
		}
	}
	digest := "sha256:" + hex.EncodeToString(h.Sum(nil))
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	dest := filepath.Join(s.root, "sha256", hexDigest)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(dest); err == nil {
		// already exists; update metadata with merged tags if needed
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		meta, err := s.readMetadata(digest)
		if err != nil {
			return Object{}, err
		}
		merged := mergeTags(meta.Tags, tags)
		if len(merged) != len(meta.Tags) {
			meta.Tags = merged
			if err := s.writeMetadata(meta); err != nil {
				return Object{}, err
			}
		}
		return s.objectFromMeta(meta)
	}
	if err := tmp.Close(); err != nil {
		return Object{}, fmt.Errorf("cas: close temp: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Object{}, fmt.Errorf("cas: ensure dir: %w", err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return Object{}, fmt.Errorf("cas: final write: %w", err)
	}
	meta := Metadata{
		Digest:    digest,
		Size:      size,
		Kind:      kind,
		Tags:      mergeTags(nil, tags),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.writeMetadata(meta); err != nil {
		return Object{}, err
	}
	return s.objectFromMeta(meta)
}

// Head returns metadata for a digest.
func (s *Store) Head(_ context.Context, digest string) (Object, error) {
	meta, err := s.readMetadata(digest)
	if err != nil {
		return Object{}, err
	}
	return s.objectFromMeta(meta)
}

// Get opens a validating reader for the provided digest.
func (s *Store) Get(_ context.Context, digest string) (io.ReadCloser, Metadata, error) {
	path, err := s.pathForDigest(digest)
	if err != nil {
		return nil, Metadata{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, Metadata{}, ErrNotFound
		}
		return nil, Metadata{}, fmt.Errorf("cas: open: %w", err)
	}
	meta, err := s.readMetadata(digest)
	if err != nil {
		_ = file.Close()
		return nil, Metadata{}, err
	}
	vr := &verifyingReader{
		f:        file,
		expected: digest,
		h:        sha256.New(),
	}
	return vr, meta, nil
}

// List returns metadata for all stored digests.
func (s *Store) List(_ context.Context) ([]Object, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "sha256"))
	if err != nil {
		return nil, fmt.Errorf("cas: list: %w", err)
	}
	var objects []Object
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if len(entry.Name()) != 64 {
			continue
		}
		metaPath := filepath.Join(s.root, "sha256", entry.Name()+".json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta Metadata
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		obj, err := s.objectFromMeta(meta)
		if err != nil {
			continue
		}
		objects = append(objects, obj)
	}
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].CreatedAt.Before(objects[j].CreatedAt)
	})
	return objects, nil
}

// Remove deletes the stored object if it is not pinned.
func (s *Store) Remove(_ context.Context, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.pathForDigest(digest)
	if err != nil {
		return err
	}
	if s.isPinned(digest) {
		return ErrPinned
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("cas: remove: %w", err)
	}
	metaPath := path + ".json"
	_ = os.Remove(metaPath)
	return nil
}

// Pin marks a digest as retained.
func (s *Store) Pin(_ context.Context, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.pathForDigest(digest)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("cas: pin stat: %w", err)
	}
	pin := s.pinPath(digest)
	return os.WriteFile(pin, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)
}

// Unpin removes a previously created pin record.
func (s *Store) Unpin(_ context.Context, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pin := s.pinPath(digest)
	if err := os.Remove(pin); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("cas: unpin: %w", err)
	}
	return nil
}

func (s *Store) objectFromMeta(meta Metadata) (Object, error) {
	pinned := s.isPinned(meta.Digest)
	return Object{Metadata: meta, Pinned: pinned}, nil
}

func (s *Store) isPinned(digest string) bool {
	_, err := os.Stat(s.pinPath(digest))
	return err == nil
}

func (s *Store) pinPath(digest string) string {
	hex := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(s.root, "pins", hex)
}

func (s *Store) readMetadata(digest string) (Metadata, error) {
	path, err := s.pathForDigest(digest)
	if err != nil {
		return Metadata{}, err
	}
	metaPath := path + ".json"
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Metadata{}, ErrNotFound
		}
		return Metadata{}, fmt.Errorf("cas: meta read: %w", err)
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, fmt.Errorf("cas: meta parse: %w", err)
	}
	return meta, nil
}

func (s *Store) writeMetadata(meta Metadata) error {
	path, err := s.pathForDigest(meta.Digest)
	if err != nil {
		return err
	}
	metaPath := path + ".json"
	buf, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("cas: meta marshal: %w", err)
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(metaPath), filepath.Base(metaPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("cas: meta temp create: %w", err)
	}
	tmpName := tmpFile.Name()
	if _, err := tmpFile.Write(buf); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("cas: meta tmp write: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("cas: meta tmp close: %w", err)
	}
	if err := os.Rename(tmpName, metaPath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("cas: meta replace: %w", err)
	}
	return nil
}

func (s *Store) pathForDigest(digest string) (string, error) {
	if !digestPattern.MatchString(digest) {
		return "", fmt.Errorf("cas: invalid digest")
	}
	hex := strings.TrimPrefix(digest, "sha256:")
	return filepath.Join(s.root, "sha256", hex), nil
}

func mergeTags(existing []string, added []string) []string {
	set := make(map[string]struct{}, len(existing)+len(added))
	for _, tag := range existing {
		set[tag] = struct{}{}
	}
	for _, tag := range added {
		if tag == "" {
			continue
		}
		set[tag] = struct{}{}
	}
	var result []string
	for tag := range set {
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}

type verifyingReader struct {
	f        *os.File
	h        hash.Hash
	expected string
	done     bool
}

func (r *verifyingReader) Read(p []byte) (int, error) {
	n, err := r.f.Read(p)
	if n > 0 {
		if _, writeErr := r.h.Write(p[:n]); writeErr != nil {
			return n, writeErr
		}
	}
	if errors.Is(err, io.EOF) {
		r.done = true
	}
	return n, err
}

func (r *verifyingReader) Close() error {
	if !r.done {
		if _, err := io.Copy(r.h, r.f); err != nil {
			_ = r.f.Close()
			return err
		}
	}
	if err := r.f.Close(); err != nil {
		return err
	}
	sum := "sha256:" + hex.EncodeToString(r.h.Sum(nil))
	if sum != r.expected {
		return fmt.Errorf("%w: expected %s got %s", ErrDigestMismatch, r.expected, sum)
	}
	return nil
}
