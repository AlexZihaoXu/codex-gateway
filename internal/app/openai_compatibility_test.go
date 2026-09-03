package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codex-gateway/internal/codex"
	"codex-gateway/internal/projects"
)

type inferenceStub struct {
	models          []codex.Model
	modelsError     error
	modelCalls      int
	generateRequest codex.GenerateRequest
	generateEvents  []codex.GenerationEvent
	generateError   error
	searchPayload   json.RawMessage
	searchResult    codex.SearchResult
	searchError     error
}

func (stub *inferenceStub) Search(_ context.Context, payload json.RawMessage) (codex.SearchResult, error) {
	stub.searchPayload = append(json.RawMessage(nil), payload...)
	return stub.searchResult, stub.searchError
}

func (stub *inferenceStub) Models(context.Context) ([]codex.Model, error) {
	stub.modelCalls++
	return stub.models, stub.modelsError
}

func (stub *inferenceStub) Generate(_ context.Context, request codex.GenerateRequest) (<-chan codex.GenerationEvent, error) {
	stub.generateRequest = request
	if stub.generateError != nil {
		return nil, stub.generateError
	}
	events := make(chan codex.GenerationEvent, len(stub.generateEvents))
	for _, event := range stub.generateEvents {
		events <- event
	}
	close(events)
	return events, nil
}

func TestModelsEndpointRequiresProjectBearerKey(t *testing.T) {
	projectService := &projectServiceStub{authenticateError: projects.ErrInvalidAPIKey}
	inference := &inferenceStub{}
	handler := newCompatibilityHandler(t, projectService, inference)

	for _, authorization := range []string{"", "Basic invalid", "Bearer invalid"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("expected status %d for %q, got %d: %s", http.StatusUnauthorized, authorization, response.Code, response.Body.String())
		}
	}
	if inference.modelCalls != 0 {
		t.Fatal("unauthorized model request reached Codex")
	}
}

func TestModelsEndpointReturnsOpenAIList(t *testing.T) {
	projectService := &projectServiceStub{access: projects.ProjectAccess{ProjectID: "prj_1", APIKeyID: "key_1"}}
	inference := &inferenceStub{models: []codex.Model{
		{ID: "gpt-5.4", DisplayName: "GPT-5.4", Default: true},
		{ID: "gpt-5.4-mini", DisplayName: "GPT-5.4 mini"},
	}}
	handler := newCompatibilityHandler(t, projectService, inference)
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer cg_project_secret")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode models response: %v", err)
	}
	if projectService.authenticatedKey != "cg_project_secret" || body.Object != "list" || len(body.Data) != 2 || body.Data[0].ID != "gpt-5.4" || body.Data[0].Object != "model" || body.Data[0].OwnedBy != "codex-gateway" {
		t.Fatalf("unexpected models response: %#v", body)
	}
}

func TestResponsesEndpointReturnsOpenAIResponseAndRecordsUsage(t *testing.T) {
	projectService := &projectServiceStub{access: projects.ProjectAccess{ProjectID: "prj_1", APIKeyID: "key_1"}}
	inference := &inferenceStub{generateEvents: completedGenerationEvents("turn-1", "Hello", 12, 3)}
	handler := newCompatibilityHandler(t, projectService, inference)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"Say hello","instructions":"Be concise.","reasoning":{"effort":"medium"}}`))
	request.Header.Set("Authorization", "Bearer cg_project_secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var body responseBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != "resp_turn-1" || body.Object != "response" || body.Status != "completed" || body.Model != "gpt-5.4" || len(body.Output) != 1 || len(body.Output[0].Content) != 1 || body.Output[0].Content[0].Text != "Hello" || body.Usage.InputTokenDetails.CachedTokens != 2 || body.Usage.OutputTokenDetails.ReasoningTokens != 1 || body.Usage.TotalTokens != 15 {
		t.Fatalf("unexpected response body: %#v", body)
	}
	if inference.generateRequest.Model != "gpt-5.4" || inference.generateRequest.Input != "Say hello" || inference.generateRequest.Instructions != "Be concise." || inference.generateRequest.ReasoningEffort != "medium" {
		t.Fatalf("unexpected generation request: %#v", inference.generateRequest)
	}
	if projectService.recordedUsage.ProjectID != "prj_1" || projectService.recordedUsage.APIKeyID != "key_1" || projectService.recordedUsage.RequestID != "resp_turn-1" || projectService.recordedUsage.InputTokens != 12 || projectService.recordedUsage.CachedInputTokens != 2 || projectService.recordedUsage.OutputTokens != 3 || projectService.recordedUsage.ReasoningOutputTokens != 1 || projectService.recordedUsage.TotalTokens != 15 {
		t.Fatalf("usage was not recorded: %#v", projectService)
	}
}

func TestResponsesEndpointStreamsNamedOpenAIEvents(t *testing.T) {
	projectService := &projectServiceStub{access: projects.ProjectAccess{ProjectID: "prj_1", APIKeyID: "key_1"}}
	inference := &inferenceStub{generateEvents: completedGenerationEvents("turn-1", "Hello", 12, 3)}
	handler := newCompatibilityHandler(t, projectService, inference)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"Say hello","stream":true}`))
	request.Header.Set("Authorization", "Bearer cg_project_secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("unexpected content type %q", contentType)
	}
	stream := response.Body.String()
	for _, expected := range []string{
		"event: response.created",
		"event: response.in_progress",
		"event: response.output_item.added",
		"event: response.content_part.added",
		"event: response.output_text.delta",
		`"delta":"Hel"`,
		`"delta":"lo"`,
		"event: response.output_text.done",
		"event: response.content_part.done",
		"event: response.output_item.done",
		"event: response.completed",
	} {
		if !strings.Contains(stream, expected) {
			t.Fatalf("stream omitted %q: %s", expected, stream)
		}
	}
	if projectService.recordedUsage.RequestID != "resp_turn-1" || projectService.recordedUsage.APIKeyID != "key_1" {
		t.Fatal("streaming response usage was not recorded")
	}
}

