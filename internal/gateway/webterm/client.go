package webterm

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

// Client represents a single WebSocket client connected to a room's terminal.
type Client struct {
	ws     *websocket.Conn
	room   *Room
	hub    *Hub
	log    zerolog.Logger
	cancel context.CancelFunc

	// output is the channel for sending PTY output to the WebSocket.
	output chan []byte

	mu     sync.Mutex
	closed bool
}

// NewClient creates a new web terminal client.
func NewClient(ws *websocket.Conn, hub *Hub, log zerolog.Logger) *Client {
	return &Client{
		ws:     ws,
		hub:    hub,
		log:    log,
		output: make(chan []byte, OutputBufferSize),
	}
}

// Run starts the client's read and write pumps. It blocks until the client
// disconnects or the context is cancelled.
func (c *Client) Run(ctx context.Context, roomID string, cols, rows uint16) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	defer func() {
		cancel()
		c.close()
		c.hub.RemoveClient(c)
	}()

	// Add client to room (checks connection limit)
	if err := c.hub.AddClient(roomID, c); err != nil {
		return err
	}

	// Get or create the PTY for this room
	pty, err := c.hub.RoomPTY(ctx, roomID, cols, rows)
	if err != nil {
		return err
	}

	// Register for PTY output
	outputID := pty.SubscribeOutput(c.output)
	defer pty.UnsubscribeOutput(outputID)

	c.log.Debug().
		Str("room", roomID).
		Uint16("cols", cols).
		Uint16("rows", rows).
		Msg("Client connected to room terminal")

	// Run read and write pumps
	var readErr, writeErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		readErr = c.readPump(ctx, pty)
		cancel() // Signal write pump to stop
	}()

	go func() {
		defer wg.Done()
		writeErr = c.writePump(ctx)
		cancel() // Signal read pump to stop
	}()

	wg.Wait()

	// Log any non-normal errors
	if readErr != nil {
		c.log.Debug().Err(readErr).Msg("Read pump ended")
	}
	if writeErr != nil {
		c.log.Debug().Err(writeErr).Msg("Write pump ended")
	}

	return nil
}

// readPump reads messages from the WebSocket and forwards to the PTY.
func (c *Client) readPump(ctx context.Context, pty *PTYProcess) error {
	for {
		msgType, data, err := c.ws.Read(ctx)
		if err != nil {
			return err
		}

		switch msgType {
		case websocket.MessageText:
			// JSON control message — check for resize
			var cm ControlMessage
			if jsonErr := json.Unmarshal(data, &cm); jsonErr == nil {
				switch cm.Type {
				case "resize":
					var rm ResizeMessage
					if unmarshalErr := json.Unmarshal(data, &rm); unmarshalErr == nil {
						if resizeErr := pty.Resize(rm.Cols, rm.Rows); resizeErr != nil {
							c.log.Debug().Err(resizeErr).
								Uint16("cols", rm.Cols).
								Uint16("rows", rm.Rows).
								Msg("PTY resize failed")
						} else {
							c.log.Debug().
								Uint16("cols", rm.Cols).
								Uint16("rows", rm.Rows).
								Msg("PTY resized")
						}
					}
				}
				continue
			}
			// Not JSON control — treat as text input to PTY
			if writeErr := pty.WriteInput(data); writeErr != nil {
				return writeErr
			}

		case websocket.MessageBinary:
			// Raw binary input — write directly to PTY
			if writeErr := pty.WriteInput(data); writeErr != nil {
				return writeErr
			}
		}
	}
}

// writePump writes PTY output to the WebSocket, with ping keepalive.
func (c *Client) writePump(ctx context.Context) error {
	pingTicker := time.NewTicker(c.hub.config.PingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case data, ok := <-c.output:
			if !ok {
				return nil
			}
			writeCtx, writeCancel := context.WithTimeout(ctx, DefaultWriteTimeout)
			err := c.ws.Write(writeCtx, websocket.MessageBinary, data)
			writeCancel()
			if err != nil {
				return err
			}

		case <-pingTicker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, DefaultWriteTimeout)
			err := c.ws.Ping(pingCtx)
			pingCancel()
			if err != nil {
				return err
			}
		}
	}
}

// close closes the WebSocket connection.
func (c *Client) close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.closed = true

	// Close with normal closure status (nil-safe)
	if c.ws != nil {
		_ = c.ws.Close(websocket.StatusNormalClosure, "")
	}

	c.log.Debug().Msg("Client WebSocket closed")
}
