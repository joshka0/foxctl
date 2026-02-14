package teams

import "encoding/json"

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

	// ChannelData is Teams-specific metadata. In some contexts, tenant ID is only present
	// under channelData.tenant.id.
	ChannelData json.RawMessage `json:"channelData,omitempty"`

	Entities []Entity `json:"entities,omitempty"`

	Attachments []Attachment    `json:"attachments,omitempty"`
	Value       json.RawMessage `json:"value,omitempty"`
}

// Attachment represents a Bot Framework attachment (e.g., an Adaptive Card).
type Attachment struct {
	ContentType string `json:"contentType"`
	Content     any    `json:"content,omitempty"`
}

// ChannelAccount identifies a user or bot in a Bot Framework conversation.
type ChannelAccount struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// ConversationAccount identifies a Bot Framework conversation and its tenant.
type ConversationAccount struct {
	ID       string `json:"id,omitempty"`
	TenantID string `json:"tenantId,omitempty"`
	IsGroup  bool   `json:"isGroup,omitempty"`
}

// Entity represents a Bot Framework entity such as a mention.
type Entity struct {
	Type      string         `json:"type,omitempty"`
	Text      string         `json:"text,omitempty"`
	Mentioned ChannelAccount `json:"mentioned,omitempty"`
}

// ResourceResponse is the Bot Framework response containing the created resource ID.
type ResourceResponse struct {
	ID string `json:"id"`
}
