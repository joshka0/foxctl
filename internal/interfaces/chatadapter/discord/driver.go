// Package discord implements the ChatAdapter interface for Discord using discordgo.
package discord

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/jkatigb/agentctl/internal/domain/identity"
	"github.com/jkatigb/agentctl/internal/interfaces/chatadapter"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/sse"
)

// Adapter implements chatadapter.ChatAdapter for Discord.
type Adapter struct {
	token      string
	guildID    string // empty = global commands
	daemonURL  string // daemon API base URL
	httpClient *http.Client
	newTicker  func(d time.Duration) *time.Ticker
	cfg        config.DiscordSettings

	session    *discordgo.Session
	cmdHandler chatadapter.CommandHandler
	intHandler chatadapter.InteractionHandler
	msgHandler chatadapter.MessageHandler

	sseHub    *sse.Hub
	sseClient *sse.Client

	threadMap sync.Map // sessionID -> threadID

	msgSem chan struct{} // limits concurrent message handlers

	mu             sync.Mutex
	registeredCmds []*discordgo.ApplicationCommand

	ctx    context.Context
	cancel context.CancelFunc

	wg sync.WaitGroup
}

// New creates a Discord adapter with the given config and daemon URL.
func New(cfg config.DiscordSettings, daemonURL string) *Adapter {
	return &Adapter{
		token:      cfg.BotToken,
		guildID:    cfg.GuildID,
		daemonURL:  daemonURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		newTicker:  time.NewTicker,
		cfg:        cfg,
		msgSem:     make(chan struct{}, 10),
	}
}

func (a *Adapter) Name() string { return "discord" }

// OnCommand sets the slash command handler.
func (a *Adapter) OnCommand(handler chatadapter.CommandHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cmdHandler = handler
}

// OnInteraction sets the interactive component handler (Phase 2).
func (a *Adapter) OnInteraction(handler chatadapter.InteractionHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.intHandler = handler
}

// OnMessage sets the natural language message handler (Phase 3).
func (a *Adapter) OnMessage(handler chatadapter.MessageHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.msgHandler = handler
}

// Connect creates the discordgo session, registers the interaction handler, and opens the gateway.
func (a *Adapter) Connect(ctx context.Context) error {
	// Ensure we always have a non-nil parent context. Discord handlers derive
	// per-request contexts from a.ctx, and context.WithCancel/WithTimeout will
	// panic if given a nil parent.
	if ctx == nil {
		ctx = context.Background()
	}
	a.ctx, a.cancel = context.WithCancel(ctx)

	sess, err := discordgo.New("Bot " + a.token)
	if err != nil {
		return fmt.Errorf("discord: create session: %w", err)
	}

	sess.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	// Register interaction create handler
	sess.AddHandler(a.handleInteraction)

	// Register message create handler (Phase 3 - natural language chat)
	sess.AddHandler(a.handleMessageCreate)

	// Set session before opening so handlers can safely reference it.
	a.session = sess

	if err := sess.Open(); err != nil {
		// Avoid leaking the adapter context on failed connects.
		if a.cancel != nil {
			a.cancel()
		}
		a.session = nil
		return fmt.Errorf("discord: open gateway: %w", err)
	}

	if sess.State != nil && sess.State.User != nil {
		observability.Emit(ctx, observability.NewEvent("discord.connected").
			WithComponent("discord").
			WithData("user", sess.State.User.Username).
			Success(0))
	} else {
		observability.Emit(ctx, observability.NewEvent("discord.connected").
			WithComponent("discord").
			Success(0))
	}

	// Start presence updater
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.startPresenceUpdater(a.ctx)
	}()

	// Start SSE event listener if hub is configured
	if a.sseHub != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.startEventListener(a.ctx)
		}()
	}

	return nil
}

// Disconnect closes the Discord session and removes registered commands.
func (a *Adapter) Disconnect(ctx context.Context) error {
	if a.session == nil {
		return nil
	}

	// Unregister SSE client
	if a.sseHub != nil && a.sseClient != nil {
		a.sseHub.Unregister(a.sseClient)
		a.sseClient = nil
	}

	// Clean up registered commands (respect ctx deadline)
	a.mu.Lock()
	cmds := a.registeredCmds
	a.registeredCmds = nil
	a.mu.Unlock()

	appID := a.appID()
	for _, cmd := range cmds {
		select {
		case <-ctx.Done():
			observability.Emit(ctx, observability.NewEvent("discord.disconnect_cancelled").
				WithComponent("discord").
				WithData("remaining_commands", len(cmds)).
				Canceled(0))
			goto close
		default:
		}
		if err := a.session.ApplicationCommandDelete(appID, a.guildID, cmd.ID); err != nil {
			observability.Emit(ctx, observability.NewEvent("discord.command_delete_failed").
				WithComponent("discord").
				WithData("cmd", cmd.Name).
				Error(err, 0))
		}
	}

close:
	if a.cancel != nil {
		a.cancel()
	}
	if err := a.session.Close(); err != nil {
		return fmt.Errorf("discord: close: %w", err)
	}

	// Wait for background goroutines (presence updater, SSE listener, message handlers).
	a.wg.Wait()

	observability.Emit(ctx, observability.NewEvent("discord.disconnected").
		WithComponent("discord").
		Success(0))
	return nil
}

