package builder

import (
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"testing/quick"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/joshka0/foxctl/internal/interfaces/openapi/loader"
)

func TestBuilder_Build(t *testing.T) {
	// Create a minimal test spec
	spec := &loader.Spec{
		Doc: &openapi3.T{
			Servers: openapi3.Servers{
				{URL: "https://api.example.com"},
			},
		},
		Operations: map[string]*loader.Operation{
			"getUser": {
				ID:     "getUser",
				Method: "GET",
				Path:   "/users/{id}",
				Parameters: openapi3.Parameters{
					{
						Value: &openapi3.Parameter{
							Name:     "id",
							In:       "path",
							Required: true,
						},
					},
				},
			},
		},
	}

	b := New(spec)

	tests := []struct {
		name        string
		operationID string
		params      Params
		wantMethod  string
		wantURL     string
		wantErr     bool
	}{
		{
			name:        "simple GET with path param",
			operationID: "getUser",
			params: Params{
				Path: map[string]any{"id": 123},
			},
			wantMethod: "GET",
			wantURL:    "https://api.example.com/users/123",
			wantErr:    false,
		},
		{
			name:        "missing required path param",
			operationID: "getUser",
			params:      Params{},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := b.Build(tt.operationID, tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("Build() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if req.Method != tt.wantMethod {
				t.Errorf("Build() method = %v, want %v", req.Method, tt.wantMethod)
			}
			if req.URL != tt.wantURL {
				t.Errorf("Build() URL = %v, want %v", req.URL, tt.wantURL)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	spec := &loader.Spec{
		Doc: &openapi3.T{
			Servers: openapi3.Servers{{URL: "https://api.example.com"}},
		},
	}
	b := New(spec)

	tests := []struct {
		name         string
		pathTemplate string
		pathParams   map[string]any
		opParams     openapi3.Parameters
		want         string
		wantErr      bool
	}{
		{
			name:         "simple replacement",
			pathTemplate: "/users/{id}",
			pathParams:   map[string]any{"id": 123},
			opParams: openapi3.Parameters{
				{Value: &openapi3.Parameter{Name: "id", In: "path", Required: true}},
			},
			want:    "/users/123",
			wantErr: false,
		},
		{
			name:         "multiple params",
			pathTemplate: "/repos/{owner}/{repo}",
			pathParams:   map[string]any{"owner": "octocat", "repo": "Hello-World"},
			opParams: openapi3.Parameters{
				{Value: &openapi3.Parameter{Name: "owner", In: "path", Required: true}},
				{Value: &openapi3.Parameter{Name: "repo", In: "path", Required: true}},
			},
			want:    "/repos/octocat/Hello-World",
			wantErr: false,
		},
		{
			name:         "missing required param",
			pathTemplate: "/users/{id}",
			pathParams:   map[string]any{},
			opParams: openapi3.Parameters{
				{Value: &openapi3.Parameter{Name: "id", In: "path", Required: true}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := b.resolvePath(tt.pathTemplate, tt.pathParams, tt.opParams)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolvePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("resolvePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddQueryParamsSerializesArrayValuesAsRepeatedParameters(t *testing.T) {
	u, err := url.Parse("https://api.example.com/search?existing=true")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	b := New(&loader.Spec{})

	err = b.addQueryParams(u, map[string]any{
		"tag":   []string{"go", "tests"},
		"ids":   []int{1, 2},
		"empty": []string{},
		"q":     "hello world",
	}, nil)
	if err != nil {
		t.Fatalf("addQueryParams: %v", err)
	}

	values := u.Query()
	if got := values["tag"]; !reflect.DeepEqual(got, []string{"go", "tests"}) {
		t.Fatalf("tag params=%v want repeated values", got)
	}
	if got := values["ids"]; !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Fatalf("ids params=%v want repeated values", got)
	}
	if got := values.Get("empty"); got != "" {
		t.Fatalf("empty array should not add a value, got %q", got)
	}
	if got := values.Get("q"); got != "hello world" {
		t.Fatalf("q=%q want original decoded query value", got)
	}
	if got := values.Get("existing"); got != "true" {
		t.Fatalf("existing query param was not preserved: %q", got)
	}
}

func TestAddQueryParamsGeneratedStringValuesRoundTrip(t *testing.T) {
	cfg := &quick.Config{MaxCount: 100}
	b := New(&loader.Spec{})

	err := quick.Check(func(rawA uint16, rawB uint16) bool {
		a := "value " + strconv.Itoa(int(rawA))
		bv := "punctuation/%2C?" + strconv.Itoa(int(rawB))
		u, err := url.Parse("https://api.example.com/search")
		if err != nil {
			t.Logf("parse url: %v", err)
			return false
		}
		err = b.addQueryParams(u, map[string]any{"a": a, "b": bv}, nil)
		if err != nil {
			t.Logf("addQueryParams: %v", err)
			return false
		}
		values := u.Query()
		if values.Get("a") != a || values.Get("b") != bv {
			t.Logf("query values=%v want a=%q b=%q raw=%q", values, a, bv, u.RawQuery)
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("query string round-trip property failed: %v", err)
	}
}
