package webterm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	writeMu sync.Mutex
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

		result, handleErr := handleClientMessage(msgType, data, pty)
		if handleErr != nil {
			return handleErr
		}
		if result.controlError != nil {
			if writeErr := c.writeControlError(ctx, *result.controlError); writeErr != nil {
				return writeErr
			}
			continue
		}
		if result.resized {
			c.log.Debug().
				Uint16("cols", result.cols).
				Uint16("rows", result.rows).
				Msg("PTY resized")
		}
	}
}

type terminalControlTarget interface {
	WriteInput([]byte) error
	Resize(cols, rows uint16) error
}

type clientMessageResult struct {
	controlError *ControlErrorMessage
	resized      bool
	cols         uint16
	rows         uint16
}

func handleClientMessage(msgType websocket.MessageType, data []byte, pty terminalControlTarget) (clientMessageResult, error) {
	switch msgType {
	case websocket.MessageBinary:
		return clientMessageResult{}, pty.WriteInput(data)
	case websocket.MessageText:
		resize, controlErr := parseTextControl(data)
		if controlErr != nil {
			return clientMessageResult{controlError: controlErr}, nil
		}
		if err := pty.Resize(resize.Cols, resize.Rows); err != nil {
			return clientMessageResult{}, err
		}
		return clientMessageResult{resized: true, cols: resize.Cols, rows: resize.Rows}, nil
	default:
		return clientMessageResult{}, nil
	}
}

type validatedResize struct {
	Cols uint16
	Rows uint16
}

func parseTextControl(data []byte) (validatedResize, *ControlErrorMessage) {
	var cm ControlMessage
	if err := decodeJSON(data, &cm); err != nil {
		return validatedResize{}, newControlError("EINVAL", "text frames must be valid JSON control messages")
	}

	switch cm.Type {
	case "resize":
		var rm ResizeMessage
		if err := decodeStrictJSON(data, &rm); err != nil {
			return validatedResize{}, newControlError("EINVAL", "resize control must contain type, cols, and rows only")
		}
		cols, rows, err := validateResizeDimensions(rm.Cols, rm.Rows)
		if err != nil {
			return validatedResize{}, newControlError("EINVAL", err.Error())
		}
		return validatedResize{Cols: cols, Rows: rows}, nil
	case "":
		return validatedResize{}, newControlError("EINVAL", "control message missing type")
	default:
		return validatedResize{}, newControlError("EUNKNOWN", fmt.Sprintf("unknown control message type: %s", cm.Type))
	}
}

func decodeStrictJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return decodeOneJSONValue(dec, v)
}

func decodeJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	return decodeOneJSONValue(dec, v)
}

func decodeOneJSONValue(dec *json.Decoder, v any) error {
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func validateResizeDimensions(cols, rows int) (uint16, uint16, error) {
	if cols <= 0 || rows <= 0 {
		return 0, 0, fmt.Errorf("resize dimensions must be positive")
	}
	if cols > MaxTerminalCols || rows > MaxTerminalRows {
		return 0, 0, fmt.Errorf("resize dimensions exceed maximum %dx%d", MaxTerminalCols, MaxTerminalRows)
	}
	return uint16(cols), uint16(rows), nil
}

func newControlError(code, message string) *ControlErrorMessage {
	return &ControlErrorMessage{
		Type:    "error",
		Code:    code,
		Message: message,
	}
}

func (c *Client) writeControlError(ctx context.Context, msg ControlErrorMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	writeCtx, writeCancel := context.WithTimeout(ctx, DefaultWriteTimeout)
	defer writeCancel()
	return c.writeWS(writeCtx, websocket.MessageText, data)
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
			err := c.writeWS(writeCtx, websocket.MessageBinary, data)
			writeCancel()
			if err != nil {
				return err
			}

		case <-pingTicker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, DefaultWriteTimeout)
			err := c.pingWS(pingCtx)
			pingCancel()
			if err != nil {
				return err
			}
		}
	}
}

func (c *Client) writeWS(ctx context.Context, msgType websocket.MessageType, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.Write(ctx, msgType, data)
}

func (c *Client) pingWS(ctx context.Context) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.Ping(ctx)
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
