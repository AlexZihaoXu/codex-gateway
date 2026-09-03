package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"time"

	"codex-gateway/internal/chats"
	"codex-gateway/internal/codex"
	"codex-gateway/internal/projects"

	"github.com/danielgtaylor/huma/v2"
)

type ChatHistory interface {
	ListSessions(ctx context.Context, owner string) ([]chats.Session, error)
	SaveSession(ctx context.Context, owner string, session chats.Session) error
	DeleteSession(ctx context.Context, owner, id string) error
	RenameSession(ctx context.Context, owner, id, title string) error
}

type dashboardChatRequestBody struct {
	Endpoint        string        `json:"endpoint" enum:"chat-completions,responses" doc:"Compatibility event format to exercise"`
	SessionID       string        `json:"session_id" minLength:"1" maxLength:"128" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]*$" doc:"Opaque dashboard conversation identifier"`
	Title           string        `json:"title,omitempty" maxLength:"200" doc:"Existing persisted title to preserve after a rename"`
	ProjectID       string        `json:"project_id,omitempty" maxLength:"128" doc:"Optional project that receives usage and limit enforcement; omitted requests use the built-in Dashboard Chat usage bucket"`
	Model           string        `json:"model" minLength:"1" maxLength:"128" doc:"Codex model identifier from GET /api/chat/models"`
	Messages        []chatMessage `json:"messages" minItems:"1" maxItems:"1024"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty" enum:"none,low,medium,high,xhigh,max,ultra" doc:"Reasoning effort passed to Codex"`
	RestartSession  bool          `json:"restart_session,omitempty" doc:"Reset the in-memory Codex continuation before replaying edited chat history"`
}

type dashboardChatInput struct {
	SessionToken string `cookie:"codex_gateway_session"`
	Body         dashboardChatRequestBody
}

type dashboardChatSessionInput struct {
	SessionToken string `cookie:"codex_gateway_session"`
	ID           string `path:"id" minLength:"1" maxLength:"128" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]*$" doc:"Dashboard chat session identifier"`
}

type dashboardChatRenameBody struct {
	Title string `json:"title" minLength:"1" maxLength:"200" doc:"New chat title"`
}

type dashboardChatRenameInput struct {
	SessionToken string `cookie:"codex_gateway_session"`
	ID           string `path:"id" minLength:"1" maxLength:"128" pattern:"^[A-Za-z0-9][A-Za-z0-9._:-]*$" doc:"Dashboard chat session identifier"`
	Body         dashboardChatRenameBody
}

type dashboardChatUsageBody struct {
	InputTokens           int64 `json:"input_tokens" minimum:"0"`
	CachedInputTokens     int64 `json:"cached_input_tokens" minimum:"0"`
	OutputTokens          int64 `json:"output_tokens" minimum:"0"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens" minimum:"0"`
	TotalTokens           int64 `json:"total_tokens" minimum:"0"`
}

type dashboardChatMessageBody struct {
	Role      string                  `json:"role" enum:"user,assistant"`
	Content   string                  `json:"content"`
	Reasoning string                  `json:"reasoning,omitempty" doc:"Visible reasoning summary returned by Codex"`
	Usage     *dashboardChatUsageBody `json:"usage,omitempty"`
}

type dashboardChatSessionBody struct {
	ID              string                     `json:"id"`
	Title           string                     `json:"title"`
	Model           string                     `json:"model"`
	ProjectID       string                     `json:"project_id" doc:"Persisted project identifier; empty selects the built-in Dashboard Chat bucket"`
	Endpoint        string                     `json:"endpoint" enum:"chat-completions,responses"`
	ReasoningEffort string                     `json:"reasoning_effort" enum:"none,low,medium,high,xhigh,max,ultra"`
	Instructions    string                     `json:"instructions"`
	Messages        []dashboardChatMessageBody `json:"messages"`
	CreatedAt       time.Time                  `json:"created_at" format:"date-time"`
	UpdatedAt       time.Time                  `json:"updated_at" format:"date-time"`
}

type dashboardChatSessionListBody struct {
	Sessions []dashboardChatSessionBody `json:"sessions" doc:"Persisted chats ordered by most recent activity"`
}

type dashboardChatSessionListOutput struct{ Body dashboardChatSessionListBody }
type noContentOutput struct{}

