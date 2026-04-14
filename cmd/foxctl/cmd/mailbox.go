package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/storage/mailbox"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

var mailboxCmd = &cobra.Command{
	Use:   "mailbox",
	Short: "Manage agent mailbox (Agent Profile v1)",
	Long:  "Manage inter-agent messaging via mailbox",
}

var mailboxSendCmd = &cobra.Command{
	Use:   "send [to-namespace]",
	Short: "Send message to agent mailbox",
	Long:  "Send a message to an agent's mailbox",
	Args:  cobra.ExactArgs(1),
	RunE:  runMailboxSend,
}

var mailboxPollCmd = &cobra.Command{
	Use:   "poll [agent-namespace]",
	Short: "Poll messages from mailbox",
	Long:  "Poll messages from an agent's mailbox (blocking)",
	Args:  cobra.ExactArgs(1),
	RunE:  runMailboxPoll,
}

var mailboxAckCmd = &cobra.Command{
	Use:   "ack [message-id]",
	Short: "Acknowledge message",
	Long:  "Acknowledge and delete a message from the mailbox",
	Args:  cobra.ExactArgs(1),
	RunE:  runMailboxAck,
}

var mailboxListCmd = &cobra.Command{
	Use:   "list [agent-namespace]",
	Short: "List mailbox messages",
	Long:  "List all messages in an agent's mailbox",
	Args:  cobra.ExactArgs(1),
	RunE:  runMailboxList,
}

// Flags for mailbox send
var (
	mailboxSendFrom        string
	mailboxSendType        string
	mailboxSendPayload     string
	mailboxSendPayloadFile string
	mailboxSendTTL         int
	mailboxSendCorrelation string
)

// Flags for mailbox poll
var (
	mailboxPollTimeout int
	mailboxPollMax     int
)

// Flags for mailbox list
var (
	mailboxListLimit int
)

func init() {
	// Add mailbox commands to root
	rootCmd.AddCommand(mailboxCmd)
	mailboxCmd.AddCommand(mailboxSendCmd)
	mailboxCmd.AddCommand(mailboxPollCmd)
	mailboxCmd.AddCommand(mailboxAckCmd)
	mailboxCmd.AddCommand(mailboxListCmd)

	// Send flags
	mailboxSendCmd.Flags().StringVar(&mailboxSendFrom, "from", "", "From namespace (required)")
	mailboxSendCmd.Flags().StringVar(&mailboxSendType, "type", "agent.cmd", "Message type (agent.ask|agent.reply|agent.cmd|agent.event)")
	mailboxSendCmd.Flags().StringVar(&mailboxSendPayload, "payload", "", "JSON payload")
	mailboxSendCmd.Flags().StringVar(&mailboxSendPayloadFile, "payload-file", "", "Path to JSON payload file")
	mailboxSendCmd.Flags().IntVar(&mailboxSendTTL, "ttl", 300000, "TTL in milliseconds (default: 5m)")
	mailboxSendCmd.Flags().StringVar(&mailboxSendCorrelation, "correlation", "", "Correlation ID (for ask/reply)")
	if err := mailboxSendCmd.MarkFlagRequired("from"); err != nil {
		// This should never happen unless there's a programmer error (flag doesn't exist)
		panic(fmt.Sprintf("failed to mark 'from' flag as required: %v", err))
	}

	// Poll flags
	mailboxPollCmd.Flags().IntVar(&mailboxPollTimeout, "timeout", 30, "Timeout in seconds")
	mailboxPollCmd.Flags().IntVar(&mailboxPollMax, "max", 10, "Maximum messages to retrieve")

	// List flags
	mailboxListCmd.Flags().IntVar(&mailboxListLimit, "limit", 20, "Maximum messages to list")
}

func parseMailboxMessageType(s string) (agent.MessageType, error) {
	mt := agent.MessageType(s)
	switch mt {
	case agent.MessageTypeAsk, agent.MessageTypeReply, agent.MessageTypeCmd, agent.MessageTypeEvent:
		return mt, nil
	default:
		return "", fmt.Errorf("invalid message type: %s", s)
	}
}

