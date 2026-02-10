package teams

// Minimal Bot Framework / Microsoft Teams activity types.

// Activity represents an inbound or outbound Bot Framework activity.
// We only model the fields we need for the Teams chat adapter MVP.
type Activity struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	ChannelID string `json:"channelId,omitempty"`

	ServiceURL string `json:"serviceUrl,omitempty"`

	Text      string `json:"text,omitempty"`
	ReplyToID string `json:"replyToId,omitempty"`

	From         ChannelAccount      `json:"from,omitempty"`
	Recipient    ChannelAccount      `json:"recipient,omitempty"`
	Conversation ConversationAccount `json:"conversation,omitempty"`

	Entities []Entity `json:"entities,omitempty"`
}

type ChannelAccount struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type ConversationAccount struct {
	ID       string `json:"id,omitempty"`
	TenantID string `json:"tenantId,omitempty"`
	IsGroup  bool   `json:"isGroup,omitempty"`
}

type Entity struct {
	Type      string         `json:"type,omitempty"`
	Text      string         `json:"text,omitempty"`
	Mentioned ChannelAccount `json:"mentioned,omitempty"`
}

type ResourceResponse struct {
	ID string `json:"id"`
}
