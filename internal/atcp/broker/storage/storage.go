// Package storage defines the persistence boundary for the ATCP broker.
//
// The in-memory Room Manager stays authoritative for read paths (routing,
// fan-out, invariant enforcement). Storage is write-through: every mutation
// flows to the in-memory structure first (to catch invariant violations
// synchronously) and then to the Store. On broker startup, a Store is
// queried once to hydrate rooms + messages; sessions are intentionally NOT
// persisted because PTYs don't survive a process restart.
//
// Policy on restart:
//   - Rooms with ArchivedAt set are loaded as archived (rejects joins).
//   - Active members (LeftAt zero) are loaded as "stale active" so that a
//     call to LoadRooms can return them to the broker, which then stamps
//     them as left and writes that back through MarkAllMembersLeft. This
//     preserves audit history (you can see the restart boundary in the DB)
//     without carrying dangling session references into live state.
package storage

import (
	"context"
	"errors"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker/room"
	"github.com/joshka0/foxctl/internal/atcp/broker/router"
)

// ErrNotFound is returned by Store methods when the requested row is absent.
// Kept coarse on purpose — callers typically translate it to
// room.ErrRoomNotFound or similar.
var ErrNotFound = errors.New("atcp storage: not found")

// Store is the persistence surface bundled into a single interface so the
// broker takes one dependency. Implementations must be safe for concurrent
// use; callers do not serialise externally.
type Store interface {
	// SaveRoom inserts or fully updates a room row. The broker calls this on
	// CreateRoom (with ArchivedAt zero) and on ArchiveRoom (with ArchivedAt
	// stamped). Implementations should treat it as an upsert keyed on ID.
	SaveRoom(ctx context.Context, r room.Room) error

	// SaveMember inserts or updates a member row. The broker calls this on
	// JoinRoom (LeftAt zero) and on LeaveRoom (LeftAt stamped). Uniqueness
	// of the active (session_id) index and the active (room_id, agent_id)
	// pair is enforced in memory before this runs, so implementations only
	// need to ensure the row ends up in the right state.
	SaveMember(ctx context.Context, m room.Member) error

	// AppendMessage persists a delivery record. Kept append-only for audit.
	// The broker writes this after SendMessage returns so the row reflects
	// actual fan-out outcomes, not just the submission intent.
	AppendMessage(ctx context.Context, m MessageRecord) error

	// LoadRooms returns every room ever created. Used once on startup.
	LoadRooms(ctx context.Context) ([]room.Room, error)

	// LoadMembers returns every member row for a room, active + past.
	LoadMembers(ctx context.Context, roomID string) ([]room.Member, error)

	// Close releases any underlying resources. Idempotent.
	Close() error
}

// MessageRecord is the persisted form of a fan-out result. Kept independent
// of router.Result so the wire package and the storage package are not
// cross-coupled.
type MessageRecord struct {
	ID        string
	RoomID    string
	Source    string
	Text      string
	Delivery  string
	SentAt    time.Time
	Delivered int
	Failed    int
	Members   []MessageDeliveryRecord
}

// MessageDeliveryRecord captures one member's outcome for a message.
type MessageDeliveryRecord struct {
	AgentID   string
	SessionID string
	Delivered bool
	ErrText   string
}

// NewMessageRecordFromResult converts a router.Result + the originating
// router.Message into a persistable MessageRecord. Centralised here so
// every writer shapes the same record.
func NewMessageRecordFromResult(msg router.Message, res router.Result, sentAt time.Time) MessageRecord {
	rec := MessageRecord{
		ID:        res.MessageID,
		RoomID:    msg.RoomID,
		Source:    msg.Source,
		Text:      msg.Text,
		Delivery:  string(msg.Delivery),
		SentAt:    sentAt.UTC(),
		Delivered: res.Delivered,
		Failed:    res.Failed,
	}
	rec.Members = make([]MessageDeliveryRecord, 0, len(res.Members))
	for _, mr := range res.Members {
		dr := MessageDeliveryRecord{
			AgentID:   mr.AgentID,
			SessionID: mr.SessionID,
			Delivered: mr.Delivered,
		}
		if mr.Err != nil {
			dr.ErrText = mr.Err.Error()
		}
		rec.Members = append(rec.Members, dr)
	}
	return rec
}

// Noop is the zero-persistence Store. Broker defaults to it so code paths
// that don't care about durability don't pay complexity.
type Noop struct{}

// NewNoop returns a Store that does nothing. Kept as a function rather than
// a bare struct so callers read `storage.NewNoop()` uniformly with the
// sqlite constructor.
func NewNoop() Store { return Noop{} }

// SaveRoom implements Store.
func (Noop) SaveRoom(context.Context, room.Room) error { return nil }

// SaveMember implements Store.
func (Noop) SaveMember(context.Context, room.Member) error { return nil }

// AppendMessage implements Store.
func (Noop) AppendMessage(context.Context, MessageRecord) error { return nil }

// LoadRooms implements Store.
func (Noop) LoadRooms(context.Context) ([]room.Room, error) { return nil, nil }

// LoadMembers implements Store.
func (Noop) LoadMembers(context.Context, string) ([]room.Member, error) { return nil, nil }

// Close implements Store.
func (Noop) Close() error { return nil }
