package ardoq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://app.ardoq.com"
	defaultTimeout = 30 * time.Second
	apiBase        = "/api/v2"
)

// Config captures Ardoq Public API connection settings.
type Config struct {
	BaseURL    string
	OrgLabel   string
	APIToken   string
	HTTPClient *http.Client
}

// Client performs Ardoq Public API requests using bearer-token auth.
type Client struct {
	baseURL    string
	orgLabel   string
	apiToken   string
	httpClient *http.Client
}

// Error captures an Ardoq error response with HTTP metadata.
type Error struct {
	StatusCode int            `json:"status_code"`
	RawBody    string         `json:"raw_body,omitempty"`
	Body       map[string]any `json:"body,omitempty"`
}

func (e *Error) Error() string {
	if message, ok := e.Body["message"].(string); ok && strings.TrimSpace(message) != "" {
		return fmt.Sprintf("ardoq request failed (%d): %s", e.StatusCode, message)
	}
	if errorText, ok := e.Body["error"].(string); ok && strings.TrimSpace(errorText) != "" {
		return fmt.Sprintf("ardoq request failed (%d): %s", e.StatusCode, errorText)
	}
	if e.RawBody != "" {
		return fmt.Sprintf("ardoq request failed (%d): %s", e.StatusCode, e.RawBody)
	}
	return fmt.Sprintf("ardoq request failed with status %d", e.StatusCode)
}

// NewClient returns an Ardoq client from explicit config.
func NewClient(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	orgLabel := strings.TrimSpace(cfg.OrgLabel)
	token := strings.TrimSpace(cfg.APIToken)
	if orgLabel == "" {
		return nil, fmt.Errorf("ardoq org label is required")
	}
	if token == "" {
		return nil, fmt.Errorf("ardoq api token is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		baseURL:    baseURL,
		orgLabel:   orgLabel,
		apiToken:   token,
		httpClient: httpClient,
	}, nil
}

// NewClientFromEnv creates an Ardoq client from environment variables.
func NewClientFromEnv() (*Client, error) {
	return NewClient(Config{
		BaseURL:  os.Getenv("ARDOQ_API_HOST"),
		OrgLabel: os.Getenv("ARDOQ_ORG_LABEL"),
		APIToken: os.Getenv("ARDOQ_API_TOKEN"),
	})
}

// ListWorkspaces lists workspaces. Ardoq recommends filtering list endpoints with query parameters.
func (c *Client) ListWorkspaces(ctx context.Context, filters map[string]any) (map[string]any, error) {
	query, err := queryValues(filters)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, apiBase+"/workspaces", query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkspace fetches one workspace by Ardoq OID.
func (c *Client) GetWorkspace(ctx context.Context, id string) (map[string]any, error) {
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, apiBase+"/workspaces/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkspaceContext fetches component and reference type metadata for one workspace.
func (c *Client) GetWorkspaceContext(ctx context.Context, id string) (map[string]any, error) {
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, apiBase+"/workspaces/"+url.PathEscape(id)+"/context", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListComponents searches components with Ardoq query parameters.
func (c *Client) ListComponents(ctx context.Context, filters map[string]any) (map[string]any, error) {
	query, err := queryValues(filters)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, apiBase+"/components", query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetComponent fetches one component by Ardoq OID.
func (c *Client) GetComponent(ctx context.Context, id string) (map[string]any, error) {
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, apiBase+"/components/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListReferences searches references with Ardoq query parameters.
func (c *Client) ListReferences(ctx context.Context, filters map[string]any) (map[string]any, error) {
	query, err := queryValues(filters)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, apiBase+"/references", query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetReference fetches one reference by Ardoq OID.
func (c *Client) GetReference(ctx context.Context, id string) (map[string]any, error) {
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, apiBase+"/references/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Batch executes Ardoq's recommended transactional Batch API for component/reference mutations.
func (c *Client) Batch(ctx context.Context, body map[string]any) (map[string]any, error) {
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodPost, apiBase+"/batch", nil, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal ardoq request body: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("create ardoq request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("X-org", c.orgLabel)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ardoq request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read ardoq response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		ardoqErr := &Error{StatusCode: resp.StatusCode, RawBody: strings.TrimSpace(string(data))}
		var env map[string]any
		if err := json.Unmarshal(data, &env); err == nil {
			ardoqErr.Body = env
		}
		return ardoqErr
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode ardoq response: %w", err)
	}
	return nil
}

func queryValues(filters map[string]any) (url.Values, error) {
	query := url.Values{}
	for key, value := range filters {
		key = strings.TrimSpace(key)
		if key == "" || value == nil {
			continue
		}
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				text, ok := scalarString(item)
				if !ok {
					return nil, fmt.Errorf("query parameter %q contains non-scalar value", key)
				}
				if text != "" {
					query.Add(key, text)
				}
			}
		case []string:
			for _, item := range typed {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					query.Add(key, trimmed)
				}
			}
		default:
			text, ok := scalarString(value)
			if !ok {
				return nil, fmt.Errorf("query parameter %q must be a scalar or array of scalars", key)
			}
			if text != "" {
				query.Set(key, text)
			}
		}
	}
	return query, nil
}

func scalarString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed), true
	case bool:
		return fmt.Sprintf("%t", typed), true
	case int:
		return fmt.Sprintf("%d", typed), true
	case int64:
		return fmt.Sprintf("%d", typed), true
	case float64:
		return fmt.Sprintf("%v", typed), true
	case json.Number:
		return typed.String(), true
	default:
		return "", false
	}
}
