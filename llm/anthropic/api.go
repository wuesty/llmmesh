package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wuesty/llmmesh/llm"
	"github.com/wuesty/llmmesh/streaming"
	"github.com/wuesty/llmmesh/types"
	"github.com/wuesty/llmmesh/utils"
	"github.com/wuesty/llmmesh/config"
)

// AnthropicApi anthropic api struct
type AnthropicApi struct {
	llm.BaseLLMApi
	client *anthropicsdk.Client
}

// NewAnthropicAPI function
func NewAnthropicAPI() llm.LLMApi {
	httpClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport, otelhttp.WithSpanNameFormatter(
			func(operation string, r *http.Request) string {
				return fmt.Sprintf("anthropic %s", r.URL.Path)
			})),
	}

	client := anthropicsdk.NewClient(
		option.WithAPIKey(config.LocalConfig.Anthropic.ApiKey),
		option.WithHTTPClient(httpClient),
	)
	return &AnthropicApi{
		client: &client,
	}
}

// Chat Function for Anthropic API Chat
func (aa AnthropicApi) Chat(ctx context.Context, request *llm.AIRequest) (*llm.AIResponse, error) {
	traceProvider := otel.GetTracerProvider()
	tracer := traceProvider.Tracer("")
	ctx, span := tracer.Start(ctx, "Anthropic Messages.Create")
	defer span.End()

	// Build up the Messages that need to go to the model
	var messages []anthropicsdk.MessageParam

	// Add conversation history
	conversationMessages := request.ConversationMessages
	// Iterate in reverse
	for i := len(conversationMessages) - 1; i >= 0; i-- {
		conversationEntry := conversationMessages[i]
		messages = append(messages, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(conversationEntry.UserMessage)))
		messages = append(messages, anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock(conversationEntry.AssistantMessage)))
	}

	// Add any User Messages
	for _, uMsg := range request.UserMessages {
		messages = append(messages, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(uMsg.Content)))
	}

	// Add any Assistant Messages
	for _, aMsg := range request.AssistantMessages {
		messages = append(messages, anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock(aMsg.Content)))
	}

	// Add the Tool Call, Tool Result Pairs
	for _, completedToolCall := range request.ToolCalls {
		// Unmarshal the function arguments from JSON string to map
		// Unmarshal the function arguments from JSON string to map
		var args map[string]interface{}
		if completedToolCall.FunctionArguments != "" {
			if err := json.Unmarshal([]byte(completedToolCall.FunctionArguments), &args); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool arguments: %w", err)
			}
		} else {
			args = map[string]interface{}{}
		}

		toolUseBlock := anthropicsdk.NewToolUseBlock(
			completedToolCall.ID,
			args,
			completedToolCall.FunctionName,
		)
		messages = append(messages, anthropicsdk.NewAssistantMessage(toolUseBlock))

		// Add the corresponding tool result
		toolResult := request.GetToolResult(completedToolCall.ID)
		if toolResult != nil {
			toolResultBlock := anthropicsdk.NewToolResultBlock(
				completedToolCall.ID,
				toolResult.Content,
				false, // is_error
			)
			messages = append(messages, anthropicsdk.NewUserMessage(toolResultBlock))
		} else {
			slog.Warn("Tool Result Not Found", slog.String("Tool Id", completedToolCall.ID))
		}
	}

	// Get the Tool definition List
	toolDefinitionList, err := aa.buildToolList(request)
	if err != nil {
		return nil, err
	}

	// Create params
	params := anthropicsdk.MessageNewParams{
		Model:       anthropicsdk.Model(config.LocalConfig.Anthropic.DefaultLLM),
		Messages:    messages,
		MaxTokens:   int64(4096),
		System:      []anthropicsdk.TextBlockParam{anthropicsdk.TextBlockParam{Text: request.Prompt}},
		Tools:       toolDefinitionList,
		Temperature: anthropicsdk.Float(1.0),
	}

	response := llm.NewAIResponse()

	// Make a message creation call to Anthropic
	message, err := aa.client.Messages.New(ctx, params)
	if err != nil {
		// Be more graceful about a context being cancelled
		if utils.IsContextCanceled(err) {
			slog.Warn("Call to Anthropic Cancelled - Context Canceled")
			response.Content = ""
			response.Status = llm.Canceled
			return response, nil
		}
		slog.ErrorContext(ctx, "Error occurred making Anthropic API call", slog.Any("error", err))
		return nil, err
	}

	span.SetAttributes(attribute.String("Model", string(message.Model)))
	span.SetAttributes(attribute.Int("InputTokens", int(message.Usage.InputTokens)))
	span.SetAttributes(attribute.Int("OutputTokens", int(message.Usage.OutputTokens)))

	// Process response content blocks
	var textContent string
	var hasToolUse bool

	for _, block := range message.Content {
		switch block.Type {
		case "text":
			textContent += block.Text
		case "tool_use":
			hasToolUse = true
			genericToolCall, err := aa.toolCallFromAnthropic(ctx, block.ID, block.Name, block.Input)
			if err != nil {
				return nil, err
			}

			// Marshal arguments for storage
			argsJSON, err := json.Marshal(block.Input)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal tool arguments: %w", err)
			}

			request.AddToolCall(llm.ToolCall{
				ID:                block.ID,
				Type:              "tool_use",
				FunctionName:      block.Name,
				FunctionArguments: string(argsJSON),
			})

			response.AddToolCall(*genericToolCall)
			response.Status = llm.Tools
		}
	}

	// If we have tool calls and the agent has tools, return them
	if hasToolUse && len(request.ToolsNames) > 0 {
		return response, nil
	}

	// Otherwise return the text content
	response.Content = textContent
	response.Status = llm.Done
	return response, nil
}

