package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"

	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/interfaces/web/sse"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// startEventListener subscribes to the SSE hub and routes agent events to Discord.
func (a *Adapter) startEventListener(ctx context.Context) {
	client := &sse.Client{
		ID:   "discord-adapter",
		Send: make(chan []byte, 64),
	}

	a.mu.Lock()
	a.sseClient = client
	a.mu.Unlock()

	a.sseHub.Register(client)
	observability.Emit(ctx, observability.NewEvent("discord.event_listener_started").WithComponent("discord").Success(0))

	defer func() {
		a.sseHub.Unregister(client)
		observability.Emit(ctx, observability.NewEvent("discord.event_listener_stopped").WithComponent("discord").Success(0))
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-client.Send:
			if !ok {
				return
			}
			a.processSSEMessage(msg)
		}
	}
}

// processSSEMessage parses an SSE message and routes activity events.
func (a *Adapter) processSSEMessage(raw []byte) {
	activity, ok, err := chatadapter.DecodeActivitySSEMessage(raw)
	if err != nil || !ok {
		return
	}

	a.routeActivityEvent(activity)
}

// routeActivityEvent dispatches an ActivityEvent to the appropriate Discord action.
func (a *Adapter) routeActivityEvent(event observability.ActivityEvent) {
	// Only handle agent.* events
	if len(event.Operation) < 6 || event.Operation[:6] != "agent." {
		return
	}

	// Post to activity channel (compact feed)
	if a.cfg.ActivityChannelID != "" {
		line := activityFeedLine(event)
		if _, err := a.SendMessage(a.cfg.ActivityChannelID, line, nil, nil); err != nil {
			observability.Emit(context.Background(), observability.NewEvent("discord.activity_feed_failed").WithComponent("discord").WithData("op", event.Operation).Error(err, 0))
		}
	}

	// Route to per-agent thread
	switch event.Operation {
	case "agent.spawn":
		a.handleAgentSpawn(event)
	case "agent.complete":
		a.handleAgentComplete(event)
	case "agent.kill":
		a.handleAgentKill(event)
	case "agent.iteration":
		a.handleAgentIteration(event)
	default:
		if event.Status == "error" {
			a.handleAgentError(event)
		}
	}
}

// handleAgentSpawn creates a thread for the agent and posts the spawn embed.
func (a *Adapter) handleAgentSpawn(event observability.ActivityEvent) {
	if a.cfg.AgentChannelID == "" {
		return
	}

	sessionID := event.SessionID
	if sessionID == "" {
		return
	}

	role := chatadapter.GetDataString(event.Data, "role")
	threadName := fmt.Sprintf("Agent %s", chatadapter.TruncateRunes(sessionID, 8))
	if role != "" {
		threadName = fmt.Sprintf("%s (%s)", threadName, role)
	}

	embed := agentSpawnEmbed(event)
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				stopButton(sessionID),
				detailsButton(sessionID),
			},
		},
	}

	threadID, err := a.CreateThread(a.cfg.AgentChannelID, threadName, embed, components)
	if err != nil {
		observability.Emit(context.Background(), observability.NewEvent("discord.thread_create_failed").WithComponent("discord").WithData("session_id", sessionID).Error(err, 0))
		return
	}

	a.threadMap.Store(sessionID, threadID)
	observability.Emit(context.Background(), observability.NewEvent("discord.thread_created").WithComponent("discord").WithData("session_id", sessionID).WithData("thread_id", threadID).Success(0))
}

// handleAgentComplete updates the thread embed and posts a summary.
func (a *Adapter) handleAgentComplete(event observability.ActivityEvent) {
	threadID := a.getThreadID(event.SessionID)
	if threadID == "" {
		return
	}

	embed := agentCompleteEmbed(event)
	if _, err := a.ReplyInThread(threadID, "", []*discordgo.MessageEmbed{embed}); err != nil {
		observability.Emit(context.Background(), observability.NewEvent("discord.thread_post_failed").WithComponent("discord").WithData("session_id", event.SessionID).WithData("op", "complete").Error(err, 0))
	}
}

// handleAgentKill updates the thread with a killed embed.
func (a *Adapter) handleAgentKill(event observability.ActivityEvent) {
	threadID := a.getThreadID(event.SessionID)
	if threadID == "" {
		return
	}

	embed := agentKilledEmbed(event)
	if _, err := a.ReplyInThread(threadID, "", []*discordgo.MessageEmbed{embed}); err != nil {
		observability.Emit(context.Background(), observability.NewEvent("discord.thread_post_failed").WithComponent("discord").WithData("session_id", event.SessionID).WithData("op", "kill").Error(err, 0))
	}
}

// handleAgentIteration posts progress in the agent thread.
func (a *Adapter) handleAgentIteration(event observability.ActivityEvent) {
	threadID := a.getThreadID(event.SessionID)
	if threadID == "" {
		return
	}

	embed := agentIterationEmbed(event)
	if _, err := a.ReplyInThread(threadID, "", []*discordgo.MessageEmbed{embed}); err != nil {
		observability.Emit(context.Background(), observability.NewEvent("discord.thread_post_failed").WithComponent("discord").WithData("session_id", event.SessionID).WithData("op", "iteration").Error(err, 0))
	}
}

// handleAgentError posts an error embed in the thread with a retry button.
func (a *Adapter) handleAgentError(event observability.ActivityEvent) {
	threadID := a.getThreadID(event.SessionID)
	if threadID == "" {
		return
	}

	embed := agentErrorEmbed(event)
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				retryButton(event.SessionID),
				detailsButton(event.SessionID),
			},
		},
	}

	if a.session != nil {
		_, err := a.session.ChannelMessageSendComplex(threadID, &discordgo.MessageSend{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		})
		if err != nil {
			observability.Emit(context.Background(), observability.NewEvent("discord.thread_post_failed").WithComponent("discord").WithData("session_id", event.SessionID).WithData("op", "error").Error(err, 0))
		}
	}
}

// getThreadID looks up the Discord thread ID for a given agent session ID.
// If no thread exists yet, returns empty string.
func (a *Adapter) getThreadID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	v, ok := a.threadMap.Load(sessionID)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