func registerDashboardChat(
	api huma.API,
	authentication Authentication,
	projectService ProjectService,
	history ChatHistory,
	inference Inference,
	clock func() time.Time,
) {
	huma.Register(api, huma.Operation{
		OperationID: "list-dashboard-chat-sessions", Method: http.MethodGet, Path: "/api/chat/sessions",
		Summary: "List the authenticated user's persisted dashboard chats.", Tags: []string{"Dashboard chat"},
	}, func(ctx context.Context, input *authenticationCookieInput) (*dashboardChatSessionListOutput, error) {
		status, err := administratorStatus(ctx, authentication, input.SessionToken)
		if err != nil {
			return nil, err
		}
		sessions, err := history.ListSessions(ctx, status.Email)
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to read dashboard chat history.")
		}
		body := dashboardChatSessionListBody{Sessions: make([]dashboardChatSessionBody, 0, len(sessions))}
		for _, session := range sessions {
			body.Sessions = append(body.Sessions, dashboardChatSessionResponse(session))
		}
		return &dashboardChatSessionListOutput{Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rename-dashboard-chat-session", Method: http.MethodPatch, Path: "/api/chat/sessions/{id}",
		Summary: "Rename one persisted dashboard chat.", Tags: []string{"Dashboard chat"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *dashboardChatRenameInput) (*noContentOutput, error) {
		status, err := administratorStatus(ctx, authentication, input.SessionToken)
		if err != nil {
			return nil, err
		}
		title := strings.TrimSpace(input.Body.Title)
		if err := history.RenameSession(ctx, status.Email, input.ID, title); err != nil {
			switch {
			case errors.Is(err, chats.ErrSessionNotFound):
				return nil, huma.Error404NotFound("Dashboard chat was not found.")
			case errors.Is(err, chats.ErrInvalidSession):
				return nil, huma.Error422UnprocessableEntity("Chat title is invalid.")
			default:
				return nil, huma.Error500InternalServerError("Unable to rename dashboard chat.")
			}
		}
		return &noContentOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-dashboard-chat-session", Method: http.MethodDelete, Path: "/api/chat/sessions/{id}",
		Summary: "Delete one persisted dashboard chat.", Tags: []string{"Dashboard chat"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *dashboardChatSessionInput) (*noContentOutput, error) {
		status, err := administratorStatus(ctx, authentication, input.SessionToken)
		if err != nil {
			return nil, err
		}
		if err := history.DeleteSession(ctx, status.Email, input.ID); err != nil {
			if errors.Is(err, chats.ErrSessionNotFound) {
				return nil, huma.Error404NotFound("Dashboard chat was not found.")
			}
			return nil, huma.Error500InternalServerError("Unable to delete dashboard chat.")
		}
		return &noContentOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-dashboard-chat-models", Method: http.MethodGet, Path: "/api/chat/models",
		Summary: "List Codex models for the administrator chat playground.", Tags: []string{"Dashboard chat"},
	}, func(ctx context.Context, input *authenticationCookieInput) (*modelListOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
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
	chatSchema := huma.SchemaFromType(api.OpenAPI().Components.Schemas, reflect.TypeOf(chatCompletionBody{}))
	huma.Register(api, huma.Operation{
		OperationID: "stream-dashboard-chat", Method: http.MethodPost, Path: "/api/chat/stream",
		Summary: "Stream an administrator chat turn and record usage against a project.", Tags: []string{"Dashboard chat"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "A Responses or Chat Completions server-sent event stream.",
				Content: map[string]*huma.MediaType{
					"text/event-stream": {Schema: &huma.Schema{OneOf: []*huma.Schema{responseSchema, chatSchema}}},
				},
			},
		},
	}, func(ctx context.Context, input *dashboardChatInput) (*huma.StreamResponse, error) {
		status, err := administratorStatus(ctx, authentication, input.SessionToken)
		if err != nil {
			return nil, err
		}
		access, err := projectService.AuthorizeDashboardChat(ctx, input.Body.ProjectID)
		if err != nil {
			return nil, mapDashboardChatAccessError(err)
		}
		prompt, instructions, err := buildChatPrompt(input.Body.Messages)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		continuationInput, err := latestDashboardUserInput(input.Body.Messages)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		createdAt := clock().UTC().Unix()
		events, err := inference.Generate(ctx, codex.GenerateRequest{
			ConversationID: dashboardConversationID(status.Email, input.Body.SessionID), Model: input.Body.Model, Input: prompt,
			ContinuationInput: continuationInput, Instructions: instructions,
			ReasoningEffort: input.Body.ReasoningEffort, Messages: codexConversation(input.Body.Messages),
			RestartConversation: input.Body.RestartSession,
		})
		if err != nil {
			return nil, codexUnavailable()
		}
		onCompleted := func(result codex.GenerationResult) error {
			return history.SaveSession(ctx, status.Email, completedDashboardChatSession(input.Body, instructions, result))
		}
		if input.Body.Endpoint == "responses" {
			return streamResponse(ctx, events, projectService, access, input.Body.Model, createdAt, true, onCompleted), nil
		}
		return streamChatCompletion(ctx, events, projectService, access, input.Body.Model, createdAt, true, true, onCompleted), nil
	})
}

func dashboardConversationID(owner, sessionID string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(owner)) + "\x00" + sessionID))
	return hex.EncodeToString(digest[:])
}