// RegisterCommands bulk-registers slash commands with Discord.
func (a *Adapter) RegisterCommands(ctx context.Context, cmds []chatadapter.CommandDef) error {
	if a.session == nil {
		return fmt.Errorf("discord: not connected")
	}

	appID := a.appID()
	if appID == "" {
		return fmt.Errorf("discord: cannot register commands: application ID unavailable (invalid session state)")
	}
	var registered []*discordgo.ApplicationCommand

	for _, cmd := range cmds {
		select {
		case <-ctx.Done():
			observability.Emit(ctx, observability.NewEvent("discord.register_cancelled").
				WithComponent("discord").
				Canceled(0))
			a.mu.Lock()
			a.registeredCmds = registered
			a.mu.Unlock()
			return ctx.Err()
		default:
		}
		dcmd := toDiscordCommand(cmd)
		created, err := a.session.ApplicationCommandCreate(appID, a.guildID, dcmd)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("discord.command_register_failed").
				WithComponent("discord").
				WithData("cmd", cmd.Name).
				Error(err, 0))
			continue
		}
		registered = append(registered, created)
		observability.Emit(ctx, observability.NewEvent("discord.command_registered").
			WithComponent("discord").
			WithData("cmd", cmd.Name).
			Success(0))
	}

	a.mu.Lock()
	a.registeredCmds = registered
	a.mu.Unlock()

	observability.Emit(ctx, observability.NewEvent("discord.commands_registered").
		WithComponent("discord").
		WithData("count", len(registered)).
		Success(0))
	return nil
}

// handleInteraction is the discordgo callback for InteractionCreate events.
func (a *Adapter) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		a.handleSlashCommand(s, i)
	case discordgo.InteractionMessageComponent:
		a.handleComponentInteraction(s, i)
	}
}

// handleComponentInteraction processes button/select interactions.
func (a *Adapter) handleComponentInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	a.mu.Lock()
	handler := a.intHandler
	a.mu.Unlock()

	if handler == nil {
		return
	}

	data := i.MessageComponentData()

	user := chatadapter.UserRef{}
	if i.Member != nil && i.Member.User != nil {
		user.ID = i.Member.User.ID
		user.Username = i.Member.User.Username
	} else if i.User != nil {
		user.ID = i.User.ID
		user.Username = i.User.Username
	}

	msgRef := chatadapter.MessageRef{}
	if i.Message != nil {
		msgRef.ChannelID = i.Message.ChannelID
		msgRef.MessageID = i.Message.ID
	}

	respond := func(content string, embeds []chatadapter.Embed) error {
		resp := &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: content,
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		}
		if len(embeds) > 0 {
			resp.Data.Embeds = toDiscordEmbeds(embeds)
		}
		return s.InteractionRespond(i.Interaction, resp)
	}

	update := func(content string, embeds []chatadapter.Embed, components []chatadapter.Component) error {
		resp := &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content: content,
			},
		}
		if len(embeds) > 0 {
			resp.Data.Embeds = toDiscordEmbeds(embeds)
		}
		if components != nil {
			resp.Data.Components = toDiscordComponents(components)
		}
		return s.InteractionRespond(i.Interaction, resp)
	}

	evt := chatadapter.NewInteractionEvent("button", data.CustomID, user, i.ChannelID, i.GuildID, msgRef, respond, update)

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	handlerCtx, handlerCancel := context.WithCancel(parent)
	defer handlerCancel()

	if err := handler(handlerCtx, evt); err != nil {
		observability.Emit(context.Background(), observability.NewEvent("discord.interaction_error").
			WithComponent("discord").
			WithData("custom_id", data.CustomID).
			Error(err, 0))
		if !evt.Responded() {
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("Error: %s", err),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
		}
	}
}

