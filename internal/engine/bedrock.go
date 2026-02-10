package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// BedrockClient wraps the AWS Bedrock Runtime Converse API.
type BedrockClient struct {
	client *bedrockruntime.Client
	region string
}

// NewBedrockClient creates a client using the default AWS credential chain.
func NewBedrockClient(ctx context.Context, region string) (*BedrockClient, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return &BedrockClient{
		client: bedrockruntime.NewFromConfig(cfg),
		region: region,
	}, nil
}

// Converse sends messages using the Bedrock Converse API, translating between
// the engine's OpenAI-compatible types and Bedrock's native types.
func (b *BedrockClient) Converse(ctx context.Context, model string, messages []oaiMessage, tools []oaiTool, temp float64, maxTokens int) (*oaiResponse, error) {
	bedrockMsgs, systemPrompts := convertMessages(messages)

	input := &bedrockruntime.ConverseInput{
		ModelId:  aws.String(model),
		Messages: bedrockMsgs,
	}

	if len(systemPrompts) > 0 {
		input.System = systemPrompts
	}

	if len(tools) > 0 {
		toolConfig := convertTools(tools)
		if toolConfig != nil {
			input.ToolConfig = toolConfig
		}
	}

	var inferenceConfig *types.InferenceConfiguration
	if temp > 0 || maxTokens > 0 {
		inferenceConfig = &types.InferenceConfiguration{}
		if temp > 0 {
			inferenceConfig.Temperature = aws.Float32(float32(temp))
		}
		if maxTokens > 0 {
			inferenceConfig.MaxTokens = aws.Int32(int32(maxTokens))
		}
		input.InferenceConfig = inferenceConfig
	}

	output, err := b.client.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock converse: %w", err)
	}

	return convertResponse(output)
}

// convertMessages translates oaiMessage slice to Bedrock message types.
// System messages are extracted into separate system content blocks.
func convertMessages(messages []oaiMessage) ([]types.Message, []types.SystemContentBlock) {
	var bedrockMsgs []types.Message
	var systemPrompts []types.SystemContentBlock

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			if strings.TrimSpace(msg.Content) != "" {
				systemPrompts = append(systemPrompts, &types.SystemContentBlockMemberText{
					Value: msg.Content,
				})
			}

		case "user":
			bedrockMsgs = append(bedrockMsgs, types.Message{
				Role:    types.ConversationRoleUser,
				Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: msg.Content}},
			})

		case "assistant":
			var content []types.ContentBlock
			if msg.Content != "" {
				content = append(content, &types.ContentBlockMemberText{Value: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				inputDoc, err := parseToolInput(tc.Function.Arguments)
				if err != nil {
					// Best-effort: pass raw string as a wrapper doc
					inputDoc = document.NewLazyDocument(map[string]interface{}{"raw": tc.Function.Arguments})
				}
				content = append(content, &types.ContentBlockMemberToolUse{
					Value: types.ToolUseBlock{
						ToolUseId: aws.String(tc.ID),
						Name:      aws.String(tc.Function.Name),
						Input:     inputDoc,
					},
				})
			}
			// Only add message if it has content (Bedrock requires non-empty content)
			if len(content) > 0 {
				bedrockMsgs = append(bedrockMsgs, types.Message{
					Role:    types.ConversationRoleAssistant,
					Content: content,
				})
			}

		case "tool":
			// Tool result messages reference a previous tool_use by ID.
			bedrockMsgs = append(bedrockMsgs, types.Message{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberToolResult{
						Value: types.ToolResultBlock{
							ToolUseId: aws.String(msg.ToolCallID),
							Content: []types.ToolResultContentBlock{
								&types.ToolResultContentBlockMemberText{Value: msg.Content},
							},
						},
					},
				},
			})
		}
	}

	return bedrockMsgs, systemPrompts
}

// convertTools translates oaiTool slice to Bedrock ToolConfiguration.
func convertTools(tools []oaiTool) *types.ToolConfiguration {
	if len(tools) == 0 {
		return nil
	}

	var bedrockTools []types.Tool
	for _, t := range tools {
		spec := types.ToolSpecification{
			Name:        aws.String(t.Function.Name),
			Description: aws.String(t.Function.Description),
		}
		if len(t.Function.Parameters) > 0 {
			var schema interface{}
			if err := json.Unmarshal(t.Function.Parameters, &schema); err == nil {
				spec.InputSchema = &types.ToolInputSchemaMemberJson{
					Value: document.NewLazyDocument(schema),
				}
			}
		}
		bedrockTools = append(bedrockTools, &types.ToolMemberToolSpec{Value: spec})
	}

	return &types.ToolConfiguration{Tools: bedrockTools}
}

// convertResponse translates Bedrock ConverseOutput to the engine's oaiResponse.
func convertResponse(output *bedrockruntime.ConverseOutput) (*oaiResponse, error) {
	resp := &oaiResponse{}

	if output.Usage != nil {
		resp.Usage.PromptTokens = int(aws.ToInt32(output.Usage.InputTokens))
		resp.Usage.CompletionTokens = int(aws.ToInt32(output.Usage.OutputTokens))
	}

	var text strings.Builder
	var toolCalls []oaiToolCall

	if output.Output != nil {
		if msgOutput, ok := output.Output.(*types.ConverseOutputMemberMessage); ok {
			for _, block := range msgOutput.Value.Content {
				switch b := block.(type) {
				case *types.ContentBlockMemberText:
					text.WriteString(b.Value)
				case *types.ContentBlockMemberToolUse:
					inputJSON, err := marshalToolInput(b.Value.Input)
					if err != nil {
						inputJSON = "{}"
					}
					toolCalls = append(toolCalls, oaiToolCall{
						ID:   aws.ToString(b.Value.ToolUseId),
						Type: "function",
						Function: oaiFunction{
							Name:      aws.ToString(b.Value.Name),
							Arguments: inputJSON,
						},
					})
				}
			}
		}
	}

	finishReason := mapBedrockStopReason(output.StopReason)

	resp.Choices = append(resp.Choices, struct {
		Message      oaiMessage `json:"message"`
		FinishReason string     `json:"finish_reason"`
	}{
		Message: oaiMessage{
			Role:      "assistant",
			Content:   text.String(),
			ToolCalls: toolCalls,
		},
		FinishReason: finishReason,
	})

	return resp, nil
}

func parseToolInput(args string) (document.Interface, error) {
	var parsed interface{}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return nil, err
	}
	return document.NewLazyDocument(parsed), nil
}

func marshalToolInput(input document.Interface) (string, error) {
	if input == nil {
		return "{}", nil
	}
	var dest interface{}
	if err := input.UnmarshalSmithyDocument(&dest); err != nil {
		return "{}", err
	}
	b, err := json.Marshal(dest)
	if err != nil {
		return "{}", err
	}
	return string(b), nil
}

func mapBedrockStopReason(reason types.StopReason) string {
	switch reason {
	case types.StopReasonEndTurn, types.StopReasonMaxTokens:
		return "stop"
	case types.StopReasonToolUse:
		return "tool_calls"
	case types.StopReasonStopSequence:
		return "stop"
	default:
		return "stop"
	}
}
