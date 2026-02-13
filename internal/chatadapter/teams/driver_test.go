package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/chatadapter"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

type fakeVerifier struct {
	err error
}

func (f fakeVerifier) Verify(_ context.Context, _ string) error { return f.err }

func TestHTTPHandler_Unauthorized(t *testing.T) {
	t.Parallel()

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1}, "", nil)
	a.verifier = fakeVerifier{err: errors.New("nope")}

	body := `{
	  "type":"message",
	  "id":"m1",
	  "serviceUrl":"https://smba.trafficmanager.net/amer/",
	  "text":"hi",
	  "from":{"id":"u1","name":"user"},
	  "recipient":{"id":"bot","name":"bot"},
	  "conversation":{"id":"c1","isGroup":true},
	  "entities":[]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/teams/messages", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	a.HTTPHandler()(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestHTTPHandler_Gating_GroupNotAllowlisted_NoMention(t *testing.T) {
	t.Parallel()

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1}, "", nil)
	a.verifier = nopJWTVerifier{}

	called := make(chan struct{}, 1)
	a.OnMessage(func(_ context.Context, _ chatadapter.MessageEvent) error {
		called <- struct{}{}
		return nil
	})

	body := `{
	  "type":"message",
	  "id":"m1",
	  "serviceUrl":"https://smba.trafficmanager.net/amer/",
	  "text":"hi",
	  "from":{"id":"u1","name":"user"},
	  "recipient":{"id":"bot","name":"bot"},
	  "conversation":{"id":"c1","isGroup":true},
	  "entities":[]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/teams/messages", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	a.HTTPHandler()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	select {
	case <-called:
		t.Fatalf("unexpected handler call")
	case <-time.After(150 * time.Millisecond):
		// ok
	}
}

