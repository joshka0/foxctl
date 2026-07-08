// Package turbovec provides a Go client for the turbovecd Unix socket sidecar.
//
// The sidecar manages compressed vector indices using the TurboQuant algorithm
// (2-4 bit scalar quantization with SIMD search). This client communicates via
// a simple binary frame protocol over Unix domain sockets.
//
// Usage:
//
//	client, err := turbovec.Dial("/tmp/turbovecd.sock")
//	client.Create("my-index", 4096, 4)
//	client.Add("my-index", vectors)       // []float32
//	scores, ids := client.Search("my-index", query, 10)
package turbovec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// roundtripTimeout bounds every request/response exchange with the sidecar.
// Legitimate operations (load of a small index, a query) complete in well under
// a second; an unbounded read otherwise hangs the caller indefinitely if the
// daemon wedges or the framed stream desyncs under concurrent load. On timeout
// the connection is dropped and the caller falls back (e.g. to exact SQL search).
const roundtripTimeout = 15 * time.Second

// Protocol command bytes — must match turbovec-server/src/protocol.rs.
const (
	cmdPing           uint8 = 0x00
	cmdCreate         uint8 = 0x01
	cmdAdd            uint8 = 0x02
	cmdSearch         uint8 = 0x03
	cmdSearchFiltered uint8 = 0x04
	cmdRemove         uint8 = 0x05
	cmdSave           uint8 = 0x06
	cmdLoad           uint8 = 0x07
	cmdInfo           uint8 = 0x08
	cmdDrop           uint8 = 0x09
	cmdPrepare        uint8 = 0x0A
	cmdAddBatch       uint8 = 0x0B
)

// Status bytes.
const (
	statusOK       uint8 = 0x00
	statusErr      uint8 = 0x01
	statusNotFound uint8 = 0x02
)

// SearchHit is one result from a vector search.
type SearchHit struct {
	Score float32
	ID    uint64
}

// IndexInfo holds metadata about a loaded index.
type IndexInfo struct {
	Dim      uint32
	NVectors uint32
	BitWidth uint8
}

// Client communicates with a turbovecd sidecar over a Unix socket.
// It is safe for concurrent use — each call acquires a mutex so frames
// are not interleaved.
type Client struct {
	mu   sync.Mutex
	conn net.Conn
}

// Dial connects to the turbovecd sidecar at the given Unix socket path.
func Dial(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("turbovec: dial %s: %w", socketPath, err)
	}
	return &Client{conn: conn}, nil
}

// Close closes the connection to the sidecar.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dropConnLocked()
}

// Connected reports whether the client still holds a live connection. After a
// failed roundtrip the connection is dropped and callers should redial.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// dropConnLocked closes and clears the connection. Caller must hold c.mu.
func (c *Client) dropConnLocked() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// Ping checks liveness of the sidecar.
func (c *Client) Ping() error {
	_, err := c.roundtrip(cmdPing, nil)
	return err
}

// Create creates a new named index with the given dimension and bit width.
func (c *Client) Create(name string, dim uint32, bitWidth uint8) error {
	payload := encodeName(name)
	payload = binary.LittleEndian.AppendUint32(payload, dim)
	payload = append(payload, bitWidth)
	_, err := c.roundtrip(cmdCreate, payload)
	return err
}

// Add adds a single vector to the named index with the given external ID.
// The dim is inferred from the vector length.
func (c *Client) Add(name string, id uint64, vector []float32) (uint32, error) {
	dim := uint32(len(vector))
	payload := encodeName(name)
	payload = binary.LittleEndian.AppendUint32(payload, 1) // n = 1
	payload = binary.LittleEndian.AppendUint32(payload, dim)
	for _, v := range vector {
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(v))
	}
	payload = binary.LittleEndian.AppendUint64(payload, id)
	resp, err := c.roundtrip(cmdAdd, payload)
	if err != nil {
		return 0, err
	}
	if len(resp) < 5 {
		return 0, fmt.Errorf("turbovec: short ADD response (%d bytes)", len(resp))
	}
	return binary.LittleEndian.Uint32(resp[1:5]), nil
}