func latestDashboardUserInput(messages []chatMessage) (string, error) {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "user" {
			continue
		}
		content := strings.TrimSpace(messages[index].Content)
		if content != "" {
			return content, nil
		}
	}
	return "", errors.New("dashboard chat requires a user message")
}

func completedDashboardChatSession(body dashboardChatRequestBody, instructions string, result codex.GenerationResult) chats.Session {
	effort := chats.ReasoningEffort(body.ReasoningEffort)
	if effort == "" {
		effort = chats.ReasoningMedium
	}
	model := result.Model
	if model == "" {
		model = body.Model
	}
	messages := make([]chats.Message, 0, len(body.Messages)+1)
	for _, message := range body.Messages {
		if message.Role != string(chats.RoleUser) && message.Role != string(chats.RoleAssistant) {
			continue
		}
		messages = append(messages, chats.Message{Role: chats.Role(message.Role), Content: strings.TrimSpace(message.Content)})
	}
	messages = append(messages, chats.Message{
		Role:      chats.RoleAssistant,
		Content:   result.Text,
		Reasoning: result.ReasoningSummary,
		Usage: &chats.Usage{
			InputTokens: result.InputTokens, CachedInputTokens: result.CachedInputTokens,
			OutputTokens: result.OutputTokens, ReasoningOutputTokens: result.ReasoningOutputTokens,
			TotalTokens: generationTotalTokens(result),
		},
	})
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = dashboardChatTitle(messages)
	}
	return chats.Session{
		ID: body.SessionID, Title: title, Model: model,
		ProjectID: body.ProjectID, Endpoint: chats.Endpoint(body.Endpoint),
		ReasoningEffort: effort, Instructions: instructions, Messages: messages,
	}
}

func dashboardChatTitle(messages []chats.Message) string {
	for _, message := range messages {
		if message.Role != chats.RoleUser {
			continue
		}
		title := []rune(strings.TrimSpace(message.Content))
		if len(title) <= 48 {
			return string(title)
		}
		return string(title[:47]) + "…"
	}
	return "Untitled chat"
}

func dashboardChatSessionResponse(session chats.Session) dashboardChatSessionBody {
	messages := make([]dashboardChatMessageBody, 0, len(session.Messages))
	for _, message := range session.Messages {
		response := dashboardChatMessageBody{
			Role: string(message.Role), Content: message.Content,
			Reasoning: displayReasoningSummary(message.Reasoning),
		}
		if message.Usage != nil {
			response.Usage = &dashboardChatUsageBody{
				InputTokens: message.Usage.InputTokens, CachedInputTokens: message.Usage.CachedInputTokens,
				OutputTokens: message.Usage.OutputTokens, ReasoningOutputTokens: message.Usage.ReasoningOutputTokens,
				TotalTokens: message.Usage.TotalTokens,
			}
		}
		messages = append(messages, response)
	}
	return dashboardChatSessionBody{
		ID: session.ID, Title: session.Title, Model: session.Model, ProjectID: session.ProjectID,
		Endpoint: string(session.Endpoint), ReasoningEffort: string(session.ReasoningEffort),
		Instructions: session.Instructions, Messages: messages,
		CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt,
	}
}

func displayReasoningSummary(summary string) string {
	return strings.ReplaceAll(summary, "****", "**\n\n**")
}

func mapDashboardChatAccessError(err error) error {
	switch {
	case errors.Is(err, projects.ErrProjectNotFound):
		return huma.Error404NotFound("The selected usage project no longer exists.")
	case errors.Is(err, projects.ErrTokenLimitExceeded):
		return huma.Error429TooManyRequests("A project usage limit has been reached.")
	default:
		return huma.Error500InternalServerError("Unable to authorize dashboard chat usage.")
	}
}
