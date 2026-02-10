// Package teams implements the ChatAdapter interface for Microsoft Teams via the Bot Framework.
package teams

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/chatadapter"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/sse"
)

// Adapter implements chatadapter.ChatAdapter for Microsoft Teams (Bot Framework webhook).
type Adapter struct {
	daemonURL string
	cfg       config.TeamsSettings

	httpClient *http.Client
	tokenMgr   *tokenManager
	botClient  *BotClient
	verifier   JWTVerifier

	cmdHandler chatadapter.CommandHandler
	intHandler chatadapter.InteractionHandler
	msgHandler chatadapter.MessageHandler

	sseHub *sse.Hub

	// conversationID -> normalized service URL
	serviceURLs sync.Map

	msgSem chan struct{}

	mu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a Teams adapter with the given config and daemon URL.
func New(cfg config.TeamsSettings, daemonURL string) *Adapter {
	limit := cfg.MaxConcurrentMessages
	if limit <= 0 {
		limit = 10
	}

	return &Adapter{
		daemonURL:  daemonURL,
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		msgSem:     make(chan struct{}, limit),
	}
}

func (a *Adapter) Name() string { return "teams" }

// SetSSEHub configures the SSE hub for future proactive messaging (Phase 3+).
func (a *Adapter) SetSSEHub(hub *sse.Hub) { a.sseHub = hub }

// OnCommand sets the handler for incoming text commands ("/cmd args").
func (a *Adapter) OnCommand(handler chatadapter.CommandHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cmdHandler = handler
}

// OnInteraction sets the handler for interactive components (not used in MVP).
func (a *Adapter) OnInteraction(handler chatadapter.InteractionHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.intHandler = handler
}

// OnMessage sets the handler for natural language messages (Phase 3).
func (a *Adapter) OnMessage(handler chatadapter.MessageHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.msgHandler = handler
}

// Connect validates config and initializes dependencies. There is no long-lived connection.
func (a *Adapter) Connect(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.ctx, a.cancel = context.WithCancel(ctx)

	if strings.TrimSpace(a.cfg.TenantID) == "" {
		return fmt.Errorf("teams: missing TenantID (TEAMS_TENANT_ID)")
	}
	if strings.TrimSpace(a.cfg.ClientID) == "" {
		return fmt.Errorf("teams: missing ClientID (TEAMS_CLIENT_ID)")
	}
	if strings.TrimSpace(a.cfg.ClientSecret) == "" {
		return fmt.Errorf("teams: missing ClientSecret (TEAMS_CLIENT_SECRET)")
	}

	a.tokenMgr = newTokenManager(a.cfg.ClientID, a.cfg.ClientSecret, a.httpClient)
	a.botClient = newBotClient(a.tokenMgr, a.httpClient)

	if a.cfg.SkipJWTVerify {
		a.verifier = nopJWTVerifier{}
		observability.Emit(ctx, observability.NewEvent("teams.jwt_verify_skipped").
			WithComponent("teams").
			WithData("warning", "TEAMS_SKIP_JWT_VERIFY is enabled (dev-only); inbound webhook auth is disabled").
			Success(0))
	} else {
		a.verifier = newJWTVerifier(a.cfg.ClientID, a.cfg.TenantID, a.httpClient)
	}

	observability.Emit(ctx, observability.NewEvent("teams.connected").
		WithComponent("teams").
		Success(0))
	return nil
}

// Disconnect cancels in-flight handlers and waits for background goroutines.
func (a *Adapter) Disconnect(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()

	a.serviceURLs.Range(func(key, _ any) bool {
		a.serviceURLs.Delete(key)
		return true
	})

	observability.Emit(ctx, observability.NewEvent("teams.disconnected").
		WithComponent("teams").
		Success(0))
	return nil
}

// RegisterCommands is a no-op for Teams (commands are parsed from message text).
func (a *Adapter) RegisterCommands(_ context.Context, _ []chatadapter.CommandDef) error { return nil }

// ShowTyping sends a typing indicator to a conversation (best-effort).
func (a *Adapter) ShowTyping(ctx context.Context, channelID string) {
	if a.botClient == nil {
		return
	}
	v, ok := a.serviceURLs.Load(channelID)
	if !ok {
		return
	}
	svcURL, ok := v.(string)
	if !ok || strings.TrimSpace(svcURL) == "" {
		return
	}

	typingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_ = a.botClient.SendTyping(typingCtx, svcURL, channelID)
}

// HTTPHandler returns the webhook handler for incoming Bot Framework activities.
func (a *Adapter) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		raw, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024))
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}

		var activity Activity
		if err := json.Unmarshal(raw, &activity); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		// Verify inbound auth before ack.
		if a.verifier != nil {
			if err := a.verifier.Verify(r.Context(), r.Header.Get("Authorization")); err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				observability.Emit(r.Context(), observability.NewEvent("teams.webhook_unauthorized").
					WithComponent("teams").
					Error(err, 0))
				return
			}
		}

		// MVP: ignore non-message activities.
		if strings.TrimSpace(activity.Type) != "message" {
			w.WriteHeader(http.StatusOK)
			return
		}

		convID := strings.TrimSpace(activity.Conversation.ID)
		if convID == "" {
			http.Error(w, "missing conversation id", http.StatusBadRequest)
			return
		}

		serviceURL, err := normalizeServiceURL(activity.ServiceURL)
		if err != nil {
			http.Error(w, "untrusted serviceUrl", http.StatusBadRequest)
			observability.Emit(r.Context(), observability.NewEvent("teams.untrusted_service_url").
				WithComponent("teams").
				Error(err, 0))
			return
		}
		a.serviceURLs.Store(convID, serviceURL)

		// Ignore self-messages.
		if strings.TrimSpace(activity.From.ID) != "" && strings.TrimSpace(activity.From.ID) == strings.TrimSpace(activity.Recipient.ID) {
			w.WriteHeader(http.StatusOK)
			return
		}

		text := strings.TrimSpace(activity.Text)
		if text == "" {
			w.WriteHeader(http.StatusOK)
			return
		}

		allowlisted := a.isChatConversation(convID)
		isOneToOne := !activity.Conversation.IsGroup
		mentioned := isBotMentioned(activity)

		if !allowlisted && !isOneToOne && !mentioned {
			// Enterprise default: do not respond in channels unless explicitly allowlisted or mentioned.
			w.WriteHeader(http.StatusOK)
			return
		}

		// Clean bot mention markup from the message text.
		text = stripBotMentions(activity, text)
		if strings.TrimSpace(text) == "" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Fast ack: Teams expects a quick 200 OK.
		w.WriteHeader(http.StatusOK)

		// Guard: adapter must be fully initialized via Connect() before dispatching.
		if a.botClient == nil {
			observability.Emit(r.Context(), observability.NewEvent("teams.webhook_uninitialized").
				WithComponent("teams").
				WithData("conversation_id", convID).
				Error(fmt.Errorf("botClient is nil; Connect() not called"), 0))
			return
		}

		a.dispatchWithLimit(a.ctx, "teams.message", convID, func(ctx context.Context) error {
			// Text commands: "/cmd args"
			if cmd, args, ok := parseTextCommand(text); ok {
				return a.handleCommand(ctx, serviceURL, activity, cmd, args)
			}
			return a.handleMessage(ctx, serviceURL, activity, text)
		})
	}
}