// Stream makes a streaming call to Anthropic
func (aa AnthropicApi) Stream(ctx context.Context, request *llm.AIRequest, streamer streaming.Streamer) (*llm.AIResponse, error) {
	traceProvider := otel.GetTracerProvider()
	tracer := traceProvider.Tracer("")
	ctx, span := tracer.Start(ctx, "Anthropic Messages.Create (Stream)")
	defer span.End()

	// Build up the Messages that need to go to the model
	var messages []anthropicsdk.MessageParam

	// Add conversation history
	conversationMessages := request.ConversationMessages
	// Iterate in reverse
	for i := len(conversationMessages) - 1; i >= 0; i-- {
		conversationEntry := conversationMessages[i]
		messages = append(messages, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(conversationEntry.UserMessage)))
		messages = append(messages, anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock(conversationEntry.AssistantMessage)))
	}

	// Add any User Messages
	for _, uMsg := range request.UserMessages {
		messages = append(messages, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(uMsg.Content)))
	}

	// Add any Assistant Messages
	for _, aMsg := range request.AssistantMessages {
		messages = append(messages, anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock(aMsg.Content)))
	}

	// Add the Tool Call, Tool Result Pairs
	for _, completedToolCall := range request.ToolCalls {
		// Unmarshal the function arguments from JSON string to map
		var args map[string]interface{}
		if completedToolCall.FunctionArguments != "" {
			if err := json.Unmarshal([]byte(completedToolCall.FunctionArguments), &args); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool arguments: %w", err)
			}
		} else {
			args = map[string]interface{}{}
		}

		toolUseBlock := anthropicsdk.NewToolUseBlock(
			completedToolCall.ID,
			args,
			completedToolCall.FunctionName,
		)

		slog.Debug("NewToolUseBlock", slog.String("Id", completedToolCall.ID),
			slog.String("Function Name", completedToolCall.FunctionName), slog.Any("toolUseBlock", toolUseBlock))
		messages = append(messages, anthropicsdk.NewAssistantMessage(toolUseBlock))

		// Add the corresponding tool result
		toolResult := request.GetToolResult(completedToolCall.ID)
		if toolResult != nil {
			toolResultBlock := anthropicsdk.NewToolResultBlock(
				completedToolCall.ID,
				toolResult.Content,
				false, // is_error
			)
			messages = append(messages, anthropicsdk.NewUserMessage(toolResultBlock))
		} else {
			slog.Warn("Tool Result Not Found", slog.String("Tool Id", completedToolCall.ID))
		}
	}

	// Get the Tool definition List
	toolDefinitionList, err := aa.buildToolList(request)
	if err != nil {
		return nil, err
	}

	// Create params
	params := anthropicsdk.MessageNewParams{
		Model:       anthropicsdk.Model(config.LocalConfig.Anthropic.DefaultLLM),
		Messages:    messages,
		MaxTokens:   int64(4096),
		System:      []anthropicsdk.TextBlockParam{anthropicsdk.TextBlockParam{Text: request.Prompt}},
		Tools:       toolDefinitionList,
		Temperature: anthropicsdk.Float(1.0),
	}

	response := llm.NewAIResponse()
	var fullContent string
	var currentToolUseID string
	var currentToolName string
	var currentToolInput string

	// Make a streaming message creation call
	slog.Debug("STREAMING REQUEST - SENDING")
	stream := aa.client.Messages.NewStreaming(ctx, params)

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case "content_block_delta":
			if &event.Delta != nil {
				if event.Delta.Type == "text_delta" {
					fullContent += event.Delta.Text
					streamer.Stream(streaming.NewOutputMessage(request.AgentRuntimeID, event.Delta.Text))
				} else if event.Delta.Type == "input_json_delta" {
					currentToolInput += event.Delta.PartialJSON
				}
			}

		case "content_block_start":
			if &event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				currentToolUseID = event.ContentBlock.ID
				currentToolName = event.ContentBlock.Name
				currentToolInput = ""
			}

		case "content_block_stop":
			// If we were collecting a tool use, finalize it
			if currentToolUseID != "" {
				var args map[string]interface{}

				if &currentToolInput != nil && currentToolInput != "" {
					if err := json.Unmarshal([]byte(currentToolInput), &args); err != nil {
						slog.Debug("Tool Input", slog.Any("args", args))
						return nil, fmt.Errorf("failed to unmarshal tool input: %w", err)
					}
				} else {
					args = make(map[string]interface{})
				}
				genericToolCall, err := aa.toolCallFromAnthropic(ctx, currentToolUseID, currentToolName, args)
				if err != nil {
					slog.Warn("Error on toolCallFromAnthropic", slog.Any("error", err))
					return nil, err
				}

				tc := llm.ToolCall{
					ID:                currentToolUseID,
					Type:              "tool_use",
					FunctionName:      currentToolName,
					FunctionArguments: currentToolInput,
				}


				request.AddToolCall(tc)

				response.AddToolCall(*genericToolCall)
				response.Status = llm.Tools

				// Reset for next tool
				currentToolUseID = ""
				currentToolName = ""
				currentToolInput = ""
			}
		}
	}

	if err := stream.Err(); err != nil {
		// Be more graceful about a context being cancelled
		if utils.IsContextCanceled(err) {
			slog.Warn("Anthropic Stream Cancelled - Context Canceled")
			response.Content = ""
			response.Status = llm.Canceled
			return response, nil
		}
		slog.ErrorContext(ctx, "Error occurred in Anthropic stream", slog.Any("error", err))
		return nil, err
	}

	// If we have tool calls, return them
	if response.Status == llm.Tools {
		return response, nil
	}

	response.Content = fullContent
	response.Status = llm.Done
	return response, nil
}

