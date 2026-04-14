package jira

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
	defaultTimeout  = 30 * time.Second
	boardAPIBase    = "/rest/agile/1.0"
	platformAPIBase = "/rest/api/3"
)

// Config captures Jira Cloud connection settings.
type Config struct {
	BaseURL    string
	Email      string
	APIToken   string
	HTTPClient *http.Client
}

// Client performs Jira Cloud REST requests using email + API token auth.
type Client struct {
	baseURL    string
	email      string
	apiToken   string
	httpClient *http.Client
}

// Error captures a Jira error response with HTTP metadata.
type Error struct {
	StatusCode    int               `json:"status_code"`
	ErrorMessages []string          `json:"error_messages,omitempty"`
	Errors        map[string]string `json:"errors,omitempty"`
	RawBody       string            `json:"raw_body,omitempty"`
}

func (e *Error) Error() string {
	if len(e.ErrorMessages) > 0 {
		return fmt.Sprintf("jira request failed: %s", strings.Join(e.ErrorMessages, "; "))
	}
	if len(e.Errors) > 0 {
		parts := make([]string, 0, len(e.Errors))
		for k, v := range e.Errors {
			parts = append(parts, fmt.Sprintf("%s: %s", k, v))
		}
		return fmt.Sprintf("jira request failed: %s", strings.Join(parts, "; "))
	}
	if e.RawBody != "" {
		return fmt.Sprintf("jira request failed (%d): %s", e.StatusCode, e.RawBody)
	}
	return fmt.Sprintf("jira request failed with status %d", e.StatusCode)
}

type errorEnvelope struct {
	ErrorMessages []string          `json:"errorMessages"`
	Errors        map[string]string `json:"errors"`
}

type issuesMutation struct {
	Issues []string `json:"issues"`
}

// NewClient returns a Jira client from explicit config.
func NewClient(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	email := strings.TrimSpace(cfg.Email)
	token := strings.TrimSpace(cfg.APIToken)
	if baseURL == "" {
		return nil, fmt.Errorf("jira base url is required")
	}
	if email == "" {
		return nil, fmt.Errorf("jira email is required")
	}
	if token == "" {
		return nil, fmt.Errorf("jira api token is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		baseURL:    baseURL,
		email:      email,
		apiToken:   token,
		httpClient: httpClient,
	}, nil
}

// NewClientFromEnv creates a Jira client from environment variables.
func NewClientFromEnv() (*Client, error) {
	return NewClient(Config{
		BaseURL:  os.Getenv("JIRA_BASE_URL"),
		Email:    os.Getenv("JIRA_EMAIL"),
		APIToken: os.Getenv("JIRA_API_TOKEN"),
	})
}

// TextToADF converts plain text into Atlassian Document Format paragraphs.
func TextToADF(text string) map[string]any {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	content := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		paragraph := map[string]any{
			"type": "paragraph",
		}
		if line != "" {
			paragraph["content"] = []map[string]any{
				{
					"type": "text",
					"text": line,
				},
			}
		}
		content = append(content, paragraph)
	}
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": content,
	}
}

func normalizeADF(body string, adf map[string]any) any {
	if len(adf) > 0 {
		return adf
	}
	return TextToADF(strings.TrimSpace(body))
}

