// Package telegram implements the ChatAdapter interface for Telegram using the Bot API.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/jkatigb/agentctl/internal/domain/identity"
	"github.com/jkatigb/agentctl/internal/interfaces/chatadapter"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/sse"
)

// Adapter implements chatadapter.ChatAdapter for Telegram.
//
// Notes:
// - Telegram doesn't have typed slash-command schemas like Discord; we parse "/cmd args" manually.
// - Telegram has no true ephemeral messages; CommandEvent.Respond sends a normal reply.
type Adapter struct {
	token     string
	daemonURL string

	httpClient *http.Client
	clock      chatadapter.Clock
	cfg        config.TelegramSettings

	bot         *tgbotapi.BotAPI
	botUsername string
	botID       int64

	cmdHandler chatadapter.CommandHandler
	intHandler chatadapter.InteractionHandler
	msgHandler chatadapter.MessageHandler

	sseHub    *sse.Hub
	sseClient *sse.Client

	agentThreads sync.Map // agentID -> agentThread (root msg + throttling state)
	agentRootIdx sync.Map // "<chatID>:<messageID>" -> agentRootIndexEntry; for reply-based agent chat

	msgSem chan struct{} // limits concurrent command/message handlers

	mu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// defaultClock implements chatadapter.Clock using real time.
type defaultClock struct{}

func (defaultClock) Now() time.Time { return time.Now() }

const (
	agentIndexTTL             = 24 * time.Hour
	agentIndexJanitorInterval = 30 * time.Minute
)

type agentRootIndexEntry struct {
	AgentID  string
	LastSeen time.Time
}

// New creates a Telegram adapter with the given config, daemon URL, and optional clock.
// If clk is nil, a real-time clock is used.
func New(cfg config.TelegramSettings, daemonURL string, clk chatadapter.Clock) *Adapter {
	limit := cfg.MaxConcurrentMessages
	if limit <= 0 {
		limit = 10
	}

	if clk == nil {
		clk = defaultClock{}
	}

	return &Adapter{
		token:      cfg.BotToken,
		daemonURL:  daemonURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		clock:      clk,
		cfg:        cfg,
		msgSem:     make(chan struct{}, limit),
	}
}

func (a *Adapter) Name() string { return "telegram" }

// SetSSEHub configures the SSE hub for activity event routing (Phase 2).
func (a *Adapter) SetSSEHub(hub *sse.Hub) {
	a.sseHub = hub
}

// OnCommand sets the command handler for "/cmd args" messages.
func (a *Adapter) OnCommand(handler chatadapter.CommandHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cmdHandler = handler
}

// OnInteraction sets the inline keyboard (callback query) handler.
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

// Connect initializes the Telegram bot and starts the long polling update loop.
func (a *Adapter) Connect(ctx context.Context) error {
	// Ensure we always have a non-nil parent context. Handlers derive per-request
	// contexts from a.ctx, and context.WithCancel/WithTimeout will panic if given nil.
	if ctx == nil {
		ctx = context.Background()
	}
	a.ctx, a.cancel = context.WithCancel(ctx)

	token := strings.TrimSpace(a.token)
	if token == "" {
		return fmt.Errorf("telegram: missing TELEGRAM_BOT_TOKEN")
	}

	bot, err := tgbotapi.NewBotAPIWithClient(token, tgbotapi.APIEndpoint, a.httpClient)
	if err != nil {
		if a.cancel != nil {
			a.cancel()
		}
		return fmt.Errorf("telegram: create bot: %w", err)
	}

	a.mu.Lock()
	a.bot = bot
	a.botUsername = bot.Self.UserName
	a.botID = bot.Self.ID
	a.mu.Unlock()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60 // long polling timeout (seconds)
	updates := bot.GetUpdatesChan(u)

	a.wg.Add(1)
	go a.updateLoop(updates)

	// Start SSE event listener if hub is configured.
	if a.sseHub != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.startEventListener(a.ctx)
		}()
	}

	// Periodically evict old agent thread/index entries (prevents unbounded growth).
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.agentIndexJanitor(a.ctx)
	}()

	observability.Emit(ctx, observability.NewEvent("telegram.connected").
		WithComponent("telegram").
		WithData("username", a.botUsername).
		Success(0))

	return nil
}

