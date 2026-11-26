package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/invopop/jsonschema"
	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared/constant"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"log/slog"
	"net/http"
	"reflect"
	"github.com/wuesty/llmmesh/llm"
	"github.com/wuesty/llmmesh/streaming"
	"github.com/wuesty/llmmesh/types"
	"github.com/wuesty/llmmesh/utils"
	"github.com/wuesty/llmmesh/config"
)

// OpenAIApi	openai api struct
type OpenAIApi struct {
	llm.BaseLLMApi
	client openaisdk.Client
}

// NewOpenAIAPI function
func NewOpenAIAPI() llm.LLMApi {
	httpClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport, otelhttp.WithSpanNameFormatter(
			func(operation string, r *http.Request) string {
				return fmt.Sprintf("openai %s", r.URL.Path)
			})),
	}
	client := openaisdk.NewClient(
		option.WithAPIKey(config.LocalConfig.OpenAI.ApiKey),
		option.WithHTTPClient(httpClient),
	)
	return &OpenAIApi{
		client: client,
	}
}

// Chat Function for OpenAI API Chat
func (oa OpenAIApi) Chat(ctx context.Context, request *llm.AIRequest) (*llm.AIResponse, error) {
	traceProvider := otel.GetTracerProvider()
	tracer := traceProvider.Tracer("")
	ctx, span := tracer.Start(ctx, "OpenAI Chat.Completions")
	defer span.End()

	//	Build up the Messages that need to go to the model:
	baseMessages := make([]openaisdk.ChatCompletionMessageParamUnion, 0)

	//	Add the Prompt as a System Messages
	baseMessages = append(baseMessages, openaisdk.SystemMessage(request.Prompt))

	//	The first thing we do is put in the conversation history
	messages := request.ConversationMessages
	// Iterate in reverse
	for i := len(messages) - 1; i >= 0; i-- {
		conversationEntry := messages[i]
		baseMessages = append(baseMessages, openaisdk.UserMessage(conversationEntry.UserMessage))
		baseMessages = append(baseMessages, openaisdk.AssistantMessage(conversationEntry.AssistantMessage))
	}
	//	Add any User Messages
	for _, uMsg := range request.UserMessages {
		baseMessages = append(baseMessages, openaisdk.UserMessage(uMsg.Content))
	}

	//	Append any Assistant Message
	for _, aMsg := range request.AssistantMessages {
		baseMessages = append(baseMessages, openaisdk.AssistantMessage(aMsg.Content))
	}

	// TO DO add the Conversation

	//	ADd the Tool Call, Tool Result Pairs
	for _, completedToolCall := range request.ToolCalls {
		//Manually construct a message with just this one tool call
		singleToolCallMsg := openaisdk.ChatCompletionMessageParamUnion{
			OfAssistant: &openaisdk.ChatCompletionAssistantMessageParam{
				Role: "assistant",
				ToolCalls: []openaisdk.ChatCompletionMessageToolCallParam{
					{
						ID:   completedToolCall.ID,
						Type: constant.Function(completedToolCall.Type),
						Function: openaisdk.ChatCompletionMessageToolCallFunctionParam{
							Name:      completedToolCall.FunctionName,
							Arguments: completedToolCall.FunctionArguments,
						},
					},
				},
			},
		}

		// Add the corresponding tool result
		toolResult := request.GetToolResult(completedToolCall.ID)
		if toolResult != nil {
			baseMessages = append(baseMessages, singleToolCallMsg)
			baseMessages = append(baseMessages, openaisdk.ToolMessage(toolResult.Content, toolResult.Id))

			//baseMessages = append(baseMessages, openaisdk.AssistantMessage(toolResult.Content))
		} else {
			slog.Warn("Tool Result Not Found", slog.String("Tool Id", completedToolCall.ID))
		}
	}

	//	Get the Tool definition List
	toolDefinitionList, err := oa.buildToolList(request)
	if err != nil {
		return nil, err
	}

	//	Create New Params
	params := openaisdk.ChatCompletionNewParams{
		Messages: baseMessages,
		Model:    config.LocalConfig.OpenAI.DefaultLLM,
		Tools:    toolDefinitionList,
	}

	if config.LocalConfig.OpenAI.DefaultLLM != "o4-mini" {
		if config.LocalConfig.OpenAI.DefaultLLM == "gpt-5-mini" {
			params.Temperature = openaisdk.Float(1.0)
		} else {
			params.Temperature = openaisdk.Float(0.2)
		}
	}

	//	Add the strict JSON Response format if it exists
	if request.JSONResponseFormat != nil {
		var responseSchema = GenerateSchemaFromReflectType(request.JSONResponseFormat.Schema)
		schemaParam := openaisdk.ResponseFormatJSONSchemaJSONSchemaParam{
			Name:        request.JSONResponseFormat.Name,
			Description: openaisdk.String(request.JSONResponseFormat.Description),
			Schema:      responseSchema,
			Strict:      openaisdk.Bool(request.JSONResponseFormat.Strict),
		}
		params.ResponseFormat = openaisdk.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openaisdk.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		}
	}

	response := llm.NewAIResponse()

	//	Make a chat completion call to OpenAI
	chatCompletion, err := oa.client.Chat.Completions.New(ctx, params)
	if err != nil {
		//	Be more graceful about a context being cancelled
		if utils.IsContextCanceled(err) {
			slog.Warn("Call to Open AI Cancelled - Context Canceled")
			response.Content = ""
			response.Status = llm.Canceled
			return response, nil
		}
		slog.ErrorContext(ctx, "Error occurred making completion sdk call", slog.Any("error", err))
		return nil, err
	}

	span.SetAttributes(attribute.String("Model", chatCompletion.Model))
	span.SetAttributes(attribute.Int("PromptTokens", int(chatCompletion.Usage.PromptTokens)))
	span.SetAttributes(attribute.Int("TotalTokens", int(chatCompletion.Usage.TotalTokens)))

	//	Check to see if any tools were specified.
	toolCalls := chatCompletion.Choices[0].Message.ToolCalls

	//	Bail if we have no tool calls
	if toolCalls == nil || len(toolCalls) == 0 {
		response.Content = chatCompletion.Choices[0].Message.Content
		response.Status = llm.Done
		return response, nil
	}
	//	WUEST:   WE SHOULD ONLY DO THIS IF THE AGENT HAS TOOLS TO CALL -->  IF
	//			 THE LLM IS HALLUCINATING THINKING IT HAS TO CALL TOOLS AND THERE ARE NO
	//			 TOOLS THAT IS A SIGNIFICANT PROBLEM :)
	if len(request.ToolsNames) > 0 {

		//	Add Tool Call to the Request - so next loop it gets appended.
		for _, toolCall := range toolCalls {
			request.AddToolCall(llm.ToolCall{
				ID:                toolCall.ID,
				Type:              string(toolCall.Type),
				FunctionName:      toolCall.Function.Name,
				FunctionArguments: toolCall.Function.Arguments,
			})
			//	Convert the tool call from open ai spec to generic tool call.
			genericToolCall, err := oa.toolCallFromOpenAI(ctx, toolCall.ID, toolCall.Function.Name, toolCall.Function.Arguments)
			if err != nil {
				return nil, err
			}
			response.AddToolCall(*genericToolCall)
			response.Status = llm.Tools
		}
		return response, nil
	}

	//	If we got here it's not in a good spot.
	slog.WarnContext(ctx, "Tool Calls found for Agent that does not have any tools", slog.Any("toolCalls", toolCalls))
	response.Content = chatCompletion.Choices[0].Message.Content
	response.Status = llm.Done
	return response, nil

}

