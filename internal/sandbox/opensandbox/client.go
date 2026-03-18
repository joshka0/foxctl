package opensandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultLifecycleBaseURL = "http://localhost:8080"
	DefaultSandboxImage     = "opensandbox/code-interpreter:v1.0.1"
	DefaultExecdPort        = 44772
	DefaultWorkspaceRoot    = "/workspace/repo"
)

type Config struct {
	BaseURL        string
	APIKey         string
	UseServerProxy bool
	HTTPClient     *http.Client
}

type Client struct {
	baseURL        string
	apiKey         string
	useServerProxy bool
	httpClient     *http.Client
}

type CreateSandboxRequest struct {
	Image          ImageSpec         `json:"image"`
	TimeoutSeconds *int              `json:"timeout,omitempty"`
	ResourceLimits map[string]string `json:"resourceLimits"`
	Env            map[string]string `json:"env,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Entrypoint     []string          `json:"entrypoint"`
	NetworkPolicy  *NetworkPolicy    `json:"networkPolicy,omitempty"`
	Extensions     map[string]string `json:"extensions,omitempty"`
	Volumes        []Volume          `json:"volumes,omitempty"`
}

type ImageSpec struct {
	URI  string     `json:"uri"`
	Auth *ImageAuth `json:"auth,omitempty"`
}

type ImageAuth struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type NetworkPolicy struct {
	DefaultAction string        `json:"defaultAction,omitempty"`
	Egress        []NetworkRule `json:"egress,omitempty"`
}

type NetworkRule struct {
	Action string `json:"action"`
	Target string `json:"target"`
}

type Volume struct {
	Name      string     `json:"name"`
	Host      *HostMount `json:"host,omitempty"`
	MountPath string     `json:"mountPath"`
	ReadOnly  bool       `json:"readOnly,omitempty"`
	SubPath   string     `json:"subPath,omitempty"`
}

type HostMount struct {
	Path string `json:"path"`
}

type SandboxStatus struct {
	State   string `json:"state,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type Sandbox struct {
	ID         string            `json:"id"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Status     SandboxStatus     `json:"status"`
	CreatedAt  string            `json:"createdAt,omitempty"`
	UpdatedAt  string            `json:"updatedAt,omitempty"`
	ExpiresAt  *string           `json:"expiresAt,omitempty"`
	Entrypoint []string          `json:"entrypoint,omitempty"`
}

type Endpoint struct {
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers,omitempty"`
}

type RunCommandRequest struct {
	Command string            `json:"command"`
	Cwd     string            `json:"cwd,omitempty"`
	Timeout int64             `json:"timeout,omitempty"`
	Envs    map[string]string `json:"envs,omitempty"`
}

type CommandExecution struct {
	Stdout string
	Stderr string
	Result string
	Error  string
}

type ProvisionWorkspaceRequest struct {
	RepoURL         string
	RepoRef         string
	Image           string
	Name            string
	WorkspaceRoot   string
	Timeout         time.Duration
	AllowEgress     []string
	UseServerProxy  bool
	ContextPackFile string
	ContextPackDest string
}

type ProvisionWorkspaceResult struct {
	SandboxID      string            `json:"sandbox_id"`
	RepoURL        string            `json:"repo_url"`
	RepoRef        string            `json:"repo_ref"`
	WorkspaceRoot  string            `json:"workspace_root"`
	ExecdEndpoint  string            `json:"execd_endpoint"`
	EndpointHeader map[string]string `json:"endpoint_headers,omitempty"`
	Bootstrap      CommandExecution  `json:"bootstrap"`
	ContextPack    *CommandExecution `json:"context_pack,omitempty"`
}

type RunSandboxCommandRequest struct {
	SandboxID string
	Command   string
	Cwd       string
	Timeout   time.Duration
	Envs      map[string]string
}

func New(cfg Config) *Client {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = DefaultLifecycleBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		apiKey:         strings.TrimSpace(cfg.APIKey),
		useServerProxy: cfg.UseServerProxy,
		httpClient:     httpClient,
	}
}

func ConfigFromEnv() Config {
	baseURL := strings.TrimSpace(os.Getenv("OPEN_SANDBOX_BASE_URL"))
	if baseURL == "" {
		if domain := strings.TrimSpace(os.Getenv("SANDBOX_DOMAIN")); domain != "" {
			if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
				baseURL = domain
			} else {
				baseURL = "http://" + domain
			}
		}
	}
	apiKey := strings.TrimSpace(os.Getenv("OPEN_SANDBOX_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("SANDBOX_API_KEY"))
	}
	useServerProxy := false
	if v := strings.TrimSpace(os.Getenv("OPEN_SANDBOX_USE_SERVER_PROXY")); v != "" {
		useServerProxy = strings.EqualFold(v, "true") || v == "1"
	}
	return Config{
		BaseURL:        baseURL,
		APIKey:         apiKey,
		UseServerProxy: useServerProxy,
	}
}

