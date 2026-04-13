package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/jkatigb/agentctl/internal/observability"
)

const presencePollInterval = 30 * time.Second

// startPresenceUpdater polls the daemon API and updates the bot's Discord presence.
// It runs until the context is cancelled.
func (a *Adapter) startPresenceUpdater(ctx context.Context) {
	if a.daemonURL == "" {
		observability.Emit(ctx, observability.NewEvent("discord.presence_disabled").WithComponent("discord").Success(0))
		return
	}

	ticker := a.newTicker(presencePollInterval)
	defer ticker.Stop()

	// Initial update
	a.updatePresence(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.updatePresence(ctx)
		}
	}
}

// updatePresence queries the daemon for active agents and sets the bot status.
func (a *Adapter) updatePresence(ctx context.Context) {
	count, err := a.fetchActiveAgentCount(ctx)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("discord.presence_fetch_failed").WithComponent("discord").Error(err, 0))
		return
	}

	var status string
	if count > 0 {
		status = fmt.Sprintf("Watching %d agent", count)
		if count != 1 {
			status += "s"
		}
	} else {
		status = "Ready"
	}

	if a.session == nil {
		return
	}

	err = a.session.UpdateStatusComplex(discordgo.UpdateStatusData{
		Activities: []*discordgo.Activity{
			{
				Name: status,
				Type: discordgo.ActivityTypeWatching,
			},
		},
		Status: "online",
	})
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("discord.presence_update_failed").WithComponent("discord").Error(err, 0))
	}
}

// fetchActiveAgentCount queries GET /api/agents and returns the count of running agents.
func (a *Adapter) fetchActiveAgentCount(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.daemonURL+"/api/agents", nil)
	if err != nil {
		return 0, err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var agents []json.RawMessage
	if err := json.Unmarshal(body, &agents); err != nil {
		// Try object wrapper: {"agents": [...]}
		var wrapper struct {
			Agents []json.RawMessage `json:"agents"`
		}
		if err2 := json.Unmarshal(body, &wrapper); err2 != nil {
			return 0, err
		}
		agents = wrapper.Agents
	}

	return len(agents), nil
}
