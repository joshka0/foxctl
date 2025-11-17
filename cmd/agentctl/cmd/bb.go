package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

var bbCmd = &cobra.Command{
	Use:   "bb",
	Short: "Manage blackboard (Agent Profile v1)",
	Long:  "Manage shared blackboard for agent coordination",
}

var bbPostCmd = &cobra.Command{
	Use:   "post [topic]",
	Short: "Post item to blackboard topic",
	Long:  "Post a new item to a blackboard topic for agent coordination",
	Args:  cobra.ExactArgs(1),
	RunE:  runBBPost,
}

var bbSearchCmd = &cobra.Command{
	Use:   "search [topic]",
	Short: "Search blackboard topic",
	Long:  "Search for items in a blackboard topic",
	Args:  cobra.ExactArgs(1),
	RunE:  runBBSearch,
}

var bbClaimCmd = &cobra.Command{
	Use:   "claim [id]",
	Short: "Claim a blackboard item",
	Long:  "Claim a blackboard item with a lease for processing",
	Args:  cobra.ExactArgs(1),
	RunE:  runBBClaim,
}

var bbReleaseCmd = &cobra.Command{
	Use:   "release [id]",
	Short: "Release a blackboard item",
	Long:  "Release a claimed blackboard item",
	Args:  cobra.ExactArgs(1),
	RunE:  runBBRelease,
}

var bbListCmd = &cobra.Command{
	Use:   "list [topic]",
	Short: "List blackboard items by topic",
	Long:  "List all items in a blackboard topic",
	Args:  cobra.ExactArgs(1),
	RunE:  runBBList,
}

var bbWatchCmd = &cobra.Command{
	Use:   "watch [topic]",
	Short: "Watch blackboard topic for updates",
	Long:  "Stream blackboard updates in real-time (NDJSON)",
	Args:  cobra.ExactArgs(1),
	RunE:  runBBWatch,
}

// Flags for bb post
var (
	bbPostNS       string
	bbPostData     string
	bbPostDataFile string
	bbPostTTL      int
	bbPostCASRef   string
)

// Flags for bb search
var (
	bbSearchNS    string
	bbSearchLimit int
)

// Flags for bb claim
var (
	bbClaimNS       string
	bbClaimAgentID  string
	bbClaimDuration int
)

// Flags for bb release
var (
	bbReleaseNS string
)

// Flags for bb list
var (
	bbListNS    string
	bbListLimit int
)

// Flags for bb watch
var (
	bbWatchNS     string
	bbWatchFromTS int64
)

func init() {
	// Add bb commands to root
	rootCmd.AddCommand(bbCmd)
	bbCmd.AddCommand(bbPostCmd)
	bbCmd.AddCommand(bbSearchCmd)
	bbCmd.AddCommand(bbClaimCmd)
	bbCmd.AddCommand(bbReleaseCmd)
	bbCmd.AddCommand(bbListCmd)
	bbCmd.AddCommand(bbWatchCmd)

	// Post flags
	bbPostCmd.Flags().StringVar(&bbPostNS, "ns", "", "Namespace (required)")
	bbPostCmd.Flags().StringVar(&bbPostData, "data", "", "JSON data payload")
	bbPostCmd.Flags().StringVar(&bbPostDataFile, "data-file", "", "Path to JSON data file")
	bbPostCmd.Flags().IntVar(&bbPostTTL, "ttl", 86400, "TTL in seconds (default: 24h)")
	bbPostCmd.Flags().StringVar(&bbPostCASRef, "cas", "", "CAS reference (optional)")
	if err := bbPostCmd.MarkFlagRequired("ns"); err != nil {
		panic(err)
	}

	// Search flags
	bbSearchCmd.Flags().StringVar(&bbSearchNS, "ns", "", "Namespace (required)")
	bbSearchCmd.Flags().IntVar(&bbSearchLimit, "limit", 20, "Maximum number of items to return")
	if err := bbSearchCmd.MarkFlagRequired("ns"); err != nil {
		panic(err)
	}

	// Claim flags
	bbClaimCmd.Flags().StringVar(&bbClaimNS, "ns", "", "Namespace (required)")
	bbClaimCmd.Flags().StringVar(&bbClaimAgentID, "agent", "", "Agent ID claiming the item (required)")
	bbClaimCmd.Flags().IntVar(&bbClaimDuration, "lease", 300, "Lease duration in seconds (default: 5m)")
	if err := bbClaimCmd.MarkFlagRequired("ns"); err != nil {
		panic(err)
	}
	if err := bbClaimCmd.MarkFlagRequired("agent"); err != nil {
		panic(err)
	}

	// Release flags
	bbReleaseCmd.Flags().StringVar(&bbReleaseNS, "ns", "", "Namespace (required)")
	if err := bbReleaseCmd.MarkFlagRequired("ns"); err != nil {
		panic(err)
	}

	// List flags
	bbListCmd.Flags().StringVar(&bbListNS, "ns", "", "Namespace (required)")
	bbListCmd.Flags().IntVar(&bbListLimit, "limit", 20, "Maximum number of items to return")
	if err := bbListCmd.MarkFlagRequired("ns"); err != nil {
		panic(err)
	}

	// Watch flags
	bbWatchCmd.Flags().StringVar(&bbWatchNS, "ns", "", "Namespace (required)")
	bbWatchCmd.Flags().Int64Var(&bbWatchFromTS, "from", 0, "Start timestamp (unix seconds, 0=now)")
	if err := bbWatchCmd.MarkFlagRequired("ns"); err != nil {
		panic(err)
	}
}

