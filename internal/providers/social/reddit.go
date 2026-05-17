package social

import (
	"net/url"
	"strings"
)

func planReddit(baseURL string, in Input) (callPlan, error) {
	q := url.Values{}
	route := ""
	limit := boundedLimit(in.Limit, 25, 100)
	q.Set("limit", intString(limit))
	addIf(q, "after", in.After)
	addIf(q, "before", in.Before)

	switch in.Operation {
	case "subreddit_listing":
		if err := require(in.Subreddit, "subreddit is required for subreddit_listing", "Pass subreddit without the r/ prefix."); err != nil {
			return callPlan{}, err
		}
		sort := redditSort(in.Sort, "hot")
		route = "/r/" + in.Subreddit + "/" + sort
		addIf(q, "t", in.TimeFilter)
	case "subreddit_search":
		if err := require(in.Subreddit, "subreddit is required for subreddit_search", "Pass subreddit without the r/ prefix."); err != nil {
			return callPlan{}, err
		}
		if err := require(in.Query, "query is required for subreddit_search", "Pass query for Reddit search."); err != nil {
			return callPlan{}, err
		}
		route = "/r/" + in.Subreddit + "/search"
		q.Set("q", in.Query)
		q.Set("restrict_sr", "1")
		addIf(q, "sort", redditSort(in.Sort, "relevance"))
		addIf(q, "t", in.TimeFilter)
	case "post_comments":
		if err := require(in.PostID, "post_id is required for post_comments", "Pass the Reddit article ID without the t3_ prefix."); err != nil {
			return callPlan{}, err
		}
		route = "/comments/" + strings.TrimPrefix(in.PostID, "t3_")
		addIf(q, "sort", redditSort(in.Sort, "confidence"))
	case "user_submitted":
		if err := require(in.Username, "username is required for user_submitted", "Pass username without u/ prefix."); err != nil {
			return callPlan{}, err
		}
		route = "/user/" + in.Username + "/submitted"
	case "user_comments":
		if err := require(in.Username, "username is required for user_comments", "Pass username without u/ prefix."); err != nil {
			return callPlan{}, err
		}
		route = "/user/" + in.Username + "/comments"
	case "subreddit_about":
		if err := require(in.Subreddit, "subreddit is required for subreddit_about", "Pass subreddit without the r/ prefix."); err != nil {
			return callPlan{}, err
		}
		route = "/r/" + in.Subreddit + "/about"
		q = url.Values{}
	default:
		return callPlan{}, unsupportedOperation("reddit", in.Operation, "subreddit_listing, subreddit_search, post_comments, user_submitted, user_comments, subreddit_about")
	}

	request, err := requestPlan(baseURL, route, q, AuthSpec{
		Type:     "oauth2",
		Env:      []string{"REDDIT_ACCESS_TOKEN", "REDDIT_CLIENT_ID", "REDDIT_CLIENT_SECRET", "REDDIT_USER_AGENT"},
		Header:   "Authorization",
		Required: true,
	})
	if err != nil {
		return callPlan{}, err
	}
	return callPlan{
		request: request,
		cost: &Cost{
			BillingUnit: "api_call",
			Estimated:   false,
			PriceSource: "Reddit Data API Terms / contract",
			Notes:       "Eligible non-commercial usage is rate-limited; commercial or high-volume usage needs Reddit approval or contract.",
		},
		warnings: []string{"Reddit search is not exhaustive historical research; use official live API data for current availability and compliance."},
		prepare:  redditAuth(),
		parse:    parseReddit,
	}, nil
}

func redditSort(value, def string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return def
	}
	return value
}

func parseReddit(raw any) ([]Item, *Pagination, error) {
	root := asMap(raw)
	if children := listingChildren(root); children != nil {
		return parseRedditChildren(children, root)
	}

	var items []Item
	var pagination *Pagination
	for _, listing := range asSlice(raw) {
		m := asMap(listing)
		children := listingChildren(m)
		parsed, page, err := parseRedditChildren(children, m)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, parsed...)
		if pagination == nil && page != nil {
			pagination = page
		}
	}
	if len(items) == 0 && root != nil {
		data := asMap(root["data"])
		if data == nil {
			data = root
		}
		items = append(items, redditItem("subreddit", data))
	}
	return items, pagination, nil
}

func listingChildren(root map[string]any) []any {
	if root == nil {
		return nil
	}
	data := asMap(root["data"])
	if data == nil {
		return nil
	}
	return asSlice(data["children"])
}

func parseRedditChildren(children []any, listing map[string]any) ([]Item, *Pagination, error) {
	items := make([]Item, 0, len(children))
	for _, rawChild := range children {
		child := asMap(rawChild)
		kind := stringify(child["kind"])
		data := asMap(child["data"])
		if data == nil {
			continue
		}
		itemType := "post"
		switch kind {
		case "t1":
			itemType = "comment"
		case "t5":
			itemType = "subreddit"
		}
		items = append(items, redditItem(itemType, data))
	}
	data := asMap(listing["data"])
	pagination := &Pagination{
		After:       stringify(data["after"]),
		Before:      stringify(data["before"]),
		ResultCount: len(items),
	}
	if pagination.After == "" && pagination.Before == "" && pagination.ResultCount == 0 {
		pagination = nil
	}
	return items, pagination, nil
}

func redditItem(itemType string, data map[string]any) Item {
	id := stringify(data["id"])
	fullname := stringify(data["name"])
	if fullname == "" {
		switch itemType {
		case "comment":
			fullname = "t1_" + id
		case "subreddit":
			fullname = "t5_" + id
		default:
			fullname = "t3_" + id
		}
	}
	text := stringify(data["selftext"])
	if text == "" {
		text = stringify(data["body"])
	}
	created := ""
	if createdUTC := data["created_utc"]; createdUTC != nil {
		created = stringify(createdUTC)
	}
	return Item{
		Platform:       "reddit",
		Provider:       providerID(PlatformReddit),
		Type:           itemType,
		ID:             fullname,
		ParentID:       stringify(data["parent_id"]),
		ThreadID:       stringify(data["link_id"]),
		AuthorID:       stringify(data["author_fullname"]),
		AuthorUsername: stringify(data["author"]),
		Title:          stringify(data["title"]),
		Text:           text,
		URL:            redditURL(data),
		CreatedAt:      created,
		Subreddit:      stringify(data["subreddit"]),
		Metrics: numberMap(map[string]any{
			"score":        data["score"],
			"upvote_ratio": data["upvote_ratio"],
			"num_comments": data["num_comments"],
			"subscribers":  data["subscribers"],
		}),
		Raw: data,
	}
}

func redditURL(data map[string]any) string {
	if permalink := stringify(data["permalink"]); permalink != "" {
		return "https://www.reddit.com" + permalink
	}
	if urlValue := stringify(data["url"]); urlValue != "" {
		return urlValue
	}
	return ""
}