// Stream	makes a streaming call to open ai
func (oa OpenAIApi) Stream(ctx context.Context, request *llm.AIRequest, streamer streaming.Streamer) (*llm.AIResponse, error) {

	traceProvider := otel.GetTracerProvider()
	tracer := traceProvider.Tracer("")
	ctx, span := tracer.Start(ctx, "OpenAI Chat.Completions (Stream)")
	defer span.End()

	//	Build up the Messages that need to go to the model:
	baseMessages := make([]openaisdk.ChatCompletionMessageParamUnion, 0)

	//	Add the Prompt as a System Messages
	baseMessages = append(baseMessages, openaisdk.SystemMessage(request.Prompt))

	//	The first thing we do is put in the conversation history
	messages := request.ConversationMessages
	// Iterate in reverse
	for i := len(messages) - 1; i >= 0; i-- {
		conversationEntry := messages[i]
		baseMessages = append(baseMessages, openaisdk.UserMessage(conversationEntry.UserMessage))
		baseMessages = append(baseMessages, openaisdk.AssistantMessage(conversationEntry.AssistantMessage))
	}

	//	Add any User Messages
	for _, uMsg := range request.UserMessages {
		baseMessages = append(baseMessages, openaisdk.UserMessage(uMsg.Content))
	}

	//	Append any Assistant Message
	for _, aMsg := range request.AssistantMessages {
		baseMessages = append(baseMessages, openaisdk.AssistantMessage(aMsg.Content))
	}

	// TO DO add the Conversation

	//	ADd the Tool Call, Tool Result Pairs
	for _, completedToolCall := range request.ToolCalls {
		// Manually construct a message with just this one tool call
		singleToolCallMsg := openaisdk.ChatCompletionMessageParamUnion{
			OfAssistant: &openaisdk.ChatCompletionAssistantMessageParam{
				Role: "assistant",
				ToolCalls: []openaisdk.ChatCompletionMessageToolCallParam{
					{
						ID:   completedToolCall.ID,
						Type: constant.Function(completedToolCall.Type),
						Function: openaisdk.ChatCompletionMessageToolCallFunctionParam{
							Name:      completedToolCall.FunctionName,
							Arguments: completedToolCall.FunctionArguments,
						},
					},
				},
			},
		}

		// Add the corresponding tool result
		toolResult := request.GetToolResult(completedToolCall.ID)
		if toolResult != nil {
			baseMessages = append(baseMessages, singleToolCallMsg)
			baseMessages = append(baseMessages, openaisdk.ToolMessage(toolResult.Content, toolResult.Id))

		} else {
			slog.Warn("Tool Result Not Found", slog.String("Tool Id", completedToolCall.ID))
		}
	}

	//	Get the Tool definition List
	toolDefinitionList, err := oa.buildToolList(request)
	if err != nil {
		return nil, err
	}

	params := openaisdk.ChatCompletionNewParams{
		Messages: baseMessages,
		Model:    config.LocalConfig.OpenAI.DefaultLLM,
		Tools:    toolDefinitionList,
	}

	if config.LocalConfig.OpenAI.DefaultLLM != "o4-mini" {
		if config.LocalConfig.OpenAI.DefaultLLM == "gpt-5-mini" {
			params.Temperature = openaisdk.Float(1.0)
		} else {
			params.Temperature = openaisdk.Float(0.2)
		}
	}

	//	Make a chat completion call to OpenAI
	stream := oa.client.Chat.Completions.NewStreaming(ctx, params)
	acc := openaisdk.ChatCompletionAccumulator{}
	response := llm.NewAIResponse()

	for stream.Next() {

		chunk := stream.Current()

		acc.AddChunk(chunk)

		// When this fires, the current chunk value will not contain content data
		if content, ok := acc.JustFinishedContent(); ok {
			response.Content = content
			response.Status = llm.Done
			return response, nil
		}

		if refusal, ok := acc.JustFinishedRefusal(); ok {
			return nil, errors.New(refusal)
		}

		if toolCall, ok := acc.JustFinishedToolCall(); ok {
			//	Add Tool Call to the Request - so next loop it gets appended.
			request.AddToolCall(llm.ToolCall{
				ID:                toolCall.ID,
				Type:              "",
				FunctionName:      toolCall.Name,
				FunctionArguments: toolCall.Arguments,
			})
			//	Convert the tool call from open ai spec to generic tool call.
			genericToolCall, err := oa.toolCallFromOpenAI(ctx, toolCall.ID, toolCall.Name, toolCall.Arguments)
			if err != nil {
				return nil, err
			}
			response.AddToolCall(*genericToolCall)
			response.Status = llm.Tools
			return response, err
		}

		// It's best to use chunks after handling JustFinished events.
		// Here we print the delta of the content, if it exists.
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			streamer.Stream(streaming.NewOutputMessage(request.AgentRuntimeID, chunk.Choices[0].Delta.Content))
		}

	}

	slog.DebugContext(ctx, "Exited stream.Next() loop")

	// Check if the stream ended due to an error
	if err := stream.Err(); err != nil {
		// Be more graceful about a context being cancelled
		if utils.IsContextCanceled(err) {
			slog.Warn("OpenAI Stream Cancelled - Context Canceled")
			response.Content = ""
			response.Status = llm.Canceled
			return response, nil
		}
		slog.ErrorContext(ctx, "Error occurred in OpenAI stream", slog.Any("error", err))
		return nil, err
	}

	//span.SetAttributes(attribute.String("Model", chatCompletion.Model))
	//span.SetAttributes(attribute.Int("PromptTokens", int(chatCompletion.Usage.PromptTokens)))
	//span.SetAttributes(attribute.Int("TotalTokens", int(chatCompletion.Usage.TotalTokens)))

	return response, nil
}