// Disconnect stops the polling loop and cancels any in-flight handlers.
func (a *Adapter) Disconnect(ctx context.Context) error {
	a.mu.Lock()
	bot := a.bot
	a.bot = nil
	if a.sseHub != nil && a.sseClient != nil {
		a.sseHub.Unregister(a.sseClient)
		a.sseClient = nil
	}
	a.mu.Unlock()

	if a.cancel != nil {
		a.cancel()
	}
	if bot != nil {
		bot.StopReceivingUpdates()
	}
	a.wg.Wait()

	observability.Emit(ctx, observability.NewEvent("telegram.disconnected").
		WithComponent("telegram").
		Success(0))

	return nil
}

// RegisterCommands registers bot commands (names + descriptions). Telegram does not support typed params.
func (a *Adapter) RegisterCommands(ctx context.Context, cmds []chatadapter.CommandDef) error {
	a.mu.Lock()
	bot := a.bot
	a.mu.Unlock()
	if bot == nil {
		return fmt.Errorf("telegram: not connected")
	}

	tgCmds := make([]tgbotapi.BotCommand, 0, len(cmds))
	for _, c := range cmds {
		name := strings.TrimSpace(c.Name)
		desc := strings.TrimSpace(c.Description)
		if name == "" || desc == "" {
			continue
		}
		tgCmds = append(tgCmds, tgbotapi.BotCommand{Command: name, Description: desc})
	}
	if len(tgCmds) == 0 {
		return nil
	}

	_, err := bot.Request(tgbotapi.NewSetMyCommands(tgCmds...))
	if err != nil {
		return fmt.Errorf("telegram: setMyCommands: %w", err)
	}
	return nil
}

func (a *Adapter) updateLoop(updates tgbotapi.UpdatesChannel) {
	defer a.wg.Done()

	for {
		select {
		case <-a.ctx.Done():
			return
		case upd, ok := <-updates:
			if !ok {
				return
			}
			a.handleUpdate(upd)
		}
	}
}

func (a *Adapter) handleUpdate(upd tgbotapi.Update) {
	switch {
	case upd.Message != nil:
		a.handleMessage(upd.Message)
	case upd.CallbackQuery != nil:
		a.handleCallbackQuery(upd.CallbackQuery)
	default:
		// ignore unsupported updates for now
	}
}

func (a *Adapter) handleMessage(m *tgbotapi.Message) {
	if m == nil || m.Chat == nil {
		return
	}
	if m.From == nil || m.From.IsBot {
		return
	}

	text := strings.TrimSpace(m.Text)
	if text == "" {
		// ignore non-text messages for MVP (photos, stickers, etc.)
		return
	}

	// tgbotapi/v5 doesn't expose Telegram forum topic fields (message_thread_id).
	// For now we key sessions by chat only and rely on reply_to_message_id to keep
	// responses in the same topic when applicable.
	chatKey := channelKey(m.Chat.ID, 0)

	// Commands take precedence over natural language chat.
	if m.IsCommand() {
		a.dispatchCommand(m, chatKey)
		return
	}

	// If this is a reply to one of our agent "thread" messages, route it to the
	// agent chat endpoint instead of the console session bridge.
	if m.ReplyToMessage != nil {
		rootKey := agentRootKey(m.Chat.ID, m.ReplyToMessage.MessageID)
		if v, ok := a.agentRootIdx.Load(rootKey); ok {
			switch ent := v.(type) {
			case agentRootIndexEntry:
				agentID := strings.TrimSpace(ent.AgentID)
				if agentID != "" {
					ent.LastSeen = a.clock.Now()
					a.agentRootIdx.Store(rootKey, ent)
					a.dispatchAgentAsk(m, agentID, text)
					return
				}
			case string:
				// Backward compatible (in-memory only) with older entry types.
				agentID := strings.TrimSpace(ent)
				if agentID != "" {
					a.agentRootIdx.Store(rootKey, agentRootIndexEntry{AgentID: agentID, LastSeen: a.clock.Now()})
					a.dispatchAgentAsk(m, agentID, text)
					return
				}
			}
		}
	}

	a.dispatchMessage(m, chatKey, text)
}

