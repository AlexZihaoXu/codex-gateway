package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codex-gateway/internal/chats"
	"codex-gateway/internal/codex"
	"codex-gateway/internal/projects"
)

func TestDashboardChatModelsRequireAdministratorSession(t *testing.T) {
	inference := &inferenceStub{models: []codex.Model{{ID: "gpt-5.6-sol"}}}
	handler := newDashboardChatHandler(t, &authenticationStub{}, &projectServiceStub{}, inference)
	request := httptest.NewRequest(http.MethodGet, "/api/chat/models", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, response.Code, response.Body.String())
	}
	if inference.modelCalls != 0 {
		t.Fatal("unauthenticated dashboard chat request reached Codex")
	}
}

func TestDashboardChatModelsUseAdministratorSessionWithoutAPIKey(t *testing.T) {
	inference := &inferenceStub{models: []codex.Model{{ID: "gpt-5.6-sol"}, {ID: "gpt-5.6-terra"}}}
	handler := newDashboardChatHandler(t, authenticatedAdministrator(), &projectServiceStub{}, inference)
	request := authenticatedRequest(http.MethodGet, "/api/chat/models", "")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var body modelListBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode model list: %v", err)
	}
	if len(body.Data) != 2 || body.Data[0].ID != "gpt-5.6-sol" || body.Object != "list" {
		t.Fatalf("unexpected model list: %#v", body)
	}
}

func TestDashboardChatHistoryRequiresAuthenticationAndIsOwnerScoped(t *testing.T) {
	history := &chatHistoryStub{sessions: []chats.Session{{
		ID: "chat-session-1", Title: "Say hello", Model: "gpt-5.6-sol",
		Endpoint: chats.EndpointChatCompletions, ReasoningEffort: chats.ReasoningMedium,
		Messages: []chats.Message{
			{Role: chats.RoleUser, Content: "Say hello"},
			{Role: chats.RoleAssistant, Content: "Hello", Reasoning: "**Planning the reply****Writing the reply**", Usage: &chats.Usage{InputTokens: 8, CachedInputTokens: 2, OutputTokens: 2, TotalTokens: 10}},
		},
		CreatedAt: time.Date(2026, time.September, 2, 15, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.September, 2, 15, 1, 0, 0, time.UTC),
	}}}
	unauthenticated := newDashboardChatHandlerWithHistory(t, &authenticationStub{}, &projectServiceStub{}, &inferenceStub{}, history)
	unauthenticatedResponse := httptest.NewRecorder()
	unauthenticated.ServeHTTP(unauthenticatedResponse, httptest.NewRequest(http.MethodGet, "/api/chat/sessions", nil))
	if unauthenticatedResponse.Code != http.StatusUnauthorized || history.listOwner != "" {
		t.Fatalf("unauthenticated history response = %d, owner = %q", unauthenticatedResponse.Code, history.listOwner)
	}

	handler := newDashboardChatHandlerWithHistory(t, authenticatedAdministrator(), &projectServiceStub{}, &inferenceStub{}, history)
	request := authenticatedRequest(http.MethodGet, "/api/chat/sessions", "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("history status = %d: %s", response.Code, response.Body.String())
	}
	if history.listOwner != administratorTestEmail {
		t.Fatalf("history owner = %q", history.listOwner)
	}
	var body struct {
		Sessions []struct {
			ID       string `json:"id"`
			Messages []struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
				Usage     *struct {
					CachedInputTokens int64 `json:"cached_input_tokens"`
				} `json:"usage,omitempty"`
			} `json:"messages"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(body.Sessions) != 1 || body.Sessions[0].ID != "chat-session-1" || len(body.Sessions[0].Messages) != 2 || body.Sessions[0].Messages[1].Usage == nil || body.Sessions[0].Messages[1].Usage.CachedInputTokens != 2 {
		t.Fatalf("unexpected history response: %#v", body)
	}
	if body.Sessions[0].Messages[1].Reasoning != "**Planning the reply**\n\n**Writing the reply**" {
		t.Fatalf("legacy reasoning boundary was not restored: %q", body.Sessions[0].Messages[1].Reasoning)
	}
	var rawBody struct {
		Sessions []map[string]json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &rawBody); err != nil {
		t.Fatalf("decode raw history: %v", err)
	}
	if _, exists := rawBody.Sessions[0]["project_id"]; !exists {
		t.Fatal("dashboard-bucket history omitted its explicit empty project_id")
	}
}

func TestDashboardChatHistoryCanBeRenamedByItsOwner(t *testing.T) {
	history := &chatHistoryStub{}
	handler := newDashboardChatHandlerWithHistory(t, authenticatedAdministrator(), &projectServiceStub{}, &inferenceStub{}, history)
	request := authenticatedRequest(http.MethodPatch, "/api/chat/sessions/chat-session-1", `{"title":"  Renamed chat  "}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("rename status = %d: %s", response.Code, response.Body.String())
	}
	if history.renamedOwner != administratorTestEmail || history.renamedID != "chat-session-1" || history.renamedTitle != "Renamed chat" {
		t.Fatalf("unexpected rename scope: owner=%q id=%q title=%q", history.renamedOwner, history.renamedID, history.renamedTitle)
	}

	history.renameError = chats.ErrSessionNotFound
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, authenticatedRequest(http.MethodPatch, "/api/chat/sessions/missing", `{"title":"Renamed"}`))
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing rename status = %d: %s", missingResponse.Code, missingResponse.Body.String())
	}
}

