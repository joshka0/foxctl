package social

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/quick"
	"time"
)

func TestCollectDryRunPlansXRecentSearchWithoutSecrets(t *testing.T) {
	client := testSocialClient(nil)

	out, err := Collect(context.Background(), client, PlatformX, Input{
		Operation: "recent_search",
		Query:     "from:openai",
		Limit:     5,
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if !out.DryRun {
		t.Fatal("expected dry run output")
	}
	if out.Provider != "x_api_v2" {
		t.Fatalf("Provider = %q, want x_api_v2", out.Provider)
	}
	if out.Request.Route != "/tweets/search/recent" {
		t.Fatalf("route = %q", out.Request.Route)
	}
	if out.Request.Query["query"] != "from:openai" {
		t.Fatalf("query = %q", out.Request.Query["query"])
	}
	if out.Request.Query["max_results"] != "5" {
		t.Fatalf("max_results = %q", out.Request.Query["max_results"])
	}
	if out.Cost == nil || out.Cost.PriceSource != "X Developer Console" {
		t.Fatalf("cost metadata = %#v", out.Cost)
	}
}

func TestCollectYouTubeSearchUsesAPIKeyAndNormalizesItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("path = %q, want /search", r.URL.Path)
		}
		if got := r.URL.Query().Get("key"); got != "yt-key" {
			t.Fatalf("key = %q, want yt-key", got)
		}
		if got := r.URL.Query().Get("q"); got != "foxctl" {
			t.Fatalf("q = %q, want foxctl", got)
		}
		w.Header().Set("X-Rate-Limit-Limit", "100")
		writeJSON(t, w, map[string]any{
			"nextPageToken": "next",
			"pageInfo":      map[string]any{"resultsPerPage": 1},
			"items": []any{
				map[string]any{
					"id": map[string]any{"kind": "youtube#video", "videoId": "vid123"},
					"snippet": map[string]any{
						"title":        "Foxctl demo",
						"description":  "Demo video",
						"publishedAt":  "2026-05-17T00:00:00Z",
						"channelId":    "chan123",
						"channelTitle": "Foxctl",
					},
					"statistics": map[string]any{"viewCount": "1234"},
				},
			},
		})
	}))
	defer server.Close()

	client := testSocialClient(map[string]string{"YOUTUBE_API_KEY": "yt-key"})
	client.HTTP = server.Client()
	client.YouTubeBaseURL = server.URL

	out, err := Collect(context.Background(), client, PlatformYouTube, Input{
		Operation: "search",
		Query:     "foxctl",
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if out.Cost == nil || out.Cost.QuotaUnits != 100 {
		t.Fatalf("cost = %#v, want 100 quota units", out.Cost)
	}
	if out.RateLimit == nil || out.RateLimit.Limit != 100 {
		t.Fatalf("rate limit = %#v", out.RateLimit)
	}
	if out.Pagination == nil || out.Pagination.NextToken != "next" {
		t.Fatalf("pagination = %#v", out.Pagination)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(out.Items))
	}
	item := out.Items[0]
	if item.ID != "vid123" || item.Type != "video" || item.URL != "https://www.youtube.com/watch?v=vid123" {
		t.Fatalf("item = %#v", item)
	}
	if item.Metrics["view_count"] != 1234 {
		t.Fatalf("view_count = %v, want 1234", item.Metrics["view_count"])
	}
}