func (a *Adapter) dispatchCommand(m *tgbotapi.Message, chatKey string) {
	a.mu.Lock()
	handler := a.cmdHandler
	a.mu.Unlock()

	cmd := strings.TrimSpace(m.Command())
	if cmd == "" {
		return
	}
	// Telegram recommends underscores in command names; normalize for our router.
	cmd = strings.ReplaceAll(cmd, "_", "-")
	args := strings.TrimSpace(m.CommandArguments())

	// Adapter-local helper commands (not routed to agentctl).
	switch cmd {
	case "chat-id":
		content := fmt.Sprintf("chat_id: %d\nchannel_key: %s", m.Chat.ID, chatKey)
		_, _ = a.sendMessage(m.Chat.ID, m.MessageID, content, nil)
		return
	}

	if handler == nil {
		return
	}

	opts := parseCommandOptions(cmd, args)

	user := chatadapter.UserRef{
		ID:       strconv.FormatInt(m.From.ID, 10),
		Username: displayName(m.From),
	}

	respond := func(content string, _ []chatadapter.Embed) error {
		_, err := a.sendMessage(m.Chat.ID, m.MessageID, content, nil)
		return err
	}

	evt := chatadapter.NewCommandEvent(cmd, opts, user, chatKey, "", respond)

	a.dispatchWithLimit("telegram.command", m.Chat.ID, func(ctx context.Context) error {
		return handler(ctx, evt)
	})
}

func (a *Adapter) dispatchMessage(m *tgbotapi.Message, chatKey string, content string) {
	a.mu.Lock()
	handler := a.msgHandler
	botUsername := a.botUsername
	botID := a.botID
	a.mu.Unlock()
	if handler == nil {
		return
	}

	// Determine if we should respond:
	// - Private chats: respond to all messages
	// - Allowlisted chats: respond to all messages
	// - Otherwise: respond only if mentioned or replying to the bot
	mentioned := botUsername != "" && strings.Contains(content, "@"+botUsername)
	replyingToBot := m.ReplyToMessage != nil &&
		m.ReplyToMessage.From != nil &&
		(m.ReplyToMessage.From.ID == botID ||
			(botUsername != "" && m.ReplyToMessage.From.UserName == botUsername))
	inChat := a.isChatID(m.Chat.ID)
	isPrivate := m.Chat.IsPrivate()

	if !isPrivate && !inChat && !mentioned && !replyingToBot {
		return
	}

	if mentioned && !inChat && !isPrivate {
		// If we're only responding due to a mention, strip the mention from the content.
		content = cleanMention(content, botUsername)
	}

	if strings.TrimSpace(content) == "" {
		return
	}

	user := chatadapter.UserRef{
		ID:       strconv.FormatInt(m.From.ID, 10),
		Username: displayName(m.From),
	}

	principal := identity.Principal{
		UserID:   strconv.FormatInt(m.From.ID, 10),
		Username: displayName(m.From),
		Platform: "telegram",
	}

	respond := func(content string, _ []chatadapter.Embed) (chatadapter.MessageRef, error) {
		sentID, err := a.sendMessage(m.Chat.ID, m.MessageID, content, nil)
		if err != nil {
			return chatadapter.MessageRef{}, err
		}
		return chatadapter.MessageRef{
			ChannelID: chatKey,
			MessageID: strconv.Itoa(sentID),
		}, nil
	}

	edit := func(ref chatadapter.MessageRef, content string, _ []chatadapter.Embed) error {
		chatID, _, ok := parseChannelKey(ref.ChannelID)
		if !ok {
			return fmt.Errorf("telegram: invalid channel ref %q", ref.ChannelID)
		}
		msgID, err := strconv.Atoi(ref.MessageID)
		if err != nil {
			return fmt.Errorf("telegram: invalid message ref %q: %w", ref.MessageID, err)
		}
		return a.editMessage(chatID, msgID, content, nil)
	}

	evt := chatadapter.NewMessageEvent(content, user, chatKey, "", strconv.Itoa(m.MessageID), respond, edit)
	evt.Principal = principal

	a.dispatchWithLimit("telegram.message", m.Chat.ID, func(ctx context.Context) error {
		return handler(ctx, evt)
	})
}

