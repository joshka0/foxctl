package social

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
)

const (
	defaultXBaseURL       = "https://api.x.com/2"
	defaultRedditBaseURL  = "https://oauth.reddit.com"
	defaultRedditTokenURL = "https://www.reddit.com/api/v1/access_token"
	defaultYouTubeBaseURL = "https://www.googleapis.com/youtube/v3"
	defaultMetaBaseURL    = "https://graph.facebook.com"
	defaultMetaVersion    = "v25.0"
)

// DefaultClient returns a production client for direct provider API calls.
func DefaultClient() *Client {
	return &Client{
		HTTP:           &http.Client{Timeout: 30 * time.Second},
		Now:            time.Now,
		Env:            os.Getenv,
		XBaseURL:       defaultXBaseURL,
		RedditBaseURL:  defaultRedditBaseURL,
		RedditTokenURL: defaultRedditTokenURL,
		YouTubeBaseURL: defaultYouTubeBaseURL,
		MetaBaseURL:    defaultMetaBaseURL,
	}
}

// Collect plans and optionally executes one direct social-provider API call.
func Collect(ctx context.Context, c *Client, platform Platform, in Input) (Output, error) {
	c = normalizeClient(c)
	op := strings.TrimSpace(in.Operation)
	if op == "" {
		return Output{}, skillerr.Arg("operation is required")
	}

	plan, err := planFor(platform, c, in)
	if err != nil {
		return Output{}, err
	}

	capturedAt := c.Now().UTC().Format(time.RFC3339)
	out := Output{
		Platform:   string(platform),
		Provider:   providerID(platform),
		Operation:  op,
		DryRun:     in.DryRun,
		Request:    plan.request,
		Cost:       plan.cost,
		Warnings:   plan.warnings,
		CapturedAt: capturedAt,
	}
	if in.DryRun {
		return out, nil
	}

	var raw any
	rateLimit, err := c.doJSON(ctx, plan, &raw)
	if err != nil {
		return Output{}, err
	}
	out.RateLimit = rateLimit

	items, pagination, err := plan.parse(raw)
	if err != nil {
		return Output{}, err
	}
	if !in.IncludeRaw {
		for i := range items {
			items[i].Raw = nil
		}
	}
	out.Items = items
	out.Pagination = pagination
	if in.IncludeRaw {
		out.Raw = raw
	}
	return out, nil
}

func normalizeClient(c *Client) *Client {
	if c == nil {
		return DefaultClient()
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Env == nil {
		c.Env = os.Getenv
	}
	if c.XBaseURL == "" {
		c.XBaseURL = defaultXBaseURL
	}
	if c.RedditBaseURL == "" {
		c.RedditBaseURL = defaultRedditBaseURL
	}
	if c.RedditTokenURL == "" {
		c.RedditTokenURL = defaultRedditTokenURL
	}
	if c.YouTubeBaseURL == "" {
		c.YouTubeBaseURL = defaultYouTubeBaseURL
	}
	if c.MetaBaseURL == "" {
		c.MetaBaseURL = defaultMetaBaseURL
	}
	return c
}

func planFor(platform Platform, c *Client, in Input) (callPlan, error) {
	switch platform {
	case PlatformX:
		return planX(c.XBaseURL, in)
	case PlatformReddit:
		return planReddit(c.RedditBaseURL, in)
	case PlatformYouTube:
		return planYouTube(c.YouTubeBaseURL, in)
	case PlatformFacebook:
		return planFacebook(c.MetaBaseURL, in)
	case PlatformInstagram:
		return planInstagram(c.MetaBaseURL, in)
	default:
		return callPlan{}, skillerr.Argf("unsupported social platform: %s", platform)
	}
}

func (c *Client) doJSON(ctx context.Context, plan callPlan, dst *any) (*RateLimit, error) {
	req, err := http.NewRequestWithContext(ctx, plan.request.Method, plan.request.URL, nil)
	if err != nil {
		return nil, skillerr.WrapRuntime("build social API request", err)
	}
	req.Header.Set("Accept", "application/json")
	if plan.prepare != nil {
		if err := plan.prepare(ctx, c, req); err != nil {
			return nil, err
		}
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, skillerr.WrapIO("call social API", err)
	}
	defer resp.Body.Close()

	rateLimit := readRateLimit(resp.Header, c.Now())
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return rateLimit, skillerr.Integration(
			fmt.Sprintf("social API returned HTTP %d", resp.StatusCode),
			skillerr.WithData("status", resp.StatusCode),
			skillerr.WithData("body", strings.TrimSpace(string(body))),
		)
	}

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return rateLimit, skillerr.WrapParse("decode social API response", err)
	}
	return rateLimit, nil
}

