package secrets

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

func TestRedact(t *testing.T) {
	input := "Authorization: Bearer abcdefghijklmnopqrstuvwxyz"
	got := Redact(input)
	if got == input {
		t.Fatalf("expected redaction; got %q", got)
	}
	if got != "Authorization: Bearer ***" {
		t.Fatalf("unexpected redact output: %q", got)
	}
}

func TestRedactMap(t *testing.T) {
	m := map[string]any{
		"password": "supersecret",
		"nested":   map[string]any{"token": "abc"},
		"names":    []any{"value", map[string]any{"api_key": "123456789012345678901234"}},
	}
	redacted := RedactMap(m)
	if redacted["password"].(string) != "***" {
		t.Fatalf("expected password redacted")
	}
	nested := redacted["nested"].(map[string]any)
	if nested["token"].(string) != "***" {
		t.Fatalf("expected nested token redacted")
	}
	slice := redacted["names"].([]any)
	nestedMap := slice[1].(map[string]any)
	if nestedMap["api_key"].(string) != "***" {
		t.Fatalf("expected api key redacted in slice")
	}
}

func TestRedactHeaders(t *testing.T) {
	headers := map[string]string{
		"Authorization":  "Bearer foo",
		"X-Trace":        "abc",
		"X-Goog-Api-Key": "short-provider-key",
	}
	got := RedactHeaders(headers)
	if got["Authorization"] != "***" {
		t.Fatalf("expected auth header redacted")
	}
	if got["X-Goog-Api-Key"] != "***" {
		t.Fatalf("expected provider API key header redacted")
	}
	if got["X-Trace"] != "abc" {
		t.Fatalf("expected non-secret header unchanged")
	}
}

func TestRedactJSONStyleKeys(t *testing.T) {
	input := `{"password":"secret","api_key":"ABCDEFGHIJKLMNOPQRST","token":"tok12345678901234567890"}`
	got := Redact(input)
	if got == input {
		t.Fatalf("expected redaction for JSON-style keys")
	}
	if want := `"password":"***"`; !strings.Contains(got, want) {
		t.Fatalf("expected password redaction, got %q", got)
	}
	if want := `"api_key":"***"`; !strings.Contains(got, want) {
		t.Fatalf("expected api_key redaction, got %q", got)
	}
	if want := `"token":"***"`; !strings.Contains(got, want) {
		t.Fatalf("expected token redaction, got %q", got)
	}
}

func TestRedactTextSensitiveKeyVariants(t *testing.T) {
	t.Parallel()

	secret := redactTestSecret("sensitive-key-variants")
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "client secret",
			input: `{"client_secret":"` + secret + `"}`,
		},
		{
			name:  "aws secret access key",
			input: "aws_secret_access_key=" + secret,
		},
		{
			name:  "refresh token",
			input: `refresh_token="` + secret + `"`,
		},
		{
			name:  "private key field",
			input: "private_key=" + secret,
		},
		{
			name:  "credential field",
			input: `credential:"` + secret + `"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.input)
			if strings.Contains(got, secret) {
				t.Fatalf("redacted output leaked secret in %q", got)
			}
			if got != Redact(got) {
				t.Fatalf("redaction is not idempotent: first %q second %q", got, Redact(got))
			}
		})
	}
}

func TestRedactRemovesKnownSecretShapes(t *testing.T) {
	t.Parallel()

	secret := redactTestSecret("known-shapes")
	cases := []struct {
		name   string
		secret string
		input  string
	}{
		{
			name:   "bearer token",
			secret: secret,
			input:  "Authorization: Bearer " + secret,
		},
		{
			name:   "github token",
			secret: "ghp_" + secret,
			input:  "token=" + "ghp_" + secret,
		},
		{
			name:   "password assignment",
			secret: secret,
			input:  `password="` + secret + `"`,
		},
		{
			name:   "api key assignment",
			secret: secret,
			input:  "api_key=" + secret,
		},
		{
			name:   "secret assignment",
			secret: secret,
			input:  "secret=" + secret,
		},
		{
			name:   "token assignment",
			secret: secret,
			input:  "token=" + secret,
		},
		{
			name:   "jwt",
			secret: redactTestJWT(secret),
			input:  "jwt=" + redactTestJWT(secret),
		},
		{
			name:   "slack token",
			secret: "xoxb-123456789012-123456789012-" + secret[:24],
			input:  "slack=xoxb-123456789012-123456789012-" + secret[:24],
		},
		{
			name:   "stripe key",
			secret: "sk_live_" + secret,
			input:  "stripe=sk_live_" + secret,
		},
		{
			name:   "docker auth",
			secret: secret,
			input:  `{"auth":"` + secret + `"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.input)
			if strings.Contains(got, tc.secret) {
				t.Fatalf("redacted output leaked secret %q in %q", tc.secret, got)
			}
			if got != Redact(got) {
				t.Fatalf("redaction is not idempotent: first %q second %q", got, Redact(got))
			}
		})
	}
}

