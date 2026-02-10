package teams

import (
	"bytes"
	"context"
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

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1}, "")
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

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1}, "")
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

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1, ChatConversationIDs: []string{"c1"}}, "")
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

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1}, "")
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

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1}, "")
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

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1, ChatConversationIDs: []string{"c1"}}, "")
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

func TestHTTPHandler_UntrustedServiceURL(t *testing.T) {
	t.Parallel()

	a := New(config.TeamsSettings{MaxConcurrentMessages: 1}, "")
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
