package social

import (
	"net/url"
	"strings"
)

func planYouTube(baseURL string, in Input) (callPlan, error) {
	q := url.Values{}
	route := ""
	quotaUnits := 1

	switch in.Operation {
	case "search":
		if err := require(in.Query, "query is required for search", "Pass query for YouTube Data API search.list."); err != nil {
			return callPlan{}, err
		}
		route = "/search"
		q.Set("part", "snippet")
		q.Set("q", in.Query)
		q.Set("maxResults", intString(boundedLimit(in.Limit, 10, 50)))
		addIf(q, "pageToken", in.PageToken)
		addIf(q, "channelId", in.ChannelID)
		addIf(q, "type", defaultString(in.Type, "video"))
		addIf(q, "publishedAfter", in.Since)
		addIf(q, "publishedBefore", in.Until)
		addIf(q, "order", in.Sort)
		quotaUnits = 100
	case "videos":
		videoIDs, err := ids(in, in.VideoID)
		if err != nil {
			return callPlan{}, err
		}
		if len(videoIDs) == 0 {
			return callPlan{}, require("", "video_id or ids are required for videos", "Pass video_id or ids for videos.list.")
		}
		route = "/videos"
		q.Set("part", "snippet,contentDetails,statistics,status,liveStreamingDetails")
		q.Set("id", strings.Join(videoIDs, ","))
	case "channel":
		route = "/channels"
		q.Set("part", "snippet,statistics,contentDetails")
		switch {
		case strings.TrimSpace(in.ChannelID) != "":
			q.Set("id", in.ChannelID)
		case strings.TrimSpace(in.Handle) != "":
			q.Set("forHandle", in.Handle)
		case strings.TrimSpace(in.Username) != "":
			q.Set("forUsername", in.Username)
		default:
			return callPlan{}, require("", "channel_id, handle, or username is required for channel", "Pass channel_id, handle, or username for channels.list.")
		}
	case "playlist_items":
		if err := require(in.PlaylistID, "playlist_id is required for playlist_items", "Pass playlist_id for playlistItems.list."); err != nil {
			return callPlan{}, err
		}
		route = "/playlistItems"
		q.Set("part", "snippet,contentDetails")
		q.Set("playlistId", in.PlaylistID)
		q.Set("maxResults", intString(boundedLimit(in.Limit, 10, 50)))
		addIf(q, "pageToken", in.PageToken)
	case "comments":
		if err := require(in.VideoID, "video_id is required for comments", "Pass video_id for commentThreads.list."); err != nil {
			return callPlan{}, err
		}
		route = "/commentThreads"
		q.Set("part", "snippet,replies")
		q.Set("videoId", in.VideoID)
		q.Set("textFormat", "plainText")
		q.Set("maxResults", intString(boundedLimit(in.Limit, 20, 100)))
		addIf(q, "pageToken", in.PageToken)
	default:
		return callPlan{}, unsupportedOperation("youtube", in.Operation, "search, videos, channel, playlist_items, comments")
	}

	request, err := requestPlan(baseURL, route, q, AuthSpec{
		Type:       "api_key",
		Env:        []string{"YOUTUBE_API_KEY"},
		QueryParam: "key",
		Required:   true,
	})
	if err != nil {
		return callPlan{}, err
	}
	return callPlan{
		request: request,
		cost: &Cost{
			BillingUnit: "quota_unit",
			QuotaUnits:  quotaUnits,
			Estimated:   true,
			PriceSource: "YouTube Data API quota table",
			Notes:       "Each page costs quota units; default project quota is commonly 10,000 units/day.",
		},
		warnings: youtubeWarnings(in.Operation),
		prepare:  youtubeAPIKeyAuth(),
		parse:    parseYouTube(in.Operation),
	}, nil
}

func parseYouTube(operation string) func(any) ([]Item, *Pagination, error) {
	return func(raw any) ([]Item, *Pagination, error) {
		root := asMap(raw)
		rawItems := asSlice(root["items"])
		items := make([]Item, 0, len(rawItems))
		for _, rawItem := range rawItems {
			m := asMap(rawItem)
			item := youtubeItem(operation, m)
			if item.ID != "" {
				items = append(items, item)
			}
		}
		pageInfo := asMap(root["pageInfo"])
		pagination := &Pagination{
			NextToken:   stringify(root["nextPageToken"]),
			PrevToken:   stringify(root["prevPageToken"]),
			ResultCount: intFromAny(pageInfo["resultsPerPage"]),
		}
		if pagination.ResultCount == 0 {
			pagination.ResultCount = len(items)
		}
		if pagination.NextToken == "" && pagination.PrevToken == "" && pagination.ResultCount == 0 {
			pagination = nil
		}
		return items, pagination, nil
	}
}