func (a *Adapter) dispatchAgentAsk(m *tgbotapi.Message, agentID string, content string) {
	agentID = strings.TrimSpace(agentID)
	content = strings.TrimSpace(content)
	if agentID == "" || content == "" || m == nil || m.Chat == nil {
		return
	}

	a.dispatchWithLimit("telegram.agent_ask", m.Chat.ID, func(ctx context.Context) error {
		// Best-effort typing indicator while we call the agent.
		a.ShowTyping(ctx, channelKey(m.Chat.ID, 0))

		reply, err := a.askAgent(ctx, agentID, content)
		if err != nil {
			_, sendErr := a.sendMessage(m.Chat.ID, m.MessageID, "Ask failed: "+err.Error(), nil)
			if sendErr != nil {
				return sendErr
			}
			return err
		}

		sentID, err := a.sendMessage(m.Chat.ID, m.MessageID, reply, telegramKeyboardDetails(agentID))
		if err == nil && sentID > 0 {
			a.agentRootIdx.Store(agentRootKey(m.Chat.ID, sentID), agentRootIndexEntry{AgentID: agentID, LastSeen: a.clock.Now()})
		}
		return err
	})
}

func (a *Adapter) handleCallbackQuery(q *tgbotapi.CallbackQuery) {
	if q == nil || q.From == nil || q.Message == nil || q.Message.Chat == nil {
		return
	}
	if q.From.IsBot {
		return
	}

	a.mu.Lock()
	handler := a.intHandler
	a.mu.Unlock()
	if handler == nil {
		return
	}

	chatKey := channelKey(q.Message.Chat.ID, 0)

	user := chatadapter.UserRef{
		ID:       strconv.FormatInt(q.From.ID, 10),
		Username: displayName(q.From),
	}

	msgRef := chatadapter.MessageRef{
		ChannelID: chatKey,
		MessageID: strconv.Itoa(q.Message.MessageID),
	}

	respond := func(content string, _ []chatadapter.Embed) error {
		// Send a normal message (Telegram has no ephemeral). Best-effort ack the callback query.
		_ = a.answerCallbackQuery(q.ID, "")
		_, err := a.sendMessage(q.Message.Chat.ID, q.Message.MessageID, content, nil)
		return err
	}

	update := func(content string, _ []chatadapter.Embed, _ []chatadapter.Component) error {
		_ = a.answerCallbackQuery(q.ID, "")
		return a.editMessage(q.Message.Chat.ID, q.Message.MessageID, content, nil)
	}

	evt := chatadapter.NewInteractionEvent("button", q.Data, user, chatKey, "", msgRef, respond, update)

	a.dispatchWithLimit("telegram.interaction", q.Message.Chat.ID, func(ctx context.Context) error {
		return handler(ctx, evt)
	})
}

