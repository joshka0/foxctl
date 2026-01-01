package cmd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

const (
	claudeAuthURL   = "https://claude.ai/oauth/authorize"
	claudeTokenURL  = "https://console.anthropic.com/v1/oauth/token"
	claudeClientID  = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeRedirectURI = "http://localhost:54545/callback"
	claudeCallbackPort = 54545
)

// PKCECodes holds PKCE verification codes for OAuth2 PKCE flow.
type PKCECodes struct {
	CodeVerifier  string
	CodeChallenge string
}

// ClaudeTokenStorage stores OAuth2 tokens for Claude Max subscription.
type ClaudeTokenStorage struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	LastRefresh  string `json:"last_refresh"`
	Email        string `json:"email"`
	Type         string `json:"type"`
	Expire       string `json:"expired"`
}

func newAuthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication for LLM providers",
		Long: `Manage authentication tokens for LLM providers.

Supports:
- Claude Max subscription (OAuth-based)`,
	}
	cmd.AddCommand(
		newClaudeLoginCommand(),
		newClaudeLogoutCommand(),
		newClaudeStatusCommand(),
	)
	return cmd
}

func newClaudeLoginCommand() *cobra.Command {
	var noBrowser bool

	cmd := &cobra.Command{
		Use:   "claude-login",
		Short: "Login with Claude Max subscription",
		Long: `Authenticate with your Claude Max subscription using OAuth.

This will:
1. Start a local callback server
2. Open your browser to the Claude login page
3. Exchange the authorization code for tokens
4. Store tokens at ~/.agentctl/auth/claude_token.json

After login, set USE_CLAUDE_MAX=1 to use Claude for codemap generation.

Examples:
  agentctl auth claude-login
  agentctl auth claude-login --no-browser  # Manual URL copy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClaudeLogin(cmd, noBrowser)
		},
	}

	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print URL instead of opening browser")
	return cmd
}

func newClaudeLogoutCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude-logout",
		Short: "Logout from Claude Max",
		Long:  `Remove stored Claude Max OAuth tokens.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			tokenPath, err := getClaudeTokenPath()
			if err != nil {
				return err
			}

			if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
				fmt.Fprintln(cmd.OutOrStdout(), "Not logged in to Claude Max")
				return nil
			}

			if err := os.Remove(tokenPath); err != nil {
				return fmt.Errorf("remove token file: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Logged out from Claude Max")
			return nil
		},
	}
	return cmd
}

func newClaudeStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude-status",
		Short: "Show Claude Max login status",
		Long:  `Show the current Claude Max authentication status.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			tokenPath, err := getClaudeTokenPath()
			if err != nil {
				return err
			}

			data, err := os.ReadFile(tokenPath)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "Not logged in to Claude Max")
					fmt.Fprintln(cmd.OutOrStdout(), "Run: agentctl auth claude-login")
					return nil
				}
				return fmt.Errorf("read token file: %w", err)
			}

			var token ClaudeTokenStorage
			if err := json.Unmarshal(data, &token); err != nil {
				return fmt.Errorf("parse token file: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Claude Max: Logged in")
			if token.Email != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Email: %s\n", token.Email)
			}
			if token.LastRefresh != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Last refresh: %s\n", token.LastRefresh)
			}
			if token.Expire != "" {
				expireTime, err := time.Parse(time.RFC3339, token.Expire)
				if err == nil {
					if time.Now().Before(expireTime) {
						fmt.Fprintf(cmd.OutOrStdout(), "Token expires: %s\n", expireTime.Format("2006-01-02 15:04:05"))
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), "Token: Expired (will refresh on next use)")
					}
				}
			}
			return nil
		},
	}
	return cmd
}

func runClaudeLogin(cmd *cobra.Command, noBrowser bool) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	// Generate PKCE codes
	pkce, err := generatePKCECodes()
	if err != nil {
		return fmt.Errorf("generate PKCE codes: %w", err)
	}

	// Generate state for CSRF protection
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return fmt.Errorf("generate state: %w", err)
	}
	state := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(stateBytes)

	// Build authorization URL
	authURL := buildClaudeAuthURL(state, pkce)

	// Start callback server
	server := newOAuthCallbackServer(claudeCallbackPort)
	if err := server.Start(); err != nil {
		return fmt.Errorf("start callback server: %w", err)
	}
	defer func() {
		_ = server.Stop(ctx)
	}()

	// Open browser or show URL
	if noBrowser {
		fmt.Fprintln(out, "Open this URL in your browser:")
		fmt.Fprintln(out)
		fmt.Fprintln(out, authURL)
		fmt.Fprintln(out)
	} else {
		fmt.Fprintln(out, "Opening browser for Claude Max login...")
		if err := openBrowser(authURL); err != nil {
			fmt.Fprintf(out, "Could not open browser: %v\n", err)
			fmt.Fprintln(out, "Open this URL manually:")
			fmt.Fprintln(out, authURL)
		}
	}

	fmt.Fprintln(out, "Waiting for authorization...")

	// Wait for callback (5 minute timeout)
	result, err := server.WaitForCallback(5 * time.Minute)
	if err != nil {
		return fmt.Errorf("waiting for callback: %w", err)
	}

	if result.Error != "" {
		return fmt.Errorf("authorization failed: %s", result.Error)
	}

	// Verify state
	receivedState := result.State
	// Handle potential # fragments in the code
	if idx := strings.Index(result.Code, "#"); idx >= 0 {
		receivedState = result.Code[idx+1:]
		result.Code = result.Code[:idx]
	}
	// Validate state to prevent CSRF attacks
	// Both the expected state and received state must be non-empty and match
	if state == "" {
		return fmt.Errorf("internal error: empty state parameter")
	}
	if receivedState == "" {
		return fmt.Errorf("missing state in callback (possible CSRF attack)")
	}
	if receivedState != state {
		return fmt.Errorf("state mismatch (possible CSRF attack)")
	}

	fmt.Fprintln(out, "Authorization received, exchanging code for tokens...")

	// Exchange code for tokens
	token, err := exchangeCodeForTokens(ctx, result.Code, state, pkce)
	if err != nil {
		return fmt.Errorf("exchange code for tokens: %w", err)
	}

	// Save tokens
	tokenPath, err := getClaudeTokenPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(tokenPath), 0700); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}

	tokenData, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	if err := os.WriteFile(tokenPath, tokenData, 0600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Successfully logged in to Claude Max!")
	if token.Email != "" {
		fmt.Fprintf(out, "Email: %s\n", token.Email)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "To use Claude for codemap generation:")
	fmt.Fprintln(out, "  export USE_CLAUDE_MAX=1")
	fmt.Fprintln(out, "  agentctl codemap generate \"your query\"")

	return nil
}

func generatePKCECodes() (*PKCECodes, error) {
	// Generate 96 random bytes (will result in 128 base64 characters)
	bytes := make([]byte, 96)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}

	// Code verifier: URL-safe base64 without padding
	codeVerifier := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes)

	// Code challenge: SHA256 hash of verifier, URL-safe base64 without padding
	hash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])

	return &PKCECodes{
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
	}, nil
}

func buildClaudeAuthURL(state string, pkce *PKCECodes) string {
	params := url.Values{
		"client_id":             {claudeClientID},
		"response_type":         {"code"},
		"redirect_uri":          {claudeRedirectURI},
		"scope":                 {"org:create_api_key user:profile user:inference"},
		"code_challenge":        {pkce.CodeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return fmt.Sprintf("%s?%s", claudeAuthURL, params.Encode())
}

func exchangeCodeForTokens(ctx context.Context, code, state string, pkce *PKCECodes) (*ClaudeTokenStorage, error) {
	reqBody := map[string]interface{}{
		"code":          code,
		"state":         state,
		"grant_type":    "authorization_code",
		"client_id":     claudeClientID,
		"redirect_uri":  claudeRedirectURI,
		"code_verifier": pkce.CodeVerifier,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", claudeTokenURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Account      struct {
			EmailAddress string `json:"email_address"`
		} `json:"account"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &ClaudeTokenStorage{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		LastRefresh:  time.Now().Format(time.RFC3339),
		Email:        tokenResp.Account.EmailAddress,
		Type:         "claude",
		Expire:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339),
	}, nil
}

func getClaudeTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".agentctl", "auth", "claude_token.json"), nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}

// OAuthCallbackServer handles the local HTTP server for OAuth callbacks.
type OAuthCallbackServer struct {
	server     *http.Server
	listener   net.Listener
	port       int
	resultChan chan *OAuthResult
	errorChan  chan error
	mu         sync.Mutex
	running    bool
}

// OAuthResult contains the result of the OAuth callback.
type OAuthResult struct {
	Code  string
	State string
	Error string
}

func newOAuthCallbackServer(port int) *OAuthCallbackServer {
	return &OAuthCallbackServer{
		port:       port,
		resultChan: make(chan *OAuthResult, 1),
		errorChan:  make(chan error, 1),
	}
}

func (s *OAuthCallbackServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	// Acquire port immediately to avoid TOCTOU race
	addr := fmt.Sprintf(":%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is already in use", s.port)
	}
	s.listener = listener

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", s.handleCallback)
	mux.HandleFunc("/success", s.handleSuccess)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	s.running = true

	go func() {
		// Reuse the already-acquired listener to avoid TOCTOU race
		if err := s.server.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.errorChan <- fmt.Errorf("server failed: %w", err)
		}
	}()

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	return nil
}

func (s *OAuthCallbackServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.server == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := s.server.Shutdown(shutdownCtx)
	s.running = false
	s.server = nil

	return err
}

func (s *OAuthCallbackServer) WaitForCallback(timeout time.Duration) (*OAuthResult, error) {
	select {
	case result := <-s.resultChan:
		return result, nil
	case err := <-s.errorChan:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for OAuth callback")
	}
}

func (s *OAuthCallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	code := query.Get("code")
	state := query.Get("state")
	errorParam := query.Get("error")

	if errorParam != "" {
		result := &OAuthResult{Error: errorParam}
		s.sendResult(result)
		http.Error(w, fmt.Sprintf("OAuth error: %s", errorParam), http.StatusBadRequest)
		return
	}

	if code == "" {
		result := &OAuthResult{Error: "no_code"}
		s.sendResult(result)
		http.Error(w, "No authorization code received", http.StatusBadRequest)
		return
	}

	result := &OAuthResult{
		Code:  code,
		State: state,
	}
	s.sendResult(result)

	http.Redirect(w, r, "/success", http.StatusFound)
}

func (s *OAuthCallbackServer) handleSuccess(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	html := `<!DOCTYPE html>
<html>
<head>
    <title>Login Successful - agentctl</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100vh;
            margin: 0;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
        }
        .container {
            background: white;
            padding: 40px 60px;
            border-radius: 12px;
            box-shadow: 0 20px 40px rgba(0,0,0,0.2);
            text-align: center;
        }
        h1 {
            color: #333;
            margin-bottom: 10px;
        }
        p {
            color: #666;
            margin-bottom: 20px;
        }
        .check {
            font-size: 48px;
            margin-bottom: 20px;
        }
        .close-note {
            color: #999;
            font-size: 14px;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="check">&#10004;</div>
        <h1>Login Successful!</h1>
        <p>You've been authenticated with Claude Max.</p>
        <p class="close-note">You can close this window and return to the terminal.</p>
    </div>
</body>
</html>`

	_, _ = w.Write([]byte(html))
}

func (s *OAuthCallbackServer) sendResult(result *OAuthResult) {
	select {
	case s.resultChan <- result:
	default:
	}
}

func init() {
	rootCmd.AddCommand(newAuthCommand())
}
