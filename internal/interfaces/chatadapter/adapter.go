// Package chatadapter defines the ChatAdapter interface for connecting agentctl
// to chat platforms (Discord, Slack, Teams). Each platform implements this
// interface to receive slash commands and dispatch them to agentctl skills.
package chatadapter

import (
	"context"
	"sync/atomic"

	"github.com/jkatigb/agentctl/internal/domain/identity"
)

// ChatAdapter is the platform-agnostic interface for chat platform integrations.
type ChatAdapter interface {
	// Connect opens the connection to the chat platform.
	Connect(ctx context.Context) error

	// Disconnect closes the connection gracefully.
	Disconnect(ctx context.Context) error

	// Name returns the adapter name (e.g. "discord", "slack").
	Name() string

	// RegisterCommands registers slash commands with the platform.
	RegisterCommands(ctx context.Context, cmds []CommandDef) error

	// OnCommand sets the handler for incoming slash commands.
	OnCommand(handler CommandHandler)

	// OnInteraction sets the handler for interactive components (buttons, modals).
	OnInteraction(handler InteractionHandler)

	// OnMessage sets the handler for natural language messages (Phase 3).
	OnMessage(handler MessageHandler)
}

// CommandHandler processes an incoming slash command.
type CommandHandler func(ctx context.Context, evt CommandEvent) error

// InteractionHandler processes an interactive component event (Phase 2).
type InteractionHandler func(ctx context.Context, evt InteractionEvent) error

// MessageHandler processes a natural language message (Phase 3).
type MessageHandler func(ctx context.Context, evt MessageEvent) error

// MessageEvent carries the context for a received natural language message.
type MessageEvent struct {
	Content   string
	Principal identity.Principal
	User      UserRef
	ChannelID string
	GuildID   string
	MessageID string

	respond   func(content string, embeds []Embed) (MessageRef, error)
	edit      func(ref MessageRef, content string, embeds []Embed) error
	responded *atomic.Bool
}

// Respond sends an initial reply to the message, returning a ref for later edits.
func (e MessageEvent) Respond(content string, embeds []Embed) (MessageRef, error) {
	if e.respond != nil {
		ref, err := e.respond(content, embeds)
		if err == nil && e.responded != nil {
			e.responded.Store(true)
		}
		return ref, err
	}
	return MessageRef{}, nil
}

// Edit updates a previously sent message.
func (e MessageEvent) Edit(ref MessageRef, content string, embeds []Embed) error {
	if e.edit != nil {
		return e.edit(ref, content, embeds)
	}
	return nil
}

// Responded returns true if Respond has already been called.
func (e MessageEvent) Responded() bool {
	if e.responded == nil {
		return false
	}
	return e.responded.Load()
}

// NewMessageEvent creates a MessageEvent with the given callbacks.
func NewMessageEvent(content string, user UserRef, channelID, guildID, messageID string, respond func(string, []Embed) (MessageRef, error), edit func(MessageRef, string, []Embed) error) MessageEvent {
	return MessageEvent{
		Content:   content,
		User:      user,
		ChannelID: channelID,
		GuildID:   guildID,
		MessageID: messageID,
		respond:   respond,
		edit:      edit,
		responded: &atomic.Bool{},
	}
}

// CommandDef defines a slash command to register with the platform.
type CommandDef struct {
	Name        string
	Description string
	Options     []CommandOption
}

// CommandOption defines a single option/argument for a slash command.
type CommandOption struct {
	Name        string
	Description string
	Type        OptionType
	Required    bool
	Choices     []Choice
}

// OptionType enumerates the types for command options.
type OptionType int

const (
	OptionTypeString OptionType = iota + 1
	OptionTypeInt
	OptionTypeBool
)

// Choice represents a predefined choice for an option.
type Choice struct {
	Name  string
	Value string
}