// ListBoards lists Jira boards with optional filtering.
func (c *Client) ListBoards(ctx context.Context, startAt, maxResults int, projectKeyOrID, name, boardType string) (map[string]any, error) {
	query := url.Values{}
	addPagination(query, startAt, maxResults)
	addIfNonEmpty(query, "projectKeyOrId", projectKeyOrID)
	addIfNonEmpty(query, "name", name)
	addIfNonEmpty(query, "type", boardType)
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, boardAPIBase+"/board", query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListBoardIssues lists issues for a specific board.
func (c *Client) ListBoardIssues(ctx context.Context, boardID int, jql string, fields []string, startAt, maxResults int) (map[string]any, error) {
	query := url.Values{}
	addPagination(query, startAt, maxResults)
	addIfNonEmpty(query, "jql", jql)
	addCSV(query, "fields", fields)
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/board/%d/issue", boardAPIBase, boardID), query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListBoardSprints lists sprints for a specific board.
func (c *Client) ListBoardSprints(ctx context.Context, boardID int, state string, startAt, maxResults int) (map[string]any, error) {
	query := url.Values{}
	addPagination(query, startAt, maxResults)
	addIfNonEmpty(query, "state", state)
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/board/%d/sprint", boardAPIBase, boardID), query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListBacklogIssues lists issues in the backlog for a specific board.
func (c *Client) ListBacklogIssues(ctx context.Context, boardID int, jql string, fields []string, startAt, maxResults int) (map[string]any, error) {
	query := url.Values{}
	addPagination(query, startAt, maxResults)
	addIfNonEmpty(query, "jql", jql)
	addCSV(query, "fields", fields)
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/board/%d/backlog", boardAPIBase, boardID), query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListSprintIssues lists issues for a specific sprint.
func (c *Client) ListSprintIssues(ctx context.Context, sprintID int, jql string, fields []string, startAt, maxResults int) (map[string]any, error) {
	query := url.Values{}
	addPagination(query, startAt, maxResults)
	addIfNonEmpty(query, "jql", jql)
	addCSV(query, "fields", fields)
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/sprint/%d/issue", boardAPIBase, sprintID), query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MoveIssuesToSprint places issues into a sprint.
func (c *Client) MoveIssuesToSprint(ctx context.Context, sprintID int, issues []string) error {
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("%s/sprint/%d/issue", boardAPIBase, sprintID), nil, issuesMutation{Issues: issues}, nil)
}

// MoveIssuesToBacklog removes issues from active or future sprints into backlog.
func (c *Client) MoveIssuesToBacklog(ctx context.Context, issues []string) error {
	return c.doJSON(ctx, http.MethodPost, boardAPIBase+"/backlog/issue", nil, issuesMutation{Issues: issues}, nil)
}

// SearchIssues executes a JQL search using Jira's enhanced search endpoint.
func (c *Client) SearchIssues(ctx context.Context, jql, nextPageToken string, maxResults int, fields, expand []string) (map[string]any, error) {
	query := url.Values{}
	addIfNonEmpty(query, "jql", jql)
	addIfNonEmpty(query, "nextPageToken", nextPageToken)
	if maxResults > 0 {
		query.Set("maxResults", fmt.Sprintf("%d", maxResults))
	}
	addCSV(query, "fields", fields)
	addCSV(query, "expand", expand)
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, platformAPIBase+"/search/jql", query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListProjects lists accessible Jira projects.
func (c *Client) ListProjects(ctx context.Context, startAt, maxResults int, queryText string) (map[string]any, error) {
	query := url.Values{}
	addPagination(query, startAt, maxResults)
	addIfNonEmpty(query, "query", queryText)
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, platformAPIBase+"/project/search", query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetProject returns one project and optional expansions.
func (c *Client) GetProject(ctx context.Context, projectKey string, expand []string) (map[string]any, error) {
	query := url.Values{}
	addCSV(query, "expand", expand)
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, platformAPIBase+"/project/"+url.PathEscape(projectKey), query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetIssue returns one issue by key or id.
func (c *Client) GetIssue(ctx context.Context, key string, fields, expand []string) (map[string]any, error) {
	query := url.Values{}
	addCSV(query, "fields", fields)
	addCSV(query, "expand", expand)
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, platformAPIBase+"/issue/"+url.PathEscape(key), query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListIssueLinks returns issue link information for an issue.
func (c *Client) ListIssueLinks(ctx context.Context, key string) (map[string]any, error) {
	return c.GetIssue(ctx, key, []string{"issuelinks", "summary", "status"}, nil)
}

// ListComments lists comments for an issue.
func (c *Client) ListComments(ctx context.Context, key string, startAt, maxResults int) (map[string]any, error) {
	query := url.Values{}
	addPagination(query, startAt, maxResults)
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, platformAPIBase+"/issue/"+url.PathEscape(key)+"/comment", query, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateComment updates an existing issue comment.
func (c *Client) UpdateComment(ctx context.Context, key, commentID, bodyText string, bodyADF, visibility map[string]any) (map[string]any, error) {
	body := map[string]any{
		"body": normalizeADF(bodyText, bodyADF),
	}
	if len(visibility) > 0 {
		body["visibility"] = visibility
	}
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodPut, platformAPIBase+"/issue/"+url.PathEscape(key)+"/comment/"+url.PathEscape(commentID), nil, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteComment removes a comment from an issue.
func (c *Client) DeleteComment(ctx context.Context, key, commentID string) error {
	return c.doJSON(ctx, http.MethodDelete, platformAPIBase+"/issue/"+url.PathEscape(key)+"/comment/"+url.PathEscape(commentID), nil, nil, nil)
}

// CreateIssue creates a Jira issue.
func (c *Client) CreateIssue(ctx context.Context, fields, update map[string]any) (map[string]any, error) {
	body := map[string]any{
		"fields": fields,
	}
	if len(update) > 0 {
		body["update"] = update
	}
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodPost, platformAPIBase+"/issue", nil, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateIssue updates an existing Jira issue.
func (c *Client) UpdateIssue(ctx context.Context, key string, fields, update map[string]any, notifyUsers *bool) error {
	query := url.Values{}
	if notifyUsers != nil {
		query.Set("notifyUsers", fmt.Sprintf("%t", *notifyUsers))
	}
	body := map[string]any{}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	if len(update) > 0 {
		body["update"] = update
	}
	return c.doJSON(ctx, http.MethodPut, platformAPIBase+"/issue/"+url.PathEscape(key), query, body, nil)
}

// AddComment appends a comment to an issue.
func (c *Client) AddComment(ctx context.Context, key, bodyText string, bodyADF, visibility map[string]any) (map[string]any, error) {
	body := map[string]any{
		"body": normalizeADF(bodyText, bodyADF),
	}
	if len(visibility) > 0 {
		body["visibility"] = visibility
	}
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodPost, platformAPIBase+"/issue/"+url.PathEscape(key)+"/comment", nil, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListLinkTypes returns the available Jira issue link types.
func (c *Client) ListLinkTypes(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, platformAPIBase+"/issueLinkType", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateIssueLink creates a Jira issue link between two issues.
func (c *Client) CreateIssueLink(ctx context.Context, outwardIssueKey, inwardIssueKey, linkTypeName, commentText string, commentADF map[string]any) error {
	body := map[string]any{
		"type": map[string]any{
			"name": linkTypeName,
		},
		"outwardIssue": map[string]any{
			"key": outwardIssueKey,
		},
		"inwardIssue": map[string]any{
			"key": inwardIssueKey,
		},
	}
	if strings.TrimSpace(commentText) != "" || len(commentADF) > 0 {
		body["comment"] = map[string]any{
			"body": normalizeADF(commentText, commentADF),
		}
	}
	return c.doJSON(ctx, http.MethodPost, platformAPIBase+"/issueLink", nil, body, nil)
}

// DeleteIssueLink removes a Jira issue link by id.
func (c *Client) DeleteIssueLink(ctx context.Context, linkID string) error {
	return c.doJSON(ctx, http.MethodDelete, platformAPIBase+"/issueLink/"+url.PathEscape(linkID), nil, nil, nil)
}

// ListWatchers returns watcher information for an issue.
func (c *Client) ListWatchers(ctx context.Context, key string) (map[string]any, error) {
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, platformAPIBase+"/issue/"+url.PathEscape(key)+"/watchers", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddWatcher adds a watcher account id to an issue. Empty account id adds the caller.
func (c *Client) AddWatcher(ctx context.Context, key, accountID string) error {
	payload := strings.TrimSpace(accountID)
	if payload == "" {
		payload = `""`
	} else {
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal watcher account id: %w", err)
		}
		payload = string(payloadBytes)
	}
	return c.doRaw(ctx, http.MethodPost, platformAPIBase+"/issue/"+url.PathEscape(key)+"/watchers", nil, []byte(payload))
}

// RemoveWatcher removes a watcher account id from an issue.
func (c *Client) RemoveWatcher(ctx context.Context, key, accountID string) error {
	query := url.Values{}
	addIfNonEmpty(query, "accountId", accountID)
	return c.doJSON(ctx, http.MethodDelete, platformAPIBase+"/issue/"+url.PathEscape(key)+"/watchers", query, nil, nil)
}

// ListTransitions returns available transitions for an issue.
func (c *Client) ListTransitions(ctx context.Context, key string) (map[string]any, error) {
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, platformAPIBase+"/issue/"+url.PathEscape(key)+"/transitions", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// TransitionIssue applies a workflow transition to an issue.
func (c *Client) TransitionIssue(ctx context.Context, key, transitionID string, fields, update map[string]any, commentText string, commentADF map[string]any) error {
	if strings.TrimSpace(commentText) != "" || len(commentADF) > 0 {
		commentOp := map[string]any{
			"add": map[string]any{
				"body": normalizeADF(commentText, commentADF),
			},
		}
		if update == nil {
			update = make(map[string]any)
		}
		update["comment"] = []map[string]any{commentOp}
	}
	body := map[string]any{
		"transition": map[string]any{
			"id": transitionID,
		},
	}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	if len(update) > 0 {
		body["update"] = update
	}
	return c.doJSON(ctx, http.MethodPost, platformAPIBase+"/issue/"+url.PathEscape(key)+"/transitions", nil, body, nil)
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
			return fmt.Errorf("marshal jira request body: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("create jira request: %w", err)
	}
	req.SetBasicAuth(c.email, c.apiToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("jira request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read jira response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		jiraErr := &Error{StatusCode: resp.StatusCode, RawBody: strings.TrimSpace(string(data))}
		var env errorEnvelope
		if err := json.Unmarshal(data, &env); err == nil {
			jiraErr.ErrorMessages = env.ErrorMessages
			jiraErr.Errors = env.Errors
		}
		return jiraErr
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode jira response: %w", err)
	}
	return nil
}

func (c *Client) doRaw(ctx context.Context, method, path string, query url.Values, body []byte) error {
	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create jira request: %w", err)
	}
	req.SetBasicAuth(c.email, c.apiToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("jira request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read jira response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		jiraErr := &Error{StatusCode: resp.StatusCode, RawBody: strings.TrimSpace(string(data))}
		var env errorEnvelope
		if err := json.Unmarshal(data, &env); err == nil {
			jiraErr.ErrorMessages = env.ErrorMessages
			jiraErr.Errors = env.Errors
		}
		return jiraErr
	}
	return nil
}

func addPagination(query url.Values, startAt, maxResults int) {
	if startAt > 0 {
		query.Set("startAt", fmt.Sprintf("%d", startAt))
	}
	if maxResults > 0 {
		query.Set("maxResults", fmt.Sprintf("%d", maxResults))
	}
}

func addIfNonEmpty(query url.Values, key, value string) {
	if strings.TrimSpace(value) != "" {
		query.Set(key, strings.TrimSpace(value))
	}
}

func addCSV(query url.Values, key string, values []string) {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) > 0 {
		query.Set(key, strings.Join(cleaned, ","))
	}
}
