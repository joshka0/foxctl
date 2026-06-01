package cas

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/joshka0/foxctl/internal/storage"
)

// S3Store implements CASStore using S3/MinIO for blob storage.
// Object metadata (kind, tags, pinned) is stored as a JSON sidecar
// at <prefix>meta/<digest>.json alongside the blob at <prefix>blob/<digest>.
type S3Store struct {
	client *s3.Client
	bucket string
	prefix string
	mu     sync.RWMutex
	Clock  func() time.Time
}

// s3Meta holds the metadata stored as a JSON sidecar object in S3.
type s3Meta struct {
	Digest    string    `json:"digest"`
	Size      int64     `json:"size"`
	Kind      string    `json:"kind"`
	Tags      []string  `json:"tags"`
	Pinned    bool      `json:"pinned"`
	CreatedAt time.Time `json:"created_at"`
}

// NewS3Store creates a new S3-based CAS store.
func NewS3Store(ctx context.Context, cfg S3Config) (*S3Store, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("cas/s3: bucket is required")
	}

	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "cas/"
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	// Build AWS config options
	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(region))

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("cas/s3: load aws config: %w", err)
	}

	// Build S3 client options
	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		endpoint := cfg.Endpoint
		if cfg.DisableSSL && !strings.HasPrefix(endpoint, "http://") {
			endpoint = strings.Replace(endpoint, "https://", "http://", 1)
			if !strings.HasPrefix(endpoint, "http://") {
				endpoint = "http://" + endpoint
			}
		}
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}
	if cfg.ForcePathStyle {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	return &S3Store{
		client: client,
		bucket: cfg.Bucket,
		prefix: prefix,
		Clock:  func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *S3Store) Close() error { return nil }

func (s *S3Store) blobKey(digest string) string {
	return s.prefix + "blob/" + digest
}

func (s *S3Store) metaKey(digest string) string {
	return s.prefix + "meta/" + digest + ".json"
}

// Put computes the SHA-256 digest of the content, uploads the blob and metadata to S3.
func (s *S3Store) Put(ctx context.Context, r io.Reader, kind string, tags []string) (storage.CASObject, error) {
	// Read all content to compute digest
	data, err := io.ReadAll(r)
	if err != nil {
		return storage.CASObject{}, fmt.Errorf("cas/s3: read content: %w", err)
	}

	h := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(h[:])

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if object already exists (idempotent put)
	existing, err := s.getMeta(ctx, digest)
	if err == nil {
		// Merge tags
		merged := mergeTags(existing.Tags, tags)
		if len(merged) != len(existing.Tags) {
			existing.Tags = merged
			if putErr := s.putMeta(ctx, existing); putErr != nil {
				return storage.CASObject{}, fmt.Errorf("cas/s3: update tags: %w", putErr)
			}
		}
		return s3MetaToObject(existing), nil
	}

	// Only proceed with upload if the error was a NotFound.
	// For any other error, return immediately.
	var nsk *types.NoSuchKey
	if !errors.As(err, &nsk) {
		return storage.CASObject{}, fmt.Errorf("cas/s3: check existing: %w", err)
	}

	// Upload blob
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(s.blobKey(digest)),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/octet-stream"),
	})
	if err != nil {
		return storage.CASObject{}, fmt.Errorf("cas/s3: put blob: %w", err)
	}

	// Upload metadata sidecar
	meta := s3Meta{
		Digest:    digest,
		Size:      int64(len(data)),
		Kind:      kind,
		Tags:      tags,
		Pinned:    false,
		CreatedAt: s.Clock(),
	}
	if err := s.putMeta(ctx, meta); err != nil {
		return storage.CASObject{}, fmt.Errorf("cas/s3: put meta: %w", err)
	}

	return s3MetaToObject(meta), nil
}

// Get returns a reader for the blob content and its metadata.
func (s *S3Store) Get(ctx context.Context, digest string) (io.ReadCloser, storage.CASMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meta, err := s.getMeta(ctx, digest)
	if err != nil {
		return nil, storage.CASMetadata{}, err
	}

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.blobKey(digest)),
	})
	if err != nil {
		return nil, storage.CASMetadata{}, fmt.Errorf("cas/s3: get blob: %w", err)
	}

	return result.Body, s3MetaToCASMetadata(meta), nil
}

// Head returns the metadata for an object without downloading the blob.
func (s *S3Store) Head(ctx context.Context, digest string) (storage.CASObject, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meta, err := s.getMeta(ctx, digest)
	if err != nil {
		return storage.CASObject{}, err
	}
	return s3MetaToObject(meta), nil
}

// List returns all objects in the store.
func (s *S3Store) List(ctx context.Context) ([]storage.CASObject, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var objects []storage.CASObject
	metaPrefix := s.prefix + "meta/"

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(metaPrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("cas/s3: list: %w", err)
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			// Extract digest from key: meta/<digest>.json
			name := strings.TrimPrefix(key, metaPrefix)
			name = strings.TrimSuffix(name, ".json")
			if name == "" {
				continue
			}

			meta, err := s.getMeta(ctx, name)
			if err != nil {
				continue // skip unreadable metadata
			}
			objects = append(objects, s3MetaToObject(meta))
		}
	}

	return objects, nil
}