// handleSlashCommand processes slash command interactions.
func (a *Adapter) handleSlashCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	a.mu.Lock()
	handler := a.cmdHandler
	a.mu.Unlock()

	if handler == nil {
		return
	}

	data := i.ApplicationCommandData()

	// Extract options
	opts := make(map[string]any)
	for _, opt := range data.Options {
		switch opt.Type {
		case discordgo.ApplicationCommandOptionString:
			opts[opt.Name] = opt.StringValue()
		case discordgo.ApplicationCommandOptionInteger:
			opts[opt.Name] = opt.IntValue()
		case discordgo.ApplicationCommandOptionBoolean:
			opts[opt.Name] = opt.BoolValue()
		default:
			opts[opt.Name] = opt.Value
		}
	}

	// Build user ref
	user := chatadapter.UserRef{}
	if i.Member != nil && i.Member.User != nil {
		user.ID = i.Member.User.ID
		user.Username = i.Member.User.Username
	} else if i.User != nil {
		user.ID = i.User.ID
		user.Username = i.User.Username
	}

	guildID := ""
	if i.GuildID != "" {
		guildID = i.GuildID
	}

	// Immediately acknowledge the interaction (Discord's 3s deadline)
	if ackErr := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); ackErr != nil {
		observability.Emit(context.Background(), observability.NewEvent("discord.interaction_ack_failed").
			WithComponent("discord").
			WithData("cmd", data.Name).
			Error(ackErr, 0))
	}

	// Build the respond callback (edits the deferred message)
	respond := func(content string, embeds []chatadapter.Embed) error {
		edit := &discordgo.WebhookEdit{
			Content: &content,
		}

		if len(embeds) > 0 {
			dEmbeds := toDiscordEmbeds(embeds)
			edit.Embeds = &dEmbeds
		}

		_, err := s.InteractionResponseEdit(i.Interaction, edit)
		return err
	}

	evt := chatadapter.NewCommandEvent(data.Name, opts, user, i.ChannelID, guildID, respond)

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	handlerCtx, handlerCancel := context.WithTimeout(parent, 5*time.Minute)
	defer handlerCancel()

	if err := handler(handlerCtx, evt); err != nil {
		observability.Emit(context.Background(), observability.NewEvent("discord.command_handler_error").
			WithComponent("discord").
			WithData("cmd", data.Name).
			Error(err, 0))
		if !evt.Responded() {
			errMsg := fmt.Sprintf("Internal error: %s\n\nPlease try again or check bot configuration.", err)
			_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: &errMsg,
			})
		}
	}
}

// toDiscordCommand converts a platform-agnostic CommandDef to a discordgo ApplicationCommand.
func toDiscordCommand(cmd chatadapter.CommandDef) *discordgo.ApplicationCommand {
	dcmd := &discordgo.ApplicationCommand{
		Name:        cmd.Name,
		Description: cmd.Description,
	}

	for _, opt := range cmd.Options {
		dopt := &discordgo.ApplicationCommandOption{
			Name:        opt.Name,
			Description: opt.Description,
			Type:        toDiscordOptionType(opt.Type),
			Required:    opt.Required,
		}
		for _, c := range opt.Choices {
			dopt.Choices = append(dopt.Choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  c.Name,
				Value: c.Value,
			})
		}
		dcmd.Options = append(dcmd.Options, dopt)
	}

	return dcmd
}

// appID returns the bot's application ID, safely handling nil State/User.
func (a *Adapter) appID() string {
	if a.session != nil && a.session.State != nil && a.session.State.User != nil {
		return a.session.State.User.ID
	}
	return ""
}

// toDiscordOptionType maps our OptionType to discordgo's.
func toDiscordOptionType(t chatadapter.OptionType) discordgo.ApplicationCommandOptionType {
	switch t {
	case chatadapter.OptionTypeString:
		return discordgo.ApplicationCommandOptionString
	case chatadapter.OptionTypeInt:
		return discordgo.ApplicationCommandOptionInteger
	case chatadapter.OptionTypeBool:
		return discordgo.ApplicationCommandOptionBoolean
	default:
		return discordgo.ApplicationCommandOptionString
	}
}

// SetSSEHub configures the SSE hub for event listening.
func (a *Adapter) SetSSEHub(hub *sse.Hub) {
	a.sseHub = hub
}