func (c *Client) CreateSandbox(ctx context.Context, req CreateSandboxRequest) (*Sandbox, error) {
	var created Sandbox
	if err := c.doJSON(ctx, http.MethodPost, "/v1/sandboxes", req, &created, nil); err != nil {
		return nil, err
	}
	return &created, nil
}

func (c *Client) GetSandbox(ctx context.Context, sandboxID string) (*Sandbox, error) {
	var sandbox Sandbox
	if err := c.doJSON(ctx, http.MethodGet, "/v1/sandboxes/"+neturl.PathEscape(strings.TrimSpace(sandboxID)), nil, &sandbox, nil); err != nil {
		return nil, err
	}
	return &sandbox, nil
}

func (c *Client) DeleteSandbox(ctx context.Context, sandboxID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/sandboxes/"+neturl.PathEscape(strings.TrimSpace(sandboxID)), nil, nil, nil)
}

func (c *Client) GetEndpoint(ctx context.Context, sandboxID string, port int) (*Endpoint, error) {
	query := neturl.Values{}
	if c.useServerProxy {
		query.Set("use_server_proxy", "true")
	}
	var endpoint Endpoint
	reqPath := fmt.Sprintf("/v1/sandboxes/%s/endpoints/%d", neturl.PathEscape(strings.TrimSpace(sandboxID)), port)
	if err := c.doJSON(ctx, http.MethodGet, reqPath, nil, &endpoint, query); err != nil {
		return nil, err
	}
	return &endpoint, nil
}

