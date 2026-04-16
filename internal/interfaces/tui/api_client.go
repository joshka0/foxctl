package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const maxAPIErrorBodyBytes = 16 * 1024

// APIClient is a small typed HTTP client for foxctl web API surfaces.
type APIClient struct {
	baseURL    string
	httpClient *http.Client
}

// HTTPStatusError reports non-2xx responses with method/url/status/body context.
type HTTPStatusError struct {
	Method     string
	URL        string
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	summary := fmt.Sprintf("%s %s failed: HTTP %d %s", e.Method, e.URL, e.StatusCode, e.Status)
	if strings.TrimSpace(e.Body) == "" {
		return summary
	}
	return summary + ": " + e.Body
}

// NewAPIClient builds an API client with normalized base URL and an injected http.Client.
func NewAPIClient(baseURL string, httpClient *http.Client) (*APIClient, error) {
	normalized, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &APIClient{
		baseURL:    normalized,
		httpClient: httpClient,
	}, nil
}

func (c *APIClient) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// RequestJSON sends a JSON request and decodes the JSON response into responseBody when provided.
func (c *APIClient) RequestJSON(ctx context.Context, method string, endpoint string, requestBody any, responseBody any) error {
	if c == nil {
		return errors.New("api client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	httpMethod := strings.ToUpper(strings.TrimSpace(method))
	if httpMethod == "" {
		return errors.New("http method is required")
	}

	requestURL, err := c.endpointURL(endpoint)
	if err != nil {
		return err
	}

	var bodyReader io.Reader
	if requestBody != nil {
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, httpMethod, requestURL, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", httpMethod, endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxAPIErrorBodyBytes))
		if readErr != nil {
			return fmt.Errorf("read error response: %w", readErr)
		}
		return &HTTPStatusError{
			Method:     httpMethod,
			URL:        requestURL,
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       strings.TrimSpace(string(raw)),
		}
	}

	if responseBody == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(responseBody); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}

func (c *APIClient) endpointURL(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", errors.New("endpoint path is required")
	}

	ref, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint %q: %w", endpoint, err)
	}
	if ref.IsAbs() {
		return "", fmt.Errorf("endpoint %q must be a relative API path", endpoint)
	}

	if ref.Path == "" {
		ref.Path = "/"
	}
	if !strings.HasPrefix(ref.Path, "/") {
		ref.Path = "/" + ref.Path
	}

	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base url %q: %w", c.baseURL, err)
	}
	basePath := strings.TrimSuffix(base.Path, "/")
	if basePath != "" {
		ref.Path = path.Clean(basePath + "/" + strings.TrimPrefix(ref.Path, "/"))
		if !strings.HasPrefix(ref.Path, "/") {
			ref.Path = "/" + ref.Path
		}
	}

	return base.ResolveReference(ref).String(), nil
}

func normalizeBaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("api base url is required")
	}

	if !strings.Contains(value, "://") {
		value = "http://" + value
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse api base url %q: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("api base url %q must include scheme and host", raw)
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""

	pathPart := strings.TrimSpace(parsed.Path)
	switch pathPart {
	case "", "/":
		parsed.Path = ""
	default:
		cleanPath := path.Clean("/" + strings.Trim(pathPart, "/"))
		if cleanPath == "/" {
			parsed.Path = ""
		} else {
			parsed.Path = cleanPath
		}
	}

	return strings.TrimSuffix(parsed.String(), "/"), nil
}
