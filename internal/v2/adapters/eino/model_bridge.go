package eino

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// oaiModelBridge implements model.BaseChatModel against an OpenAI-compatible endpoint.
// It is used only when FOXCTL_ENGINE_BACKEND=eino to provision the Eino ChatModelAgent.
// Tool-call support is out of scope for the spike; only plain Generate is wired.
type oaiModelBridge struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// newOAIModelBridge constructs a bridge from provider-resolved connection parameters.
func newOAIModelBridge(apiKey, baseURL, modelID string, timeout time.Duration) *oaiModelBridge {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &oaiModelBridge{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   modelID,
		client:  &http.Client{Timeout: timeout},
	}
}

type bridgeRequest struct {
	Model    string         `json:"model"`
	Messages []bridgeOAIMsg `json:"messages"`
}

type bridgeOAIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type bridgeResponse struct {
	Choices []struct {
		Message bridgeOAIMsg `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Generate calls the OpenAI-compatible /chat/completions endpoint and returns the
// first assistant message. Implements model.BaseChatModel.
func (b *oaiModelBridge) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	msgs := make([]bridgeOAIMsg, 0, len(input))
	for _, m := range input {
		role, err := schemaRoleToOAI(m.Role)
		if err != nil {
			return nil, fmt.Errorf("oaiModelBridge: convert role: %w", err)
		}
		msgs = append(msgs, bridgeOAIMsg{Role: role, Content: m.Content})
	}

	body, err := json.Marshal(bridgeRequest{Model: b.model, Messages: msgs})
	if err != nil {
		return nil, fmt.Errorf("oaiModelBridge: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("oaiModelBridge: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if b.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.apiKey)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oaiModelBridge: request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("oaiModelBridge: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oaiModelBridge: status %d: %s", resp.StatusCode, raw)
	}

	var br bridgeResponse
	if err := json.Unmarshal(raw, &br); err != nil {
		return nil, fmt.Errorf("oaiModelBridge: parse response: %w", err)
	}
	if br.Error != nil {
		return nil, fmt.Errorf("oaiModelBridge: API error: %s", br.Error.Message)
	}
	if len(br.Choices) == 0 {
		return nil, fmt.Errorf("oaiModelBridge: empty choices in response")
	}

	return &schema.Message{
		Role:    schema.Assistant,
		Content: br.Choices[0].Message.Content,
	}, nil
}

// Stream is not implemented for the spike — callers should use Generate.
func (b *oaiModelBridge) Stream(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("oaiModelBridge: Stream not implemented in spike (use Generate)")
}

func schemaRoleToOAI(r schema.RoleType) (string, error) {
	switch r {
	case schema.User:
		return "user", nil
	case schema.Assistant:
		return "assistant", nil
	case schema.System:
		return "system", nil
	case schema.Tool:
		return "tool", nil
	default:
		return "", fmt.Errorf("unknown schema role %q", r)
	}
}
