package social

import (
	"context"
	"net/http"
	"time"
)

// Platform identifies the first-party social provider API used by a skill.
type Platform string

const (
	PlatformX         Platform = "x"
	PlatformReddit    Platform = "reddit"
	PlatformFacebook  Platform = "facebook"
	PlatformInstagram Platform = "instagram"
	PlatformYouTube   Platform = "youtube"
)

// Input is shared by the social research skills. Platform-specific fields are
// optional so each skill can expose one stable, typed contract while rejecting
// unknown JSON keys through skillmain.
type Input struct {
	Operation  string   `json:"operation" validate:"required"`
	Query      string   `json:"query,omitempty"`
	IDs        []string `json:"ids,omitempty"`
	ID         string   `json:"id,omitempty"`
	Username   string   `json:"username,omitempty"`
	UserID     string   `json:"user_id,omitempty"`
	Handle     string   `json:"handle,omitempty"`
	Subreddit  string   `json:"subreddit,omitempty"`
	PostID     string   `json:"post_id,omitempty"`
	PageID     string   `json:"page_id,omitempty"`
	IGUserID   string   `json:"ig_user_id,omitempty"`
	MediaID    string   `json:"media_id,omitempty"`
	VideoID    string   `json:"video_id,omitempty"`
	ChannelID  string   `json:"channel_id,omitempty"`
	PlaylistID string   `json:"playlist_id,omitempty"`
	Type       string   `json:"type,omitempty"`
	Sort       string   `json:"sort,omitempty"`
	TimeFilter string   `json:"time_filter,omitempty"`
	Since      string   `json:"since,omitempty"`
	Until      string   `json:"until,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	PageToken  string   `json:"page_token,omitempty"`
	After      string   `json:"after,omitempty"`
	Before     string   `json:"before,omitempty"`
	APIVersion string   `json:"api_version,omitempty"`
	DryRun     bool     `json:"dry_run,omitempty"`
	IncludeRaw bool     `json:"include_raw,omitempty"`
}

// Output is the provider-neutral response shape emitted by every social skill.
type Output struct {
	Platform   string      `json:"platform"`
	Provider   string      `json:"provider"`
	Operation  string      `json:"operation"`
	DryRun     bool        `json:"dry_run,omitempty"`
	Request    RequestPlan `json:"request"`
	Items      []Item      `json:"items,omitempty"`
	Pagination *Pagination `json:"pagination,omitempty"`
	RateLimit  *RateLimit  `json:"rate_limit,omitempty"`
	Cost       *Cost       `json:"cost,omitempty"`
	Warnings   []string    `json:"warnings,omitempty"`
	CapturedAt string      `json:"captured_at"`
	Raw        any         `json:"raw,omitempty"`
}

// RequestPlan describes the direct API call without exposing secrets.
type RequestPlan struct {
	Method string            `json:"method"`
	URL    string            `json:"url"`
	Route  string            `json:"route"`
	Query  map[string]string `json:"query,omitempty"`
	Auth   AuthSpec          `json:"auth"`
}

// AuthSpec describes how the API call authenticates.
type AuthSpec struct {
	Type       string   `json:"type"`
	Env        []string `json:"env,omitempty"`
	Header     string   `json:"header,omitempty"`
	QueryParam string   `json:"query_param,omitempty"`
	Required   bool     `json:"required"`
}

// Item is the common record shape for posts, users, channels, comments, media,
// playlists, and other social objects.
type Item struct {
	Platform       string             `json:"platform"`
	Provider       string             `json:"provider"`
	Type           string             `json:"type"`
	ID             string             `json:"id"`
	ParentID       string             `json:"parent_id,omitempty"`
	ThreadID       string             `json:"thread_id,omitempty"`
	AuthorID       string             `json:"author_id,omitempty"`
	AuthorUsername string             `json:"author_username,omitempty"`
	Title          string             `json:"title,omitempty"`
	Text           string             `json:"text,omitempty"`
	URL            string             `json:"url,omitempty"`
	CreatedAt      string             `json:"created_at,omitempty"`
	UpdatedAt      string             `json:"updated_at,omitempty"`
	Subreddit      string             `json:"subreddit,omitempty"`
	ChannelID      string             `json:"channel_id,omitempty"`
	Metrics        map[string]float64 `json:"metrics,omitempty"`
	Raw            map[string]any     `json:"raw,omitempty"`
}

// Pagination captures the next provider cursor and the original cursor.
type Pagination struct {
	NextToken   string `json:"next_token,omitempty"`
	PrevToken   string `json:"prev_token,omitempty"`
	After       string `json:"after,omitempty"`
	Before      string `json:"before,omitempty"`
	ResultCount int    `json:"result_count,omitempty"`
}

// RateLimit captures runtime rate signals returned by provider headers.
type RateLimit struct {
	Limit        int               `json:"limit,omitempty"`
	Remaining    int               `json:"remaining,omitempty"`
	Used         float64           `json:"used,omitempty"`
	ResetUnix    int64             `json:"reset_unix,omitempty"`
	RetryAfter   string            `json:"retry_after,omitempty"`
	ObservedAt   string            `json:"observed_at"`
	ProviderHint map[string]string `json:"provider_hint,omitempty"`
}

// Cost captures quota/billing metadata without pretending volatile prices are
// static constants.
type Cost struct {
	BillingUnit   string `json:"billing_unit,omitempty"`
	QuotaUnits    int    `json:"quota_units,omitempty"`
	Estimated     bool   `json:"estimated"`
	PriceSource   string `json:"price_source,omitempty"`
	Notes         string `json:"notes,omitempty"`
	BillableCount int    `json:"billable_count,omitempty"`
}

// Client owns provider endpoints and test seams for direct HTTP API calls.
type Client struct {
	HTTP           *http.Client
	Now            func() time.Time
	Env            func(string) string
	XBaseURL       string
	RedditBaseURL  string
	RedditTokenURL string
	YouTubeBaseURL string
	MetaBaseURL    string
}

type callPlan struct {
	request  RequestPlan
	cost     *Cost
	warnings []string
	prepare  func(ctx context.Context, c *Client, req *http.Request) error
	parse    func(raw any) ([]Item, *Pagination, error)
}