func runMailboxSend(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	toNS := args[0]

	if mailboxSendPayloadFile == "" && mailboxSendPayload == "" {
		return writeErrorEnvelope(cmd, "mailbox/send", string(protocol.ErrorCodeEARG), "either --payload or --payload-file is required")
	}
	payloadBytes, err := readPayload(cmd, mailboxSendPayloadFile, mailboxSendPayload)
	if err != nil {
		return writeErrorEnvelope(cmd, "mailbox/send", string(protocol.ErrorCodeEARG), fmt.Sprintf("failed to read payload: %v", err))
	}
	if err := requireValidJSON(payloadBytes); err != nil {
		return writeErrorEnvelope(cmd, "mailbox/send", string(protocol.ErrorCodeEARG), fmt.Sprintf("invalid JSON payload: %v", err))
	}

	msgType, err := parseMailboxMessageType(mailboxSendType)
	if err != nil {
		return writeErrorEnvelope(cmd, "mailbox/send", string(protocol.ErrorCodeEARG), err.Error())
	}

	// Open mailbox store
	mbStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "mailbox/send", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer func() { errs.Ignore(mbStore.Close(), "close mailbox store") }()

	// Create message
	now := time.Now().Unix()
	headers := make(map[string]string)
	if mailboxSendCorrelation != "" {
		headers["correlation"] = mailboxSendCorrelation
	}

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    mailboxSendFrom,
		ToNS:      toNS,
		Type:      msgType,
		TTLMS:     int64(mailboxSendTTL),
		Headers:   headers,
		Payload:   json.RawMessage(payloadBytes),
		VisibleAt: now,
		Attempt:   0,
		Timestamp: now,
	}

	// Send message
	if err := mbStore.Send(ctx, msg); err != nil {
		return writeErrorEnvelope(cmd, "mailbox/send", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to send message: %v", err))
	}

	// Write success envelope
	data := map[string]any{
		"message_id": msg.ID,
		"to":         msg.ToNS,
		"from":       msg.FromNS,
		"type":       msg.Type,
		"sent_at":    time.Unix(msg.Timestamp, 0).UTC().Format(time.RFC3339),
	}

	return writeOK(cmd, "mailbox/send", data, "run", profilesCoreAgent, func(m *envelope.Meta) {
		if mailboxSendCorrelation != "" {
			m.CorrelID = mailboxSendCorrelation
		}
	})
}

func runMailboxPoll(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentNS := args[0]

	// Open mailbox store
	mbStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "mailbox/poll", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer func() { errs.Ignore(mbStore.Close(), "close mailbox store") }()

	// Poll messages
	timeout := time.Duration(mailboxPollTimeout) * time.Second
	messages, err := mbStore.Poll(ctx, agentNS, timeout, mailboxPollMax)
	if err != nil {
		return writeErrorEnvelope(cmd, "mailbox/poll", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to poll messages: %v", err))
	}

	// Write success envelope
	return writeOK(cmd, "mailbox/poll", map[string]any{
		"messages": messages,
		"count":    len(messages),
	}, "run", profilesCoreAgent)
}

func runMailboxAck(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	messageID := args[0]

	// Open mailbox store
	mbStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "mailbox/ack", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer func() { errs.Ignore(mbStore.Close(), "close mailbox store") }()

	// Acknowledge message
	if err := mbStore.Ack(ctx, messageID); err != nil {
		if err == mailbox.ErrNotFound {
			return writeErrorEnvelope(cmd, "mailbox/ack", string(protocol.ErrorCodeENotFound), fmt.Sprintf("message not found: %v", err))
		}
		return writeErrorEnvelope(cmd, "mailbox/ack", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to ack message: %v", err))
	}

	// Write success envelope
	data := map[string]any{
		"message_id":   messageID,
		"acknowledged": true,
	}

	return writeOK(cmd, "mailbox/ack", data, "run", profilesCoreAgent)
}

func runMailboxList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentNS := args[0]

	// Open mailbox store
	mbStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "mailbox/list", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer func() { errs.Ignore(mbStore.Close(), "close mailbox store") }()

	// List messages
	messages, err := mbStore.List(ctx, agentNS, mailboxListLimit)
	if err != nil {
		return writeErrorEnvelope(cmd, "mailbox/list", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to list messages: %v", err))
	}

	// Write success envelope
	return writeOK(cmd, "mailbox/list", map[string]any{
		"messages": messages,
		"count":    len(messages),
	}, "run", profilesCoreAgent)
}