func (a *Adapter) dispatchWithLimit(op string, chatID int64, fn func(ctx context.Context) error) {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}

	select {
	case a.msgSem <- struct{}{}:
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			defer func() { <-a.msgSem }()

			handlerCtx, cancel := context.WithTimeout(parent, 5*time.Minute)
			defer cancel()

			if err := fn(handlerCtx); err != nil {
				observability.Emit(handlerCtx, observability.NewEvent(op+"_error").
					WithComponent("telegram").
					WithData("chat_id", chatID).
					Error(err, 0))
			}
		}()
	default:
		observability.Emit(parent, observability.NewEvent(op+"_dropped").
			WithComponent("telegram").
			WithData("chat_id", chatID).
			WithData("reason", "concurrency limit reached").
			Success(0))
	}
}

func (a *Adapter) isChatID(chatID int64) bool {
	for _, id := range a.cfg.ChatIDs {
		if id == chatID {
			return true
		}
	}
	return false
}

func displayName(u *tgbotapi.User) string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.UserName) != "" {
		return u.UserName
	}
	name := strings.TrimSpace(strings.TrimSpace(u.FirstName) + " " + strings.TrimSpace(u.LastName))
	if name != "" {
		return name
	}
	return strconv.FormatInt(u.ID, 10)
}

func cleanMention(content string, botUsername string) string {
	botUsername = strings.TrimSpace(botUsername)
	if botUsername == "" {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(strings.ReplaceAll(content, "@"+botUsername, ""))
}

func channelKey(chatID int64, threadID int) string {
	if threadID > 0 {
		return fmt.Sprintf("%d:%d", chatID, threadID)
	}
	return fmt.Sprintf("%d", chatID)
}

func agentRootKey(chatID int64, messageID int) string {
	return fmt.Sprintf("%d:%d", chatID, messageID)
}

func parseChannelKey(key string) (chatID int64, threadID int, ok bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, 0, false
	}
	parts := strings.SplitN(key, ":", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if len(parts) == 1 {
		return id, 0, true
	}
	tid, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return id, tid, true
}

func parseCommandOptions(cmd string, args string) map[string]any {
	// Best-effort parsing for MVP commands.
	opts := make(map[string]any)
	switch cmd {
	case "search", "memory":
		if strings.TrimSpace(args) != "" {
			opts["query"] = strings.TrimSpace(args)
		}
	case "logs":
		if strings.Contains(strings.ToLower(args), "error") {
			opts["errors_only"] = true
		}
	case "todo":
		action := "list"
		if strings.TrimSpace(args) != "" {
			fields := strings.Fields(args)
			if len(fields) > 0 {
				action = fields[0]
			}
		}
		opts["action"] = action
		switch action {
		case "add":
			title := strings.TrimSpace(strings.TrimPrefix(args, "add"))
			title = strings.TrimSpace(title)
			if title != "" {
				opts["title"] = title
			}
		case "complete":
			fields := strings.Fields(args)
			if len(fields) >= 2 {
				opts["id"] = fields[1]
			}
		}
	case "agent-spawn":
		fields := strings.Fields(args)
		if len(fields) > 0 {
			opts["role"] = fields[0]
			opts["prompt"] = strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
		}
	case "agent-list":
		// no args
	default:
		// pass raw args through for debugging
		if strings.TrimSpace(args) != "" {
			opts["args"] = strings.TrimSpace(args)
		}
	}
	return opts
}

func (a *Adapter) sendMessage(chatID int64, replyToMsgID int, content string, keyboard *tgbotapi.InlineKeyboardMarkup) (int, error) {
	a.mu.Lock()
	bot := a.bot
	a.mu.Unlock()
	if bot == nil {
		return 0, fmt.Errorf("telegram: not connected")
	}

	content = truncateForTelegram(content)

	msg := tgbotapi.NewMessage(chatID, content)
	msg.DisableWebPagePreview = true
	if replyToMsgID > 0 {
		msg.ReplyToMessageID = replyToMsgID
		msg.AllowSendingWithoutReply = true
	}
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}

	sent, err := bot.Send(msg)
	if err != nil {
		if d, ok := retryAfterDelay(err); ok {
			if !sleepWithContext(a.ctx, d) {
				return 0, fmt.Errorf("telegram: send message: %w", err)
			}
			sent, err = bot.Send(msg)
		}
		if err != nil {
			return 0, fmt.Errorf("telegram: send message: %w", err)
		}
	}
	return sent.MessageID, nil
}

