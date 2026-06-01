package artifacts

import (
	"strings"
	"testing"
)

func TestDigests(t *testing.T) {
	data := []byte(`{"data":{"artifact":"sha256:abc","artifacts":["sha256:def","notadigest"]}}`)
	got := Digests(data)
	if len(got) != 2 || got[0] != "sha256:abc" || got[1] != "sha256:def" {
		t.Fatalf("unexpected digests: %#v", got)
	}
}

func TestDigestsTrimsAndDeduplicates(t *testing.T) {
	data := []byte(`{"data":{"artifact":" sha256:abc ","artifacts":["sha256:def","sha256:abc","notadigest"," sha256:def "]}}`)

	got := Digests(data)

	if len(got) != 2 || got[0] != "sha256:abc" || got[1] != "sha256:def" {
		t.Fatalf("unexpected digests: %#v", got)
	}
}

func FuzzDigests(f *testing.F) {
	f.Add([]byte(`{"data":{"artifact":"sha256:abc","artifacts":["sha256:def"]}}`))
	f.Add([]byte(`{"data":{"artifact":" sha256:abc ","artifacts":["sha256:abc","notadigest"]}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`not-json`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		got := Digests(raw)
		seen := map[string]struct{}{}
		for _, digest := range got {
			if digest != strings.TrimSpace(digest) {
				t.Fatalf("digest was not trimmed: %q", digest)
			}
			if !strings.HasPrefix(digest, "sha256:") {
				t.Fatalf("digest missing sha256 prefix: %q", digest)
			}
			if _, ok := seen[digest]; ok {
				t.Fatalf("duplicate digest returned: %q in %#v", digest, got)
			}
			seen[digest] = struct{}{}
		}
	})
}
