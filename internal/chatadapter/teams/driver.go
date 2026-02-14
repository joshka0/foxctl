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
	"github.com/jkatigb/agentctl/internal/domain/identity"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/convref"
	"github.com/jkatigb/agentctl/internal/web/sse"
)

// serviceURLEntry holds the normalized service URL together with the raw
// (platform-native) conversation ID so that outbound Bot Framework API calls
// can use the raw ID while the map key uses a tenant-scoped ConversationKey.
type serviceURLEntry struct {
	url       string // normalized service URL
	rawConvID string // platform-native conversation ID (not tenant-scoped)
}

// Adapter implements chatadapter.ChatAdapter for Microsoft Teams (Bot Framework webhook).
type Adapter struct {
	daemonURL string
	cfg       config.TeamsSettings

	httpClient *http.Client
	tokenMgr   *tokenManager
	botClient  *BotClient
	verifier   JWTVerifier
	clock      chatadapter.Clock

	cmdHandler chatadapter.CommandHandler
	intHandler chatadapter.InteractionHandler
	msgHandler chatadapter.MessageHandler

	sseHub    *sse.Hub
	sseClient *sse.Client

	// persistent conversation refs (optional)
	convRefStore convref.Store

	// convKey (tenant-scoped) -> serviceURLEntry
	serviceURLs sync.Map

	agentThreads sync.Map // agentID -> agentThread (root card + throttling state)
	agentRootIdx sync.Map // "convKey:activityID" -> agentRootIndexEntry; for reply-based agent chat

	msgSem chan struct{}

	mu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// defaultClock implements chatadapter.Clock using real time.
type defaultClock struct{}

func (defaultClock) Now() time.Time { return time.Now() }

// New creates a Teams adapter with the given config, daemon URL, and optional clock.
// If clk is nil, a real-time clock is used.
func New(cfg config.TeamsSettings, daemonURL string, clk chatadapter.Clock) *Adapter {
	limit := cfg.MaxConcurrentMessages
	if limit <= 0 {
		limit = 10
	}

	if clk == nil {
		clk = defaultClock{}
	}

	return &Adapter{
		daemonURL:  daemonURL,
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		clock:      clk,
		msgSem:     make(chan struct{}, limit),
	}
}

// Name returns the adapter identifier ("teams").
func (a *Adapter) Name() string { return "teams" }

// SetSSEHub configures the SSE hub for proactive messaging.
func (a *Adapter) SetSSEHub(hub *sse.Hub) { a.sseHub = hub }

// SetConvRefStore configures the persistent conversation reference store.
func (a *Adapter) SetConvRefStore(store convref.Store) { a.convRefStore = store }

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

	// Start SSE event listener if hub is configured.
	if a.sseHub != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.startEventListener(a.ctx)
		}()
	}

	// Periodically evict old agent thread/index entries.
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.agentIndexJanitor(a.ctx)
	}()

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
// channelID is the tenant-scoped conversation key (as stored in serviceURLs).
func (a *Adapter) ShowTyping(ctx context.Context, channelID string) {
	if a.botClient == nil {
		return
	}
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return
	}

	var entry serviceURLEntry
	if v, ok := a.serviceURLs.Load(channelID); ok {
		cachedEntry, cachedOK := v.(serviceURLEntry)
		if !cachedOK {
			return
		}
		entry = cachedEntry
	} else if a.convRefStore != nil {
		ref, err := a.convRefStore.Get(ctx, channelID)
		if err == nil && ref != nil {
			entry = serviceURLEntry{
				url:       ref.ServiceURL,
				rawConvID: ref.RawConversationID,
			}
			// Populate cache for subsequent lookups.
			a.serviceURLs.Store(channelID, entry)
		}
	}
	if strings.TrimSpace(entry.url) == "" {
		return
	}

	typingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// SendTyping requires the raw (platform-native) conversation ID for the
	// Bot Framework API URL, not the tenant-scoped key.
	_ = a.botClient.SendTyping(typingCtx, entry.url, entry.rawConvID)
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

		// Construct Principal early so the tenant-scoped conversation key
		// is available before any map stores or gating checks. This prevents
		// two tenants with colliding raw conversation IDs from overwriting
		// each other's service URL.
		tenantID := extractTenantID(activity, a.cfg.TenantID)
		if tenantID == "" {
			// Not fatal: single-tenant deployments may not have tenant IDs. This can cause
			// cross-tenant collisions if multiple tenants share conversation IDs.
			observability.Emit(r.Context(), observability.NewEvent("teams.missing_tenant_id").
				WithComponent("teams").
				WithData("conversation_id", convID).
				WithData("from_id", strings.TrimSpace(activity.From.ID)).
				WithData("warning", "missing tenant id; conversation key will be unscoped").
				Success(0))
		}
		principal := identity.Principal{
			TenantID: tenantID,
			UserID:   strings.TrimSpace(activity.From.ID),
			Username: strings.TrimSpace(activity.From.Name),
			Platform: "teams",
		}
		convKey := principal.ConversationKey(convID)

		a.serviceURLs.Store(convKey, serviceURLEntry{
			url:       serviceURL,
			rawConvID: convID,
		})
		if a.convRefStore != nil {
			_ = a.convRefStore.Upsert(r.Context(), convref.Ref{
				ConversationKey:   convKey,
				Platform:          "teams",
				TenantID:          tenantID,
				RawConversationID: convID,
				ServiceURL:        serviceURL,
				LastActivityID:    strings.TrimSpace(activity.ID),
				BotID:             strings.TrimSpace(activity.Recipient.ID),
			})
		}

		// Route invoke activities (Adaptive Card Action.Submit).
		if strings.TrimSpace(activity.Type) == "invoke" {
			w.WriteHeader(http.StatusOK)
			a.dispatchWithLimit(a.ctx, "teams.invoke", convKey, func(ctx context.Context) error {
				return a.handleInvoke(ctx, serviceURL, activity, convKey)
			})
			return
		}

		// Ignore non-message activities.
		if strings.TrimSpace(activity.Type) != "message" {
			w.WriteHeader(http.StatusOK)
			return
		}

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

		// The allowlist in config uses raw (platform-native) conversation IDs,
		// so we check against the raw convID here.
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
				WithData("conversation_id", convKey).
				Error(fmt.Errorf("botClient is nil; Connect() not called"), 0))
			return
		}

		// Check if this is a reply to a known agent root card.
		if replyToID := strings.TrimSpace(activity.ReplyToID); replyToID != "" {
			key := agentRootKey(convKey, replyToID)
			if entry, ok := a.agentRootIdx.Load(key); ok {
				if idx, ok := entry.(agentRootIndexEntry); ok && idx.AgentID != "" {
					a.dispatchAgentAsk(a.ctx, serviceURL, activity, convKey, idx.AgentID, text)
					return
				}
			}
		}

		a.dispatchWithLimit(a.ctx, "teams.message", convKey, func(ctx context.Context) error {
			// Text commands: "/cmd args"
			if cmd, args, ok := parseTextCommand(text); ok {
				return a.handleCommand(ctx, serviceURL, activity, principal, convKey, cmd, args)
			}
			return a.handleMessage(ctx, serviceURL, activity, principal, convKey, text)
		})
	}
}