// handleMessageCreate processes incoming Discord messages for natural language chat.
func (a *Adapter) handleMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore bot messages
	if m.Author == nil || m.Author.Bot {
		return
	}

	// Ignore non-default message types (joins, pins, etc.)
	if m.Type != discordgo.MessageTypeDefault && m.Type != discordgo.MessageTypeReply {
		return
	}

	// Determine if we should respond: all messages in chat channels, only @mentions elsewhere
	inChatChannel := a.isChatChannel(m.ChannelID)
	mentioned := a.isBotMentioned(m.Message)
	if !inChatChannel && !mentioned {
		return
	}

	content := m.Content
	if mentioned && !inChatChannel {
		content = a.cleanMention(content)
	}

	// Ignore empty messages (e.g., image-only)
	if strings.TrimSpace(content) == "" {
		if m.Content == "" && len(m.Attachments) == 0 {
			observability.Emit(context.Background(), observability.NewEvent("discord.message_empty_content").
				WithComponent("discord").
				WithData("channel_id", m.ChannelID).
				WithData("hint", "ensure Message Content Intent is enabled in Discord Developer Portal").
				Success(0))
		}
		return
	}

	a.mu.Lock()
	handler := a.msgHandler
	a.mu.Unlock()

	if handler == nil {
		return
	}

	user := chatadapter.UserRef{
		ID:       m.Author.ID,
		Username: m.Author.Username,
	}

	principal := identity.Principal{
		UserID:   m.Author.ID,
		Username: m.Author.Username,
		Platform: "discord",
	}

	guildID := ""
	if m.GuildID != "" {
		guildID = m.GuildID
	}

	respond := func(content string, embeds []chatadapter.Embed) (chatadapter.MessageRef, error) {
		var dEmbeds []*discordgo.MessageEmbed
		if len(embeds) > 0 {
			dEmbeds = toDiscordEmbeds(embeds)
		}
		return a.SendMessage(m.ChannelID, content, dEmbeds, nil)
	}

	edit := func(ref chatadapter.MessageRef, content string, embeds []chatadapter.Embed) error {
		var dEmbeds []*discordgo.MessageEmbed
		if len(embeds) > 0 {
			dEmbeds = toDiscordEmbeds(embeds)
		}
		return a.EditMessage(ref, content, dEmbeds, nil)
	}

	evt := chatadapter.NewMessageEvent(content, user, m.ChannelID, guildID, m.ID, respond, edit)
	evt.Principal = principal

	// Limit concurrent message handlers to prevent resource exhaustion.
	// Use a non-blocking select so messages are dropped (not queued) when at capacity.
	select {
	case a.msgSem <- struct{}{}:
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			defer func() { <-a.msgSem }()
			parent := a.ctx
			if parent == nil {
				parent = context.Background()
			}
			handlerCtx, handlerCancel := context.WithTimeout(parent, 5*time.Minute)
			defer handlerCancel()

			if err := handler(handlerCtx, evt); err != nil {
				observability.Emit(context.Background(), observability.NewEvent("discord.message_handler_error").
					WithComponent("discord").
					WithData("channel_id", m.ChannelID).
					Error(err, 0))
			}
		}()
	default:
		observability.Emit(context.Background(), observability.NewEvent("discord.message_handler_dropped").
			WithComponent("discord").
			WithData("channel_id", m.ChannelID).
			WithData("reason", "concurrency limit reached").
			Success(0))
	}
}

// isChatChannel returns true if the channel is in the configured chat channel list.
func (a *Adapter) isChatChannel(channelID string) bool {
	for _, id := range a.cfg.ChatChannelIDs {
		if id == channelID {
			return true
		}
	}
	return false
}

// isBotMentioned returns true if the bot is mentioned in the message.
func (a *Adapter) isBotMentioned(m *discordgo.Message) bool {
	botID := a.appID()
	if botID == "" {
		return false
	}
	for _, u := range m.Mentions {
		if u.ID == botID {
			return true
		}
	}
	return false
}

// cleanMention strips the bot's @mention from message content.
func (a *Adapter) cleanMention(content string) string {
	botID := a.appID()
	if botID == "" {
		return content
	}
	// Discord mentions: <@ID> or <@!ID>
	content = strings.ReplaceAll(content, "<@"+botID+">", "")
	content = strings.ReplaceAll(content, "<@!"+botID+">", "")
	return strings.TrimSpace(content)
}

// ShowTyping sends a typing indicator to a channel, honoring context cancellation.
func (a *Adapter) ShowTyping(ctx context.Context, channelID string) {
	if a.session == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	_ = a.session.ChannelTyping(channelID)
}

