package cas

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/quick"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3HTTPClientFunc func(*http.Request) (*http.Response, error)

func (f s3HTTPClientFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestS3DigestOperationsRejectInvalidDigestsBeforeClientUse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := &S3Store{bucket: "bucket", prefix: "cas/"}
	invalidDigests := []string{
		"",
		"sha256:",
		"sha256:abc",
		"sha256:" + strings.Repeat("0", 63),
		"sha256:" + strings.Repeat("0", 65),
		"sha256:" + strings.Repeat("g", 64),
		"SHA256:" + strings.Repeat("0", 64),
		"../sentinel",
		"sha256:../sentinel",
		strings.Repeat("0", 64),
	}
	operations := []struct {
		name string
		run  func(string) error
	}{
		{name: "Head", run: func(digest string) error {
			_, err := store.Head(ctx, digest)
			return err
		}},
		{name: "Get", run: func(digest string) error {
			_, _, err := store.Get(ctx, digest)
			return err
		}},
		{name: "Remove", run: func(digest string) error {
			return store.Remove(ctx, digest)
		}},
		{name: "Pin", run: func(digest string) error {
			return store.Pin(ctx, digest)
		}},
		{name: "Unpin", run: func(digest string) error {
			return store.Unpin(ctx, digest)
		}},
		{name: "AddTags", run: func(digest string) error {
			return store.AddTags(ctx, digest, []string{"tag"})
		}},
	}

	for _, digest := range invalidDigests {
		for _, op := range operations {
			t.Run(op.name+"/"+digest, func(t *testing.T) {
				err := op.run(digest)
				if err == nil {
					t.Fatalf("%s(%q) expected error", op.name, digest)
				}
				if !strings.Contains(err.Error(), "invalid digest") {
					t.Fatalf("%s(%q) error = %v, want invalid digest", op.name, digest, err)
				}
			})
		}
	}
}

func TestS3PutMetaRejectsInvalidDigestBeforeClientUse(t *testing.T) {
	t.Parallel()

	store := &S3Store{bucket: "bucket", prefix: "cas/"}
	err := store.putMeta(context.Background(), s3Meta{Digest: "../sentinel"})
	if err == nil {
		t.Fatalf("putMeta invalid digest expected error")
	}
	if !strings.Contains(err.Error(), "invalid digest") {
		t.Fatalf("putMeta error = %v, want invalid digest", err)
	}
}

func TestS3GetMetaRejectsMismatchedDigestSidecar(t *testing.T) {
	t.Parallel()

	requested := casTestDigest("requested")
	sidecar := casTestDigest("sidecar")
	store := newS3HTTPTestStore(func(req *http.Request) (*http.Response, error) {
		body := `{"digest":"` + sidecar + `","kind":"text/plain","tags":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	_, err := store.Head(context.Background(), requested)
	if err == nil {
		t.Fatalf("Head with mismatched sidecar digest expected error")
	}
	if !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("Head error = %v, want digest mismatch", err)
	}
}

func TestS3KeyPropertyValidDigestsStayUnderConfiguredPrefixes(t *testing.T) {
	t.Parallel()

	store := &S3Store{prefix: "tenant/cas/"}
	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(seed string) bool {
		digest := casTestDigest(seed)
		blobKey := store.blobKey(digest)
		metaKey := store.metaKey(digest)
		return strings.HasPrefix(blobKey, "tenant/cas/blob/") &&
			strings.HasSuffix(blobKey, digest) &&
			!strings.Contains(blobKey, "..") &&
			strings.HasPrefix(metaKey, "tenant/cas/meta/") &&
			strings.HasSuffix(metaKey, digest+".json") &&
			!strings.Contains(metaKey, "..")
	}, cfg)
	if err != nil {
		t.Fatalf("s3 key property failed: %v", err)
	}
}

func newS3HTTPTestStore(client s3HTTPClientFunc) *S3Store {
	return &S3Store{
		client: s3.New(s3.Options{
			Region:       "us-east-1",
			BaseEndpoint: aws.String("https://s3.test"),
			Credentials:  credentials.NewStaticCredentialsProvider("access", "secret", ""),
			UsePathStyle: true,
			HTTPClient:   client,
		}),
		bucket: "bucket",
		prefix: "cas/",
	}
}