func TestHTTPHandler_Gating_AllowlistedConversation(t *testing.T) {
	t.Parallel()

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1, ChatConversationIDs: []string{"c1"}}, "", nil)
	a.verifier = nopJWTVerifier{}
	a.botClient = &BotClient{} // non-nil so dispatch guard passes (handlers don't send replies)

	got := make(chan string, 1)
	a.OnMessage(func(_ context.Context, evt chatadapter.MessageEvent) error {
		got <- evt.Content
		return nil
	})

	body := `{
	  "type":"message",
	  "id":"m1",
	  "serviceUrl":"https://smba.trafficmanager.net/amer/",
	  "text":"hi",
	  "from":{"id":"u1","name":"user"},
	  "recipient":{"id":"bot","name":"bot"},
	  "conversation":{"id":"c1","isGroup":true},
	  "entities":[]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/teams/messages", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	a.HTTPHandler()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	select {
	case content := <-got:
		if content != "hi" {
			t.Fatalf("expected content %q, got %q", "hi", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for handler")
	}
	a.wg.Wait()
}

func TestHTTPHandler_Gating_MentionInGroup(t *testing.T) {
	t.Parallel()

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1}, "", nil)
	a.verifier = nopJWTVerifier{}
	a.botClient = &BotClient{} // non-nil so dispatch guard passes

	got := make(chan string, 1)
	a.OnMessage(func(_ context.Context, evt chatadapter.MessageEvent) error {
		got <- evt.Content
		return nil
	})

	body := `{
	  "type":"message",
	  "id":"m1",
	  "serviceUrl":"https://smba.trafficmanager.net/amer/",
	  "text":"<at>bot</at> hello",
	  "from":{"id":"u1","name":"user"},
	  "recipient":{"id":"bot","name":"bot"},
	  "conversation":{"id":"c1","isGroup":true},
	  "entities":[{"type":"mention","text":"<at>bot</at>","mentioned":{"id":"bot","name":"bot"}}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/teams/messages", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	a.HTTPHandler()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	select {
	case content := <-got:
		if content != "hello" {
			t.Fatalf("expected content %q, got %q", "hello", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for handler")
	}
	a.wg.Wait()
}

func TestHTTPHandler_Gating_OneToOne(t *testing.T) {
	t.Parallel()

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1}, "", nil)
	a.verifier = nopJWTVerifier{}
	a.botClient = &BotClient{} // non-nil so dispatch guard passes

	got := make(chan string, 1)
	a.OnMessage(func(_ context.Context, evt chatadapter.MessageEvent) error {
		got <- evt.Content
		return nil
	})

	body := `{
	  "type":"message",
	  "id":"m1",
	  "serviceUrl":"https://smba.trafficmanager.net/amer/",
	  "text":"hi",
	  "from":{"id":"u1","name":"user"},
	  "recipient":{"id":"bot","name":"bot"},
	  "conversation":{"id":"c1","isGroup":false},
	  "entities":[]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/teams/messages", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	a.HTTPHandler()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	select {
	case <-got:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for handler")
	}
	a.wg.Wait()
}

func TestHTTPHandler_CommandRouting(t *testing.T) {
	t.Parallel()

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1, ChatConversationIDs: []string{"c1"}}, "", nil)
	a.verifier = nopJWTVerifier{}
	a.botClient = &BotClient{} // non-nil so dispatch guard passes

	called := make(chan chatadapter.CommandEvent, 1)
	unexpectedMsg := make(chan struct{}, 1)
	a.OnCommand(func(_ context.Context, evt chatadapter.CommandEvent) error {
		called <- evt
		return nil
	})
	a.OnMessage(func(_ context.Context, _ chatadapter.MessageEvent) error {
		unexpectedMsg <- struct{}{}
		return nil
	})

	body := `{
	  "type":"message",
	  "id":"m1",
	  "serviceUrl":"https://smba.trafficmanager.net/amer/",
	  "text":"/search foo",
	  "from":{"id":"u1","name":"user"},
	  "recipient":{"id":"bot","name":"bot"},
	  "conversation":{"id":"c1","isGroup":true},
	  "entities":[]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/teams/messages", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	a.HTTPHandler()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}

	select {
	case evt := <-called:
		if evt.Command != "search" {
			t.Fatalf("expected cmd %q, got %q", "search", evt.Command)
		}
		if q, _ := evt.Options["query"].(string); q != "foo" {
			t.Fatalf("expected query %q, got %v", "foo", evt.Options["query"])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for command handler")
	}
	a.wg.Wait()

	select {
	case <-unexpectedMsg:
		t.Fatalf("expected command handler, got message handler")
	default:
	}
}

func TestHTTPHandler_TenantIsolation_ServiceURLs(t *testing.T) {
	t.Parallel()

	a := New(config.TeamsSettings{MaxConcurrentMessages: 2}, "", nil)
	a.verifier = nopJWTVerifier{}
	a.botClient = &BotClient{} // non-nil so dispatch guard passes

	// Two tenants both use conversation ID "c1" but with different tenantIds.
	// The serviceURLs map must store them separately.
	tenantABody := `{
	  "type":"message",
	  "id":"m1",
	  "serviceUrl":"https://smba.trafficmanager.net/amer/",
	  "text":"hi",
	  "from":{"id":"u1","name":"user"},
	  "recipient":{"id":"bot","name":"bot"},
	  "conversation":{"id":"c1","tenantId":"tenant-a","isGroup":false},
	  "entities":[]
	}`
	tenantBBody := `{
	  "type":"message",
	  "id":"m2",
	  "serviceUrl":"https://smba.trafficmanager.net/emea/",
	  "text":"hello",
	  "from":{"id":"u2","name":"other"},
	  "recipient":{"id":"bot","name":"bot"},
	  "conversation":{"id":"c1","tenantId":"tenant-b","isGroup":false},
	  "entities":[]
	}`

	got := make(chan chatadapter.MessageEvent, 2)
	a.OnMessage(func(_ context.Context, evt chatadapter.MessageEvent) error {
		got <- evt
		return nil
	})

	// Send tenant A's message.
	reqA := httptest.NewRequest(http.MethodPost, "/api/teams/messages", bytes.NewBufferString(tenantABody))
	recA := httptest.NewRecorder()
	a.HTTPHandler()(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("tenant A: expected %d, got %d", http.StatusOK, recA.Code)
	}

	// Send tenant B's message.
	reqB := httptest.NewRequest(http.MethodPost, "/api/teams/messages", bytes.NewBufferString(tenantBBody))
	recB := httptest.NewRecorder()
	a.HTTPHandler()(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("tenant B: expected %d, got %d", http.StatusOK, recB.Code)
	}

	// Wait for both handlers.
	events := make([]chatadapter.MessageEvent, 0, 2)
	for i := 0; i < 2; i++ {
		select {
		case evt := <-got:
			events = append(events, evt)
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for handler %d", i+1)
		}
	}
	a.wg.Wait()

	// Both serviceURLs entries must exist (tenant-scoped keys differ).
	keyA := "teams:tenant-a:c1"
	keyB := "teams:tenant-b:c1"

	vA, okA := a.serviceURLs.Load(keyA)
	vB, okB := a.serviceURLs.Load(keyB)
	if !okA {
		t.Fatalf("expected serviceURLs entry for %q", keyA)
	}
	if !okB {
		t.Fatalf("expected serviceURLs entry for %q", keyB)
	}

	entryA, _ := vA.(serviceURLEntry)
	entryB, _ := vB.(serviceURLEntry)

	if entryA.rawConvID != "c1" {
		t.Fatalf("tenant A: expected rawConvID %q, got %q", "c1", entryA.rawConvID)
	}
	if entryB.rawConvID != "c1" {
		t.Fatalf("tenant B: expected rawConvID %q, got %q", "c1", entryB.rawConvID)
	}

	// Service URLs differ by region, confirming no overwrite.
	if entryA.url == entryB.url {
		t.Fatalf("expected different service URLs for different tenants, both got %q", entryA.url)
	}

	// Verify events got tenant-scoped ChannelIDs.
	channelIDs := map[string]bool{}
	for _, evt := range events {
		channelIDs[evt.ChannelID] = true
	}
	if !channelIDs[keyA] {
		t.Fatalf("expected event with ChannelID %q, got %v", keyA, channelIDs)
	}
	if !channelIDs[keyB] {
		t.Fatalf("expected event with ChannelID %q, got %v", keyB, channelIDs)
	}
}

func TestHTTPHandler_UntrustedServiceURL(t *testing.T) {
	t.Parallel()

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1}, "", nil)
	a.verifier = nopJWTVerifier{}

	body := `{
	  "type":"message",
	  "id":"m1",
	  "serviceUrl":"http://evil.example/",
	  "text":"hi",
	  "from":{"id":"u1","name":"user"},
	  "recipient":{"id":"bot","name":"bot"},
	  "conversation":{"id":"c1","isGroup":false},
	  "entities":[]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/teams/messages", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	a.HTTPHandler()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestExtractTenantID(t *testing.T) {
	t.Parallel()

	t.Run("primary_conversation_tenantId", func(t *testing.T) {
		t.Parallel()
		a := Activity{
			Conversation: ConversationAccount{TenantID: " tenant-a "},
			ChannelData:  json.RawMessage(`{"tenant":{"id":"tenant-b"}}`),
		}
		if got := extractTenantID(a, "cfg-tenant"); got != "tenant-a" {
			t.Fatalf("expected %q, got %q", "tenant-a", got)
		}
	})

	t.Run("fallback_channelData_tenant_id", func(t *testing.T) {
		t.Parallel()
		a := Activity{
			Conversation: ConversationAccount{TenantID: ""},
			ChannelData:  json.RawMessage(`{"tenant":{"id":" tenant-b "}}`),
		}
		if got := extractTenantID(a, "cfg-tenant"); got != "tenant-b" {
			t.Fatalf("expected %q, got %q", "tenant-b", got)
		}
	})

	t.Run("fallback_config_tenantID", func(t *testing.T) {
		t.Parallel()
		a := Activity{
			Conversation: ConversationAccount{TenantID: ""},
			ChannelData:  nil,
		}
		if got := extractTenantID(a, " cfg-tenant "); got != "cfg-tenant" {
			t.Fatalf("expected %q, got %q", "cfg-tenant", got)
		}
	})

	t.Run("all_empty", func(t *testing.T) {
		t.Parallel()
		a := Activity{
			Conversation: ConversationAccount{TenantID: ""},
			ChannelData:  nil,
		}
		if got := extractTenantID(a, ""); got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})
}
