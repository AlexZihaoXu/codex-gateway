package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"codex-gateway/internal/codex"
	"codex-gateway/internal/projects"

	"github.com/danielgtaylor/huma/v2"
)

const projectAPIKeySecurityScheme = "ProjectAPIKey"

type Inference interface {
	Models(ctx context.Context) ([]codex.Model, error)
	Generate(ctx context.Context, request codex.GenerateRequest) (<-chan codex.GenerationEvent, error)
}

type projectAuthorizationInput struct {
	Authorization string `header:"Authorization" doc:"Project API key using the Bearer scheme" example:"Bearer cg_example"`
}

type modelBody struct {
	ID      string `json:"id" doc:"Model identifier accepted by Codex" example:"gpt-5.4"`
	Object  string `json:"object" enum:"model"`
	Created int64  `json:"created" minimum:"0" doc:"Unix creation timestamp; zero when Codex does not provide one"`
	OwnedBy string `json:"owned_by" doc:"Gateway model owner" example:"codex-gateway"`
}

type modelListBody struct {
	Object string      `json:"object" enum:"list"`
	Data   []modelBody `json:"data"`
}

type modelListOutput struct{ Body modelListBody }

type createResponseInput struct {
	Authorization string `header:"Authorization" doc:"Project API key using the Bearer scheme" example:"Bearer cg_example"`
	Body          struct {
		Model        string `json:"model" minLength:"1" maxLength:"128" doc:"Codex model identifier from GET /v1/models"`
		Input        string `json:"input" minLength:"1" maxLength:"1048576" doc:"Text input for the model"`
		Instructions string `json:"instructions,omitempty" maxLength:"65536" doc:"Additional response instructions"`
		Reasoning    *struct {
			Effort string `json:"effort" enum:"none,low,medium,high,xhigh,max,ultra" doc:"Reasoning effort passed to Codex"`
		} `json:"reasoning,omitempty"`
		Stream bool `json:"stream,omitempty" doc:"Stream typed server-sent events when true"`
	}
}

type responseTextContent struct {
	Type        string               `json:"type" enum:"output_text"`
	Text        string               `json:"text"`
	Annotations []responseAnnotation `json:"annotations"`
	Logprobs    []responseLogprob    `json:"logprobs"`
}

type responseAnnotation struct{}
type responseLogprob struct{}

type responseOutputItem struct {
	ID      string                `json:"id"`
	Type    string                `json:"type" enum:"message"`
	Status  string                `json:"status" enum:"in_progress,completed"`
	Role    string                `json:"role" enum:"assistant"`
	Content []responseTextContent `json:"content"`
}

type responseInputTokenDetails struct {
	CachedTokens int64 `json:"cached_tokens" minimum:"0"`
}

type responseOutputTokenDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens" minimum:"0"`
}

type responseUsage struct {
	InputTokens        int64                      `json:"input_tokens" minimum:"0"`
	InputTokenDetails  responseInputTokenDetails  `json:"input_tokens_details"`
	OutputTokens       int64                      `json:"output_tokens" minimum:"0"`
	OutputTokenDetails responseOutputTokenDetails `json:"output_tokens_details"`
	TotalTokens        int64                      `json:"total_tokens" minimum:"0"`
}

type responseBody struct {
	ID        string               `json:"id" doc:"Response identifier" example:"resp_example"`
	Object    string               `json:"object" enum:"response"`
	CreatedAt int64                `json:"created_at" minimum:"0" doc:"Unix creation timestamp"`
	Status    string               `json:"status" enum:"in_progress,completed,failed"`
	Model     string               `json:"model"`
	Output    []responseOutputItem `json:"output"`
	Usage     responseUsage        `json:"usage"`
}