// Remove deletes both the blob and metadata from S3.
func (s *S3Store) Remove(ctx context.Context, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateDigest(digest); err != nil {
		return err
	}

	// Delete both blob and metadata
	_, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(s.bucket),
		Delete: &types.Delete{
			Objects: []types.ObjectIdentifier{
				{Key: aws.String(s.blobKey(digest))},
				{Key: aws.String(s.metaKey(digest))},
			},
			Quiet: aws.Bool(true),
		},
	})
	if err != nil {
		return fmt.Errorf("cas/s3: remove: %w", err)
	}
	return nil
}

// Pin marks an object as pinned (protected from GC).
func (s *S3Store) Pin(ctx context.Context, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta, err := s.getMeta(ctx, digest)
	if err != nil {
		return err
	}
	meta.Pinned = true
	return s.putMeta(ctx, meta)
}

// Unpin removes the pin from an object.
func (s *S3Store) Unpin(ctx context.Context, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta, err := s.getMeta(ctx, digest)
	if err != nil {
		return err
	}
	meta.Pinned = false
	return s.putMeta(ctx, meta)
}

// AddTags adds tags to an existing object.
func (s *S3Store) AddTags(ctx context.Context, digest string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	meta, err := s.getMeta(ctx, digest)
	if err != nil {
		return err
	}
	meta.Tags = mergeTags(meta.Tags, tags)
	return s.putMeta(ctx, meta)
}

// GC removes unpinned objects older than the specified age.
func (s *S3Store) GC(ctx context.Context, opts storage.CASGCOptions) (storage.CASGCResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result storage.CASGCResult
	metaPrefix := s.prefix + "meta/"

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(metaPrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			result.Errors++
			return result, fmt.Errorf("cas/s3: gc list: %w", err)
		}

		for _, obj := range page.Contents {
			if opts.MaxDelete > 0 && result.ObjectsDeleted >= opts.MaxDelete {
				return result, nil
			}

			key := aws.ToString(obj.Key)
			name := strings.TrimPrefix(key, metaPrefix)
			name = strings.TrimSuffix(name, ".json")
			if name == "" {
				continue
			}

			meta, err := s.getMeta(ctx, name)
			if err != nil {
				result.Errors++
				continue
			}

			// Skip pinned objects
			if opts.KeepPinned && meta.Pinned {
				result.ObjectsSkipped++
				continue
			}

			// Skip recent objects
			if opts.OlderThan > 0 && s.Clock().Sub(meta.CreatedAt) < opts.OlderThan {
				result.ObjectsSkipped++
				continue
			}

			if opts.DryRun {
				result.ObjectsDeleted++
				result.BytesFreed += meta.Size
				continue
			}

			// Delete blob and metadata
			_, delErr := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(s.bucket),
				Delete: &types.Delete{
					Objects: []types.ObjectIdentifier{
						{Key: aws.String(s.blobKey(meta.Digest))},
						{Key: aws.String(s.metaKey(meta.Digest))},
					},
					Quiet: aws.Bool(true),
				},
			})
			if delErr != nil {
				result.Errors++
				continue
			}
			result.ObjectsDeleted++
			result.BytesFreed += meta.Size
		}
	}

	return result, nil
}

// getMeta reads the metadata sidecar for a digest.
func (s *S3Store) getMeta(ctx context.Context, digest string) (s3Meta, error) {
	if err := validateDigest(digest); err != nil {
		return s3Meta{}, err
	}

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.metaKey(digest)),
	})
	if err != nil {
		return s3Meta{}, fmt.Errorf("cas/s3: get meta %s: %w", digest, err)
	}
	defer result.Body.Close()

	var meta s3Meta
	if err := json.NewDecoder(result.Body).Decode(&meta); err != nil {
		return s3Meta{}, fmt.Errorf("cas/s3: decode meta: %w", err)
	}
	if err := validateDigest(meta.Digest); err != nil {
		return s3Meta{}, fmt.Errorf("cas/s3: invalid meta digest: %w", err)
	}
	if meta.Digest != digest {
		return s3Meta{}, fmt.Errorf("cas/s3: meta digest mismatch: key %s contains %s", digest, meta.Digest)
	}
	return meta, nil
}

// putMeta writes the metadata sidecar for a digest.
func (s *S3Store) putMeta(ctx context.Context, meta s3Meta) error {
	if err := validateDigest(meta.Digest); err != nil {
		return err
	}

	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("cas/s3: encode meta: %w", err)
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(s.metaKey(meta.Digest)),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("cas/s3: put meta: %w", err)
	}
	return nil
}

func s3MetaToObject(m s3Meta) storage.CASObject {
	return storage.CASObject{
		Metadata: storage.CASMetadata{
			Digest:    m.Digest,
			Size:      m.Size,
			Kind:      m.Kind,
			Tags:      m.Tags,
			CreatedAt: m.CreatedAt,
		},
		Pinned: m.Pinned,
	}
}

func s3MetaToCASMetadata(m s3Meta) storage.CASMetadata {
	return storage.CASMetadata{
		Digest:    m.Digest,
		Size:      m.Size,
		Kind:      m.Kind,
		Tags:      m.Tags,
		CreatedAt: m.CreatedAt,
	}
}
