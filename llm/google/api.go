package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/google/uuid"
	"github.com/invopop/jsonschema"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/genai"

	"github.com/wuesty/llmmesh/llm"
	"github.com/wuesty/llmmesh/streaming"
	"github.com/wuesty/llmmesh/types"
	"github.com/wuesty/llmmesh/utils"
	"github.com/wuesty/llmmesh/config"
)

// GeminiApi google genai api struct
type GeminiApi struct {
	llm.BaseLLMApi
	client *genai.Client
}

// NewGeminiAPI function - uses Google AI Studio
func NewGeminiAPI() llm.LLMApi {
	ctx := context.Background()

	// For Google AI Studio, you need an API key
	apiKey := config.LocalConfig.Gcp.AIStudioKey // You'll need to add this to your config
	if apiKey == "" {
		slog.Error("Google AI Studio API key not provided")
		return nil
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		slog.Error("failed to create google ai studio client", slog.Any("error", err))
		return nil
	}

	slog.Debug("Created Google AI Studio client")

	return &GeminiApi{
		client: client,
	}
}

// Chat Function for Google GenAI
func (ga *GeminiApi) Chat(ctx context.Context, request *llm.AIRequest) (*llm.AIResponse, error) {
	traceProvider := otel.GetTracerProvider()
	tracer := traceProvider.Tracer("")
	ctx, span := tracer.Start(ctx, "GoogleGenAI_GenerateContent")
	defer span.End()

	// Build chatConfig
	chatConfig := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr[float32](1),
		MaxOutputTokens: 65536,
		TopP:            genai.Ptr[float32](0.95),
		//TopK:            genai.Ptr[float32](10),
	}

	// Set system instruction
	if request.Prompt != "" {
		chatConfig.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{
				{Text: request.Prompt},
			},
		}
	}

	// Add tools if any
	if len(request.ToolsNames) > 0 {
		tools, err := ga.buildTools(request)
		if err != nil {
			return nil, fmt.Errorf("failed to build tools: %w", err)
		}
		chatConfig.Tools = tools
		chatConfig.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAuto,
			},
		}
	}

	// Handle JSON response format
	if request.JSONResponseFormat != nil {
		chatConfig.ResponseMIMEType = "application/json"
		schema := GenerateSchemaFromReflectType(request.JSONResponseFormat.Schema)
		if jsonSchema, ok := schema.(*jsonschema.Schema); ok {
			chatConfig.ResponseSchema = ga.convertJSONSchemaToGenAI(jsonSchema)
		}
	}

	// Build contents
	contents, err := ga.buildContents(request)
	if err != nil {
		return nil, fmt.Errorf("failed to build contents: %w", err)
	}

	response := llm.NewAIResponse()

	// Generate content
	var resp *genai.GenerateContentResponse
	if len(contents) == 0 {
		// No previous conversation, just generate
		resp, err = ga.client.Models.GenerateContent(ctx, config.LocalConfig.Gcp.DefaultLLM, nil, chatConfig)
	} else {
		// Pass contents array
		resp, err = ga.client.Models.GenerateContent(ctx, config.LocalConfig.Gcp.DefaultLLM, contents, chatConfig)
	}

	if err != nil {
		if utils.IsContextCanceled(err) {
			slog.Warn("Call to Google GenAI Cancelled - Context Canceled")
			response.Content = ""
			response.Status = llm.Canceled
			return response, nil
		}
		slog.ErrorContext(ctx, "Error occurred making Google GenAI API call", slog.Any("error", err))
		return nil, fmt.Errorf("google genai api call failed: %w", err)
	}

	// Set telemetry attributes
	if resp.UsageMetadata != nil {
		span.SetAttributes(
			attribute.String("model", config.LocalConfig.Gcp.DefaultLLM),
			attribute.Int("prompt_tokens", int(resp.UsageMetadata.PromptTokenCount)),
			attribute.Int("completion_tokens", int(resp.UsageMetadata.CandidatesTokenCount)),
			attribute.Int("total_tokens", int(resp.UsageMetadata.TotalTokenCount)),
		)
	}

	// Process the response
	if len(resp.Candidates) == 0 {
		return nil, errors.New("no candidates returned from Google GenAI")
	}

	candidate := resp.Candidates[0]

	// Check for function calls (tools)
	var hasFunctionCalls bool

	for _, part := range candidate.Content.Parts {
		if part.FunctionCall != nil {
			hasFunctionCalls = true
			genericToolCall, err := ga.toolCallFromGenAI(ctx, part.FunctionCall)
			if err != nil {
				return nil, fmt.Errorf("failed to convert tool call: %w", err)
			}

			request.AddToolCall(llm.ToolCall{
				ID:                genericToolCall.Id,
				Type:              "function",
				FunctionName:      genericToolCall.Name,
				FunctionArguments: mustMarshalJSON(genericToolCall.Arguments),
			})

			response.AddToolCall(*genericToolCall)
			response.Status = llm.Tools
		}
	}

	if hasFunctionCalls && len(request.ToolsNames) > 0 {
		return response, nil
	}

	// Extract text content
	response.Content = resp.Text()
	response.Status = llm.Done
	return response, nil
}