// CommandEvent carries the context for a received slash command.
type CommandEvent struct {
	Command   string
	Options   map[string]any
	Principal identity.Principal
	User      UserRef
	ChannelID string
	GuildID   string

	// respond is the platform callback for sending a reply.
	respond   func(content string, embeds []Embed) error
	responded *atomic.Bool
}

// Respond sends a reply to the command invocation.
func (e CommandEvent) Respond(content string, embeds []Embed) error {
	if e.respond != nil {
		err := e.respond(content, embeds)
		if err == nil && e.responded != nil {
			e.responded.Store(true)
		}
		return err
	}
	return nil
}

// Responded returns true if Respond has already been called.
func (e CommandEvent) Responded() bool {
	if e.responded == nil {
		return false
	}
	return e.responded.Load()
}

// InteractionEvent carries the context for an interactive component event.
type InteractionEvent struct {
	Type       string // "button", "select"
	CustomID   string
	Principal  identity.Principal
	User       UserRef
	ChannelID  string
	GuildID    string
	MessageRef MessageRef

	respond   func(content string, embeds []Embed) error
	update    func(content string, embeds []Embed, components []Component) error
	responded *atomic.Bool
}

// Respond sends a reply to the interaction.
func (e InteractionEvent) Respond(content string, embeds []Embed) error {
	if e.respond != nil {
		err := e.respond(content, embeds)
		if err == nil && e.responded != nil {
			e.responded.Store(true)
		}
		return err
	}
	return nil
}

// Update edits the original message that triggered the interaction.
func (e InteractionEvent) Update(content string, embeds []Embed, components []Component) error {
	if e.update != nil {
		return e.update(content, embeds, components)
	}
	return nil
}

// Responded returns true if Respond has already been called.
func (e InteractionEvent) Responded() bool {
	if e.responded == nil {
		return false
	}
	return e.responded.Load()
}

// NewInteractionEvent creates an InteractionEvent with the given callbacks.
func NewInteractionEvent(typ, customID string, user UserRef, channelID, guildID string, msgRef MessageRef, respond func(string, []Embed) error, update func(string, []Embed, []Component) error) InteractionEvent {
	return InteractionEvent{
		Type:       typ,
		CustomID:   customID,
		User:       user,
		ChannelID:  channelID,
		GuildID:    guildID,
		MessageRef: msgRef,
		respond:    respond,
		update:     update,
		responded:  &atomic.Bool{},
	}
}

// MessageRef identifies a specific message in a channel for editing.
type MessageRef struct {
	ChannelID string
	MessageID string
}

// Component represents an interactive component (button, action row).
type Component struct {
	Type     ComponentType
	CustomID string
	Label    string
	Style    ButtonStyle
	Disabled bool
	Children []Component // for action rows
}

// ComponentType enumerates interactive component types.
type ComponentType int

const (
	ComponentActionRow ComponentType = iota + 1
	ComponentButton
)

// ButtonStyle enumerates button visual styles.
type ButtonStyle int

const (
	ButtonPrimary ButtonStyle = iota + 1
	ButtonSecondary
	_ // skip 3 (Discord uses 3 for Success which we don't need)
	ButtonDanger
)

// UserRef identifies the user who triggered the event.
type UserRef struct {
	ID       string
	Username string
}

// Embed is a platform-agnostic rich embed for responses.
type Embed struct {
	Title       string
	Description string
	Color       int
	Fields      []Field
	Footer      string
}

// Field is a key-value field inside an Embed.
type Field struct {
	Name   string
	Value  string
	Inline bool
}

// NewCommandEvent creates a CommandEvent with the given respond callback.
// This is used by platform drivers to construct events.
func NewCommandEvent(command string, options map[string]any, user UserRef, channelID, guildID string, respond func(string, []Embed) error) CommandEvent {
	return CommandEvent{
		Command:   command,
		Options:   options,
		User:      user,
		ChannelID: channelID,
		GuildID:   guildID,
		respond:   respond,
		responded: &atomic.Bool{},
	}
}