// AddBatch adds multiple vectors with explicit IDs.
func (c *Client) AddBatch(name string, vectors []float32, dim uint32, ids []uint64) (uint32, error) {
	n := uint32(len(vectors)) / dim
	if len(ids) != int(n) {
		return 0, fmt.Errorf("turbovec: %d vectors but %d ids", n, len(ids))
	}
	payload := encodeName(name)
	payload = binary.LittleEndian.AppendUint32(payload, n)
	payload = binary.LittleEndian.AppendUint32(payload, dim)
	for _, v := range vectors {
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(v))
	}
	for _, id := range ids {
		payload = binary.LittleEndian.AppendUint64(payload, id)
	}
	resp, err := c.roundtrip(cmdAddBatch, payload)
	if err != nil {
		return 0, err
	}
	if len(resp) < 5 {
		return 0, fmt.Errorf("turbovec: short ADD_BATCH response (%d bytes)", len(resp))
	}
	return binary.LittleEndian.Uint32(resp[1:5]), nil
}

// Search runs a top-k search against the named index.
func (c *Client) Search(name string, query []float32, k uint32) ([]SearchHit, error) {
	return c.search(cmdSearch, name, query, k, nil)
}

// SearchFiltered runs a top-k search restricted to the given allowlist of IDs.
func (c *Client) SearchFiltered(name string, query []float32, k uint32, allowlist []uint64) ([]SearchHit, error) {
	return c.search(cmdSearchFiltered, name, query, k, allowlist)
}

func (c *Client) search(cmd uint8, name string, query []float32, k uint32, allowlist []uint64) ([]SearchHit, error) {
	dim := uint32(len(query))
	nq := uint32(1)

	payload := encodeName(name)
	payload = binary.LittleEndian.AppendUint32(payload, k)
	payload = binary.LittleEndian.AppendUint32(payload, nq)
	payload = binary.LittleEndian.AppendUint32(payload, dim)
	for _, v := range query {
		payload = binary.LittleEndian.AppendUint32(payload, math.Float32bits(v))
	}

	if allowlist != nil {
		payload = binary.LittleEndian.AppendUint32(payload, uint32(len(allowlist)))
		for _, id := range allowlist {
			payload = binary.LittleEndian.AppendUint64(payload, id)
		}
	}

	resp, err := c.roundtrip(cmd, payload)
	if err != nil {
		return nil, err
	}

	if len(resp) < 9 {
		return nil, fmt.Errorf("turbovec: short SEARCH response (%d bytes)", len(resp))
	}

	respNQ := binary.LittleEndian.Uint32(resp[1:5])
	respK := binary.LittleEndian.Uint32(resp[5:9])
	_ = respNQ

	hits := make([]SearchHit, 0, respK)
	off := 9
	for i := uint32(0); i < respK; i++ {
		if off+12 > len(resp) {
			break
		}
		scoreBits := binary.LittleEndian.Uint32(resp[off : off+4])
		id := binary.LittleEndian.Uint64(resp[off+4 : off+12])
		off += 12
		// Skip zero entries (the server pads to k with id=0 when results < k).
		if id == 0 {
			continue
		}
		hits = append(hits, SearchHit{
			Score: math.Float32frombits(scoreBits),
			ID:    id,
		})
	}
	return hits, nil
}

// Remove removes a vector by external ID.
func (c *Client) Remove(name string, id uint64) (bool, error) {
	payload := encodeName(name)
	payload = binary.LittleEndian.AppendUint64(payload, id)
	resp, err := c.roundtrip(cmdRemove, payload)
	if err != nil {
		return false, err
	}
	if len(resp) < 2 {
		return false, fmt.Errorf("turbovec: short REMOVE response")
	}
	return resp[1] == 1, nil
}

// Save persists the named index to a file path.
func (c *Client) Save(name, path string) error {
	payload := encodeName(name)
	pathBytes := []byte(path)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(pathBytes)))
	payload = append(payload, pathBytes...)
	_, err := c.roundtrip(cmdSave, payload)
	return err
}

