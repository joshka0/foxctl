package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

type sessionSummary struct {
	ID           string   `json:"id"`
	Summary      string   `json:"summary,omitempty"`
	Accomplished []string `json:"accomplished,omitempty"`
	Decisions    []string `json:"decisions,omitempty"`
	Gotchas      []string `json:"gotchas,omitempty"`
	Status       string   `json:"status"`
	MessageCount int      `json:"message_count"`
	TotalTokens  int      `json:"total_tokens"`
	AgentID      string   `json:"agent_id"`
	AgentType    string   `json:"agent_type,omitempty"`
}

type sessionDetailEnvelope struct {
	Session sessionSummary `json:"session"`
}

func (a *Adapter) fetchSessionSummary(ctx context.Context, sessionID string) (sessionSummary, error) {
	if strings.TrimSpace(a.daemonURL) == "" {
		return sessionSummary{}, fmt.Errorf("daemon URL not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sessionSummary{}, fmt.Errorf("missing session id")
	}

	url := fmt.Sprintf("%s/api/sessions/%s", strings.TrimRight(a.daemonURL, "/"), sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return sessionSummary{}, fmt.Errorf("create request: %w", err)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return sessionSummary{}, fmt.Errorf("fetch session: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return sessionSummary{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return sessionSummary{}, fmt.Errorf("fetch session failed (HTTP %d): %s", resp.StatusCode, truncateForTelegram(string(raw)))
	}

	var env sessionDetailEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return sessionSummary{}, fmt.Errorf("parse response: %w", err)
	}
	return env.Session, nil
}

func formatSessionSummaryText(agentID string, sess sessionSummary) string {
	agentShort := chatadapter.TruncateRunes(agentID, 8)
	sessShort := chatadapter.TruncateRunes(sess.ID, 8)

	var b strings.Builder
	b.WriteString("Result ")
	if agentShort != "" {
		b.WriteString("agent ")
		b.WriteString(agentShort)
		b.WriteString(" ")
	}
	if sessShort != "" {
		b.WriteString("session ")
		b.WriteString(sessShort)
	}
	if strings.TrimSpace(sess.Status) != "" {
		b.WriteString(" (")
		b.WriteString(strings.TrimSpace(sess.Status))
		b.WriteString(")")
	}
	b.WriteString("\n")

	if strings.TrimSpace(sess.Summary) != "" {
		b.WriteString("\nSummary:\n")
		b.WriteString(strings.TrimSpace(sess.Summary))
		b.WriteString("\n")
	}

	writeList := func(title string, items []string, max int) {
		if len(items) == 0 {
			return
		}
		b.WriteString("\n")
		b.WriteString(title)
		b.WriteString(":\n")
		n := len(items)
		if max > 0 && n > max {
			n = max
		}
		for i := 0; i < n; i++ {
			item := strings.TrimSpace(items[i])
			if item == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteString("\n")
		}
		if max > 0 && len(items) > max {
			b.WriteString(fmt.Sprintf("... (%d more)\n", len(items)-max))
		}
	}

	writeList("Accomplished", sess.Accomplished, 12)
	writeList("Decisions", sess.Decisions, 8)
	writeList("Gotchas", sess.Gotchas, 8)

	// Keep full IDs available for copy/paste.
	if strings.TrimSpace(agentID) != "" || strings.TrimSpace(sess.ID) != "" {
		b.WriteString("\n")
		if strings.TrimSpace(agentID) != "" {
			b.WriteString("agent_id: ")
			b.WriteString(strings.TrimSpace(agentID))
			b.WriteString("\n")
		}
		if strings.TrimSpace(sess.ID) != "" {
			b.WriteString("session_id: ")
			b.WriteString(strings.TrimSpace(sess.ID))
			b.WriteString("\n")
		}
	}

	return truncateForTelegram(strings.TrimSpace(b.String()))
}

func (a *Adapter) postAgentSummary(parent context.Context, agentID string, state agentThread) {
	if a.cfg.AgentChatID == 0 || state.RootMessageID == 0 {
		return
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return
	}
	sessionID := strings.TrimSpace(state.SessionID)
	if sessionID == "" {
		return
	}

	if parent == nil {
		parent = context.Background()
	}

	// Avoid blocking the event listener on network I/O.
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()

		ctx, cancel := context.WithTimeout(parent, 20*time.Second)
		defer cancel()

		sess, err := a.fetchSessionSummary(ctx, sessionID)
		if err != nil {
			observability.Emit(context.Background(),
				observability.NewEvent("telegram.session_summary_failed").
					WithComponent("telegram").
					WithData("agent_id", agentID).
					WithData("session_id", sessionID).
					Error(err, 0))
			return
		}

		content := formatSessionSummaryText(agentID, sess)
		msgID, err := a.sendMessage(a.cfg.AgentChatID, state.RootMessageID, content, telegramKeyboardDetails(agentID))
		if err == nil && msgID > 0 {
			a.agentRootIdx.Store(agentRootKey(a.cfg.AgentChatID, msgID), agentRootIndexEntry{AgentID: agentID, LastSeen: time.Now()})
		}
	}()
}