type createChatCompletionInput struct {
	Authorization string `header:"Authorization" doc:"Project API key using the Bearer scheme" example:"Bearer cg_example"`
	Body          struct {
		Model           string        `json:"model" minLength:"1" maxLength:"128" doc:"Codex model identifier from GET /v1/models"`
		Messages        []chatMessage `json:"messages" minItems:"1" maxItems:"1024"`
		ReasoningEffort string        `json:"reasoning_effort,omitempty" enum:"none,low,medium,high,xhigh,max,ultra" doc:"Reasoning effort passed to Codex"`
		Stream          bool          `json:"stream,omitempty"`
		StreamOptions   *struct {
			IncludeUsage bool `json:"include_usage,omitempty"`
		} `json:"stream_options,omitempty"`
	}
}

type chatMessage struct {
	Role    string `json:"role" enum:"system,developer,user,assistant"`
	Content string `json:"content" minLength:"1" maxLength:"1048576"`
}

type chatAssistantMessage struct {
	Role    string `json:"role" enum:"assistant"`
	Content string `json:"content"`
}

type chatChoice struct {
	Index        int                  `json:"index" minimum:"0"`
	Message      chatAssistantMessage `json:"message"`
	FinishReason string               `json:"finish_reason" enum:"stop"`
}

type chatUsage struct {
	PromptTokens           int64                      `json:"prompt_tokens" minimum:"0"`
	PromptTokenDetails     chatPromptTokenDetails     `json:"prompt_tokens_details"`
	CompletionTokens       int64                      `json:"completion_tokens" minimum:"0"`
	CompletionTokenDetails chatCompletionTokenDetails `json:"completion_tokens_details"`
	TotalTokens            int64                      `json:"total_tokens" minimum:"0"`
}

type chatPromptTokenDetails struct {
	CachedTokens int64 `json:"cached_tokens" minimum:"0"`
}

type chatCompletionTokenDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens" minimum:"0"`
}

