package social

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