// BuildToolList		Builds the tool list definitions in OpenAI Language
func (oa OpenAIApi) buildToolList(request *llm.AIRequest) ([]openaisdk.ChatCompletionToolParam, error) {

	toolList := make([]openaisdk.ChatCompletionToolParam, 0)

	for _, toolName := range request.ToolsNames {
		tool := request.GetAgentWorkflow().GetTool(toolName)
		if tool == nil {
			slog.Error("Tool Not Found", slog.String("ToolName", toolName))
			return nil, errors.New("tool not found")
		}
		toolList = append(toolList, tool.GetOpenAIDefinition())
	}

	return toolList, nil
}

// toolCallFromOpenAI	Converts an OpenAI completion to a generic tool call
func (oa OpenAIApi) toolCallFromOpenAI(ctx context.Context, id string, name string, arguments string) (*types.ToolCall, error) {
	var args map[string]interface{}
	err := json.Unmarshal([]byte(arguments), &args)
	if err != nil {

		slog.ErrorContext(ctx, "could not unmarshall tool call from openai", slog.Any("error", err))
		return nil, err
	}
	toolCall := types.ToolCall{
		Id:        id,
		Name:      name,
		Arguments: args,
	}
	return &toolCall, nil
}

func GenerateSchemaFromReflectType(t reflect.Type) interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	schema := reflector.ReflectFromType(t)
	return schema
}
