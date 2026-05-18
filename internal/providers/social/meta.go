package social

import (
	"net/url"
	"strings"
)

func planFacebook(baseURL string, in Input) (callPlan, error) {
	q := url.Values{}
	version := graphVersion(in.APIVersion)
	route := ""

	switch in.Operation {
	case "page_info":
		if err := require(in.PageID, "page_id is required for page_info", "Pass a Facebook Page ID."); err != nil {
			return callPlan{}, err
		}
		route = "/" + version + "/" + in.PageID
		q.Set("fields", "id,name,username,category,link,fan_count,followers_count,verification_status,picture")
	case "page_posts":
		if err := require(in.PageID, "page_id is required for page_posts", "Pass a Facebook Page ID."); err != nil {
			return callPlan{}, err
		}
		route = "/" + version + "/" + in.PageID + "/posts"
		q.Set("fields", "id,message,story,created_time,permalink_url,from,shares,comments.summary(true),reactions.summary(true)")
		q.Set("limit", intString(boundedLimit(in.Limit, 25, 100)))
		addIf(q, "after", in.After)
		addIf(q, "since", in.Since)
		addIf(q, "until", in.Until)
	case "post_comments":
		if err := require(in.PostID, "post_id is required for post_comments", "Pass a Facebook Graph post ID."); err != nil {
			return callPlan{}, err
		}
		route = "/" + version + "/" + in.PostID + "/comments"
		q.Set("fields", "id,message,created_time,from,like_count,comment_count,permalink_url")
		q.Set("limit", intString(boundedLimit(in.Limit, 25, 100)))
		addIf(q, "after", in.After)
	default:
		return callPlan{}, unsupportedOperation("facebook", in.Operation, "page_info, page_posts, post_comments")
	}

	request, err := requestPlan(baseURL, route, q, AuthSpec{
		Type:     "oauth2_bearer",
		Env:      []string{"META_ACCESS_TOKEN", "META_PAGE_ACCESS_TOKEN", "META_USER_ACCESS_TOKEN"},
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
			PriceSource: "Meta Graph API rate-limit headers and app review",
			Notes:       "Facebook Page access is permission and review gated; no simple public per-request price is modeled.",
		},
		warnings: []string{"Facebook Graph is not broad public search; Page posts/comments require Page permissions or approved public-content access."},
		prepare:  bearerAuth("META_ACCESS_TOKEN", "META_PAGE_ACCESS_TOKEN", "META_USER_ACCESS_TOKEN"),
		parse:    parseMeta(PlatformFacebook, in.Operation),
	}, nil
}

func planInstagram(baseURL string, in Input) (callPlan, error) {
	q := url.Values{}
	version := graphVersion(in.APIVersion)
	route := ""

	switch in.Operation {
	case "user_media":
		if err := require(in.IGUserID, "ig_user_id is required for user_media", "Pass an authorized Instagram Business/Creator user ID."); err != nil {
			return callPlan{}, err
		}
		route = "/" + version + "/" + in.IGUserID + "/media"
		q.Set("fields", "id,caption,media_type,media_product_type,media_url,thumbnail_url,permalink,timestamp,username,like_count,comments_count,children")
		q.Set("limit", intString(boundedLimit(in.Limit, 25, 100)))
		addIf(q, "after", in.After)
		addIf(q, "since", in.Since)
		addIf(q, "until", in.Until)
	case "media_details":
		if err := require(in.MediaID, "media_id is required for media_details", "Pass an Instagram media ID."); err != nil {
			return callPlan{}, err
		}
		route = "/" + version + "/" + in.MediaID
		q.Set("fields", "id,caption,media_type,media_product_type,media_url,thumbnail_url,permalink,timestamp,username,like_count,comments_count,children")
	case "media_comments":
		if err := require(in.MediaID, "media_id is required for media_comments", "Pass an Instagram media ID."); err != nil {
			return callPlan{}, err
		}
		route = "/" + version + "/" + in.MediaID + "/comments"
		q.Set("fields", "id,text,timestamp,username,like_count,replies")
		q.Set("limit", intString(boundedLimit(in.Limit, 25, 100)))
		addIf(q, "after", in.After)
	case "business_discovery":
		if err := require(in.IGUserID, "ig_user_id is required for business_discovery", "Pass your authorized IG user ID as ig_user_id."); err != nil {
			return callPlan{}, err
		}
		if err := require(in.Username, "username is required for business_discovery", "Pass the target Business/Creator username."); err != nil {
			return callPlan{}, err
		}
		route = "/" + version + "/" + in.IGUserID
		q.Set("fields", "business_discovery.username("+in.Username+"){id,username,name,biography,website,followers_count,follows_count,media_count,profile_picture_url,media.limit(25){id,caption,media_type,permalink,timestamp,like_count,comments_count}}")
	default:
		return callPlan{}, unsupportedOperation("instagram", in.Operation, "user_media, media_details, media_comments, business_discovery")
	}

	request, err := requestPlan(baseURL, route, q, AuthSpec{
		Type:     "oauth2_bearer",
		Env:      []string{"META_ACCESS_TOKEN", "META_IG_ACCESS_TOKEN", "META_USER_ACCESS_TOKEN"},
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
			PriceSource: "Meta Graph API rate-limit headers and app review",
			Notes:       "Instagram Graph access is permission, account-type, and app-review gated; no simple public per-request price is modeled.",
		},
		warnings: []string{"Instagram Graph is for authorized Business/Creator surfaces; arbitrary personal/private accounts are not available."},
		prepare:  bearerAuth("META_ACCESS_TOKEN", "META_IG_ACCESS_TOKEN", "META_USER_ACCESS_TOKEN"),
		parse:    parseMeta(PlatformInstagram, in.Operation),
	}, nil
}

func graphVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultMetaVersion
	}
	return strings.TrimPrefix(value, "/")
}

func parseMeta(platform Platform, operation string) func(any) ([]Item, *Pagination, error) {
	return func(raw any) ([]Item, *Pagination, error) {
		root := asMap(raw)
		rawItems := asSlice(root["data"])
		if len(rawItems) == 0 {
			if bd := asMap(root["business_discovery"]); bd != nil {
				rawItems = append(rawItems, bd)
				if media := asMap(bd["media"]); media != nil {
					rawItems = append(rawItems, asSlice(media["data"])...)
				}
			} else if root != nil {
				rawItems = []any{root}
			}
		}

		items := make([]Item, 0, len(rawItems))
		for _, rawItem := range rawItems {
			m := asMap(rawItem)
			if len(m) == 0 {
				continue
			}
			items = append(items, metaItem(platform, operation, m))
		}

		paging := asMap(root["paging"])
		cursors := asMap(paging["cursors"])
		pagination := &Pagination{
			NextToken:   stringify(cursors["after"]),
			PrevToken:   stringify(cursors["before"]),
			After:       stringify(cursors["after"]),
			Before:      stringify(cursors["before"]),
			ResultCount: len(items),
		}
		if pagination.NextToken == "" && pagination.PrevToken == "" && pagination.ResultCount == 0 {
			pagination = nil
		}
		return items, pagination, nil
	}
}

func metaItem(platform Platform, operation string, m map[string]any) Item {
	itemType := "post"
	if operation == "page_info" {
		itemType = "page"
	}
	if strings.Contains(operation, "comments") {
		itemType = "comment"
	}
	if platform == PlatformInstagram {
		itemType = "media"
		if operation == "media_comments" {
			itemType = "comment"
		}
		if operation == "business_discovery" && m["followers_count"] != nil {
			itemType = "profile"
		}
	}

	text := stringify(m["message"])
	if text == "" {
		text = stringify(m["story"])
	}
	if text == "" {
		text = stringify(m["caption"])
	}
	if text == "" {
		text = stringify(m["text"])
	}
	from := asMap(m["from"])
	platformName := string(platform)
	return Item{
		Platform:       platformName,
		Provider:       providerID(platform),
		Type:           itemType,
		ID:             stringify(m["id"]),
		AuthorID:       stringify(from["id"]),
		AuthorUsername: firstNonEmpty(stringify(from["name"]), stringify(m["username"])),
		Title:          firstNonEmpty(stringify(m["name"]), stringify(m["username"])),
		Text:           text,
		URL:            firstNonEmpty(stringify(m["permalink_url"]), stringify(m["permalink"]), stringify(m["link"])),
		CreatedAt:      firstNonEmpty(stringify(m["created_time"]), stringify(m["timestamp"])),
		Metrics: numberMap(map[string]any{
			"fan_count":       m["fan_count"],
			"followers_count": m["followers_count"],
			"like_count":      m["like_count"],
			"comments_count":  m["comments_count"],
			"comment_count":   m["comment_count"],
			"share_count":     graphShareCount(m),
			"reaction_count":  graphSummaryCount(m["reactions"]),
		}),
		Raw: m,
	}
}

func graphShareCount(m map[string]any) any {
	return asMap(m["shares"])["count"]
}

func graphSummaryCount(raw any) any {
	return asMap(asMap(raw)["summary"])["total_count"]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
