package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveProvider(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "default", input: "", want: providerElevenLabs},
		{name: "elevenlabs", input: "elevenlabs", want: providerElevenLabs},
		{name: "eleven alias", input: "eleven", want: providerElevenLabs},
		{name: "pocket", input: "pocket", want: providerPocketTTS},
		{name: "pocket alias", input: "pocket-tts", want: providerPocketTTS},
		{name: "invalid", input: "foo", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveProvider(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveProvider returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveProvider(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolvePocketBaseURL(t *testing.T) {
	t.Run("input has precedence", func(t *testing.T) {
		t.Setenv("POCKET_TTS_BASE_URL", "http://env-base:9000")
		got := resolvePocketBaseURL("http://input:8000/")
		if got != "http://input:8000" {
			t.Fatalf("resolvePocketBaseURL() = %q, want %q", got, "http://input:8000")
		}
	})

	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("POCKET_TTS_BASE_URL", "http://env-base:9000/")
		got := resolvePocketBaseURL("")
		if got != "http://env-base:9000" {
			t.Fatalf("resolvePocketBaseURL() = %q, want %q", got, "http://env-base:9000")
		}
	})

	t.Run("default fallback", func(t *testing.T) {
		t.Setenv("POCKET_TTS_BASE_URL", "")
		t.Setenv("POCKET_TTS_URL", "")
		got := resolvePocketBaseURL("")
		if got != defaultPocketBaseURL {
			t.Fatalf("resolvePocketBaseURL() = %q, want %q", got, defaultPocketBaseURL)
		}
	})
}

func TestNormalizeRewriteText(t *testing.T) {
	got := normalizeRewriteText("  \"Hello\\n   world\"  ", 20)
	want := "Hello\\n world"
	if got != want {
		t.Fatalf("normalizeRewriteText() = %q, want %q", got, want)
	}
}

func TestClampToMaxChars(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxChars int
		want     string
	}{
		{name: "short text", text: "hello", maxChars: 10, want: "hello"},
		{name: "clamp at punctuation", text: "Hello there. This is long", maxChars: 14, want: "Hello there."},
		{name: "clamp at space", text: "Hello there this is long", maxChars: 12, want: "Hello there"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampToMaxChars(tt.text, tt.maxChars)
			if got != tt.want {
				t.Fatalf("clampToMaxChars() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateSpeechPocket(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		const wavBody = "RIFFTESTWAV"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if r.URL.Path != "/tts" {
				t.Fatalf("path = %s, want /tts", r.URL.Path)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("ParseMultipartForm() error: %v", err)
			}
			if got := r.FormValue("text"); got != "hello from test" {
				t.Fatalf("text = %q, want %q", got, "hello from test")
			}
			if got := r.FormValue("voice_url"); got != "alba" {
				t.Fatalf("voice_url = %q, want %q", got, "alba")
			}
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write([]byte(wavBody))
		}))
		defer server.Close()

		got, err := generateSpeechPocket(context.Background(), server.URL, "alba", "hello from test")
		if err != nil {
			t.Fatalf("generateSpeechPocket() error: %v", err)
		}
		if string(got) != wavBody {
			t.Fatalf("audio body = %q, want %q", string(got), wavBody)
		}
	})

	t.Run("api error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"bad voice"}`))
		}))
		defer server.Close()

		_, err := generateSpeechPocket(context.Background(), server.URL, "invalid", "hello")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "bad voice") {
			t.Fatalf("error = %q, want to contain %q", err.Error(), "bad voice")
		}
	})
}