func TestCollectRedditUsesClientCredentialsAndParsesListing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/access_token":
			if r.Method != http.MethodPost {
				t.Fatalf("token method = %s", r.Method)
			}
			wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("client:secret"))
			if got := r.Header.Get("Authorization"); got != wantAuth {
				t.Fatalf("token auth = %q, want %q", got, wantAuth)
			}
			writeJSON(t, w, map[string]any{"access_token": "reddit-token"})
		case "/r/golang/new":
			if got := r.Header.Get("Authorization"); got != "Bearer reddit-token" {
				t.Fatalf("api auth = %q", got)
			}
			w.Header().Set("X-Ratelimit-Remaining", "99")
			writeJSON(t, w, map[string]any{
				"kind": "Listing",
				"data": map[string]any{
					"after": "t3_next",
					"children": []any{
						map[string]any{
							"kind": "t3",
							"data": map[string]any{
								"id":          "abc",
								"name":        "t3_abc",
								"title":       "Go news",
								"selftext":    "Body",
								"author":      "alice",
								"subreddit":   "golang",
								"created_utc": 1770000000,
								"permalink":   "/r/golang/comments/abc/go_news/",
								"score":       42,
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := testSocialClient(map[string]string{
		"REDDIT_CLIENT_ID":     "client",
		"REDDIT_CLIENT_SECRET": "secret",
		"REDDIT_USER_AGENT":    "foxctl-test/0.1 contact",
	})
	client.HTTP = server.Client()
	client.RedditBaseURL = server.URL
	client.RedditTokenURL = server.URL + "/api/v1/access_token"

	out, err := Collect(context.Background(), client, PlatformReddit, Input{
		Operation: "subreddit_listing",
		Subreddit: "golang",
		Sort:      "new",
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if out.Provider != "reddit_data_api" {
		t.Fatalf("provider = %q", out.Provider)
	}
	if out.Pagination == nil || out.Pagination.After != "t3_next" {
		t.Fatalf("pagination = %#v", out.Pagination)
	}
	if len(out.Items) != 1 || out.Items[0].ID != "t3_abc" || out.Items[0].Subreddit != "golang" {
		t.Fatalf("items = %#v", out.Items)
	}
}

func TestCollectInstagramMediaParsesGraphUsageHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v25.0/ig123/media" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer meta-token" {
			t.Fatalf("auth = %q", got)
		}
		w.Header().Set("X-App-Usage", `{"call_count":1}`)
		writeJSON(t, w, map[string]any{
			"data": []any{
				map[string]any{
					"id":             "media123",
					"caption":        "Launch notes",
					"media_type":     "IMAGE",
					"permalink":      "https://www.instagram.com/p/media123/",
					"timestamp":      "2026-05-17T00:00:00+0000",
					"username":       "foxctl",
					"like_count":     9,
					"comments_count": 2,
				},
			},
			"paging": map[string]any{"cursors": map[string]any{"after": "after-cursor"}},
		})
	}))
	defer server.Close()

	client := testSocialClient(map[string]string{"META_ACCESS_TOKEN": "meta-token"})
	client.HTTP = server.Client()
	client.MetaBaseURL = server.URL

	out, err := Collect(context.Background(), client, PlatformInstagram, Input{
		Operation: "user_media",
		IGUserID:  "ig123",
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if out.Provider != "meta_instagram_graph" {
		t.Fatalf("provider = %q", out.Provider)
	}
	if out.RateLimit == nil || out.RateLimit.ProviderHint["x-app-usage"] == "" {
		t.Fatalf("rate limit = %#v", out.RateLimit)
	}
	if out.Pagination == nil || out.Pagination.After != "after-cursor" {
		t.Fatalf("pagination = %#v", out.Pagination)
	}
	if len(out.Items) != 1 || out.Items[0].ID != "media123" || out.Items[0].Text != "Launch notes" {
		t.Fatalf("items = %#v", out.Items)
	}
}

func TestCollectFacebookDryRunKeepsPagesProviderSeparate(t *testing.T) {
	out, err := Collect(context.Background(), testSocialClient(nil), PlatformFacebook, Input{
		Operation: "page_posts",
		PageID:    "page123",
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if out.Provider != "meta_facebook_pages" {
		t.Fatalf("provider = %q", out.Provider)
	}
	if out.Request.Route != "/v25.0/page123/posts" {
		t.Fatalf("route = %q", out.Request.Route)
	}
	if len(out.Warnings) == 0 {
		t.Fatal("expected permission warning")
	}
}

func TestCollectDryRunRejectsRouteShapingPathInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		platform Platform
		input    Input
	}{
		{
			name:     "reddit subreddit",
			platform: PlatformReddit,
			input: Input{
				Operation: "subreddit_listing",
				Subreddit: "golang/about",
				DryRun:    true,
			},
		},
		{
			name:     "reddit username",
			platform: PlatformReddit,
			input: Input{
				Operation: "user_submitted",
				Username:  "alice/comments",
				DryRun:    true,
			},
		},
		{
			name:     "x username",
			platform: PlatformX,
			input: Input{
				Operation: "user_lookup",
				Username:  "alice/followers",
				DryRun:    true,
			},
		},
		{
			name:     "facebook page id",
			platform: PlatformFacebook,
			input: Input{
				Operation: "page_posts",
				PageID:    "page-id/feed",
				DryRun:    true,
			},
		},
		{
			name:     "instagram media id",
			platform: PlatformInstagram,
			input: Input{
				Operation: "media_details",
				MediaID:   "media-id/comments",
				DryRun:    true,
			},
		},
		{
			name:     "meta api version",
			platform: PlatformFacebook,
			input: Input{
				Operation:  "page_info",
				PageID:     "page-id",
				APIVersion: "v25.0/debug",
				DryRun:     true,
			},
		},
		{
			name:     "empty meta api version segment",
			platform: PlatformFacebook,
			input: Input{
				Operation:  "page_info",
				PageID:     "page-id",
				APIVersion: "/",
				DryRun:     true,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Collect(context.Background(), testSocialClient(nil), tt.platform, tt.input); err == nil {
				t.Fatal("expected route-shaping path input to be rejected")
			}
		})
	}
}

func TestCollectDryRunAcceptsLeadingSlashMetaAPIVersion(t *testing.T) {
	t.Parallel()

	out, err := Collect(context.Background(), testSocialClient(nil), PlatformFacebook, Input{
		Operation:  "page_info",
		PageID:     "page-id",
		APIVersion: "/v25.0",
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if out.Request.Route != "/v25.0/page-id" {
		t.Fatalf("route = %q, want /v25.0/page-id", out.Request.Route)
	}
}

func TestCollectDryRunRejectsCommaDelimitedIDTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		platform Platform
		input    Input
	}{
		{
			name:     "x fallback id",
			platform: PlatformX,
			input: Input{
				Operation: "posts_lookup",
				ID:        "111,222",
				DryRun:    true,
			},
		},
		{
			name:     "x ids element",
			platform: PlatformX,
			input: Input{
				Operation: "posts_lookup",
				IDs:       []string{"111,222"},
				DryRun:    true,
			},
		},
		{
			name:     "youtube video id",
			platform: PlatformYouTube,
			input: Input{
				Operation: "videos",
				VideoID:   "vid1,vid2",
				DryRun:    true,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Collect(context.Background(), testSocialClient(nil), tt.platform, tt.input); err == nil {
				t.Fatal("expected comma-delimited ID token to be rejected")
			}
		})
	}
}

func TestCollectDryRunKeepsExplicitIDListElements(t *testing.T) {
	t.Parallel()

	out, err := Collect(context.Background(), testSocialClient(nil), PlatformX, Input{
		Operation: "posts_lookup",
		IDs:       []string{"111", "222"},
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := out.Request.Query["ids"]; got != "111,222" {
		t.Fatalf("ids query = %q, want 111,222", got)
	}
}

func TestCollectDryRunPropertyRejectsGeneratedRedditSubredditPathSegments(t *testing.T) {
	t.Parallel()

	prop := func(prefix, suffix string) bool {
		in := Input{
			Operation: "subreddit_listing",
			Subreddit: prefix + "/" + suffix,
			DryRun:    true,
		}
		_, err := Collect(context.Background(), testSocialClient(nil), PlatformReddit, in)
		return err != nil
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatalf("generated subreddit path segment was accepted: %v", err)
	}
}

func TestCollectDryRunRejectsBusinessDiscoveryFieldExpressionUsername(t *testing.T) {
	t.Parallel()

	for _, username := range []string{
		"target)",
		"target){id,media{comments}}",
		"target,media",
		"target{media}",
		"target media",
		"target\nmedia",
	} {
		username := username
		t.Run(username, func(t *testing.T) {
			t.Parallel()

			_, err := Collect(context.Background(), testSocialClient(nil), PlatformInstagram, Input{
				Operation:  "business_discovery",
				IGUserID:   "ig123",
				Username:   username,
				APIVersion: "v25.0",
				DryRun:     true,
			})
			if err == nil {
				t.Fatal("expected field-expression username to be rejected")
			}
		})
	}
}

func TestCollectDryRunBusinessDiscoveryKeepsUsernameAsSingleFieldArgument(t *testing.T) {
	t.Parallel()

	out, err := Collect(context.Background(), testSocialClient(nil), PlatformInstagram, Input{
		Operation:  "business_discovery",
		IGUserID:   "ig123",
		Username:   "target.account_1",
		APIVersion: "v25.0",
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := out.Request.Query["fields"]; got != "business_discovery.username(target.account_1){id,username,name,biography,website,followers_count,follows_count,media_count,profile_picture_url,media.limit(25){id,caption,media_type,permalink,timestamp,like_count,comments_count}}" {
		t.Fatalf("fields = %q", got)
	}
}

func testSocialClient(env map[string]string) *Client {
	if env == nil {
		env = map[string]string{}
	}
	return &Client{
		Now: func() time.Time {
			return time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
		},
		Env: func(key string) string {
			return env[key]
		},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
