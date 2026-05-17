package social

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
)

const xFieldSet = "created_at,public_metrics,conversation_id,author_id,lang,entities,referenced_tweets"

func planX(baseURL string, in Input) (callPlan, error) {
	q := url.Values{}
	route := ""
	limit := boundedLimit(in.Limit, 10, 100)

	switch in.Operation {
	case "recent_search":
		if err := require(in.Query, "query is required for recent_search", "Pass query using X API v2 search syntax."); err != nil {
			return callPlan{}, err
		}
		route = "/tweets/search/recent"
		q.Set("query", in.Query)
		q.Set("max_results", intString(limit))
		addIf(q, "next_token", in.PageToken)
		addIf(q, "start_time", in.Since)
		addIf(q, "end_time", in.Until)
		q.Set("tweet.fields", xFieldSet)
		q.Set("expansions", "author_id")
		q.Set("user.fields", "created_at,description,location,public_metrics,verified,url,username,name")
	case "posts_lookup":
		postIDs := ids(in, in.ID)
		if len(postIDs) == 0 {
			return callPlan{}, require("", "ids are required for posts_lookup", "Pass ids:[\"...\"] or id:\"...\".")
		}
		route = "/tweets"
		q.Set("ids", strings.Join(postIDs, ","))
		q.Set("tweet.fields", xFieldSet)
		q.Set("expansions", "author_id")
		q.Set("user.fields", "created_at,description,location,public_metrics,verified,url,username,name")
	case "user_lookup":
		if strings.TrimSpace(in.Username) != "" {
			route = "/users/by/username/" + in.Username
		} else {
			if err := require(in.UserID, "username or user_id is required for user_lookup", "Pass username for /users/by/username or user_id for /users/:id."); err != nil {
				return callPlan{}, err
			}
			route = "/users/" + in.UserID
		}
		q.Set("user.fields", "created_at,description,location,public_metrics,verified,url,username,name,protected")
	case "user_posts":
		if err := require(in.UserID, "user_id is required for user_posts", "Resolve a username first with operation:user_lookup."); err != nil {
			return callPlan{}, err
		}
		route = "/users/" + in.UserID + "/tweets"
		q.Set("max_results", intString(limit))
		addIf(q, "pagination_token", in.PageToken)
		addIf(q, "start_time", in.Since)
		addIf(q, "end_time", in.Until)
		q.Set("tweet.fields", xFieldSet)
	case "post_counts":
		if err := require(in.Query, "query is required for post_counts", "Pass query using X API v2 search syntax."); err != nil {
			return callPlan{}, err
		}
		route = "/tweets/counts/recent"
		q.Set("query", in.Query)
		addIf(q, "start_time", in.Since)
		addIf(q, "end_time", in.Until)
	default:
		return callPlan{}, unsupportedOperation("x", in.Operation, "recent_search, posts_lookup, user_lookup, user_posts, post_counts")
	}

	request, err := requestPlan(baseURL, route, q, AuthSpec{
		Type:     "oauth2_app_bearer",
		Env:      []string{"X_BEARER_TOKEN", "TWITTER_BEARER_TOKEN"},
		Header:   "Authorization",
		Required: true,
	})
	if err != nil {
		return callPlan{}, err
	}
	return callPlan{
		request: request,
		cost: &Cost{
			BillingUnit: "post_read_or_request",
			Estimated:   false,
			PriceSource: "X Developer Console",
			Notes:       "X pricing is pay-per-use and endpoint-specific; record observed usage and configured prices at runtime.",
		},
		warnings: []string{"X endpoint access, full archive search, and prices vary by developer tier."},
		prepare:  bearerAuth("X_BEARER_TOKEN", "TWITTER_BEARER_TOKEN"),
		parse:    parseX(in.Operation),
	}, nil
}

func parseX(operation string) func(any) ([]Item, *Pagination, error) {
	return func(raw any) ([]Item, *Pagination, error) {
		root := asMap(raw)
		data := asSlice(root["data"])
		if single, ok := root["data"].(map[string]any); ok {
			data = []any{single}
		}

		usersByID := map[string]map[string]any{}
		if includes := asMap(root["includes"]); includes != nil {
			for _, rawUser := range asSlice(includes["users"]) {
				user := asMap(rawUser)
				if id := stringify(user["id"]); id != "" {
					usersByID[id] = user
				}
			}
		}

		items := make([]Item, 0, len(data))
		itemType := "post"
		if operation == "user_lookup" {
			itemType = "user"
		}
		if operation == "post_counts" {
			for _, bucket := range data {
				m := asMap(bucket)
				items = append(items, Item{
					Platform: "x",
					Provider: providerID(PlatformX),
					Type:     "count_bucket",
					ID:       stringify(m["start"]),
					Title:    stringify(m["end"]),
					Metrics:  numberMap(map[string]any{"tweet_count": m["tweet_count"]}),
					Raw:      m,
				})
			}
		} else {
			for _, rawItem := range data {
				m := asMap(rawItem)
				item := Item{
					Platform:  "x",
					Provider:  providerID(PlatformX),
					Type:      itemType,
					ID:        stringify(m["id"]),
					Text:      stringify(m["text"]),
					CreatedAt: stringify(m["created_at"]),
					Metrics:   numberMap(m["public_metrics"]),
					Raw:       m,
				}
				if itemType == "user" {
					item.AuthorUsername = stringify(m["username"])
					item.Title = stringify(m["name"])
					item.Text = stringify(m["description"])
					item.URL = stringify(m["url"])
				} else {
					item.AuthorID = stringify(m["author_id"])
					item.ThreadID = stringify(m["conversation_id"])
					item.URL = "https://x.com/i/web/status/" + item.ID
					if user := usersByID[item.AuthorID]; user != nil {
						item.AuthorUsername = stringify(user["username"])
					}
				}
				items = append(items, item)
			}
		}

		meta := asMap(root["meta"])
		pagination := &Pagination{
			NextToken:   stringify(meta["next_token"]),
			PrevToken:   stringify(meta["previous_token"]),
			ResultCount: intFromAny(meta["result_count"]),
		}
		if pagination.NextToken == "" && pagination.PrevToken == "" && pagination.ResultCount == 0 {
			pagination = nil
		}
		return items, pagination, nil
	}
}

func addIf(q url.Values, key, value string) {
	if strings.TrimSpace(value) != "" {
		q.Set(key, value)
	}
}

func intString(v int) string {
	return strconv.Itoa(v)
}

func unsupportedOperation(platform, operation, supported string) error {
	return skillerr.Arg(
		"unsupported "+platform+" operation: "+operation,
		skillerr.WithHint("Supported operations: "+supported+"."),
	)
}
