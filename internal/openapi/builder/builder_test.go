package builder

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jkatigb/agentctl/internal/openapi/loader"
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
							Schema:   &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Type{Types: []string{"integer"}}}},
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