func (a *Adapter) dispatchWithLimit(ctx context.Context, op string, conversationID string, fn func(ctx context.Context) error) {
	parent := ctx
	if parent == nil {
		parent = a.ctx
	}
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
				observability.Emit(parent, observability.NewEvent(op+"_error").
					WithComponent("teams").
					WithData("conversation_id", conversationID).
					Error(err, 0))
			}
		}()
	default:
		observability.Emit(parent, observability.NewEvent(op+"_dropped").
			WithComponent("teams").
			WithData("conversation_id", conversationID).
			WithData("reason", "concurrency limit reached").
			Success(0))
	}
}

func (a *Adapter) handleCommand(ctx context.Context, serviceURL string, activity Activity, cmd string, args string) error {
	a.mu.Lock()
	handler := a.cmdHandler
	a.mu.Unlock()
	if handler == nil {
		return nil
	}

	convID := strings.TrimSpace(activity.Conversation.ID)
	if convID == "" {
		return fmt.Errorf("teams: missing conversation id")
	}

	user := chatadapter.UserRef{
		ID:       strings.TrimSpace(activity.From.ID),
		Username: strings.TrimSpace(activity.From.Name),
	}
	if user.Username == "" {
		user.Username = user.ID
	}

	respond := func(content string, _ []chatadapter.Embed) error {
		if strings.TrimSpace(content) == "" {
			return nil
		}
		out := Activity{Type: "message", Text: content}
		replyTo := strings.TrimSpace(activity.ID)
		if replyTo != "" {
			_, err := a.botClient.ReplyToActivity(ctx, serviceURL, convID, replyTo, out)
			return err
		}
		_, err := a.botClient.SendActivity(ctx, serviceURL, convID, out)
		return err
	}

	opts := parseCommandOptions(cmd, args)
	evt := chatadapter.NewCommandEvent(cmd, opts, user, convID, "", respond)
	return handler(ctx, evt)
}

