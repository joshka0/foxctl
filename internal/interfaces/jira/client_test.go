package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTextToADF(t *testing.T) {
	doc := TextToADF("First line\n\nSecond line")
	content, ok := doc["content"].([]map[string]any)
	if !ok {
		t.Fatalf("content type = %T", doc["content"])
	}
	if len(content) != 3 {
		t.Fatalf("paragraph count = %d, want 3", len(content))
	}
	if got := content[0]["type"]; got != "paragraph" {
		t.Fatalf("first paragraph type = %v", got)
	}
}

func TestSearchIssuesUsesBasicAuthAndBody(t *testing.T) {
	t.Helper()
	var gotAuth string
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/rest/api/3/search/jql" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"key":"TEST-1"}],"total":1}`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:    srv.URL,
		Email:      "user@example.com",
		APIToken:   "secret",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	result, err := client.SearchIssues(context.Background(), "project = TEST", "next-token", 10, []string{"summary"}, nil)
	if err != nil {
		t.Fatalf("SearchIssues() error = %v", err)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user@example.com:secret"))
	if gotAuth != wantAuth {
		t.Fatalf("Authorization = %q, want %q", gotAuth, wantAuth)
	}
	if !strings.Contains(gotQuery, "jql=project+%3D+TEST") {
		t.Fatalf("query = %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "nextPageToken=next-token") {
		t.Fatalf("query = %q", gotQuery)
	}
	issues, ok := result["issues"].([]any)
	if !ok || len(issues) != 1 {
		t.Fatalf("issues = %#v", result["issues"])
	}
}

func TestMoveIssuesToBacklogUsesAgileEndpoint(t *testing.T) {
	t.Helper()
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/rest/agile/1.0/backlog/issue" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:    srv.URL,
		Email:      "user@example.com",
		APIToken:   "secret",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if err := client.MoveIssuesToBacklog(context.Background(), []string{"TEST-1", "TEST-2"}); err != nil {
		t.Fatalf("MoveIssuesToBacklog() error = %v", err)
	}
	issues, ok := gotBody["issues"].([]any)
	if !ok || len(issues) != 2 {
		t.Fatalf("issues = %#v", gotBody["issues"])
	}
}

func TestCreateIssueLinkUsesExpectedPayload(t *testing.T) {
	t.Helper()
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/rest/api/3/issueLink" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:    srv.URL,
		Email:      "user@example.com",
		APIToken:   "secret",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if err := client.CreateIssueLink(context.Background(), "TEST-1", "TEST-2", "Blocks", "hello", nil); err != nil {
		t.Fatalf("CreateIssueLink() error = %v", err)
	}
	if gotBody["type"].(map[string]any)["name"] != "Blocks" {
		t.Fatalf("type = %#v", gotBody["type"])
	}
	if gotBody["outwardIssue"].(map[string]any)["key"] != "TEST-1" {
		t.Fatalf("outwardIssue = %#v", gotBody["outwardIssue"])
	}
	if gotBody["inwardIssue"].(map[string]any)["key"] != "TEST-2" {
		t.Fatalf("inwardIssue = %#v", gotBody["inwardIssue"])
	}
}

func TestAddCommentConvertsPlainTextToADF(t *testing.T) {
	t.Helper()
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/TEST-1/comment" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"10000"}`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:    srv.URL,
		Email:      "user@example.com",
		APIToken:   "secret",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.AddComment(context.Background(), "TEST-1", "Hello Jira", nil, nil); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	body, ok := gotBody["body"].(map[string]any)
	if !ok {
		t.Fatalf("body type = %T", gotBody["body"])
	}
	if body["type"] != "doc" {
		t.Fatalf("body.type = %v", body["type"])
	}
}

func TestListBoardsReturnsStructuredError(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["bad request"],"errors":{"jql":"invalid"}}`))
	}))
	defer srv.Close()

	client, err := NewClient(Config{
		BaseURL:    srv.URL,
		Email:      "user@example.com",
		APIToken:   "secret",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ListBoards(context.Background(), 0, 0, "", "", "")
	if err == nil {
		t.Fatal("ListBoards() error = nil, want error")
	}
	jiraErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if jiraErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", jiraErr.StatusCode)
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("error = %q", err.Error())
	}
}