func TestDashboardChatHistoryCanBeDeletedByItsOwner(t *testing.T) {
	history := &chatHistoryStub{}
	handler := newDashboardChatHandlerWithHistory(t, authenticatedAdministrator(), &projectServiceStub{}, &inferenceStub{}, history)
	request := authenticatedRequest(http.MethodDelete, "/api/chat/sessions/chat-session-1", "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d: %s", response.Code, response.Body.String())
	}
	if history.deletedOwner != administratorTestEmail || history.deletedID != "chat-session-1" {
		t.Fatalf("unexpected delete scope: owner=%q id=%q", history.deletedOwner, history.deletedID)
	}

	history.deleteError = chats.ErrSessionNotFound
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, authenticatedRequest(http.MethodDelete, "/api/chat/sessions/missing", ""))
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing delete status = %d: %s", missingResponse.Code, missingResponse.Body.String())
	}
}

func TestDashboardChatStreamsBothFormatsAndRecordsProjectUsage(t *testing.T) {
	for _, test := range []struct {
		endpoint string
		expected string
	}{
		{endpoint: "chat-completions", expected: "data: [DONE]"},
		{endpoint: "responses", expected: "event: response.completed"},
	} {
		t.Run(test.endpoint, func(t *testing.T) {
			projectService := &projectServiceStub{dashboardAccess: projects.ProjectAccess{ProjectID: "prj_1", APIKeyID: "key_dashboard"}}
			inference := &inferenceStub{generateEvents: completedGenerationEvents("turn-1", "Hello", 8, 2)}
			history := &chatHistoryStub{}
			handler := newDashboardChatHandlerWithHistory(t, authenticatedAdministrator(), projectService, inference, history)
			request := authenticatedRequest(http.MethodPost, "/api/chat/stream", `{
				"endpoint":"`+test.endpoint+`",
				"session_id":"chat-session-1",
				"project_id":"prj_1",
				"model":"gpt-5.6-sol",
				"reasoning_effort":"medium",
				"messages":[{"role":"system","content":"Be concise."},{"role":"user","content":"Say hello"}]
			}`)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
			}
			if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
				t.Fatalf("unexpected content type %q", contentType)
			}
			if !strings.Contains(response.Body.String(), test.expected) {
				t.Fatalf("stream omitted %q: %s", test.expected, response.Body.String())
			}
			if projectService.dashboardProjectID != "prj_1" {
				t.Fatalf("usage project was not authorized: %#v", projectService)
			}
			if projectService.recordedUsage.ProjectID != "prj_1" || projectService.recordedUsage.APIKeyID != "key_dashboard" || projectService.recordedUsage.TotalTokens != 10 {
				t.Fatalf("dashboard chat usage was not recorded: %#v", projectService.recordedUsage)
			}
			if inference.generateRequest.ConversationID != dashboardConversationID(administratorTestEmail, "chat-session-1") || inference.generateRequest.ContinuationInput != "Say hello" || inference.generateRequest.Instructions != "Be concise." || inference.generateRequest.Input != "User: Say hello" || len(inference.generateRequest.Messages) != 1 || inference.generateRequest.Messages[0].Role != "user" || inference.generateRequest.Messages[0].Content != "Say hello" {
				t.Fatalf("unexpected generation request: %#v", inference.generateRequest)
			}
			if history.savedOwner != administratorTestEmail || history.savedSession.ID != "chat-session-1" || history.savedSession.Title != "Say hello" || history.savedSession.ProjectID != "prj_1" || len(history.savedSession.Messages) != 2 || history.savedSession.Messages[1].Content != "Hello" || history.savedSession.Messages[1].Usage == nil || history.savedSession.Messages[1].Usage.TotalTokens != 10 {
				t.Fatalf("completed chat was not persisted: owner=%q session=%#v", history.savedOwner, history.savedSession)
			}
		})
	}
}