// Stream makes a streaming call to Google GenAI
func (ga *GeminiApi) Stream(ctx context.Context, request *llm.AIRequest, streamer streaming.Streamer) (*llm.AIResponse, error) {
	traceProvider := otel.GetTracerProvider()
	tracer := traceProvider.Tracer("")
	ctx, span := tracer.Start(ctx, "GoogleGenAI_GenerateContentStream")
	defer span.End()

	// Build streamConfig
	streamConfig := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr[float32](1),
		MaxOutputTokens: 65536,
		TopP:            genai.Ptr[float32](0.95),
		//TopK:            genai.Ptr[float32](10),
	}

	// Set system instruction
	if request.Prompt != "" {
		streamConfig.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{
				{Text: request.Prompt},
			},
		}
	}

	// Add tools if any
	if len(request.ToolsNames) > 0 {
		slog.Debug("Adding Tools", slog.Any("tools", request.ToolsNames))
		tools, err := ga.buildTools(request)
		if err != nil {
			return nil, fmt.Errorf("failed to build tools: %w", err)
		}
		streamConfig.Tools = tools
		streamConfig.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAuto,
			},
		}
	}

	// Build contents
	contents, err := ga.buildContents(request)
	if err != nil {
		return nil, fmt.Errorf("failed to build contents: %w", err)
	}

	response := llm.NewAIResponse()
	var fullContent string
	var hasFunctionCalls bool

	// Create streaming call
	var streamInput []*genai.Content
	if len(contents) == 0 {
		streamInput = nil
	} else {
		streamInput = contents
	}

	// Process streaming response
	for result, err := range ga.client.Models.GenerateContentStream(ctx, config.LocalConfig.Gcp.DefaultLLM, streamInput, streamConfig) {
		if err != nil {
			slog.Error("Error occurred during streaming", slog.Any("error", err))
			return nil, fmt.Errorf("streaming error: %w", err)
		}

		if len(result.Candidates) == 0 {
			continue
		}

		candidate := result.Candidates[0]

		// Process each part
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				content := part.Text
				if !part.Thought {
					fullContent += content
					streamer.Stream(streaming.NewOutputMessage(request.AgentRuntimeID, content))
				} else {
					streamer.Stream(streaming.NewPlanMessage(request.AgentRuntimeID, content))
				}
			} else if part.FunctionCall != nil {
				hasFunctionCalls = true
				genericToolCall, err := ga.toolCallFromGenAI(ctx, part.FunctionCall)
				if err != nil {
					return nil, fmt.Errorf("failed to convert tool call: %w", err)
				}

				request.AddToolCall(llm.ToolCall{
					ID:                genericToolCall.Id,
					Type:              "function",
					FunctionName:      genericToolCall.Name,
					FunctionArguments: mustMarshalJSON(genericToolCall.Arguments),
				})

				response.AddToolCall(*genericToolCall)
				response.Status = llm.Tools
			}
		}
	}

	if hasFunctionCalls {
		return response, nil
	}

	response.Content = fullContent
	response.Status = llm.Done
	return response, nil
}