func runBBPost(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	topic := args[0]

	// Load data payload
	var dataBytes []byte
	var err error
	if bbPostDataFile != "" {
		dataBytes, err = os.ReadFile(bbPostDataFile)
		if err != nil {
			return writeErrorEnvelope(cmd, "bb/post", string(protocol.ErrorCodeEARG), fmt.Sprintf("failed to read data file: %v", err))
		}
	} else if bbPostData != "" {
		dataBytes = []byte(bbPostData)
	} else {
		return writeErrorEnvelope(cmd, "bb/post", string(protocol.ErrorCodeEARG), "either --data or --data-file is required")
	}

	// Validate JSON
	var payload map[string]interface{}
	if err := json.Unmarshal(dataBytes, &payload); err != nil {
		return writeErrorEnvelope(cmd, "bb/post", string(protocol.ErrorCodeEARG), fmt.Sprintf("invalid JSON data: %v", err))
	}

	// Open blackboard store
	bbStore, err := blackboard.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "bb/post", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open blackboard store: %v", err))
	}
	defer errs.Ignore(bbStore.Close(), "close blackboard store")

	// Create blackboard record
	now := time.Now().UTC().Unix()
	record := agent.BlackboardRecord{
		ID:      ulid.Make().String(),
		NS:      bbPostNS,
		Topic:   topic,
		TS:      now,
		TTLSec:  bbPostTTL,
		Payload: json.RawMessage(dataBytes),
		CASRef:  bbPostCASRef,
	}

	// Post to blackboard
	if err := bbStore.Post(ctx, record); err != nil {
		return writeErrorEnvelope(cmd, "bb/post", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to post to blackboard: %v", err))
	}

	// Write success envelope
	data := map[string]interface{}{
		"item_id":    record.ID,
		"topic":      record.Topic,
		"created_at": time.Unix(record.TS, 0).UTC().Format(time.RFC3339),
	}

	env := envelope.OK("bb/post", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runBBSearch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	topic := args[0]

	// Open blackboard store
	bbStore, err := blackboard.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "bb/search", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open blackboard store: %v", err))
	}
	defer errs.Ignore(bbStore.Close(), "close blackboard store")

	// Search blackboard
	records, err := bbStore.Search(ctx, bbSearchNS, topic, bbSearchLimit)
	if err != nil {
		return writeErrorEnvelope(cmd, "bb/search", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to search blackboard: %v", err))
	}

	// Write success envelope
	env := envelope.OK("bb/search", map[string]interface{}{
		"results": records,
		"count":   len(records),
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runBBClaim(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	itemID := args[0]

	// Open blackboard store
	bbStore, err := blackboard.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "bb/claim", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open blackboard store: %v", err))
	}
	defer errs.Ignore(bbStore.Close(), "close blackboard store")

	// Claim item
	leaseDuration := time.Duration(bbClaimDuration) * time.Second
	record, err := bbStore.Claim(ctx, itemID, bbClaimAgentID, leaseDuration)
	if err != nil {
		if err == blackboard.ErrAlreadyLeased {
			return writeErrorEnvelope(cmd, "bb/claim", string(protocol.ErrorCodeEPolicy), "item already leased")
		}
		if err == blackboard.ErrNotFound {
			return writeErrorEnvelope(cmd, "bb/claim", string(protocol.ErrorCodeENotFound), fmt.Sprintf("item not found: %v", err))
		}
		return writeErrorEnvelope(cmd, "bb/claim", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to claim item: %v", err))
	}

	// Write success envelope
	if record.Lease == nil {
		return writeErrorEnvelope(cmd, "bb/claim", string(protocol.ErrorCodeERuntime), "claim succeeded but lease not set")
	}

	data := map[string]interface{}{
		"item_id":          record.ID,
		"lease_id":         record.Lease.Holder + "-" + fmt.Sprintf("%d", record.Lease.Until),
		"item":             json.RawMessage(record.Payload),
		"lease_expires_at": time.Unix(record.Lease.Until, 0).UTC().Format(time.RFC3339),
	}

	env := envelope.OK("bb/claim", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runBBRelease(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	itemID := args[0]

	// Open blackboard store
	bbStore, err := blackboard.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "bb/release", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open blackboard store: %v", err))
	}
	defer errs.Ignore(bbStore.Close(), "close blackboard store")

	// Release item
	if err := bbStore.Release(ctx, itemID); err != nil {
		if err == blackboard.ErrNotFound {
			return writeErrorEnvelope(cmd, "bb/release", string(protocol.ErrorCodeENotFound), fmt.Sprintf("item not found: %v", err))
		}
		return writeErrorEnvelope(cmd, "bb/release", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to release item: %v", err))
	}

	// Write success envelope
	data := map[string]interface{}{
		"item_id":  itemID,
		"released": true,
	}

	env := envelope.OK("bb/release", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runBBList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	topic := args[0]

	// Open blackboard store
	bbStore, err := blackboard.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "bb/list", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open blackboard store: %v", err))
	}
	defer errs.Ignore(bbStore.Close(), "close blackboard store")

	// List items by topic
	records, err := bbStore.ListByTopic(ctx, bbListNS, topic, bbListLimit)
	if err != nil {
		return writeErrorEnvelope(cmd, "bb/list", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to list blackboard items: %v", err))
	}

	// Write success envelope
	env := envelope.OK("bb/list", map[string]interface{}{
		"items": records,
		"count": len(records),
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
		m.Profiles = []string{"core/v1", "agent/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runBBWatch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	topic := args[0]

	// Open blackboard store
	bbStore, err := blackboard.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "bb/watch", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open blackboard store: %v", err))
	}
	defer errs.Ignore(bbStore.Close(), "close blackboard store")

	// Determine start timestamp
	fromTS := bbWatchFromTS
	if fromTS == 0 {
		fromTS = time.Now().Unix()
	}

	// Start watching
	recordCh, errCh := bbStore.Watch(ctx, bbWatchNS, topic, fromTS)

	// Create NDJSON writer
	writer := envelope.NewWriter(os.Stdout)

	// Track sequence number
	seq := 0

	// Stream records as progress envelopes
	for {
		select {
		case <-ctx.Done():
			// Write final envelope
			finalBool := true
			env := envelope.OK("bb/watch", map[string]interface{}{
				"status": "stopped",
			}, envelope.WithMetaMutator(func(m *envelope.Meta) {
				m.Source = "run"
				m.Profiles = []string{"core/v1", "agent/v1"}
				m.Seq = &seq
				m.Final = &finalBool
			}))
			if err := writer.Write(env); err != nil {
				return fmt.Errorf("write final envelope: %w", err)
			}
			return nil

		case err := <-errCh:
			if err != nil {
				return writeErrorEnvelope(cmd, "bb/watch", string(protocol.ErrorCodeERuntime), fmt.Sprintf("watch error: %v", err))
			}
			return nil

		case record, ok := <-recordCh:
			if !ok {
				// Channel closed
				finalBool := true
				env := envelope.OK("bb/watch", map[string]interface{}{
					"status": "completed",
				}, envelope.WithMetaMutator(func(m *envelope.Meta) {
					m.Source = "run"
					m.Profiles = []string{"core/v1", "agent/v1"}
					m.Seq = &seq
					m.Final = &finalBool
				}))
				if err := writer.Write(env); err != nil {
					return fmt.Errorf("write final envelope: %w", err)
				}
				return nil
			}

			// Write progress envelope with record
			seq++
			finalBool := false
			data := map[string]interface{}{
				"event":   "blackboard_update",
				"topic":   record.Topic,
				"item_id": record.ID,
				"item":    json.RawMessage(record.Payload),
				"ts":      time.Unix(record.TS, 0).UTC().Format(time.RFC3339),
			}

			env := envelope.Envelope{
				Version: envelope.Version,
				Status:  "progress",
				Command: "bb/watch",
				Data:    data,
				Meta: envelope.Meta{
					TS:       time.Now().UTC().Format(time.RFC3339),
					Source:   "run",
					Profiles: []string{"core/v1", "agent/v1"},
					Seq:      &seq,
					Final:    &finalBool,
				},
			}

			if err := writer.Write(env); err != nil {
				return fmt.Errorf("write progress envelope: %w", err)
			}
		}
	}
}