func (c *Client) RunCommand(ctx context.Context, endpoint Endpoint, req RunCommandRequest) (*CommandExecution, error) {
	targetURL, err := c.execdURL(endpoint.Endpoint, "/command")
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: marshal command request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("opensandbox: build command request: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range endpoint.Headers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("opensandbox: run command: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("opensandbox: run command status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	exec := &CommandExecution{}
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		var evt struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		switch evt.Type {
		case "stdout":
			exec.Stdout += evt.Text
		case "stderr":
			exec.Stderr += evt.Text
		case "result":
			exec.Result += evt.Text
		case "error":
			exec.Error += evt.Text
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("opensandbox: scan command stream: %w", err)
	}
	return exec, nil
}

func (c *Client) ProvisionShallowCloneWorkspace(ctx context.Context, req ProvisionWorkspaceRequest) (*ProvisionWorkspaceResult, error) {
	if strings.TrimSpace(req.RepoURL) == "" {
		return nil, fmt.Errorf("opensandbox: repo url is required")
	}
	repoRef := strings.TrimSpace(req.RepoRef)
	if repoRef == "" {
		repoRef = "main"
	}
	image := strings.TrimSpace(req.Image)
	if image == "" {
		image = DefaultSandboxImage
	}
	workspaceRoot := strings.TrimSpace(req.WorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = DefaultWorkspaceRoot
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = time.Hour
	}
	timeoutSeconds := int(timeout / time.Second)

	lifecycleReq := CreateSandboxRequest{
		Image: ImageSpec{URI: image},
		ResourceLimits: map[string]string{
			"cpu":    "1000m",
			"memory": "1Gi",
		},
		Entrypoint:     []string{"/bin/sh", "-lc", "while true; do sleep 3600; done"},
		TimeoutSeconds: &timeoutSeconds,
		Metadata: map[string]string{
			"name":     firstNonEmpty(req.Name, "agentctl-sandbox"),
			"repo_url": req.RepoURL,
			"repo_ref": repoRef,
		},
		NetworkPolicy: buildNetworkPolicy(req.RepoURL, req.AllowEgress),
	}
	sandbox, err := c.CreateSandbox(ctx, lifecycleReq)
	if err != nil {
		return nil, err
	}

	endpoint, err := c.GetEndpoint(ctx, sandbox.ID, DefaultExecdPort)
	if err != nil {
		return nil, err
	}

	bootstrap, err := c.RunCommand(ctx, *endpoint, RunCommandRequest{
		Command: buildShallowCloneCommand(req.RepoURL, repoRef, workspaceRoot),
	})
	if err != nil {
		return nil, err
	}

	result := &ProvisionWorkspaceResult{
		SandboxID:      sandbox.ID,
		RepoURL:        req.RepoURL,
		RepoRef:        repoRef,
		WorkspaceRoot:  workspaceRoot,
		ExecdEndpoint:  endpoint.Endpoint,
		EndpointHeader: endpoint.Headers,
		Bootstrap:      *bootstrap,
	}

	if strings.TrimSpace(req.ContextPackFile) != "" {
		contextDest := strings.TrimSpace(req.ContextPackDest)
		if contextDest == "" {
			contextDest = path.Join(workspaceRoot, ".agentctl", "context-pack.md")
		}
		absSrc, err := filepath.Abs(req.ContextPackFile)
		if err != nil {
			absSrc = req.ContextPackFile
		}
		contextData, err := os.ReadFile(absSrc)
		if err != nil {
			return nil, fmt.Errorf("opensandbox: read context pack: %w", err)
		}
		contextRun, err := c.RunCommand(ctx, *endpoint, RunCommandRequest{
			Command: buildContextPackWriteCommand(string(contextData), contextDest),
		})
		if err != nil {
			return nil, err
		}
		result.ContextPack = contextRun
	}

	return result, nil
}

func (c *Client) RunSandboxCommand(ctx context.Context, req RunSandboxCommandRequest) (*CommandExecution, error) {
	if strings.TrimSpace(req.SandboxID) == "" {
		return nil, fmt.Errorf("opensandbox: sandbox id is required")
	}
	if strings.TrimSpace(req.Command) == "" {
		return nil, fmt.Errorf("opensandbox: command is required")
	}
	endpoint, err := c.GetEndpoint(ctx, req.SandboxID, DefaultExecdPort)
	if err != nil {
		return nil, err
	}
	timeoutMS := int64(0)
	if req.Timeout > 0 {
		timeoutMS = req.Timeout.Milliseconds()
	}
	return c.RunCommand(ctx, *endpoint, RunCommandRequest{
		Command: req.Command,
		Cwd:     strings.TrimSpace(req.Cwd),
		Timeout: timeoutMS,
		Envs:    req.Envs,
	})
}

func (c *Client) doJSON(ctx context.Context, method, reqPath string, body any, out any, query neturl.Values) error {
	base, err := neturl.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("opensandbox: invalid base url %q: %w", c.baseURL, err)
	}
	base.Path = path.Join(base.Path, reqPath)
	base.RawQuery = query.Encode()
	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("opensandbox: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, base.String(), bodyReader)
	if err != nil {
		return fmt.Errorf("opensandbox: build request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		httpReq.Header.Set("OPEN-SANDBOX-API-KEY", c.apiKey)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("opensandbox: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("opensandbox: %s %s failed with status %d: %s", method, reqPath, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("opensandbox: decode response: %w", err)
	}
	return nil
}

func (c *Client) execdURL(endpointHost, reqPath string) (string, error) {
	endpointHost = strings.TrimSpace(endpointHost)
	if endpointHost == "" {
		return "", fmt.Errorf("opensandbox: endpoint host is required")
	}
	if strings.HasPrefix(endpointHost, "http://") || strings.HasPrefix(endpointHost, "https://") {
		u, err := neturl.Parse(endpointHost)
		if err != nil {
			return "", fmt.Errorf("opensandbox: parse endpoint url: %w", err)
		}
		u.Path = path.Join(u.Path, reqPath)
		return u.String(), nil
	}
	base, err := neturl.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("opensandbox: parse base url: %w", err)
	}
	base.Host = endpointHost
	base.Path = path.Join("/", reqPath)
	if strings.Contains(endpointHost, "/") {
		parts := strings.SplitN(endpointHost, "/", 2)
		base.Host = parts[0]
		base.Path = path.Join("/", parts[1], reqPath)
	}
	return base.String(), nil
}

func buildNetworkPolicy(repoURL string, allow []string) *NetworkPolicy {
	targets := make([]string, 0, len(allow)+4)
	targets = append(targets, allow...)
	if u, err := neturl.Parse(strings.TrimSpace(repoURL)); err == nil {
		if host := strings.TrimSpace(u.Hostname()); host != "" {
			targets = append(targets, host)
			if host == "github.com" {
				targets = append(targets, "codeload.github.com", "objects.githubusercontent.com")
			}
		}
	}
	targets = dedupeStrings(targets)
	if len(targets) == 0 {
		return nil
	}
	policy := &NetworkPolicy{DefaultAction: "deny"}
	for _, target := range targets {
		policy.Egress = append(policy.Egress, NetworkRule{Action: "allow", Target: target})
	}
	return policy
}

func buildShallowCloneCommand(repoURL, repoRef, workspaceRoot string) string {
	return fmt.Sprintf(
		"set -eu; rm -rf %s; mkdir -p %s; git init %s; git -C %s remote add origin %s; git -C %s fetch --depth 1 origin %s; git -C %s checkout FETCH_HEAD",
		shellQuote(workspaceRoot),
		shellQuote(workspaceRoot),
		shellQuote(workspaceRoot),
		shellQuote(workspaceRoot),
		shellQuote(repoURL),
		shellQuote(workspaceRoot),
		shellQuote(repoRef),
		shellQuote(workspaceRoot),
	)
}

func buildContextPackWriteCommand(contents, destPath string) string {
	encoded, _ := json.Marshal(contents)
	destDir := path.Dir(destPath)
	return fmt.Sprintf(
		"set -eu; mkdir -p %s; python3 - <<'PY'\nfrom pathlib import Path\nPath(%s).write_text(%s)\nPY",
		shellQuote(destDir),
		stringLiteralJSON(destPath),
		string(encoded),
	)
}

func stringLiteralJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
