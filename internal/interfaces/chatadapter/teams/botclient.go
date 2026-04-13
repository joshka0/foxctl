package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BotClient calls the Bot Framework Connector API for outbound messages/edits.
type BotClient struct {
	tokenMgr   *tokenManager
	httpClient *http.Client
}

func newBotClient(tokenMgr *tokenManager, httpClient *http.Client) *BotClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &BotClient{
		tokenMgr:   tokenMgr,
		httpClient: httpClient,
	}
}

func (c *BotClient) SendActivity(ctx context.Context, serviceURL, conversationID string, activity Activity) (ResourceResponse, error) {
	return c.postActivity(ctx, serviceURL, conversationID, "", activity)
}

func (c *BotClient) ReplyToActivity(ctx context.Context, serviceURL, conversationID, replyToActivityID string, activity Activity) (ResourceResponse, error) {
	return c.postActivity(ctx, serviceURL, conversationID, replyToActivityID, activity)
}

func (c *BotClient) postActivity(ctx context.Context, serviceURL, conversationID, replyToID string, activity Activity) (ResourceResponse, error) {
	baseURL, err := normalizeServiceURL(serviceURL)
	if err != nil {
		return ResourceResponse{}, err
	}
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		return ResourceResponse{}, fmt.Errorf("teams: missing conversation id")
	}

	path := baseURL + "/v3/conversations/" + url.PathEscape(conv) + "/activities"
	if strings.TrimSpace(replyToID) != "" {
		path += "/" + url.PathEscape(strings.TrimSpace(replyToID))
	}

	if strings.TrimSpace(activity.Type) == "" {
		activity.Type = "message"
	}
	if strings.TrimSpace(replyToID) != "" && strings.TrimSpace(activity.ReplyToID) == "" {
		activity.ReplyToID = strings.TrimSpace(replyToID)
	}

	var resp ResourceResponse
	if err := c.doJSON(ctx, http.MethodPost, path, activity, &resp); err != nil {
		return ResourceResponse{}, err
	}
	return resp, nil
}

func (c *BotClient) UpdateActivity(ctx context.Context, serviceURL, conversationID, activityID string, activity Activity) error {
	baseURL, err := normalizeServiceURL(serviceURL)
	if err != nil {
		return err
	}
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		return fmt.Errorf("teams: missing conversation id")
	}
	actID := strings.TrimSpace(activityID)
	if actID == "" {
		return fmt.Errorf("teams: missing activity id")
	}

	path := baseURL + "/v3/conversations/" + url.PathEscape(conv) + "/activities/" + url.PathEscape(actID)
	activity.ID = actID
	if strings.TrimSpace(activity.Type) == "" {
		activity.Type = "message"
	}
	return c.doJSON(ctx, http.MethodPut, path, activity, nil)
}

func (c *BotClient) SendTyping(ctx context.Context, serviceURL, conversationID string) error {
	baseURL, err := normalizeServiceURL(serviceURL)
	if err != nil {
		return err
	}
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		return fmt.Errorf("teams: missing conversation id")
	}

	path := baseURL + "/v3/conversations/" + url.PathEscape(conv) + "/activities"
	activity := Activity{Type: "typing"}
	return c.doJSON(ctx, http.MethodPost, path, activity, nil)
}

func (c *BotClient) doJSON(ctx context.Context, method, urlStr string, body any, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.tokenMgr == nil {
		return fmt.Errorf("teams: token manager not configured")
	}

	token, err := c.tokenMgr.Token(ctx)
	if err != nil {
		return err
	}

	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("teams: marshal request: %w", err)
		}
	}

	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, urlStr, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("teams: create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Context cancellation should exit quickly.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("teams: request: %w", err)
		}

		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("teams: read response: %w", readErr)
		}

		// Retry on 429 and transient 5xx.
		if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 500 && resp.StatusCode <= 599) {
			if attempt == maxRetries-1 {
				return fmt.Errorf("teams: connector API failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			}
			delay := backoffDelay(resp, attempt)
			if !sleepWithContext(ctx, delay) {
				return ctx.Err()
			}
			continue
		}

		// Refresh token on 401 and retry.
		if resp.StatusCode == http.StatusUnauthorized && attempt < maxRetries-1 {
			newToken, tokenErr := c.tokenMgr.Token(ctx)
			if tokenErr != nil {
				return fmt.Errorf("teams: refresh token after 401: %w", tokenErr)
			}
			token = newToken
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("teams: connector API failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}

		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("teams: decode response: %w", err)
			}
		}
		return nil
	}

	return fmt.Errorf("teams: request failed after retries")
}

func backoffDelay(resp *http.Response, attempt int) time.Duration {
	if resp == nil {
		return time.Second
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		if v := strings.TrimSpace(resp.Header.Get("Retry-After")); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				return time.Duration(secs) * time.Second
			}
		}
		return 2 * time.Second
	}

	// Simple exponential backoff for 5xx.
	switch attempt {
	case 0:
		return 500 * time.Millisecond
	case 1:
		return time.Second
	default:
		return 2 * time.Second
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func normalizeServiceURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("teams: missing serviceUrl")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("teams: invalid serviceUrl: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("teams: untrusted serviceUrl scheme %q (expected https)", u.Scheme)
	}
	if u.User != nil {
		return "", fmt.Errorf("teams: untrusted serviceUrl (userinfo not allowed)")
	}

	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if !isTrustedServiceHost(host) {
		return "", fmt.Errorf("teams: untrusted serviceUrl host %q", host)
	}

	u.Fragment = ""
	u.RawQuery = ""

	// Keep path (regional endpoint), but normalize trailing slash.
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/"), nil
}

func isTrustedServiceHost(host string) bool {
	if host == "" {
		return false
	}
	if host == "smba.trafficmanager.net" {
		return true
	}
	if strings.HasSuffix(host, ".botframework.com") {
		return true
	}
	if strings.HasSuffix(host, ".teams.microsoft.com") {
		return true
	}
	return false
}