// ChatSession creates a chat session and handles the conversation
func (ga *GeminiApi) ChatSession(ctx context.Context, request *llm.AIRequest) (*llm.AIResponse, error) {
	// Build chatConfig
	chatConfig := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr[float32](1),
		MaxOutputTokens: 65536,
		TopP:            genai.Ptr[float32](0.95),
		//TopK:            genai.Ptr[float32](10),
	}

	// Set system instruction
	if request.Prompt != "" {
		chatConfig.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{
				{Text: request.Prompt},
			},
		}
	}

	// Add tools if any
	if len(request.ToolsNames) > 0 {
		tools, err := ga.buildTools(request)
		if err != nil {
			return nil, fmt.Errorf("failed to build tools: %w", err)
		}
		chatConfig.Tools = tools
		chatConfig.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAuto,
			},
		}
	}

	// Build initial history for chat session
	history, err := ga.buildChatHistory(request)
	if err != nil {
		return nil, fmt.Errorf("failed to build chat history: %w", err)
	}

	// Create chat session
	chat, err := ga.client.Chats.Create(ctx, config.LocalConfig.Gcp.DefaultLLM, chatConfig, history)
	if err != nil {
		return nil, fmt.Errorf("failed to create chat session: %w", err)
	}

	// Get the current user message
	var userMessage string
	if len(request.UserMessages) > 0 {
		userMessage = request.UserMessages[len(request.UserMessages)-1].Content
	}

	// Send message
	result, err := chat.SendMessage(ctx, genai.Part{Text: userMessage})
	if err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	response := llm.NewAIResponse()
	response.Content = result.Text()
	response.Status = llm.Done
	return response, nil
}

// buildContents builds the content array for the request
func (ga *GeminiApi) buildContents(request *llm.AIRequest) ([]*genai.Content, error) {
	var contents []*genai.Content

	// Add conversation history in correct chronological order
	for _, conversationEntry := range request.ConversationMessages {
		contents = append(contents, &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{Text: conversationEntry.UserMessage},
			},
		})
		contents = append(contents, &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{Text: conversationEntry.AssistantMessage},
			},
		})
	}

	// Add user messages
	for _, uMsg := range request.UserMessages {

		slog.Debug("Adding User Message", slog.String("uMsg.Content", uMsg.Content))
		contents = append(contents, &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{Text: uMsg.Content},
			},
		})
	}

	// Add tool calls and results
	for _, completedToolCall := range request.ToolCalls {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(completedToolCall.FunctionArguments), &args); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tool arguments: %w", err)
		}

		// Function call from model
		contents = append(contents, &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{
					FunctionCall: &genai.FunctionCall{
						Name: completedToolCall.FunctionName,
						Args: args,
					},
				},
			},
		})

		slog.Debug("Adding Tool Results", slog.String("Function", completedToolCall.FunctionName))
		toolResult := request.GetToolResult(completedToolCall.ID)
		if toolResult != nil {

			slog.Debug("Function Response", slog.Any("data", toolResult.Content))
			contents = append(contents, &genai.Content{
				Role: "tool",
				Parts: []*genai.Part{
					{
						FunctionResponse: &genai.FunctionResponse{
							Name: completedToolCall.FunctionName,
							Response: map[string]interface{}{
								"content": toolResult.Content,
							},
						},
					},
				},
			})
		}
	}

	// Add assistant messages
	for _, aMsg := range request.AssistantMessages {

		contents = append(contents, &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{Text: aMsg.Content},
			},
		})
	}

	return contents, nil
}