func TestDashboardChatStreamsAndPersistsReasoningSummary(t *testing.T) {
	for _, test := range []struct {
		endpoint string
		expected string
	}{
		{endpoint: "chat-completions", expected: `"reasoning_content":"Checked the request."`},
		{endpoint: "responses", expected: "event: response.reasoning_summary_text.delta"},
	} {
		t.Run(test.endpoint, func(t *testing.T) {
			result := codex.GenerationResult{ID: "turn-1", Model: "gpt-5.6-luna", Text: "Hello", ReasoningSummary: "Checked the request.", InputTokens: 8, OutputTokens: 3, ReasoningOutputTokens: 1, TotalTokens: 11}
			inference := &inferenceStub{generateEvents: []codex.GenerationEvent{
				{Type: codex.GenerationStarted, ID: "turn-1"},
				{Type: codex.GenerationReasoningDelta, ID: "turn-1", Delta: "Checked the request."},
				{Type: codex.GenerationDelta, ID: "turn-1", Delta: "Hello"},
				{Type: codex.GenerationCompleted, ID: "turn-1", Result: &result},
			}}
			history := &chatHistoryStub{}
			handler := newDashboardChatHandlerWithHistory(t, authenticatedAdministrator(), &projectServiceStub{dashboardAccess: projects.ProjectAccess{ProjectID: "prj_dashboard", APIKeyID: "key_dashboard"}}, inference, history)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/chat/stream", `{
				"endpoint":"`+test.endpoint+`", "session_id":"chat-reasoning", "model":"gpt-5.6-luna",
				"messages":[{"role":"user","content":"Hello"}]
			}`))

			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.expected) {
				t.Fatalf("reasoning stream = %d: %s", response.Code, response.Body.String())
			}
			if len(history.savedSession.Messages) != 2 || history.savedSession.Messages[1].Reasoning != "Checked the request." {
				t.Fatalf("reasoning was not persisted: %#v", history.savedSession)
			}
		})
	}
}

func TestDashboardChatOptsIntoWebSearchAndStreamsToolLifecycle(t *testing.T) {
	for _, endpoint := range []string{"chat-completions", "responses"} {
		t.Run(endpoint, func(t *testing.T) {
			searches := []codex.WebSearchEvent{{CallID: "web-call-1", Query: "current release", Results: []map[string]any{{"type": "text_result", "url": "https://example.test/release", "title": "Release", "snippet": "Current release details"}}}}
			result := codex.GenerationResult{ID: "turn-search", Model: "gpt-5.6-luna", Text: "Verified answer", InputTokens: 12, OutputTokens: 3, TotalTokens: 15, WebSearches: searches}
			inference := &inferenceStub{generateEvents: []codex.GenerationEvent{
				{Type: codex.GenerationStarted, ID: "turn-search"},
				{Type: codex.GenerationWebSearchStarted, ID: "turn-search", WebSearch: &codex.WebSearchEvent{CallID: "web-call-1", Query: "current release"}},
				{Type: codex.GenerationWebSearchCompleted, ID: "turn-search", WebSearch: &codex.WebSearchEvent{
					CallID: "web-call-1", Query: "current release", Output: "private model context",
					Results: []map[string]any{{"type": "text_result", "url": "https://example.test/release", "title": "Release", "snippet": "Current release details"}},
				}},
				{Type: codex.GenerationDelta, ID: "turn-search", Delta: "Verified answer"},
				{Type: codex.GenerationCompleted, ID: "turn-search", Result: &result},
			}}
			history := &chatHistoryStub{}
			handler := newDashboardChatHandlerWithHistory(t, authenticatedAdministrator(), &projectServiceStub{dashboardAccess: projects.ProjectAccess{ProjectID: "prj_dashboard", APIKeyID: "key_dashboard"}}, inference, history)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/chat/stream", `{
				"endpoint":"`+endpoint+`", "session_id":"chat-search", "model":"gpt-5.6-luna",
				"web_search":true, "messages":[{"role":"user","content":"Find the current release"}]
			}`))

			if response.Code != http.StatusOK {
				t.Fatalf("stream status = %d: %s", response.Code, response.Body.String())
			}
			if !inference.generateRequest.EnableWebSearch {
				t.Fatal("dashboard chat did not enable the Codex web search tool")
			}
			body := response.Body.String()
			if !strings.Contains(body, "event: gateway.web_search.started") || !strings.Contains(body, "event: gateway.web_search.completed") || !strings.Contains(body, `"query":"current release"`) || !strings.Contains(body, `"url":"https://example.test/release"`) {
				t.Fatalf("stream omitted web search lifecycle: %s", body)
			}
			if strings.Contains(body, "private model context") {
				t.Fatalf("stream exposed raw search model context: %s", body)
			}
			if !history.savedSession.WebSearch || len(history.savedSession.Messages) != 2 || len(history.savedSession.Messages[1].WebSearches) != 1 || history.savedSession.Messages[1].WebSearches[0].Query != "current release" {
				t.Fatalf("web search activity was not persisted: %#v", history.savedSession)
			}
		})
	}
}

func TestDashboardChatCanRestartContinuationAfterEditingHistory(t *testing.T) {
	inference := &inferenceStub{generateEvents: completedGenerationEvents("turn-1", "Rewritten", 6, 2)}
	handler := newDashboardChatHandler(t, authenticatedAdministrator(), &projectServiceStub{dashboardAccess: projects.ProjectAccess{ProjectID: "prj_dashboard", APIKeyID: "key_dashboard"}}, inference)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/chat/stream", `{
		"endpoint":"chat-completions", "session_id":"chat-edited", "restart_session":true,
		"model":"gpt-5.6-luna", "messages":[{"role":"user","content":"Edited prompt"}]
	}`))
	if response.Code != http.StatusOK {
		t.Fatalf("restart stream = %d: %s", response.Code, response.Body.String())
	}
	if !inference.generateRequest.RestartConversation {
		t.Fatal("dashboard edit did not reset the Codex continuation state")
	}
}

func TestDashboardChatPreservesAnExplicitRenamedTitleOnLaterTurns(t *testing.T) {
	inference := &inferenceStub{generateEvents: completedGenerationEvents("turn-1", "Answer", 6, 2)}
	history := &chatHistoryStub{}
	handler := newDashboardChatHandlerWithHistory(t, authenticatedAdministrator(), &projectServiceStub{dashboardAccess: projects.ProjectAccess{ProjectID: "prj_dashboard", APIKeyID: "key_dashboard"}}, inference, history)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/chat/stream", `{
		"endpoint":"chat-completions", "session_id":"chat-renamed", "title":"My custom title",
		"model":"gpt-5.6-luna", "messages":[{"role":"user","content":"Original prompt"},{"role":"assistant","content":"Earlier"},{"role":"user","content":"Next"}]
	}`))
	if response.Code != http.StatusOK {
		t.Fatalf("stream = %d: %s", response.Code, response.Body.String())
	}
	if history.savedSession.Title != "My custom title" {
		t.Fatalf("saved title = %q", history.savedSession.Title)
	}
}

func TestDashboardConversationIDIsStableOpaqueAndOwnerScoped(t *testing.T) {
	first := dashboardConversationID("Alex@Example.Test", "chat-session-1")
	if len(first) > 64 {
		t.Fatalf("continuation key exceeds the upstream prompt_cache_key limit: got %d characters", len(first))
	}
	if first != dashboardConversationID(" alex@example.test ", "chat-session-1") {
		t.Fatal("equivalent normalized owners received different continuation keys")
	}
	if first == dashboardConversationID("other@example.test", "chat-session-1") {
		t.Fatal("different owners shared a continuation key")
	}
	if strings.Contains(first, "alex") || strings.Contains(first, "chat-session-1") {
		t.Fatalf("continuation key exposed owner or browser session identifier: %q", first)
	}
}

func TestDashboardChatStreamsWithoutAnExplicitProject(t *testing.T) {
	projectService := &projectServiceStub{dashboardAccess: projects.ProjectAccess{
		ProjectID: "prj_dashboard", APIKeyID: "key_dashboard",
	}}
	inference := &inferenceStub{generateEvents: completedGenerationEvents("turn-1", "Hello", 8, 2)}
	handler := newDashboardChatHandler(t, authenticatedAdministrator(), projectService, inference)
	request := authenticatedRequest(http.MethodPost, "/api/chat/stream", `{
		"endpoint":"chat-completions",
		"session_id":"chat-session-default",
		"model":"gpt-5.6-sol",
		"reasoning_effort":"medium",
		"messages":[{"role":"user","content":"Say hello"}]
	}`)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if projectService.dashboardProjectID != "" {
		t.Fatalf("expected default dashboard attribution, got %q", projectService.dashboardProjectID)
	}
	if projectService.recordedUsage.ProjectID != "prj_dashboard" || projectService.recordedUsage.APIKeyID != "key_dashboard" || projectService.recordedUsage.TotalTokens != 10 {
		t.Fatalf("default dashboard chat usage was not recorded: %#v", projectService.recordedUsage)
	}
}

func TestDashboardChatDoesNotEmitASuccessfulTerminalEventWhenHistoryCannotBeSaved(t *testing.T) {
	for _, test := range []struct {
		endpoint        string
		failure         string
		successTerminal string
	}{
		{endpoint: "chat-completions", failure: "could not persist chat history", successTerminal: `"finish_reason":"stop"`},
		{endpoint: "responses", failure: "response.failed", successTerminal: "response.completed"},
	} {
		t.Run(test.endpoint, func(t *testing.T) {
			projectService := &projectServiceStub{dashboardAccess: projects.ProjectAccess{ProjectID: "prj_dashboard", APIKeyID: "key_dashboard"}}
			history := &chatHistoryStub{saveError: errors.New("database unavailable")}
			handler := newDashboardChatHandlerWithHistory(t, authenticatedAdministrator(), projectService,
				&inferenceStub{generateEvents: completedGenerationEvents("turn-1", "Hello", 8, 2)}, history)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/chat/stream", `{
				"endpoint":"`+test.endpoint+`",
				"session_id":"chat-session-save-failure",
				"model":"gpt-5.6-sol",
				"messages":[{"role":"user","content":"Say hello"}]
			}`))

			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.failure) {
				t.Fatalf("expected a sanitized persistence failure, got %d: %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), test.successTerminal) {
				t.Fatalf("persistence failure emitted successful terminal event %q: %s", test.successTerminal, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "database unavailable") {
				t.Fatalf("persistence failure leaked internal error: %s", response.Body.String())
			}
			if projectService.recordedUsage.TotalTokens != 10 {
				t.Fatalf("completed inference usage was not recorded: %#v", projectService.recordedUsage)
			}
		})
	}
}

func TestDashboardChatMapsProjectAuthorizationFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		accessErr  error
		statusCode int
		message    string
	}{
		{
			name:       "missing project",
			accessErr:  projects.ErrProjectNotFound,
			statusCode: http.StatusNotFound,
			message:    "The selected usage project no longer exists.",
		},
		{
			name:       "project limit reached",
			accessErr:  projects.ErrTokenLimitExceeded,
			statusCode: http.StatusTooManyRequests,
			message:    "A project usage limit has been reached.",
		},
		{
			name:       "internal failure",
			accessErr:  errors.New("database unavailable"),
			statusCode: http.StatusInternalServerError,
			message:    "Unable to authorize dashboard chat usage.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			projectService := &projectServiceStub{dashboardAccessError: test.accessErr}
			inference := &inferenceStub{}
			handler := newDashboardChatHandler(t, authenticatedAdministrator(), projectService, inference)
			request := authenticatedRequest(http.MethodPost, "/api/chat/stream", `{
				"endpoint":"chat-completions",
				"session_id":"chat-session-error",
				"project_id":"prj_1",
				"model":"gpt-5.6-sol",
				"messages":[{"role":"user","content":"Hello"}]
			}`)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != test.statusCode {
				t.Fatalf("expected status %d, got %d: %s", test.statusCode, response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), test.message) {
				t.Fatalf("expected safe message %q, got %s", test.message, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "database unavailable") {
				t.Fatalf("internal failure leaked into response: %s", response.Body.String())
			}
			if inference.generateRequest.Model != "" {
				t.Fatal("project authorization failure reached Codex")
			}
		})
	}
}

func newDashboardChatHandler(t *testing.T, authentication Authentication, projectService ProjectService, inference Inference) http.Handler {
	return newDashboardChatHandlerWithHistory(t, authentication, projectService, inference, &chatHistoryStub{})
}

func newDashboardChatHandlerWithHistory(t *testing.T, authentication Authentication, projectService ProjectService, inference Inference, history ChatHistory) http.Handler {
	t.Helper()
	handler, err := New(Options{
		Version:          "test",
		Dashboard:        http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Authentication:   authentication,
		IdentityVerifier: &identityVerifierStub{},
		Codex:            &codexStub{},
		ChatHistory:      history,
		Projects:         projectService,
		Inference:        inference,
	})
	if err != nil {
		t.Fatalf("build application: %v", err)
	}
	return handler
}
