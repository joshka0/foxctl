// Package client is a thin HTTP/JSON client for the ATCP daemon.
//
// The client targets the daemon's Unix socket by default but accepts any URL
// so tests can point it at an httptest.Server. Every method maps 1:1 to a
// broker intent so the "call the daemon from a separate process" path is
// exactly as expressive as the in-process broker surface.
//
// The wire DTOs are re-exported from internal/atcp/transport/httpjson so
// there is exactly one definition. This package intentionally does NOT add
// its own types on top of the wire shape — that keeps the contract single-
// sourced.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/transport/httpjson"
)

// Client talks to an ATCP daemon over HTTP.
type Client struct {
	base string
	http *http.Client
}

// ErrHTTP is returned when the daemon replies with a non-2xx status. Callers
// may inspect Status + Body for richer handling.
type ErrHTTP struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *ErrHTTP) Error() string {
	return fmt.Sprintf("atcp client: %s %s: %d %s", e.Method, e.Path, e.Status, e.Body)
}

// ForSocket builds a Client that dials the Unix socket at path. The synthetic
// http://atcp base works because the custom DialContext ignores addresses.
func ForSocket(path string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}
	return &Client{
		base: "http://atcp",
		http: &http.Client{Transport: tr, Timeout: 30 * time.Second},
	}
}

// ForURL builds a Client that talks plain HTTP to base. Intended for tests
// that drive an httptest.Server.
func ForURL(base string) *Client {
	return &Client{base: base, http: &http.Client{Timeout: 30 * time.Second}}
}

// Health checks liveness. Useful for CLI "wait for daemon".
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/v1/health", nil, nil)
}

// --- sessions ---

// CreateSession mirrors POST /v1/sessions.
func (c *Client) CreateSession(ctx context.Context, req httpjson.CreateSessionRequest) (httpjson.SessionResponse, error) {
	var out httpjson.SessionResponse
	err := c.do(ctx, http.MethodPost, "/v1/sessions", req, &out)
	return out, err
}

// DeleteSession mirrors DELETE /v1/sessions/{id}.
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/sessions/"+url.PathEscape(id), nil, nil)
}

// ListSessions mirrors GET /v1/sessions.
func (c *Client) ListSessions(ctx context.Context) ([]httpjson.SessionResponse, error) {
	var env struct {
		Sessions []httpjson.SessionResponse `json:"sessions"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/sessions", nil, &env)
	return env.Sessions, err
}

// --- rooms ---

// CreateRoom mirrors POST /v1/rooms.
func (c *Client) CreateRoom(ctx context.Context, req httpjson.CreateRoomRequest) (httpjson.RoomResponse, error) {
	var out httpjson.RoomResponse
	err := c.do(ctx, http.MethodPost, "/v1/rooms", req, &out)
	return out, err
}

// ListRooms mirrors GET /v1/rooms.
func (c *Client) ListRooms(ctx context.Context) ([]httpjson.RoomResponse, error) {
	var env struct {
		Rooms []httpjson.RoomResponse `json:"rooms"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/rooms", nil, &env)
	return env.Rooms, err
}

// JoinRoom mirrors POST /v1/rooms/{id}/join.
func (c *Client) JoinRoom(ctx context.Context, roomID string, req httpjson.JoinRoomRequest) (httpjson.MemberResponse, error) {
	var out httpjson.MemberResponse
	err := c.do(ctx, http.MethodPost, "/v1/rooms/"+url.PathEscape(roomID)+"/join", req, &out)
	return out, err
}

// LeaveRoom mirrors POST /v1/rooms/{id}/leave.
func (c *Client) LeaveRoom(ctx context.Context, roomID string, req httpjson.LeaveRoomRequest) (httpjson.MemberResponse, error) {
	var out httpjson.MemberResponse
	err := c.do(ctx, http.MethodPost, "/v1/rooms/"+url.PathEscape(roomID)+"/leave", req, &out)
	return out, err
}

// RoomMembers mirrors GET /v1/rooms/{id}/members.
func (c *Client) RoomMembers(ctx context.Context, roomID string) ([]httpjson.MemberResponse, error) {
	var env struct {
		Members []httpjson.MemberResponse `json:"members"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/rooms/"+url.PathEscape(roomID)+"/members", nil, &env)
	return env.Members, err
}

// --- messages ---

// SendMessage mirrors POST /v1/messages.
func (c *Client) SendMessage(ctx context.Context, req httpjson.SendMessageRequest) (httpjson.SendMessageResponse, error) {
	var out httpjson.SendMessageResponse
	err := c.do(ctx, http.MethodPost, "/v1/messages", req, &out)
	return out, err
}

// --- core ---

// do is the shared request path. body is JSON-encoded if non-nil; out is
// decoded from the response when non-nil and the server returned any body.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("atcp client: marshal %s %s: %w", method, path, err)
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return fmt.Errorf("atcp client: build %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("atcp client: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return &ErrHTTP{Method: method, Path: path, Status: resp.StatusCode, Body: string(raw)}
	}
	if out == nil {
		// Drain so keep-alive works and the server's goroutine isn't pinned.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("atcp client: decode %s %s: %w", method, path, err)
	}
	return nil
}