type chatCompletionBody struct {
	ID      string       `json:"id"`
	Object  string       `json:"object" enum:"chat.completion"`
	Created int64        `json:"created" minimum:"0"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

func registerOpenAICompatibility(api huma.API, projectService ProjectService, inference Inference, clock func() time.Time) {
	if api.OpenAPI().Components.SecuritySchemes == nil {
		api.OpenAPI().Components.SecuritySchemes = make(map[string]*huma.SecurityScheme)
	}
	api.OpenAPI().Components.SecuritySchemes[projectAPIKeySecurityScheme] = &huma.SecurityScheme{
		Type: "http", Scheme: "bearer", BearerFormat: "Codex Gateway project API key",
	}
	security := []map[string][]string{{projectAPIKeySecurityScheme: {}}}

	huma.Register(api, huma.Operation{
		OperationID: "list-models", Method: http.MethodGet, Path: "/v1/models",
		Summary: "List Codex models available through this gateway.", Tags: []string{"OpenAI compatibility"},
		Security: security,
	}, func(ctx context.Context, input *projectAuthorizationInput) (*modelListOutput, error) {
		if _, err := requireProjectAPIKey(ctx, projectService, input.Authorization); err != nil {
			return nil, err
		}
		models, err := inference.Models(ctx)
		if err != nil {
			return nil, codexUnavailable()
		}
		body := modelListBody{Object: "list", Data: make([]modelBody, 0, len(models))}
		for _, model := range models {
			body.Data = append(body.Data, modelBody{ID: model.ID, Object: "model", Created: 0, OwnedBy: "codex-gateway"})
		}
		return &modelListOutput{Body: body}, nil
	})

	responseSchema := huma.SchemaFromType(api.OpenAPI().Components.Schemas, reflect.TypeOf(responseBody{}))
	huma.Register(api, huma.Operation{
		OperationID: "create-response", Method: http.MethodPost, Path: "/v1/responses",
		Summary: "Generate text using an ephemeral, read-only Codex turn.", Tags: []string{"OpenAI compatibility"},
		Security: security,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "A completed JSON response or typed server-sent event stream.",
				Content: map[string]*huma.MediaType{
					"application/json":  {Schema: responseSchema},
					"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeString}},
				},
			},
		},
	}, func(ctx context.Context, input *createResponseInput) (*huma.StreamResponse, error) {
		access, err := requireProjectAPIKey(ctx, projectService, input.Authorization)
		if err != nil {
			return nil, err
		}
		createdAt := clock().UTC().Unix()
		reasoningEffort := ""
		if input.Body.Reasoning != nil {
			reasoningEffort = input.Body.Reasoning.Effort
		}
		events, err := inference.Generate(ctx, codex.GenerateRequest{
			Model: input.Body.Model, Input: input.Body.Input, Instructions: input.Body.Instructions,
			ReasoningEffort: reasoningEffort,
		})
		if err != nil {
			return nil, codexUnavailable()
		}
		if input.Body.Stream {
			return streamResponse(ctx, events, projectService, access, input.Body.Model, createdAt, false, nil), nil
		}
		result, err := collectGeneration(events)
		if err != nil {
			return nil, codexUnavailable()
		}
		body := completedResponse(result, input.Body.Model, createdAt)
		if err := projectService.RecordUsage(ctx, usageRecord(access, body.ID, result)); err != nil {
			return nil, huma.Error500InternalServerError("Unable to record response usage.")
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to encode the response.")
		}
		return &huma.StreamResponse{Body: func(streamContext huma.Context) {
			streamContext.SetHeader("Content-Type", "application/json")
			_, _ = streamContext.BodyWriter().Write(payload)
		}}, nil
	})

	chatSchema := huma.SchemaFromType(api.OpenAPI().Components.Schemas, reflect.TypeOf(chatCompletionBody{}))
	huma.Register(api, huma.Operation{
		OperationID: "create-chat-completion", Method: http.MethodPost, Path: "/v1/chat/completions",
		Summary: "Create a text chat completion using an ephemeral, read-only Codex turn.", Tags: []string{"OpenAI compatibility"},
		Security: security,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "A completed chat completion or server-sent event stream of chunks.",
				Content: map[string]*huma.MediaType{
					"application/json":  {Schema: chatSchema},
					"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeString}},
				},
			},
		},
	}, func(ctx context.Context, input *createChatCompletionInput) (*huma.StreamResponse, error) {
		access, err := requireProjectAPIKey(ctx, projectService, input.Authorization)
		if err != nil {
			return nil, err
		}
		prompt, instructions, err := buildChatPrompt(input.Body.Messages)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		createdAt := clock().UTC().Unix()
		events, err := inference.Generate(ctx, codex.GenerateRequest{
			Model: input.Body.Model, Input: prompt, Instructions: instructions,
			ReasoningEffort: input.Body.ReasoningEffort, Messages: codexConversation(input.Body.Messages),
		})
		if err != nil {
			return nil, codexUnavailable()
		}
		if input.Body.Stream {
			includeUsage := input.Body.StreamOptions != nil && input.Body.StreamOptions.IncludeUsage
			return streamChatCompletion(ctx, events, projectService, access, input.Body.Model, createdAt, includeUsage, false, nil), nil
		}
		result, err := collectGeneration(events)
		if err != nil {
			return nil, codexUnavailable()
		}
		body := completedChatCompletion(result, input.Body.Model, createdAt)
		if err := projectService.RecordUsage(ctx, usageRecord(access, body.ID, result)); err != nil {
			return nil, huma.Error500InternalServerError("Unable to record chat completion usage.")
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to encode the chat completion.")
		}
		return &huma.StreamResponse{Body: func(streamContext huma.Context) {
			streamContext.SetHeader("Content-Type", "application/json")
			_, _ = streamContext.BodyWriter().Write(payload)
		}}, nil
	})
}

func requireProjectAPIKey(ctx context.Context, service ProjectService, authorization string) (projects.ProjectAccess, error) {
	scheme, secret, ok := strings.Cut(strings.TrimSpace(authorization), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || secret == "" || strings.ContainsAny(secret, " \t\r\n") {
		return projects.ProjectAccess{}, huma.Error401Unauthorized("A valid project API key is required.")
	}
	access, err := service.AuthenticateAPIKey(ctx, secret)
	switch {
	case errors.Is(err, projects.ErrInvalidAPIKey):
		return projects.ProjectAccess{}, huma.Error401Unauthorized("A valid project API key is required.")
	case errors.Is(err, projects.ErrTokenLimitExceeded):
		return projects.ProjectAccess{}, huma.Error429TooManyRequests("A project usage limit has been reached.")
	case err != nil:
		return projects.ProjectAccess{}, huma.Error500InternalServerError("Unable to verify the project API key.")
	default:
		return access, nil
	}
}

func collectGeneration(events <-chan codex.GenerationEvent) (codex.GenerationResult, error) {
	for event := range events {
		if event.Err != nil {
			return codex.GenerationResult{}, event.Err
		}
		if event.Type == codex.GenerationCompleted && event.Result != nil {
			return *event.Result, nil
		}
	}
	return codex.GenerationResult{}, errors.New("Codex generation ended without a completed response")
}

func completedResponse(result codex.GenerationResult, requestedModel string, createdAt int64) responseBody {
	model := result.Model
	if model == "" {
		model = requestedModel
	}
	responseID := "resp_" + result.ID
	return responseBody{
		ID: responseID, Object: "response", CreatedAt: createdAt, Status: "completed", Model: model,
		Output: []responseOutputItem{{
			ID: "msg_" + result.ID, Type: "message", Status: "completed", Role: "assistant",
			Content: []responseTextContent{{
				Type: "output_text", Text: result.Text,
				Annotations: make([]responseAnnotation, 0), Logprobs: make([]responseLogprob, 0),
			}},
		}},
		Usage: responseUsage{
			InputTokens: result.InputTokens, InputTokenDetails: responseInputTokenDetails{CachedTokens: result.CachedInputTokens},
			OutputTokens: result.OutputTokens, OutputTokenDetails: responseOutputTokenDetails{ReasoningTokens: result.ReasoningOutputTokens},
			TotalTokens: generationTotalTokens(result),
		},
	}
}

func buildChatPrompt(messages []chatMessage) (string, string, error) {
	instructions := make([]string, 0)
	conversation := make([]string, 0)
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			return "", "", errors.New("chat messages must contain text")
		}
		switch message.Role {
		case "system", "developer":
			instructions = append(instructions, content)
		case "user":
			conversation = append(conversation, "User: "+content)
		case "assistant":
			conversation = append(conversation, "Assistant: "+content)
		default:
			return "", "", errors.New("unsupported chat message role")
		}
	}
	if len(conversation) == 0 {
		return "", "", errors.New("at least one user or assistant message is required")
	}
	return strings.Join(conversation, "\n\n"), strings.Join(instructions, "\n\n"), nil
}

func codexConversation(messages []chatMessage) []codex.Message {
	conversation := make([]codex.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		conversation = append(conversation, codex.Message{Role: message.Role, Content: strings.TrimSpace(message.Content)})
	}
	return conversation
}

func completedChatCompletion(result codex.GenerationResult, requestedModel string, createdAt int64) chatCompletionBody {
	model := result.Model
	if model == "" {
		model = requestedModel
	}
	return chatCompletionBody{
		ID: "chatcmpl_" + result.ID, Object: "chat.completion", Created: createdAt, Model: model,
		Choices: []chatChoice{{
			Index: 0, Message: chatAssistantMessage{Role: "assistant", Content: result.Text}, FinishReason: "stop",
		}},
		Usage: chatUsage{
			PromptTokens: result.InputTokens, PromptTokenDetails: chatPromptTokenDetails{CachedTokens: result.CachedInputTokens},
			CompletionTokens: result.OutputTokens, CompletionTokenDetails: chatCompletionTokenDetails{ReasoningTokens: result.ReasoningOutputTokens},
			TotalTokens: generationTotalTokens(result),
		},
	}
}

func usageRecord(access projects.ProjectAccess, requestID string, result codex.GenerationResult) projects.UsageRecord {
	return projects.UsageRecord{
		RequestID: requestID, ProjectID: access.ProjectID, APIKeyID: access.APIKeyID,
		InputTokens: result.InputTokens, CachedInputTokens: result.CachedInputTokens,
		OutputTokens: result.OutputTokens, ReasoningOutputTokens: result.ReasoningOutputTokens,
		TotalTokens: generationTotalTokens(result),
	}
}

func generationTotalTokens(result codex.GenerationResult) int64 {
	return result.InputTokens + result.OutputTokens
}

func streamResponse(
	requestContext context.Context,
	events <-chan codex.GenerationEvent,
	projectService ProjectService,
	access projects.ProjectAccess,
	model string,
	createdAt int64,
	exposeReasoning bool,
	onCompleted func(codex.GenerationResult) error,
) *huma.StreamResponse {
	return &huma.StreamResponse{Body: func(streamContext huma.Context) {
		streamContext.SetHeader("Content-Type", "text/event-stream")
		streamContext.SetHeader("Cache-Control", "no-cache")
		streamContext.SetHeader("X-Accel-Buffering", "no")
		writer := streamContext.BodyWriter()
		sequence := int64(0)
		responseID := ""
		messageID := ""
		text := ""
		for event := range events {
			if event.Err != nil {
				writeSSE(writer, "response.failed", map[string]any{
					"type": "response.failed", "sequence_number": sequence,
					"response": map[string]any{"id": responseID, "object": "response", "status": "failed"},
				})
				return
			}
			switch event.Type {
			case codex.GenerationStarted:
				responseID = "resp_" + event.ID
				messageID = "msg_" + event.ID
				inProgress := responseBody{
					ID: responseID, Object: "response", CreatedAt: createdAt, Status: "in_progress", Model: model,
					Output: make([]responseOutputItem, 0), Usage: responseUsage{},
				}
				writeSSE(writer, "response.created", map[string]any{"type": "response.created", "sequence_number": sequence, "response": inProgress})
				sequence++
				writeSSE(writer, "response.in_progress", map[string]any{"type": "response.in_progress", "sequence_number": sequence, "response": inProgress})
				sequence++
				writeSSE(writer, "response.output_item.added", map[string]any{
					"type": "response.output_item.added", "sequence_number": sequence, "output_index": 0,
					"item": responseOutputItem{ID: messageID, Type: "message", Status: "in_progress", Role: "assistant", Content: make([]responseTextContent, 0)},
				})
				sequence++
				writeSSE(writer, "response.content_part.added", map[string]any{
					"type": "response.content_part.added", "sequence_number": sequence,
					"item_id": messageID, "output_index": 0, "content_index": 0,
					"part": responseTextContent{
						Type: "output_text", Text: "", Annotations: []responseAnnotation{}, Logprobs: []responseLogprob{},
					},
				})
				sequence++
			case codex.GenerationDelta:
				text += event.Delta
				writeSSE(writer, "response.output_text.delta", map[string]any{
					"type": "response.output_text.delta", "sequence_number": sequence,
					"item_id": messageID, "output_index": 0, "content_index": 0,
					"delta": event.Delta, "logprobs": []responseLogprob{},
				})
				sequence++
			case codex.GenerationReasoningDelta:
				if exposeReasoning {
					writeSSE(writer, "response.reasoning_summary_text.delta", map[string]any{
						"type": "response.reasoning_summary_text.delta", "sequence_number": sequence,
						"item_id": "rs_" + event.ID, "output_index": 0, "summary_index": 0,
						"delta": event.Delta,
					})
					sequence++
				}
			case codex.GenerationCompleted:
				if event.Result == nil {
					return
				}
				body := completedResponse(*event.Result, model, createdAt)
				if err := projectService.RecordUsage(requestContext, usageRecord(access, body.ID, *event.Result)); err != nil {
					writeSSE(writer, "response.failed", map[string]any{
						"type": "response.failed", "sequence_number": sequence,
						"response": map[string]any{"id": body.ID, "object": "response", "status": "failed"},
					})
					return
				}
				if onCompleted != nil {
					if err := onCompleted(*event.Result); err != nil {
						writeSSE(writer, "response.failed", map[string]any{
							"type": "response.failed", "sequence_number": sequence,
							"response": map[string]any{"id": body.ID, "object": "response", "status": "failed"},
						})
						return
					}
				}
				writeSSE(writer, "response.output_text.done", map[string]any{
					"type": "response.output_text.done", "sequence_number": sequence,
					"item_id": messageID, "output_index": 0, "content_index": 0, "text": text,
				})
				sequence++
				writeSSE(writer, "response.content_part.done", map[string]any{
					"type": "response.content_part.done", "sequence_number": sequence,
					"item_id": messageID, "output_index": 0, "content_index": 0,
					"part": body.Output[0].Content[0],
				})
				sequence++
				writeSSE(writer, "response.output_item.done", map[string]any{
					"type": "response.output_item.done", "sequence_number": sequence,
					"output_index": 0, "item": body.Output[0],
				})
				sequence++
				writeSSE(writer, "response.completed", map[string]any{
					"type": "response.completed", "sequence_number": sequence, "response": body,
				})
				return
			}
		}
	}}
}

func streamChatCompletion(
	requestContext context.Context,
	events <-chan codex.GenerationEvent,
	projectService ProjectService,
	access projects.ProjectAccess,
	model string,
	createdAt int64,
	includeUsage bool,
	exposeReasoning bool,
	onCompleted func(codex.GenerationResult) error,
) *huma.StreamResponse {
	return &huma.StreamResponse{Body: func(streamContext huma.Context) {
		streamContext.SetHeader("Content-Type", "text/event-stream")
		streamContext.SetHeader("Cache-Control", "no-cache")
		streamContext.SetHeader("X-Accel-Buffering", "no")
		writer := streamContext.BodyWriter()
		completionID := ""
		for event := range events {
			if event.Err != nil {
				writeChatSSE(writer, map[string]any{
					"error": map[string]any{"message": "Codex could not complete the chat request.", "type": "server_error"},
				})
				writeChatDone(writer)
				return
			}
			switch event.Type {
			case codex.GenerationStarted:
				completionID = "chatcmpl_" + event.ID
				writeChatSSE(writer, map[string]any{
					"id": completionID, "object": "chat.completion.chunk", "created": createdAt, "model": model,
					"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": ""}, "finish_reason": nil}},
				})
			case codex.GenerationDelta:
				writeChatSSE(writer, map[string]any{
					"id": completionID, "object": "chat.completion.chunk", "created": createdAt, "model": model,
					"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": event.Delta}, "finish_reason": nil}},
				})
			case codex.GenerationReasoningDelta:
				if exposeReasoning {
					writeChatSSE(writer, map[string]any{
						"id": completionID, "object": "chat.completion.chunk", "created": createdAt, "model": model,
						"choices": []map[string]any{{"index": 0, "delta": map[string]any{"reasoning_content": event.Delta}, "finish_reason": nil}},
					})
				}
			case codex.GenerationCompleted:
				if event.Result == nil {
					return
				}
				body := completedChatCompletion(*event.Result, model, createdAt)
				if err := projectService.RecordUsage(requestContext, usageRecord(access, body.ID, *event.Result)); err != nil {
					writeChatSSE(writer, map[string]any{
						"error": map[string]any{"message": "Codex Gateway could not record usage.", "type": "server_error"},
					})
					writeChatDone(writer)
					return
				}
				if onCompleted != nil {
					if err := onCompleted(*event.Result); err != nil {
						writeChatSSE(writer, map[string]any{
							"error": map[string]any{"message": "Codex Gateway could not persist chat history.", "type": "server_error"},
						})
						writeChatDone(writer)
						return
					}
				}
				writeChatSSE(writer, map[string]any{
					"id": body.ID, "object": "chat.completion.chunk", "created": createdAt, "model": body.Model,
					"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
				})
				if includeUsage {
					writeChatSSE(writer, map[string]any{
						"id": body.ID, "object": "chat.completion.chunk", "created": createdAt, "model": body.Model,
						"choices": []any{}, "usage": body.Usage,
					})
				}
				writeChatDone(writer)
				return
			}
		}
	}}
}

func writeSSE(writer io.Writer, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, payload)
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeChatSSE(writer io.Writer, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(writer, "data: %s\n\n", payload)
	flushWriter(writer)
}

func writeChatDone(writer io.Writer) {
	_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	flushWriter(writer)
}

func flushWriter(writer io.Writer) {
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
}
