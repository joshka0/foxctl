package semantic

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// EmbeddingServiceError classifies failures from external embedding services.
// It keeps provider-specific hints close to the provider so CLI/MCP surfaces can
// report actionable configuration and availability guidance.
type EmbeddingServiceError struct {
	Provider string
	Model    string
	BaseURL  string
	Kind     string
	Hint     string
	Err      error
}

func (e *EmbeddingServiceError) Error() string {
	parts := []string{"embedding service"}
	if strings.TrimSpace(e.Provider) != "" {
		parts = append(parts, "provider="+strings.TrimSpace(e.Provider))
	}
	if strings.TrimSpace(e.Model) != "" {
		parts = append(parts, "model="+strings.TrimSpace(e.Model))
	}
	if strings.TrimSpace(e.BaseURL) != "" {
		parts = append(parts, "base_url="+strings.TrimSpace(e.BaseURL))
	}
	if strings.TrimSpace(e.Kind) != "" {
		parts = append(parts, "kind="+strings.TrimSpace(e.Kind))
	}
	msg := strings.Join(parts, " ")
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	if strings.TrimSpace(e.Hint) != "" {
		msg += " (" + strings.TrimSpace(e.Hint) + ")"
	}
	return msg
}

func (e *EmbeddingServiceError) Unwrap() error {
	return e.Err
}

func newOpenAICompatRequestError(model, baseURL string, err error) error {
	kind := "request_failed"
	hint := "verify the OpenAI-compatible embedding endpoint is reachable and the configured model is loaded"
	if isConnectionRefused(err) {
		kind = "connection_refused"
		hint = "start LM Studio's local server, load an embedding model, or set FOXCTL_EMBEDDING_BASE_URL to the reachable /v1 endpoint"
	} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) || isTimeout(err) {
		kind = "timeout"
		hint = "check the embedding server health or increase the embedding timeout"
	}
	return &EmbeddingServiceError{
		Provider: "openai_compat",
		Model:    model,
		BaseURL:  baseURL,
		Kind:     kind,
		Hint:     hint,
		Err:      err,
	}
}

func newOpenAICompatStatusError(model, baseURL string, statusCode int, message string) error {
	kind := fmt.Sprintf("http_%d", statusCode)
	hint := "check the embedding endpoint, API key, and model name"
	if statusCode == 404 {
		hint = "verify the endpoint exposes /v1/embeddings and the configured embedding model is loaded"
	} else if statusCode == 401 || statusCode == 403 {
		hint = "check FOXCTL_EMBEDDING_API_KEY or the server authentication setting"
	}
	return &EmbeddingServiceError{
		Provider: "openai_compat",
		Model:    model,
		BaseURL:  baseURL,
		Kind:     kind,
		Hint:     hint,
		Err:      fmt.Errorf("status %d: %s", statusCode, strings.TrimSpace(message)),
	}
}

func isConnectionRefused(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		err = opErr.Err
	}
	var syscallErr *os.SyscallError
	if errors.As(err, &syscallErr) {
		err = syscallErr.Err
	}
	return strings.Contains(strings.ToLower(fmt.Sprint(err)), "connection refused")
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