func bearerAuth(envNames ...string) func(context.Context, *Client, *http.Request) error {
	return func(_ context.Context, c *Client, req *http.Request) error {
		token, _ := firstEnv(c, envNames...)
		if token == "" {
			return skillerr.Auth(
				"missing social API bearer token",
				skillerr.WithHint("Set "+strings.Join(envNames, " or ")+". Use dry_run:true to inspect the request without credentials."),
			)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

func youtubeAPIKeyAuth() func(context.Context, *Client, *http.Request) error {
	return func(_ context.Context, c *Client, req *http.Request) error {
		key := strings.TrimSpace(c.Env("YOUTUBE_API_KEY"))
		if key == "" {
			return skillerr.Auth(
				"missing YouTube Data API key",
				skillerr.WithHint("Set YOUTUBE_API_KEY. Use dry_run:true to inspect the request without credentials."),
			)
		}
		q := req.URL.Query()
		q.Set("key", key)
		req.URL.RawQuery = q.Encode()
		return nil
	}
}

func redditAuth() func(context.Context, *Client, *http.Request) error {
	return func(ctx context.Context, c *Client, req *http.Request) error {
		userAgent := strings.TrimSpace(c.Env("REDDIT_USER_AGENT"))
		if userAgent == "" {
			return skillerr.Auth(
				"missing Reddit User-Agent",
				skillerr.WithHint("Set REDDIT_USER_AGENT to a unique app/version/contact string."),
			)
		}
		token := strings.TrimSpace(c.Env("REDDIT_ACCESS_TOKEN"))
		if token == "" {
			var err error
			token, err = c.redditClientCredentialsToken(ctx, userAgent)
			if err != nil {
				return err
			}
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", userAgent)
		return nil
	}
}

func (c *Client) redditClientCredentialsToken(ctx context.Context, userAgent string) (string, error) {
	clientID := strings.TrimSpace(c.Env("REDDIT_CLIENT_ID"))
	clientSecret := strings.TrimSpace(c.Env("REDDIT_CLIENT_SECRET"))
	if clientID == "" || clientSecret == "" {
		return "", skillerr.Auth(
			"missing Reddit OAuth credentials",
			skillerr.WithHint("Set REDDIT_ACCESS_TOKEN or REDDIT_CLIENT_ID and REDDIT_CLIENT_SECRET."),
		)
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.RedditTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", skillerr.WrapRuntime("build Reddit token request", err)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", skillerr.WrapIO("request Reddit access token", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", skillerr.Integration(
			fmt.Sprintf("Reddit token endpoint returned HTTP %d", resp.StatusCode),
			skillerr.WithData("body", strings.TrimSpace(string(body))),
		)
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", skillerr.WrapParse("decode Reddit token response", err)
	}
	if tokenResp.AccessToken == "" {
		return "", skillerr.Integration("Reddit token response did not include access_token")
	}
	return tokenResp.AccessToken, nil
}

func firstEnv(c *Client, names ...string) (string, string) {
	for _, name := range names {
		if value := strings.TrimSpace(c.Env(name)); value != "" {
			return value, name
		}
	}
	return "", ""
}

func requestPlan(baseURL, route string, q url.Values, auth AuthSpec) (RequestPlan, error) {
	u, err := joinEndpoint(baseURL, route)
	if err != nil {
		return RequestPlan{}, err
	}
	u.RawQuery = q.Encode()
	return RequestPlan{
		Method: http.MethodGet,
		URL:    u.String(),
		Route:  route,
		Query:  valuesMap(q),
		Auth:   auth,
	}, nil
}

func joinEndpoint(baseURL, route string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, skillerr.WrapArg("parse provider base URL", err)
	}
	trimmed := strings.TrimLeft(route, "/")
	parts := strings.Split(trimmed, "/")
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(part))
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.Join(escaped, "/")
	return base, nil
}

func valuesMap(values url.Values) map[string]string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(values))
	for _, key := range keys {
		out[key] = values.Get(key)
	}
	return out
}