func (a *Adapter) handleMessage(ctx context.Context, serviceURL string, activity Activity, content string) error {
	a.mu.Lock()
	handler := a.msgHandler
	a.mu.Unlock()
	if handler == nil {
		return nil
	}

	convID := strings.TrimSpace(activity.Conversation.ID)
	if convID == "" {
		return fmt.Errorf("teams: missing conversation id")
	}

	user := chatadapter.UserRef{
		ID:       strings.TrimSpace(activity.From.ID),
		Username: strings.TrimSpace(activity.From.Name),
	}
	if user.Username == "" {
		user.Username = user.ID
	}

	respond := func(content string, _ []chatadapter.Embed) (chatadapter.MessageRef, error) {
		out := Activity{Type: "message", Text: content}
		replyTo := strings.TrimSpace(activity.ID)
		var rr ResourceResponse
		var err error
		if replyTo != "" {
			rr, err = a.botClient.ReplyToActivity(ctx, serviceURL, convID, replyTo, out)
		} else {
			rr, err = a.botClient.SendActivity(ctx, serviceURL, convID, out)
		}
		if err != nil {
			return chatadapter.MessageRef{}, err
		}
		return chatadapter.MessageRef{ChannelID: convID, MessageID: rr.ID}, nil
	}

	edit := func(ref chatadapter.MessageRef, content string, _ []chatadapter.Embed) error {
		return a.botClient.UpdateActivity(ctx, serviceURL, convID, ref.MessageID, Activity{Type: "message", Text: content})
	}

	evt := chatadapter.NewMessageEvent(content, user, convID, "", strings.TrimSpace(activity.ID), respond, edit)
	return handler(ctx, evt)
}

func (a *Adapter) isChatConversation(conversationID string) bool {
	for _, id := range a.cfg.ChatConversationIDs {
		if strings.TrimSpace(id) == conversationID {
			return true
		}
	}
	return false
}

func isBotMentioned(a Activity) bool {
	botID := strings.TrimSpace(a.Recipient.ID)
	if botID == "" {
		return false
	}
	for _, e := range a.Entities {
		if strings.EqualFold(strings.TrimSpace(e.Type), "mention") && strings.TrimSpace(e.Mentioned.ID) == botID {
			return true
		}
	}
	return false
}

func stripBotMentions(a Activity, text string) string {
	botID := strings.TrimSpace(a.Recipient.ID)
	if botID == "" {
		return strings.TrimSpace(text)
	}

	out := text
	for _, e := range a.Entities {
		if !strings.EqualFold(strings.TrimSpace(e.Type), "mention") {
			continue
		}
		if strings.TrimSpace(e.Mentioned.ID) != botID {
			continue
		}
		if strings.TrimSpace(e.Text) != "" {
			out = strings.ReplaceAll(out, e.Text, "")
		}
	}

	// Best-effort: strip Teams <at> markup.
	out = strings.ReplaceAll(out, "<at>", "")
	out = strings.ReplaceAll(out, "</at>", "")

	return strings.TrimSpace(out)
}

func parseTextCommand(text string) (cmd string, args string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}

	fields := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(fields) == 0 {
		return "", "", false
	}
	cmd = strings.TrimSpace(fields[0])
	if cmd == "" {
		return "", "", false
	}
	// Mirror Telegram normalization.
	cmd = strings.ReplaceAll(cmd, "_", "-")

	if len(fields) > 1 {
		args = strings.TrimSpace(strings.TrimPrefix(text, "/"+fields[0]))
		args = strings.TrimSpace(args)
	}
	return cmd, args, true
}

func parseCommandOptions(cmd string, args string) map[string]any {
	// Best-effort parsing for MVP commands (mirrors Telegram).
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
		if strings.TrimSpace(args) != "" {
			opts["args"] = strings.TrimSpace(args)
		}
	}
	return opts
}