func TestRedactPropertyKnownSecretValuesDoNotSurvive(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 200}
	err := quick.Check(func(raw string) bool {
		secret := redactTestSecret(raw)
		inputs := []struct {
			input  string
			secret string
		}{
			{input: "Authorization: Bearer " + secret, secret: secret},
			{input: `{"password":"` + secret + `"}`, secret: secret},
			{input: `api_key="` + secret + `"`, secret: secret},
			{input: "token=" + secret, secret: secret},
			{input: "jwt=" + redactTestJWT(secret), secret: redactTestJWT(secret)},
			{input: "stripe=sk_test_" + secret, secret: "sk_test_" + secret},
		}

		for _, tc := range inputs {
			redacted := Redact(tc.input)
			if strings.Contains(redacted, tc.secret) {
				return false
			}
			if Redact(redacted) != redacted {
				return false
			}
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("redaction property failed: %v", err)
	}
}

func TestRedactPropertySensitiveAssignmentKeysDoNotLeakValues(t *testing.T) {
	t.Parallel()

	keys := []string{
		"access_token",
		"auth",
		"authorization",
		"aws_secret_access_key",
		"client_secret",
		"credential",
		"credentials",
		"encryption_key",
		"private-key",
		"refresh_token",
		"session_secret",
		"signing_key",
		"ssh_key",
	}
	separators := []string{":", "=", " : ", " = "}
	quotes := []string{"", `"`, `'`}

	cfg := &quick.Config{MaxCount: 200}
	err := quick.Check(func(raw string, keySeed, sepSeed, quoteSeed uint8) bool {
		secret := redactTestSecret(raw)
		key := keys[int(keySeed)%len(keys)]
		separator := separators[int(sepSeed)%len(separators)]
		quote := quotes[int(quoteSeed)%len(quotes)]

		redacted := Redact(key + separator + quote + secret + quote)
		if strings.Contains(redacted, secret) {
			return false
		}
		return Redact(redacted) == redacted
	}, cfg)
	if err != nil {
		t.Fatalf("sensitive assignment key property failed: %v", err)
	}
}

func TestRedactMapRecursivelyRedactsWithoutMutatingInput(t *testing.T) {
	t.Parallel()

	secret := redactTestSecret("map")
	input := map[string]any{
		"password": secret,
		"nested": map[string]any{
			"message": "Authorization: Bearer " + secret,
		},
		"slice": []any{
			"api_key=" + secret,
			map[string]any{"client_secret": secret},
		},
		"plain": "visible",
	}
	original := cloneAny(input).(map[string]any)

	got := RedactMap(input)
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("RedactMap mutated input: got %#v want %#v", input, original)
	}
	if containsString(got, secret) {
		t.Fatalf("RedactMap leaked secret in %#v", got)
	}
	if got["plain"] != "visible" {
		t.Fatalf("RedactMap changed non-secret value: %#v", got["plain"])
	}
}

func TestRedactHeadersPropertySensitiveHeadersAreFullyMasked(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 100}
	err := quick.Check(func(raw string) bool {
		secret := redactTestSecret(raw)
		headers := map[string]string{
			"Authorization":      "Bearer " + secret,
			"X-Api-Key":          secret,
			"Cookie":             "sid=" + secret,
			"X-Request-Id":       "req-" + secret[:12],
			"X-Forwarded-For":    "127.0.0.1",
			"X-Debug-Auth-Value": "token=" + secret,
		}
		got := RedactHeaders(headers)
		return got["Authorization"] == "***" &&
			got["X-Api-Key"] == "***" &&
			got["Cookie"] == "***" &&
			got["X-Request-Id"] == headers["X-Request-Id"] &&
			got["X-Forwarded-For"] == headers["X-Forwarded-For"] &&
			!strings.Contains(got["X-Debug-Auth-Value"], secret)
	}, cfg)
	if err != nil {
		t.Fatalf("redact headers property failed: %v", err)
	}
}

func TestRedactHeadersPropertyProviderSecretHeadersAreMaskedByName(t *testing.T) {
	t.Parallel()

	sensitiveHeaders := []string{
		"Api-Key",
		"X-Amz-Credential",
		"X-Amz-Security-Token",
		"X-Api-Token",
		"X-CSRF-Token",
		"X-Goog-Api-Key",
		"X-Session-Token",
		"X-XSRF-Token",
	}

	cfg := &quick.Config{MaxCount: 100}
	err := quick.Check(func(raw string, headerSeed uint8) bool {
		secret := "short-" + raw
		header := sensitiveHeaders[int(headerSeed)%len(sensitiveHeaders)]
		got := RedactHeaders(map[string]string{
			header:       secret,
			"X-Trace-Id": "trace-" + raw,
		})
		return got[header] == "***" && got["X-Trace-Id"] == "trace-"+raw
	}, cfg)
	if err != nil {
		t.Fatalf("provider secret header property failed: %v", err)
	}
}

func redactTestSecret(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func redactTestJWT(secret string) string {
	return fmt.Sprintf("eyJ%s.eyJ%s.%s", secret[:18], secret[18:36], secret[36:54])
}

func cloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = cloneAny(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneAny(item)
		}
		return out
	default:
		return v
	}
}

func containsString(value any, needle string) bool {
	switch v := value.(type) {
	case string:
		return strings.Contains(v, needle)
	case map[string]any:
		for _, item := range v {
			if containsString(item, needle) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if containsString(item, needle) {
				return true
			}
		}
	}
	return false
}