func readRateLimit(header http.Header, now time.Time) *RateLimit {
	rate := &RateLimit{
		ObservedAt: now.UTC().Format(time.RFC3339),
	}
	rate.Limit = firstHeaderInt(header, "x-rate-limit-limit", "x-ratelimit-limit")
	rate.Remaining = firstHeaderInt(header, "x-rate-limit-remaining", "x-ratelimit-remaining")
	rate.ResetUnix = firstHeaderInt64(header, "x-rate-limit-reset", "x-ratelimit-reset")
	rate.Used = firstHeaderFloat(header, "x-ratelimit-used")
	rate.RetryAfter = header.Get("Retry-After")

	hints := map[string]string{}
	for _, key := range []string{"x-app-usage", "x-page-usage", "x-business-use-case-usage"} {
		if value := header.Get(key); value != "" {
			hints[key] = value
		}
	}
	if len(hints) > 0 {
		rate.ProviderHint = hints
	}
	if rate.Limit == 0 && rate.Remaining == 0 && rate.ResetUnix == 0 && rate.Used == 0 && rate.RetryAfter == "" && len(rate.ProviderHint) == 0 {
		return nil
	}
	return rate
}

func firstHeaderInt(header http.Header, names ...string) int {
	for _, name := range names {
		if value := header.Get(name); value != "" {
			if parsed, err := strconv.Atoi(value); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func firstHeaderInt64(header http.Header, names ...string) int64 {
	for _, name := range names {
		if value := header.Get(name); value != "" {
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func firstHeaderFloat(header http.Header, names ...string) float64 {
	for _, name := range names {
		if value := header.Get(name); value != "" {
			if parsed, err := strconv.ParseFloat(value, 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func providerID(platform Platform) string {
	switch platform {
	case PlatformX:
		return "x_api_v2"
	case PlatformReddit:
		return "reddit_data_api"
	case PlatformYouTube:
		return "youtube_data_api"
	case PlatformFacebook:
		return "meta_facebook_pages"
	case PlatformInstagram:
		return "meta_instagram_graph"
	default:
		return string(platform)
	}
}

func boundedLimit(value, def, max int) int {
	if value <= 0 {
		return def
	}
	if value > max {
		return max
	}
	return value
}

func ids(in Input, fallback string) ([]string, error) {
	out := make([]string, 0, len(in.IDs)+1)
	for _, id := range in.IDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			token, err := idListToken(trimmed)
			if err != nil {
				return nil, err
			}
			out = append(out, token)
		}
	}
	if len(out) == 0 && strings.TrimSpace(fallback) != "" {
		token, err := idListToken(fallback)
		if err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, nil
}

func idListToken(value string) (string, error) {
	token := strings.TrimSpace(value)
	if token == "" {
		return "", skillerr.Arg("id must be non-empty")
	}
	for _, r := range token {
		if r == ',' || unicode.IsControl(r) {
			return "", skillerr.Arg(
				"id must be a single list token",
				skillerr.WithHint("Pass multiple IDs as separate ids array elements, not as one comma-delimited string."),
			)
		}
	}
	return token, nil
}

func require(value, message, hint string) error {
	if strings.TrimSpace(value) == "" {
		return skillerr.Arg(message, skillerr.WithHint(hint))
	}
	return nil
}

func requirePathToken(value, field, message, hint string) (string, error) {
	if err := require(value, message, hint); err != nil {
		return "", err
	}
	return pathToken(value, field, hint)
}

func pathToken(value, field, hint string) (string, error) {
	token := strings.TrimSpace(value)
	if token == "" {
		return "", skillerr.Arg(
			field+" must be a non-empty path segment",
			skillerr.WithHint(hint),
		)
	}
	for _, r := range token {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return "", skillerr.Arg(
				field+" must be a single path segment",
				skillerr.WithHint(hint),
			)
		}
	}
	return token, nil
}

func requireFieldArgumentToken(value, field, message, hint string) (string, error) {
	if err := require(value, message, hint); err != nil {
		return "", err
	}
	token := strings.TrimSpace(value)
	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' {
			continue
		}
		return "", skillerr.Arg(
			field+" must be a single field argument token",
			skillerr.WithHint(hint),
		)
	}
	return token, nil
}

func asMap(raw any) map[string]any {
	m, _ := raw.(map[string]any)
	return m
}

func asSlice(raw any) []any {
	switch v := raw.(type) {
	case []any:
		return v
	default:
		return nil
	}
}

func stringify(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func numberMap(raw any) map[string]float64 {
	source, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]float64, len(source))
	for key, value := range source {
		switch v := value.(type) {
		case json.Number:
			if parsed, err := v.Float64(); err == nil {
				out[key] = parsed
			}
		case float64:
			out[key] = v
		case int:
			out[key] = float64(v)
		case string:
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				out[key] = parsed
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	default:
		return 0
	}
}