func youtubeItem(operation string, m map[string]any) Item {
	snippet := asMap(m["snippet"])
	stats := asMap(m["statistics"])
	content := asMap(m["contentDetails"])
	id := youtubeID(operation, m)
	itemType := youtubeItemType(operation, m)
	item := Item{
		Platform:  "youtube",
		Provider:  providerID(PlatformYouTube),
		Type:      itemType,
		ID:        id,
		Title:     stringify(snippet["title"]),
		Text:      stringify(snippet["description"]),
		CreatedAt: stringify(snippet["publishedAt"]),
		ChannelID: stringify(snippet["channelId"]),
		Metrics: numberMap(map[string]any{
			"view_count":     stats["viewCount"],
			"like_count":     stats["likeCount"],
			"comment_count":  stats["commentCount"],
			"favorite_count": stats["favoriteCount"],
			"item_count":     content["itemCount"],
		}),
		Raw: m,
	}
	item.AuthorID = item.ChannelID
	item.AuthorUsername = stringify(snippet["channelTitle"])
	item.ParentID = stringify(content["videoId"])
	if itemType == "comment" {
		top := asMap(snippet["topLevelComment"])
		commentSnippet := asMap(asMap(top["snippet"]))
		if commentSnippet != nil {
			item.ID = stringify(top["id"])
			item.Text = stringify(commentSnippet["textDisplay"])
			item.AuthorID = youtubeAuthorChannelID(commentSnippet["authorChannelId"])
			item.AuthorUsername = stringify(commentSnippet["authorDisplayName"])
			item.CreatedAt = stringify(commentSnippet["publishedAt"])
			item.UpdatedAt = stringify(commentSnippet["updatedAt"])
			item.ParentID = stringify(commentSnippet["videoId"])
			item.ThreadID = stringify(m["id"])
			item.Metrics = numberMap(map[string]any{"like_count": commentSnippet["likeCount"], "reply_count": snippet["totalReplyCount"]})
		}
	}
	item.URL = youtubeURL(item)
	return item
}

func youtubeAuthorChannelID(raw any) string {
	if m := asMap(raw); m != nil {
		return stringify(m["value"])
	}
	return stringify(raw)
}

func youtubeID(operation string, m map[string]any) string {
	id := m["id"]
	if idMap := asMap(id); idMap != nil {
		for _, key := range []string{"videoId", "channelId", "playlistId"} {
			if value := stringify(idMap[key]); value != "" {
				return value
			}
		}
	}
	if content := asMap(m["contentDetails"]); content != nil {
		if videoID := stringify(content["videoId"]); videoID != "" {
			return videoID
		}
	}
	return stringify(id)
}

func youtubeItemType(operation string, m map[string]any) string {
	if operation == "search" {
		if idMap := asMap(m["id"]); idMap != nil {
			kind := stringify(idMap["kind"])
			if strings.Contains(kind, "channel") {
				return "channel"
			}
			if strings.Contains(kind, "playlist") {
				return "playlist"
			}
		}
		return "video"
	}
	switch operation {
	case "channel":
		return "channel"
	case "playlist_items":
		return "playlist_item"
	case "comments":
		return "comment"
	default:
		return "video"
	}
}

func youtubeURL(item Item) string {
	switch item.Type {
	case "channel":
		return "https://www.youtube.com/channel/" + item.ID
	case "playlist", "playlist_item":
		return "https://www.youtube.com/playlist?list=" + item.ID
	case "comment":
		if item.ParentID != "" {
			return "https://www.youtube.com/watch?v=" + item.ParentID + "&lc=" + item.ID
		}
		return ""
	default:
		return "https://www.youtube.com/watch?v=" + item.ID
	}
}

func youtubeWarnings(operation string) []string {
	if operation == "search" {
		return []string{"YouTube search returns pointers; hydrate video/channel IDs with videos or channel operations for durable metadata."}
	}
	if operation == "comments" {
		return []string{"Comment threads may include incomplete replies; fetch comment replies separately if a future skill needs full trees."}
	}
	return nil
}

func defaultString(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}