// buildChatHistory builds history for chat sessions
func (ga *GeminiApi) buildChatHistory(request *llm.AIRequest) ([]*genai.Content, error) {
	var history []*genai.Content

	// Only add conversation history for chat sessions
	for _, conversationEntry := range request.ConversationMessages {
		history = append(history, &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{Text: conversationEntry.UserMessage},
			},
		})
		history = append(history, &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{Text: conversationEntry.AssistantMessage},
			},
		})
	}

	return history, nil
}

// buildTools builds the tool definitions
func (ga *GeminiApi) buildTools(request *llm.AIRequest) ([]*genai.Tool, error) {
	var tools []*genai.Tool
	var functionDeclarations []*genai.FunctionDeclaration

	for _, toolName := range request.ToolsNames {
		tool := request.GetAgentWorkflow().GetTool(toolName)
		if tool == nil {
			slog.Error("Tool Not Found", slog.String("tool_name", toolName))
			return nil, fmt.Errorf("tool not found: %s", toolName)
		}

		slog.Debug("Adding Tool to List", slog.String("name", toolName))
		// Convert from your tool format to genai.FunctionDeclaration
		googleToolDefinition := tool.GetGoogleDefinition()
		functionDeclarations = append(functionDeclarations, googleToolDefinition)
	}

	if len(functionDeclarations) > 0 {
		tools = append(tools, &genai.Tool{
			FunctionDeclarations: functionDeclarations,
		})
	}

	return tools, nil
}

// Rest of the helper functions remain the same...
func (ga *GeminiApi) convertJSONSchemaToGenAI(jsonSchema *jsonschema.Schema) *genai.Schema {
	if jsonSchema == nil {
		return &genai.Schema{Type: genai.TypeString}
	}

	schema := &genai.Schema{}

	switch jsonSchema.Type {
	case "string":
		schema.Type = genai.TypeString
	case "number":
		schema.Type = genai.TypeNumber
	case "integer":
		schema.Type = genai.TypeInteger
	case "boolean":
		schema.Type = genai.TypeBoolean
	case "array":
		schema.Type = genai.TypeArray
	case "object":
		schema.Type = genai.TypeObject
	default:
		schema.Type = genai.TypeString
	}

	if jsonSchema.Description != "" {
		schema.Description = jsonSchema.Description
	}

	// Handle properties using OrderedMap
	if jsonSchema.Properties != nil && jsonSchema.Properties.Len() > 0 {
		schema.Properties = make(map[string]*genai.Schema)
		for pair := jsonSchema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			name := pair.Key
			prop := pair.Value
			if prop != nil {
				schema.Properties[name] = ga.convertJSONSchemaToGenAI(prop)
			}
		}
	}

	if jsonSchema.Required != nil {
		var validRequired []string
		for _, req := range jsonSchema.Required {
			if req != "" {
				validRequired = append(validRequired, req)
			}
		}
		if len(validRequired) > 0 {
			schema.Required = validRequired
		}
	}

	if jsonSchema.Items != nil {
		schema.Items = ga.convertJSONSchemaToGenAI(jsonSchema.Items)
	}

	if jsonSchema.Enum != nil {
		for _, enumVal := range jsonSchema.Enum {
			if strVal, ok := enumVal.(string); ok && strVal != "" {
				schema.Enum = append(schema.Enum, strVal)
			}
		}
	}

	return schema
}

func mustMarshalJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal JSON: %v", err))
	}
	return string(b)
}

func (ga *GeminiApi) toolCallFromGenAI(ctx context.Context, funcCall *genai.FunctionCall) (*types.ToolCall, error) {
	id := uuid.New().String()
	return &types.ToolCall{
		Id:        id,
		Name:      funcCall.Name,
		Arguments: funcCall.Args,
	}, nil
}

func GenerateSchemaFromReflectType(t reflect.Type) interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	schema := reflector.ReflectFromType(t)
	return schema
}