func (a *Adapter) editMessage(chatID int64, messageID int, content string, keyboard *tgbotapi.InlineKeyboardMarkup) error {
	a.mu.Lock()
	bot := a.bot
	a.mu.Unlock()
	if bot == nil {
		return fmt.Errorf("telegram: not connected")
	}

	content = truncateForTelegram(content)

	edit := tgbotapi.NewEditMessageText(chatID, messageID, content)
	edit.DisableWebPagePreview = true
	if keyboard != nil {
		edit.ReplyMarkup = keyboard
	} else {
		// Remove keyboard by default on edits unless explicitly set.
		edit.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{}
	}

	if _, err := bot.Send(edit); err != nil {
		if d, ok := retryAfterDelay(err); ok {
			if !sleepWithContext(a.ctx, d) {
				return fmt.Errorf("telegram: edit message: %w", err)
			}
			_, err = bot.Send(edit)
		}
		if err != nil {
			return fmt.Errorf("telegram: edit message: %w", err)
		}
	}
	return nil
}

func retryAfterDelay(err error) (time.Duration, bool) {
	var tgErr *tgbotapi.Error
	if !errors.As(err, &tgErr) || tgErr == nil {
		return 0, false
	}
	if tgErr.Code != http.StatusTooManyRequests {
		return 0, false
	}
	if tgErr.RetryAfter <= 0 {
		return 0, false
	}
	return time.Duration(tgErr.RetryAfter) * time.Second, true
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (a *Adapter) answerCallbackQuery(callbackID string, text string) error {
	a.mu.Lock()
	bot := a.bot
	a.mu.Unlock()
	if bot == nil {
		return fmt.Errorf("telegram: not connected")
	}
	cfg := tgbotapi.NewCallback(callbackID, text)
	_, err := bot.Request(cfg)
	return err
}

// ShowTyping sends a typing indicator to the chat for best-effort UX, honoring context cancellation.
func (a *Adapter) ShowTyping(ctx context.Context, channelID string) {
	chatID, _, ok := parseChannelKey(channelID)
	if !ok {
		return
	}

	select {
	case <-ctx.Done():
		return
	default:
	}

	a.mu.Lock()
	bot := a.bot
	a.mu.Unlock()
	if bot == nil {
		return
	}

	_, _ = bot.Request(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
}

func (a *Adapter) agentIndexJanitor(ctx context.Context) {
	ticker := time.NewTicker(agentIndexJanitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.evictAgentIndexes(a.clock.Now())
		}
	}
}

func (a *Adapter) evictAgentIndexes(now time.Time) {
	cutoff := now.Add(-agentIndexTTL)

	a.agentThreads.Range(func(key any, value any) bool {
		st, ok := value.(agentThread)
		if !ok {
			a.agentThreads.Delete(key)
			return true
		}
		if st.LastSeen.IsZero() || st.LastSeen.Before(cutoff) {
			a.agentThreads.Delete(key)
		}
		return true
	})

	a.agentRootIdx.Range(func(key any, value any) bool {
		ent, ok := value.(agentRootIndexEntry)
		if !ok {
			a.agentRootIdx.Delete(key)
			return true
		}
		if strings.TrimSpace(ent.AgentID) == "" || ent.LastSeen.IsZero() || ent.LastSeen.Before(cutoff) {
			a.agentRootIdx.Delete(key)
		}
		return true
	})
}
