package builder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/joshka0/foxctl/internal/interfaces/openapi/loader"
)

// Params organizes request parameters by OpenAPI location.
type Params struct {
	Path   map[string]any `json:"path"`
	Query  map[string]any `json:"query"`
	Header map[string]any `json:"header"`
	Body   any            `json:"body"`
}

// Request represents a built HTTP request ready for execution.
type Request struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

// Builder constructs HTTP requests from OpenAPI operations.
type Builder struct {
	spec *loader.Spec
}

// New creates a new request builder for the given spec.
func New(spec *loader.Spec) *Builder {
	return &Builder{spec: spec}
}

// Build constructs an HTTP request from an operation and parameters.
//
// Index:
// - Purpose: Build an HTTP request from an OpenAPI operation and parameter sets
// - Flow: resolve operation -> choose base URL -> resolve path params -> add query params -> build headers -> serialize body
// - SideEffects: none
// - FailureModes: missing operation, invalid parameters, serialization errors
// - Related: Builder.getBaseURL, Builder.resolvePath, Builder.addQueryParams, Builder.buildHeaders, Builder.serializeBody
// - Keywords: operation_id, path_params, query_params, headers, request_body, content_type
func (b *Builder) Build(operationID string, params Params) (*Request, error) {
	op, err := b.spec.GetOperation(operationID)
	if err != nil {
		return nil, fmt.Errorf("operation not found: %w", err)
	}

	// Start with base URL from servers
	baseURL, err := b.getBaseURL()
	if err != nil {
		return nil, fmt.Errorf("determine base URL: %w", err)
	}

	// Resolve path parameters
	path, err := b.resolvePath(op.Path, params.Path, op.Parameters)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	// Build full URL
	fullURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}

	rel, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse path: %w", err)
	}
	fullURL = fullURL.ResolveReference(rel)

	// Add query parameters
	if err := b.addQueryParams(fullURL, params.Query, op.Parameters); err != nil {
		return nil, fmt.Errorf("add query params: %w", err)
	}

	// Build headers
	headers, err := b.buildHeaders(params.Header, op.Parameters)
	if err != nil {
		return nil, fmt.Errorf("build headers: %w", err)
	}

	// Serialize body
	var body []byte
	if params.Body != nil && op.RequestBody != nil {
		body, err = b.serializeBody(params.Body, op.RequestBody)
		if err != nil {
			return nil, fmt.Errorf("serialize body: %w", err)
		}
		// Set Content-Type if not already set
		if _, ok := headers["Content-Type"]; !ok && op.RequestBody.Value != nil {
			contentType := b.inferContentType(op.RequestBody)
			if contentType != "" {
				headers["Content-Type"] = contentType
			}
		}
	}

	return &Request{
		Method:  strings.ToUpper(op.Method),
		URL:     fullURL.String(),
		Headers: headers,
		Body:    body,
	}, nil
}

// getBaseURL extracts the base URL from the OpenAPI spec servers.
func (b *Builder) getBaseURL() (string, error) {
	if b.spec.Doc == nil || len(b.spec.Doc.Servers) == 0 {
		return "", fmt.Errorf("no servers defined in spec")
	}
	// Use first server as default
	return strings.TrimRight(b.spec.Doc.Servers[0].URL, "/"), nil
}

// resolvePath replaces path parameters with actual values.
func (b *Builder) resolvePath(pathTemplate string, pathParams map[string]any, opParams openapi3.Parameters) (string, error) {
	result := pathTemplate

	// Find all path parameters from the operation
	pathParamDefs := make(map[string]*openapi3.Parameter)
	for _, paramRef := range opParams {
		if paramRef.Value != nil && paramRef.Value.In == "path" {
			pathParamDefs[paramRef.Value.Name] = paramRef.Value
		}
	}

	// Check required path parameters
	for name, paramDef := range pathParamDefs {
		if paramDef.Required {
			if pathParams == nil {
				return "", fmt.Errorf("missing required path parameter: %s", name)
			}
			if _, ok := pathParams[name]; !ok {
				return "", fmt.Errorf("missing required path parameter: %s", name)
			}
		}
	}

	// Replace path parameters
	for name, value := range pathParams {
		placeholder := "{" + name + "}"
		if !strings.Contains(result, placeholder) {
			return "", fmt.Errorf("path parameter %q not found in path template %q", name, pathTemplate)
		}
		// Convert value to string
		strValue := fmt.Sprintf("%v", value)
		result = strings.ReplaceAll(result, placeholder, url.PathEscape(strValue))
	}

	// Check if any placeholders remain
	if strings.Contains(result, "{") && strings.Contains(result, "}") {
		return "", fmt.Errorf("unresolved path parameters in %q", result)
	}

	return result, nil
}

// addQueryParams adds query parameters to the URL.
func (b *Builder) addQueryParams(u *url.URL, queryParams map[string]any, _ openapi3.Parameters) error {
	if queryParams == nil {
		return nil
	}

	q := u.Query()
	for name, value := range queryParams {
		// Convert value to string
		strValue := fmt.Sprintf("%v", value)
		q.Add(name, strValue)
	}
	u.RawQuery = q.Encode()

	return nil
}

// buildHeaders constructs the headers map.
func (b *Builder) buildHeaders(headerParams map[string]any, _ openapi3.Parameters) (map[string]string, error) {
	headers := make(map[string]string)

	// Add user-provided headers
	for name, value := range headerParams {
		strValue := fmt.Sprintf("%v", value)
		headers[name] = strValue
	}

	// Set default User-Agent if not provided
	if _, ok := headers["User-Agent"]; !ok {
		headers["User-Agent"] = "foxctl/1.0.0"
	}

	return headers, nil
}

// serializeBody serializes the request body according to the content type.
func (b *Builder) serializeBody(body any, requestBody *openapi3.RequestBodyRef) ([]byte, error) {
	if requestBody.Value == nil || requestBody.Value.Content == nil {
		// No content spec, just marshal as JSON
		return json.Marshal(body)
	}

	// Check for JSON content type
	for contentType := range requestBody.Value.Content {
		if strings.Contains(contentType, "json") {
			return json.Marshal(body)
		}
	}

	// Default to JSON
	return json.Marshal(body)
}

// inferContentType determines the appropriate Content-Type for the request body.
func (b *Builder) inferContentType(requestBody *openapi3.RequestBodyRef) string {
	if requestBody.Value == nil || requestBody.Value.Content == nil {
		return "application/json"
	}

	// Look for JSON first
	for contentType := range requestBody.Value.Content {
		if strings.Contains(contentType, "json") {
			return contentType
		}
	}

	// Return first available content type
	for contentType := range requestBody.Value.Content {
		return contentType
	}

	return "application/json"
}

// ToHTTPRequest converts a built Request to an *http.Request.
func (r *Request) ToHTTPRequest() (*http.Request, error) {
	var bodyReader *bytes.Reader
	if len(r.Body) > 0 {
		bodyReader = bytes.NewReader(r.Body)
	}

	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequest(r.Method, r.URL, bodyReader)
	} else {
		req, err = http.NewRequest(r.Method, r.URL, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}

	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}

	return req, nil
}
