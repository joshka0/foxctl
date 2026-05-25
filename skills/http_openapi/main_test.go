package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	openapiauth "github.com/joshka0/foxctl/internal/interfaces/openapi/auth"
	"github.com/joshka0/foxctl/internal/interfaces/openapi/builder"
	"github.com/joshka0/foxctl/internal/interfaces/openapi/loader"
	"github.com/joshka0/foxctl/internal/platform/config"
)

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer) *skillmain.RunContext {
	t.Helper()
	rc, err := skillmain.BuildRunContext(config.Config{}, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func TestRunHttpOpenApi(t *testing.T) {
	stdout := &bytes.Buffer{}
	cfg := config.Config{}
	rc, err := skillmain.BuildRunContext(cfg, stdout)
	if err != nil {
		t.Fatal(err)
	}

	in := Input{
		Spec:        "http://example.com/spec.yaml",
		OperationID: "getUsers",
	}

	ctx := context.Background()
	// This is expected to fail due to missing spec, but covers the initial logic
	_ = run(ctx, rc, in) //nolint:errcheck
}

func TestGenerateHint(t *testing.T) {
	hint := generateHint("EAUTH", 401)
	if hint == "" {
		t.Error("expected hint")
	}

	hint = generateHint("EPAGINATION", 0)
	if hint == "" {
		t.Error("expected pagination hint")
	}
}

func TestConvertHeaders(t *testing.T) {
	input := map[string]string{
		"Content-Type": "application/json",
		"X-Custom":     "value",
	}

	header := convertHeaders(input)

	if header.Get("Content-Type") != "application/json" {
		t.Error("Content-Type not set")
	}
	if header.Get("X-Custom") != "value" {
		t.Error("X-Custom not set")
	}
}

func TestAggregateResponses(t *testing.T) {
	// Test empty
	if aggregateResponses(nil) != nil {
		t.Error("expected nil for empty input")
	}

	// Test single
	single := []any{"foo"}
	if res := aggregateResponses(single); res != "foo" {
		t.Errorf("expected 'foo', got %v", res)
	}

	// Test arrays
	bodies := []any{
		[]any{1, 2},
		[]any{3, 4},
	}

	aggregated := aggregateResponses(bodies).([]any)
	if len(aggregated) != 4 {
		t.Errorf("expected 4 items, got %d", len(aggregated))
	}
}

func TestSuggestOperationsReturnsSortedStableIDs(t *testing.T) {
	spec := &loader.Spec{Operations: map[string]*loader.Operation{
		"zeta":  nil,
		"alpha": nil,
		"gamma": nil,
	}}

	const want = "alpha, gamma, zeta"
	for i := 0; i < 20; i++ {
		if got := suggestOperations(spec, "missing"); got != want {
			t.Fatalf("suggestOperations iteration %d=%q want %q", i, got, want)
		}
	}
}

func TestSuggestOperationsReturnsSortedSimilarIDs(t *testing.T) {
	spec := &loader.Spec{Operations: map[string]*loader.Operation{
		"updateUser": nil,
		"getOrder":   nil,
		"deleteUser": nil,
		"listUsers":  nil,
		"createUser": nil,
		"getUser":    nil,
	}}

	const want = "createUser, deleteUser, getUser, listUsers, updateUser..."
	for i := 0; i < 20; i++ {
		if got := suggestOperations(spec, "user"); got != want {
			t.Fatalf("suggestOperations iteration %d=%q want %q", i, got, want)
		}
	}
}

func TestAggregateResponsesGeneratedArraysPreservePageOrder(t *testing.T) {
	cfg := &quick.Config{MaxCount: 100}

	err := quick.Check(func(pages [][]uint8) bool {
		bodies := make([]any, len(pages))
		want := make([]any, 0)
		for i, page := range pages {
			values := make([]any, len(page))
			for j, raw := range page {
				values[j] = int(raw)
				want = append(want, int(raw))
			}
			bodies[i] = values
		}

		got := aggregateResponses(bodies)
		if len(pages) == 0 {
			return got == nil
		}
		if len(pages) == 1 {
			return reflect.DeepEqual(got, bodies[0])
		}
		return reflect.DeepEqual(got, want)
	}, cfg)
	if err != nil {
		t.Fatalf("array aggregation property failed: %v", err)
	}
}

func TestAggregateResponsesKeepsEmptyArrayShape(t *testing.T) {
	got := aggregateResponses([]any{[]any{}, []any{}})
	want := []any{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aggregateResponses(empty array pages)=%#v want %#v", got, want)
	}
}

func TestAggregateResponsesWrapsMixedPageBodies(t *testing.T) {
	bodies := []any{
		[]any{"first"},
		map[string]any{"next": "cursor"},
	}

	got, ok := aggregateResponses(bodies).(map[string]any)
	if !ok {
		t.Fatalf("mixed page bodies should be wrapped with metadata, got %T", aggregateResponses(bodies))
	}
	if !reflect.DeepEqual(got["pages"], bodies) {
		t.Fatalf("pages=%v want %v", got["pages"], bodies)
	}
}

func TestEmitDryRunRedactsQueryAPIKey(t *testing.T) {
	stdout := &bytes.Buffer{}
	rc := newTestRunnerContext(t, stdout)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	secret := httpOpenAPITestSecret("dry-run-query")
	req := &builder.Request{
		Method:  "GET",
		URL:     "https://api.example.com/items?key=" + url.QueryEscape(secret) + "&visible=keep",
		Headers: map[string]string{"X-Api-Key": secret},
	}
	in := Input{
		Auth: openapiauth.Config{
			Type:   "apiKey",
			APIKey: secret,
			Query:  "key",
		},
	}

	if err := emitDryRun(rc, req, in); err != nil {
		t.Fatalf("emitDryRun: %v", err)
	}

	plan := emittedRequestPlan(t, stdout.Bytes())
	redactedURL, ok := plan["url"].(string)
	if !ok {
		t.Fatalf("url is %T, want string", plan["url"])
	}
	if containsString(plan, secret) {
		t.Fatalf("dry-run plan leaked secret: %#v", plan)
	}

	parsed, err := url.Parse(redactedURL)
	if err != nil {
		t.Fatalf("parse redacted URL: %v", err)
	}
	if got := parsed.Query().Get("key"); got != "***" {
		t.Fatalf("key query param=%q want redacted marker", got)
	}
	if got := parsed.Query().Get("visible"); got != "keep" {
		t.Fatalf("visible query param=%q want keep", got)
	}
}

func TestRedactDryRunURLPropertyQueryCredentialDoesNotSurvive(t *testing.T) {
	cfg := &quick.Config{MaxCount: 300}
	err := quick.Check(func(raw string, nameSeed uint8) bool {
		secret := httpOpenAPITestSecret(raw)
		queryName := httpOpenAPITestQueryName(nameSeed)
		rawURL := "https://api.example.com/search?visible=keep&" + url.QueryEscape(queryName) + "=" + url.QueryEscape(secret)
		redacted := redactDryRunURL(rawURL, openapiauth.Config{
			Type:   "apiKey",
			APIKey: secret,
			Query:  queryName,
		})
		if contains := strings.Contains(redacted, secret); contains {
			t.Logf("redacted URL leaked secret for query %q: %q", queryName, redacted)
			return false
		}
		parsed, err := url.Parse(redacted)
		if err != nil {
			t.Logf("parse redacted URL: %v", err)
			return false
		}
		return parsed.Query().Get(queryName) == "***" && parsed.Query().Get("visible") == "keep"
	}, cfg)
	if err != nil {
		t.Fatalf("dry-run URL redaction property failed: %v", err)
	}
}

func emittedRequestPlan(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	summary, ok := env.Data["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary is %T, want map", env.Data["summary"])
	}
	plan, ok := summary["request_plan"].(map[string]any)
	if !ok {
		t.Fatalf("request_plan is %T, want map", summary["request_plan"])
	}
	return plan
}

func httpOpenAPITestSecret(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func httpOpenAPITestQueryName(seed uint8) string {
	names := []string{"key", "token", "api_key", "access_token", "client_secret", "credential"}
	return names[int(seed)%len(names)]
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