// Load loads an index from disk into a named slot.
func (c *Client) Load(name, path string) (*IndexInfo, error) {
	payload := encodeName(name)
	pathBytes := []byte(path)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(pathBytes)))
	payload = append(payload, pathBytes...)
	resp, err := c.roundtrip(cmdLoad, payload)
	if err != nil {
		return nil, err
	}
	if len(resp) < 9 {
		return nil, fmt.Errorf("turbovec: short LOAD response")
	}
	info := &IndexInfo{
		Dim:      binary.LittleEndian.Uint32(resp[1:5]),
		NVectors: binary.LittleEndian.Uint32(resp[5:9]),
		BitWidth: resp[9],
	}
	return info, nil
}

// Info returns metadata about a named index.
func (c *Client) Info(name string) (*IndexInfo, error) {
	payload := encodeName(name)
	resp, err := c.roundtrip(cmdInfo, payload)
	if err != nil {
		return nil, err
	}
	if len(resp) < 9 {
		return nil, fmt.Errorf("turbovec: short INFO response")
	}
	info := &IndexInfo{
		Dim:      binary.LittleEndian.Uint32(resp[1:5]),
		NVectors: binary.LittleEndian.Uint32(resp[5:9]),
		BitWidth: resp[9],
	}
	return info, nil
}

// Drop removes a named index from memory.
func (c *Client) Drop(name string) error {
	payload := encodeName(name)
	_, err := c.roundtrip(cmdDrop, payload)
	return err
}

// Prepare eagerly populates search caches for the named index.
func (c *Client) Prepare(name string) error {
	payload := encodeName(name)
	_, err := c.roundtrip(cmdPrepare, payload)
	return err
}

// roundtrip sends a command frame and reads the response.
func (c *Client) roundtrip(cmd uint8, payload []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, errors.New("turbovec: connection closed")
	}

	// Bound the exchange so a wedged daemon or a desynced stream cannot hang the
	// caller forever. Any failure leaves the framed stream in an unknown state,
	// so drop the connection; the next use redials.
	_ = c.conn.SetDeadline(time.Now().Add(roundtripTimeout))

	if err := writeFrame(c.conn, cmd, payload); err != nil {
		_ = c.dropConnLocked()
		return nil, fmt.Errorf("turbovec: write: %w", err)
	}

	respCmd, respPayload, err := readFrame(c.conn)
	if err != nil {
		_ = c.dropConnLocked()
		return nil, fmt.Errorf("turbovec: read: %w", err)
	}

	_ = c.conn.SetDeadline(time.Time{}) // clear deadline on success

	// Response uses same command byte.
	_ = respCmd

	if len(respPayload) == 0 {
		return nil, errors.New("turbovec: empty response")
	}

	status := respPayload[0]
	switch status {
	case statusOK:
		return respPayload, nil
	case statusNotFound:
		msg := "not found"
		if len(respPayload) > 1 {
			msg = string(respPayload[1:])
		}
		return nil, &NotFoundError{Message: msg}
	case statusErr:
		msg := "unknown error"
		if len(respPayload) > 1 {
			msg = string(respPayload[1:])
		}
		return nil, fmt.Errorf("turbovec: %s", msg)
	default:
		return nil, fmt.Errorf("turbovec: unknown status %d", status)
	}
}

// --- Frame protocol ---

// Frame layout: [cmd:u8] [payload_len:u32 BE] [payload:[]byte]

func writeFrame(w io.Writer, cmd uint8, payload []byte) error {
	header := [5]byte{}
	header[0] = cmd
	binary.BigEndian.PutUint32(header[1:5], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func readFrame(r io.Reader) (uint8, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	cmd := header[0]
	payloadLen := binary.BigEndian.Uint32(header[1:5])
	if payloadLen == 0 {
		return cmd, nil, nil
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return cmd, payload, nil
}

// --- Helpers ---

func encodeName(name string) []byte {
	b := []byte(name)
	var buf []byte
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(b)))
	buf = append(buf, b...)
	return buf
}

// NotFoundError indicates the named index was not found.
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("turbovec: not found: %s", e.Message)
}

// DefaultSocketPath returns the default Unix socket path for the sidecar.
func DefaultSocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".foxctl", "turbovecd.sock")
}

// IsAvailable returns true if the turbovec sidecar socket exists and is reachable.
func IsAvailable() bool {
	path := DefaultSocketPath()
	if _, err := os.Stat(path); err != nil {
		return false
	}
	c, err := Dial(path)
	if err != nil {
		return false
	}
	c.Close()
	return true
}