// buildToolList builds the tool list definitions in Anthropic format
func (aa AnthropicApi) buildToolList(request *llm.AIRequest) ([]anthropicsdk.ToolUnionParam, error) {
	toolList := make([]anthropicsdk.ToolUnionParam, 0)

	for _, toolName := range request.ToolsNames {
		tool := request.GetAgentWorkflow().GetTool(toolName)
		if tool == nil {
			slog.Error("Tool Not Found", slog.String("ToolName", toolName))
			return nil, errors.New("tool not found")
		}
		toolDef := tool.GetAnthropicDefinition()
		toolList = append(toolList, anthropicsdk.ToolUnionParam{OfTool: &toolDef})
	}

	return toolList, nil
}

// toolCallFromAnthropic converts an Anthropic tool use to a generic tool call
func (aa AnthropicApi) toolCallFromAnthropic(ctx context.Context, id string, name string, input interface{}) (*types.ToolCall, error) {
	args, ok := input.(map[string]interface{})
	if !ok {
		// Try to convert via JSON if it's not already a map
		jsonBytes, err := json.Marshal(input)
		if err != nil {
			slog.ErrorContext(ctx, "could not marshal tool input from anthropic", slog.Any("error", err))
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &args); err != nil {
			slog.ErrorContext(ctx, "could not unmarshal tool call from anthropic", slog.Any("error", err))
			return nil, err
		}
	}

	toolCall := types.ToolCall{
		Id:        id,
		Name:      name,
		Arguments: args,
	}
	return &toolCall, nil
}
