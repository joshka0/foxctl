package ci

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/quick"
)

func TestGithubPullsByHeadURLEncodesBranchQuery(t *testing.T) {
	tests := []struct {
		name   string
		owner  string
		repo   string
		branch string
	}{
		{name: "slash branch", owner: "acme", repo: "foxctl", branch: "feature/harden-ci"},
		{name: "query characters", owner: "acme", repo: "foxctl", branch: "fix?state=open&head=other:main"},
		{name: "fragment character", owner: "acme", repo: "foxctl", branch: "bug#123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := mustParseURL(t, githubPullsByHeadURL(tt.owner, tt.repo, tt.branch))
			if got := u.Query().Get("head"); got != tt.owner+":"+tt.branch {
				t.Fatalf("head query=%q want %q", got, tt.owner+":"+tt.branch)
			}
			if got := u.Query().Get("state"); got != "all" {
				t.Fatalf("state query=%q want all", got)
			}
			if strings.Contains(u.RawQuery, tt.branch) && strings.ContainsAny(tt.branch, " ?&#") {
				t.Fatalf("raw query contains unsafe unescaped branch %q in %q", tt.branch, u.RawQuery)
			}
		})
	}
}

func TestGithubPullsByHeadURLGeneratedBranchesRoundTrip(t *testing.T) {
	err := quick.Check(func(raw string) bool {
		branch := generatedBranch(raw)
		u, err := url.Parse(githubPullsByHeadURL("owner", "repo", branch))
		if err != nil {
			return false
		}
		if u.Host != "api.github.com" {
			return false
		}
		if got := u.Query().Get("head"); got != "owner:"+branch {
			return false
		}
		return !strings.ContainsAny(u.RawQuery, " #")
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeOwnerRepo(t *testing.T) {
	tests := []struct {
		name      string
		owner     string
		repo      string
		wantOwner string
		wantRepo  string
	}{
		{name: "separate values trim", owner: " acme ", repo: " foxctl ", wantOwner: "acme", wantRepo: "foxctl"},
		{name: "repo full name fills owner", owner: "", repo: "acme/foxctl", wantOwner: "acme", wantRepo: "foxctl"},
		{name: "explicit owner wins over repo full name owner", owner: "override", repo: "acme/foxctl", wantOwner: "override", wantRepo: "foxctl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo := NormalizeOwnerRepo(tt.owner, tt.repo)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Fatalf("NormalizeOwnerRepo(%q, %q) = (%q, %q), want (%q, %q)", tt.owner, tt.repo, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestGitHubGETSendsContractHeadersAndDecodesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization=%q want bearer token", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github.v3+json" {
			t.Fatalf("Accept=%q want GitHub v3 JSON", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":123}`))
	}))
	defer server.Close()

	var out struct {
		Number int `json:"number"`
	}
	if err := GitHubGET(server.Client(), "test-token", server.URL, &out); err != nil {
		t.Fatalf("GitHubGET: %v", err)
	}
	if out.Number != 123 {
		t.Fatalf("decoded number=%d want 123", out.Number)
	}
}

func TestGitHubGETReturnsStatusAndBodyOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit", http.StatusTooManyRequests)
	}))
	defer server.Close()

	var out map[string]any
	err := GitHubGET(server.Client(), "", server.URL, &out)
	if err == nil {
		t.Fatal("expected GitHubGET error")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("error=%q, want status and response body", err.Error())
	}
}

func generatedBranch(raw string) string {
	if raw == "" {
		return "main"
	}
	replacer := strings.NewReplacer(
		"\x00", "0",
		"\n", "-",
		"\r", "-",
	)
	branch := replacer.Replace(raw)
	if strings.TrimSpace(branch) == "" {
		return "main"
	}
	if len(branch) > 120 {
		branch = branch[:120]
	}
	return branch
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u
}