func TestCompatibilityEndpointsMapLimitAndCodexFailures(t *testing.T) {
	limitedProjects := &projectServiceStub{authenticateError: projects.ErrTokenLimitExceeded}
	handler := newCompatibilityHandler(t, limitedProjects, &inferenceStub{})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer cg_limited")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d: %s", http.StatusTooManyRequests, response.Code, response.Body.String())
	}

	projectService := &projectServiceStub{access: projects.ProjectAccess{ProjectID: "prj_1"}}
	inference := &inferenceStub{generateError: errors.New("internal credential failure")}
	handler = newCompatibilityHandler(t, projectService, inference)
	request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"Hello"}`))
	request.Header.Set("Authorization", "Bearer cg_project_secret")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "credential") {
		t.Fatalf("unexpected Codex failure response: %d %s", response.Code, response.Body.String())
	}
}

func TestChatCompletionsEndpointReturnsAssistantMessage(t *testing.T) {
	projectService := &projectServiceStub{access: projects.ProjectAccess{ProjectID: "prj_1", APIKeyID: "key_1"}}
	inference := &inferenceStub{generateEvents: completedGenerationEvents("turn-2", "Hello there", 20, 4)}
	handler := newCompatibilityHandler(t, projectService, inference)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-5.4",
		"reasoning_effort":"ultra",
		"messages":[
			{"role":"system","content":"Be concise."},
			{"role":"user","content":"Say hello"}
		]
	}`))
	request.Header.Set("Authorization", "Bearer cg_project_secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var body chatCompletionBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode chat completion: %v", err)
	}
	if body.ID != "chatcmpl_turn-2" || body.Object != "chat.completion" || len(body.Choices) != 1 || body.Choices[0].Message.Role != "assistant" || body.Choices[0].Message.Content != "Hello there" || body.Choices[0].FinishReason != "stop" || body.Usage.TotalTokens != 24 {
		t.Fatalf("unexpected chat completion: %#v", body)
	}
	if inference.generateRequest.Instructions != "Be concise." || inference.generateRequest.Input != "User: Say hello" || inference.generateRequest.ReasoningEffort != "ultra" || len(inference.generateRequest.Messages) != 1 || inference.generateRequest.Messages[0].Role != "user" || inference.generateRequest.Messages[0].Content != "Say hello" {
		t.Fatalf("unexpected chat generation request: %#v", inference.generateRequest)
	}
	if projectService.recordedUsage.RequestID != "chatcmpl_turn-2" || projectService.recordedUsage.APIKeyID != "key_1" {
		t.Fatal("chat completion usage was not recorded")
	}
}

func TestChatCompletionsEndpointStreamsChunksAndDoneSentinel(t *testing.T) {
	projectService := &projectServiceStub{access: projects.ProjectAccess{ProjectID: "prj_1", APIKeyID: "key_1"}}
	inference := &inferenceStub{generateEvents: completedGenerationEvents("turn-2", "Hello", 20, 4)}
	handler := newCompatibilityHandler(t, projectService, inference)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-5.4",
		"messages":[{"role":"user","content":"Say hello"}],
		"stream":true,
		"stream_options":{"include_usage":true}
	}`))
	request.Header.Set("Authorization", "Bearer cg_project_secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	stream := response.Body.String()
	for _, expected := range []string{`"role":"assistant"`, `"content":"Hel"`, `"content":"lo"`, `"finish_reason":"stop"`, `"cached_tokens":2`, `"reasoning_tokens":1`, `"total_tokens":24`, "data: [DONE]"} {
		if !strings.Contains(stream, expected) {
			t.Fatalf("chat stream omitted %q: %s", expected, stream)
		}
	}
}

func TestGenerationTotalTokensUsesInputAndOutputInsteadOfAnUpstreamCumulativeValue(t *testing.T) {
	result := codex.GenerationResult{InputTokens: 12, OutputTokens: 3, TotalTokens: 999}

	if total := generationTotalTokens(result); total != 15 {
		t.Fatalf("expected contract total 15, got %d", total)
	}
}

func completedGenerationEvents(id, text string, inputTokens, outputTokens int64) []codex.GenerationEvent {
	middle := (len(text) + 1) / 2
	return []codex.GenerationEvent{
		{Type: codex.GenerationStarted, ID: id},
		{Type: codex.GenerationDelta, ID: id, Delta: text[:middle]},
		{Type: codex.GenerationDelta, ID: id, Delta: text[middle:]},
		{Type: codex.GenerationCompleted, ID: id, Result: &codex.GenerationResult{
			ID: id, ThreadID: "thread-1", Model: "gpt-5.4", Text: text,
			InputTokens: inputTokens, CachedInputTokens: 2, OutputTokens: outputTokens,
			ReasoningOutputTokens: 1, TotalTokens: inputTokens + outputTokens,
		}},
	}
}

func newCompatibilityHandler(t *testing.T, projectService ProjectService, inference Inference) http.Handler {
	t.Helper()
	handler, err := New(Options{
		Version:          "test",
		Dashboard:        http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Authentication:   &authenticationStub{},
		IdentityVerifier: &identityVerifierStub{},
		Codex:            &codexStub{},
		Projects:         projectService,
		ChatHistory:      &chatHistoryStub{},
		Inference:        inference,
	})
	if err != nil {
		t.Fatalf("build application: %v", err)
	}
	return handler
}
