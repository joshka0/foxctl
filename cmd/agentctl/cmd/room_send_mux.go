package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/runtime/terminal/tmuxbridge"
	"github.com/jkatigb/agentctl/internal/runtime/terminal/zellijbridge"
)

// roomSendMuxOpts configures an optional mux submit after a successful room send.
type roomSendMuxOpts struct {
	// NoMuxSubmit skips injecting keys into the current tmux/zellij pane.
	NoMuxSubmit bool
	// MuxSubmitMode is escape-enter or enter-only. Empty defaults to enter-only.
	MuxSubmitMode string
	// NoLiveRelay skips fan-out to other participants' mux panes after persist.
	NoLiveRelay bool
}

// roomSendMuxSubmitHook runs after a durable room message is stored. Tests may replace it.
var roomSendMuxSubmitHook = muxSubmitCurrentPaneAfterRoomSend

func muxSubmitCurrentPaneAfterRoomSend(ctx context.Context, bridgeMode string) (map[string]any, string) {
	if strings.TrimSpace(os.Getenv("TMUX_PANE")) != "" {
		client := newRoomTmuxClient()
		pane, err := client.CurrentPane(ctx)
		if err != nil {
			return nil, fmt.Sprintf("mux submit skipped: current tmux pane: %v", err)
		}
		target := strings.TrimSpace(pane.Label)
		if target == "" {
			target = pane.ID
		}
		res, err := client.Submit(ctx, target, tmuxbridge.SubmitOptions{Mode: bridgeMode})
		if err != nil {
			return nil, fmt.Sprintf("mux submit failed: %v", err)
		}
		return map[string]any{"backend": "tmux", "result": res}, ""
	}
	session := strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME"))
	paneID := normalizeZellijPaneID(os.Getenv("ZELLIJ_PANE_ID"))
	if session != "" && paneID != "" {
		client := zellijbridge.New()
		res, err := client.Submit(ctx, session, zellijbridge.SubmitOptions{Mode: bridgeMode, PaneID: paneID})
		if err != nil {
			return nil, fmt.Sprintf("mux submit failed: %v", err)
		}
		return map[string]any{"backend": "zellij", "result": res}, ""
	}
	return nil, ""
}