func extractTenantID(activity Activity, cfgTenantID string) string {
	if tid := strings.TrimSpace(activity.Conversation.TenantID); tid != "" {
		return tid
	}

	// Teams sends channelData.tenant.id in all contexts (conversation.tenantId may be absent).
	if len(activity.ChannelData) > 0 {
		var cd struct {
			Tenant struct {
				ID string `json:"id"`
			} `json:"tenant"`
		}
		if err := json.Unmarshal(activity.ChannelData, &cd); err == nil {
			if tid := strings.TrimSpace(cd.Tenant.ID); tid != "" {
				return tid
			}
		}
	}

	return strings.TrimSpace(cfgTenantID)
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

func (a *Adapter) handleCommand(ctx context.Context, serviceURL string, activity Activity, principal identity.Principal, convKey string, cmd string, args string) error {
	a.mu.Lock()
	handler := a.cmdHandler
	a.mu.Unlock()
	if handler == nil {
		return nil
	}

	// Raw conversation ID for Bot Framework API calls.
	rawConvID := strings.TrimSpace(activity.Conversation.ID)
	if rawConvID == "" {
		return fmt.Errorf("teams: missing conversation id")
	}

	user := chatadapter.UserRef{
		ID:       principal.UserID,
		Username: principal.Username,
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
			_, err := a.botClient.ReplyToActivity(ctx, serviceURL, rawConvID, replyTo, out)
			return err
		}
		_, err := a.botClient.SendActivity(ctx, serviceURL, rawConvID, out)
		return err
	}

	opts := parseCommandOptions(cmd, args)
	// Use convKey (tenant-scoped) as the event's ChannelID for consistent
	// session bridging and serviceURLs lookups.
	evt := chatadapter.NewCommandEvent(cmd, opts, user, convKey, "", respond)
	evt.Principal = principal
	return handler(ctx, evt)
}

func (a *Adapter) handleMessage(ctx context.Context, serviceURL string, activity Activity, principal identity.Principal, convKey string, content string) error {
	a.mu.Lock()
	handler := a.msgHandler
	a.mu.Unlock()
	if handler == nil {
		return nil
	}

	// Raw conversation ID for Bot Framework API calls.
	rawConvID := strings.TrimSpace(activity.Conversation.ID)
	if rawConvID == "" {
		return fmt.Errorf("teams: missing conversation id")
	}

	user := chatadapter.UserRef{
		ID:       principal.UserID,
		Username: principal.Username,
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
			rr, err = a.botClient.ReplyToActivity(ctx, serviceURL, rawConvID, replyTo, out)
		} else {
			rr, err = a.botClient.SendActivity(ctx, serviceURL, rawConvID, out)
		}
		if err != nil {
			return chatadapter.MessageRef{}, err
		}
		// MessageRef uses convKey for consistent lookups.
		return chatadapter.MessageRef{ChannelID: convKey, MessageID: rr.ID}, nil
	}

	edit := func(ref chatadapter.MessageRef, content string, _ []chatadapter.Embed) error {
		return a.botClient.UpdateActivity(ctx, serviceURL, rawConvID, ref.MessageID, Activity{Type: "message", Text: content})
	}

	// Use convKey (tenant-scoped) as the event's ChannelID for consistent
	// session bridging and serviceURLs lookups.
	evt := chatadapter.NewMessageEvent(content, user, convKey, "", strings.TrimSpace(activity.ID), respond, edit)
	evt.Principal = principal
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
