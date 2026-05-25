package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/quick"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

func TestCASHandlerRejectsMalformedDigest(t *testing.T) {
	handler := CASHandler(config.Config{Paths: config.Paths{CAS: t.TempDir()}}, zerolog.Nop())

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/cas/not-a-digest"},
		{method: http.MethodHead, path: "/api/cas/sha256:nothex"},
		{method: http.MethodGet, path: "/api/cas/abc/read"},
		{method: http.MethodPost, path: "/api/cas/sha256:00000000000000000000000000000000000000000000000000000000000000xz/pin"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s %s status=%d want %d body=%s", tc.method, tc.path, rr.Code, http.StatusBadRequest, rr.Body.String())
		}
	}
}

func TestCASHandlerReadPaginatesAndReportsNavigation(t *testing.T) {
	casRoot := t.TempDir()
	store, err := cas.NewStore(casRoot)
	if err != nil {
		t.Fatalf("new cas store: %v", err)
	}
	obj, err := store.Put(context.Background(), strings.NewReader("hello world"), "text/plain", []string{"test"})
	if err != nil {
		t.Fatalf("put cas object: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close cas store: %v", err)
	}

	handler := CASHandler(config.Config{Paths: config.Paths{CAS: casRoot}}, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/cas/"+strings.TrimPrefix(obj.Digest, "sha256:")+"/read?page=2&page_size=5", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("read status=%d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if got := asString(t, body["digest"]); got != obj.Digest {
		t.Fatalf("digest=%q want %q", got, obj.Digest)
	}
	if got := asString(t, body["content"]); got != " worl" {
		t.Fatalf("content=%q want %q", got, " worl")
	}
	if got := int(body["page"].(float64)); got != 2 {
		t.Fatalf("page=%d want 2", got)
	}
	if got := int(body["total_pages"].(float64)); got != 3 {
		t.Fatalf("total_pages=%d want 3", got)
	}
	if got := int(body["prev_page"].(float64)); got != 1 {
		t.Fatalf("prev_page=%d want 1", got)
	}
	if got := int(body["next_page"].(float64)); got != 3 {
		t.Fatalf("next_page=%d want 3", got)
	}
}

func TestNormalizeCASDigestPropertyAcceptsHexWithOrWithoutPrefix(t *testing.T) {
	check := func(seed [32]byte, withPrefix bool, uppercase bool) bool {
		hexDigest := hex.EncodeToString(seed[:])
		raw := hexDigest
		if uppercase {
			raw = strings.ToUpper(raw)
		}
		if withPrefix {
			raw = "sha256:" + raw
			if uppercase {
				raw = strings.ToUpper(raw)
			}
		}

		got := normalizeCASDigest(raw)
		return got == "sha256:"+hexDigest && validCASDigest(got)
	}

	if err := quick.Check(check, &quick.Config{MaxCount: 128}); err != nil {
		t.Fatalf("normalizeCASDigest should canonicalize valid sha256 hex: %v", err)
	}
}

func TestReadCASPagePropertyMatchesByteWindow(t *testing.T) {
	check := func(data []byte, pageSeed uint8, pageSizeSeed uint16) bool {
		page := int(pageSeed%32) + 1
		pageSize := int(pageSizeSeed%4096) + 1

		got, total, err := readCASPage(bytes.NewReader(data), page, pageSize)
		if err != nil || total != int64(len(data)) {
			return false
		}

		start := (page - 1) * pageSize
		if start > len(data) {
			start = len(data)
		}
		end := start + pageSize
		if end > len(data) {
			end = len(data)
		}
		return bytes.Equal(got, data[start:end])
	}

	if err := quick.Check(check, &quick.Config{MaxCount: 256}); err != nil {
		t.Fatalf("readCASPage should return the exact requested byte window: %v", err)
	}
}

func FuzzNormalizeCASDigest(f *testing.F) {
	valid := sha256.Sum256([]byte("foxctl"))
	f.Add("")
	f.Add("not-a-digest")
	f.Add(hex.EncodeToString(valid[:]))
	f.Add("sha256:" + hex.EncodeToString(valid[:]))
	f.Add("SHA256:" + strings.ToUpper(hex.EncodeToString(valid[:])))
	f.Add("sha256:00000000000000000000000000000000000000000000000000000000000000xz")

	f.Fuzz(func(t *testing.T, raw string) {
		got := normalizeCASDigest(raw)
		if strings.TrimSpace(raw) == "" {
			if got != "" {
				t.Fatalf("empty digest normalized to %q", got)
			}
			return
		}
		if validCASDigest(got) {
			if len(got) != len("sha256:")+64 || !strings.HasPrefix(got, "sha256:") {
				t.Fatalf("valid digest has non-canonical shape: %q", got)
			}
			if got != strings.ToLower(got) {
				t.Fatalf("valid digest is not lowercase: %q", got)
			}
		}
	})
}