// SendMessage sends a message to a Discord channel.
func (a *Adapter) SendMessage(channelID, content string, embeds []*discordgo.MessageEmbed, components []discordgo.MessageComponent) (chatadapter.MessageRef, error) {
	if a.session == nil {
		return chatadapter.MessageRef{}, fmt.Errorf("discord: not connected")
	}

	msg, err := a.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    content,
		Embeds:     embeds,
		Components: components,
	})
	if err != nil {
		return chatadapter.MessageRef{}, fmt.Errorf("discord: send message: %w", err)
	}

	return chatadapter.MessageRef{ChannelID: channelID, MessageID: msg.ID}, nil
}

// EditMessage edits an existing Discord message.
func (a *Adapter) EditMessage(ref chatadapter.MessageRef, content string, embeds []*discordgo.MessageEmbed, components []discordgo.MessageComponent) error {
	if a.session == nil {
		return fmt.Errorf("discord: not connected")
	}

	edit := &discordgo.MessageEdit{
		Channel:    ref.ChannelID,
		ID:         ref.MessageID,
		Content:    &content,
		Embeds:     &embeds,
		Components: &components,
	}

	_, err := a.session.ChannelMessageEditComplex(edit)
	if err != nil {
		return fmt.Errorf("discord: edit message: %w", err)
	}
	return nil
}

// CreateThread creates a new thread in a channel and returns the thread ID.
func (a *Adapter) CreateThread(channelID, name string, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) (string, error) {
	if a.session == nil {
		return "", fmt.Errorf("discord: not connected")
	}

	// Create a starter message for the thread
	var embeds []*discordgo.MessageEmbed
	if embed != nil {
		embeds = []*discordgo.MessageEmbed{embed}
	}

	msg, err := a.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Embeds:     embeds,
		Components: components,
	})
	if err != nil {
		return "", fmt.Errorf("discord: send thread starter: %w", err)
	}

	// Create thread from the message
	thread, err := a.session.MessageThreadStartComplex(channelID, msg.ID, &discordgo.ThreadStart{
		Name:                name,
		AutoArchiveDuration: 1440, // 24 hours
	})
	if err != nil {
		return "", fmt.Errorf("discord: create thread: %w", err)
	}

	return thread.ID, nil
}

// ReplyInThread sends a message in a thread.
func (a *Adapter) ReplyInThread(threadID, content string, embeds []*discordgo.MessageEmbed) (chatadapter.MessageRef, error) {
	if a.session == nil {
		return chatadapter.MessageRef{}, fmt.Errorf("discord: not connected")
	}

	msg, err := a.session.ChannelMessageSendComplex(threadID, &discordgo.MessageSend{
		Content: content,
		Embeds:  embeds,
	})
	if err != nil {
		return chatadapter.MessageRef{}, fmt.Errorf("discord: reply in thread: %w", err)
	}

	return chatadapter.MessageRef{ChannelID: threadID, MessageID: msg.ID}, nil
}

// toDiscordEmbeds converts platform-agnostic embeds to discordgo embeds.
func toDiscordEmbeds(embeds []chatadapter.Embed) []*discordgo.MessageEmbed {
	result := make([]*discordgo.MessageEmbed, 0, len(embeds))
	for _, e := range embeds {
		de := &discordgo.MessageEmbed{
			Title:       e.Title,
			Description: e.Description,
			Color:       e.Color,
		}
		if e.Footer != "" {
			de.Footer = &discordgo.MessageEmbedFooter{Text: e.Footer}
		}
		for _, f := range e.Fields {
			de.Fields = append(de.Fields, &discordgo.MessageEmbedField{
				Name:   f.Name,
				Value:  f.Value,
				Inline: f.Inline,
			})
		}
		result = append(result, de)
	}
	return result
}

// toDiscordComponents converts platform-agnostic components to discordgo message components.
func toDiscordComponents(components []chatadapter.Component) []discordgo.MessageComponent {
	if len(components) == 0 {
		return nil
	}

	result := make([]discordgo.MessageComponent, 0, len(components))
	for _, c := range components {
		switch c.Type {
		case chatadapter.ComponentActionRow:
			children := make([]discordgo.MessageComponent, 0, len(c.Children))
			for _, child := range c.Children {
				if child.Type == chatadapter.ComponentButton {
					children = append(children, discordgo.Button{
						Label:    child.Label,
						Style:    discordgo.ButtonStyle(child.Style),
						CustomID: child.CustomID,
						Disabled: child.Disabled,
					})
				}
			}
			result = append(result, discordgo.ActionsRow{Components: children})
		case chatadapter.ComponentButton:
			result = append(result, discordgo.Button{
				Label:    c.Label,
				Style:    discordgo.ButtonStyle(c.Style),
				CustomID: c.CustomID,
				Disabled: c.Disabled,
			})
		}
	}
	return result
}
