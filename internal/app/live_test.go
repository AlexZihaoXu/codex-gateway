//go:build integration

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"codex-gateway/internal/codex"
	"codex-gateway/internal/projects"
)

const liveMatrixPrompt = "Reply with exactly OK."

func requireLiveDataDirectory(t *testing.T) string {
	t.Helper()
	directory := os.Getenv("CODEX_GATEWAY_LIVE_DATA_DIR")
	if directory == "" {
		t.Fatal("CODEX_GATEWAY_LIVE_DATA_DIR must identify a gateway data directory with an authenticated Codex account")
	}
	return directory
}

func TestLiveCompatibilityStreamingMatrix(t *testing.T) {
	if os.Getenv("CODEX_GATEWAY_LIVE_MATRIX") != "1" {
		t.Skip("set CODEX_GATEWAY_LIVE_MATRIX=1 to run the live compatibility matrix")
	}

	dataDirectory := requireLiveDataDirectory(t)

	startContext, cancelStart := context.WithTimeout(context.Background(), 30*time.Second)
	inference, err := codex.Start(startContext, codex.StartOptions{
		Version: "integration-test", DataDirectory: dataDirectory,
	})
	cancelStart()
	if err != nil {
		t.Fatalf("start Codex transport: %v", err)
	}
	t.Cleanup(func() {
		if err := inference.Close(); err != nil {
			t.Errorf("close Codex transport: %v", err)
		}
	})

	projectService, err := projects.Open(projects.Options{DataSourceName: ":memory:"})
	if err != nil {
		t.Fatalf("open ephemeral project service: %v", err)
	}
	t.Cleanup(func() {
		if err := projectService.Close(); err != nil {
			t.Errorf("close ephemeral project service: %v", err)
		}
	})
	project, err := projectService.CreateProject(context.Background(), "Live compatibility matrix", nil)
	if err != nil {
		t.Fatalf("create ephemeral project: %v", err)
	}
	createdKey, err := projectService.CreateAPIKey(context.Background(), project.ID, "Live matrix")
	if err != nil {
		t.Fatalf("create ephemeral API key: %v", err)
	}

	handler := newCompatibilityHandler(t, projectService, inference)
	availableModels := readLiveModels(t, handler, createdKey.Secret)
	directModels, err := inference.Models(context.Background())
	if err != nil {
		t.Fatalf("read model reasoning metadata: %v", err)
	}

	familyModels := make([]codex.Model, 0)
	var sol *codex.Model
	for index := range directModels {
		model := &directModels[index]
		if model.ID == "gpt-5.6" || strings.HasPrefix(model.ID, "gpt-5.6-") {
			if !slices.Contains(availableModels, model.ID) {
				t.Fatalf("GET /v1/models omitted %s", model.ID)
			}
			familyModels = append(familyModels, *model)
		}
		if model.ID == "gpt-5.6-sol" {
			sol = model
		}
	}
	if len(familyModels) == 0 {
		t.Fatal("the authenticated Codex account did not advertise a GPT-5.6 model")
	}
	if sol == nil {
		t.Fatal("the authenticated Codex account did not advertise gpt-5.6-sol")
	}

	expected := liveUsage{}
	for _, model := range familyModels {
		if !slices.Contains(model.SupportedReasoningEfforts, "medium") {
			t.Fatalf("%s did not advertise medium reasoning", model.ID)
		}
		expected.addResponse(streamLiveResponse(t, handler, createdKey.Secret, model.ID, "medium"))
		t.Logf("verified Responses SSE for %s at medium reasoning", model.ID)
	}

	if len(sol.SupportedReasoningEfforts) == 0 {
		t.Fatal("gpt-5.6-sol did not advertise any reasoning efforts")
	}
	t.Logf("gpt-5.6-sol advertised reasoning efforts: %s", strings.Join(sol.SupportedReasoningEfforts, ", "))
	for _, effort := range sol.SupportedReasoningEfforts {
		expected.addChat(streamLiveChatCompletion(t, handler, createdKey.Secret, sol.ID, effort))
		t.Logf("verified Chat Completions SSE for gpt-5.6-sol at %s reasoning", effort)
	}
	assertLiveUsagePersistence(t, projectService, project.ID, createdKey.Key.ID, expected)
}

func TestLiveDashboardLunaConversationCachingAndTotals(t *testing.T) {
	if os.Getenv("CODEX_GATEWAY_LIVE_DASHBOARD") != "1" {
		t.Skip("set CODEX_GATEWAY_LIVE_DASHBOARD=1 to run the live dashboard conversation test")
	}
	dataDirectory := requireLiveDataDirectory(t)

	startContext, cancelStart := context.WithTimeout(context.Background(), 30*time.Second)
	inference, err := codex.Start(startContext, codex.StartOptions{
		Version: "integration-test", DataDirectory: dataDirectory,
	})
	cancelStart()
	if err != nil {
		t.Fatalf("start Codex transport: %v", err)
	}
	t.Cleanup(func() {
		if err := inference.Close(); err != nil {
			t.Errorf("close Codex transport: %v", err)
		}
	})

	projectService, err := projects.Open(projects.Options{DataSourceName: ":memory:"})
	if err != nil {
		t.Fatalf("open ephemeral project service: %v", err)
	}
	t.Cleanup(func() {
		if err := projectService.Close(); err != nil {
			t.Errorf("close ephemeral project service: %v", err)
		}
	})
	handler := newDashboardChatHandler(t, authenticatedAdministrator(), projectService, inference)

	marker := fmt.Sprintf("dashboard-cache-probe-%d ", time.Now().UnixNano())
	firstPrompt := strings.Repeat(marker, 320) + "\nReply with exactly LUNA."
	firstText, firstUsage := streamLiveDashboardTurn(t, handler, map[string]any{
		"endpoint": "chat-completions", "session_id": "live-dashboard-luna",
		"model": "gpt-5.6-luna", "reasoning_effort": "medium",
		"messages": []map[string]string{{"role": "user", "content": firstPrompt}},
	})
	secondPrompt := "What single word did I ask you to return? Reply with only that word."
	_, secondUsage := streamLiveDashboardTurn(t, handler, map[string]any{
		"endpoint": "chat-completions", "session_id": "live-dashboard-luna",
		"model": "gpt-5.6-luna", "reasoning_effort": "medium",
		"messages": []map[string]string{
			{"role": "user", "content": firstPrompt},
			{"role": "assistant", "content": firstText},
			{"role": "user", "content": secondPrompt},
		},
	})

	assertLiveTokenCounts(t, "gpt-5.6-luna", "medium", firstUsage.PromptTokens, firstUsage.CompletionTokens, firstUsage.TotalTokens)
	assertLiveTokenCounts(t, "gpt-5.6-luna", "medium", secondUsage.PromptTokens, secondUsage.CompletionTokens, secondUsage.TotalTokens)
	t.Logf(
		"Dashboard Luna usage: first input=%d cached=%d total=%d; second input=%d cached=%d total=%d",
		firstUsage.PromptTokens, firstUsage.PromptTokenDetails.CachedTokens, firstUsage.TotalTokens,
		secondUsage.PromptTokens, secondUsage.PromptTokenDetails.CachedTokens, secondUsage.TotalTokens,
	)
	if secondUsage.PromptTokenDetails.CachedTokens <= firstUsage.PromptTokenDetails.CachedTokens {
		t.Fatalf(
			"continued Luna turn did not reuse the growing prompt prefix: first cached=%d second cached=%d",
			firstUsage.PromptTokenDetails.CachedTokens, secondUsage.PromptTokenDetails.CachedTokens,
		)
	}
	summary, err := projectService.UsageSummary(context.Background())
	if err != nil {
		t.Fatalf("read dashboard usage summary: %v", err)
	}
	expectedTotal := firstUsage.TotalTokens + secondUsage.TotalTokens
	if summary.Requests != 2 || summary.TotalTokens != expectedTotal || summary.InputTokens != firstUsage.PromptTokens+secondUsage.PromptTokens || summary.OutputTokens != firstUsage.CompletionTokens+secondUsage.CompletionTokens {
		t.Fatalf("dashboard usage did not aggregate both Luna turns: %#v", summary)
	}
	t.Logf(
		"Dashboard Luna usage: first input=%d cached=%d total=%d; second input=%d cached=%d total=%d; dashboard total=%d",
		firstUsage.PromptTokens, firstUsage.PromptTokenDetails.CachedTokens, firstUsage.TotalTokens,
		secondUsage.PromptTokens, secondUsage.PromptTokenDetails.CachedTokens, secondUsage.TotalTokens,
		summary.TotalTokens,
	)
}

func TestLiveStandaloneSearchThroughGateway(t *testing.T) {
	if os.Getenv("CODEX_GATEWAY_LIVE_SEARCH") != "1" {
		t.Skip("set CODEX_GATEWAY_LIVE_SEARCH=1 to run the live standalone search test")
	}
	dataDirectory := requireLiveDataDirectory(t)

	startContext, cancelStart := context.WithTimeout(context.Background(), 30*time.Second)
	inference, err := codex.Start(startContext, codex.StartOptions{
		Version: "integration-test", DataDirectory: dataDirectory,
	})
	cancelStart()
	if err != nil {
		t.Fatalf("start Codex transport: %v", err)
	}
	t.Cleanup(func() { _ = inference.Close() })

	projectService, err := projects.Open(projects.Options{DataSourceName: ":memory:"})
	if err != nil {
		t.Fatalf("open ephemeral project service: %v", err)
	}
	t.Cleanup(func() { _ = projectService.Close() })
	project, err := projectService.CreateProject(context.Background(), "Live Search", nil)
	if err != nil {
		t.Fatalf("create live search project: %v", err)
	}
	key, err := projectService.CreateAPIKey(context.Background(), project.ID, "Live Search")
	if err != nil {
		t.Fatalf("create live search key: %v", err)
	}
	handler := newCompatibilityHandler(t, projectService, inference)
	payload := `{
		"id":"live-search-session",
		"model":"gpt-5.6-luna",
		"input":"Find the official OpenAI Codex source repository.",
		"commands":{"search_query":[{"q":"OpenAI Codex official GitHub repository","domains":["github.com"]}],"response_length":"short"},
		"settings":{"search_context_size":"low","allowed_callers":["direct"],"external_web_access":true},
		"max_output_tokens":500
	}`
	requestContext, cancelRequest := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelRequest()
	request := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(payload)).WithContext(requestContext)
	request.Header.Set("Authorization", "Bearer "+key.Secret)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("live standalone search returned %d", response.Code)
	}
	var body standaloneSearchResponseBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode live search response: %v", err)
	}
	if strings.TrimSpace(body.Output) == "" && len(body.Results) == 0 {
		t.Fatal("live standalone search returned neither output nor structured results")
	}
	usage, err := projectService.UsageSummary(context.Background())
	if err != nil {
		t.Fatalf("read live search usage: %v", err)
	}
	if usage.Requests != 1 || usage.TotalTokens != 0 {
		t.Fatalf("live standalone search request was not tracked: %#v", usage)
	}
	t.Logf("verified standalone search with %d structured results", len(body.Results))
}

func streamLiveDashboardTurn(t *testing.T, handler http.Handler, body map[string]any) (string, chatUsage) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode dashboard request: %v", err)
	}
	requestContext, cancelRequest := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancelRequest()
	request := authenticatedRequest(http.MethodPost, "/api/chat/stream", string(payload)).WithContext(requestContext)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard chat returned %d: %s", response.Code, response.Body.String())
	}

	text := ""
	var usage *chatUsage
	for _, block := range strings.Split(response.Body.String(), "\n\n") {
		data, found := strings.CutPrefix(block, "data: ")
		if !found || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *chatUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("decode dashboard stream chunk: %v", err)
		}
		if len(chunk.Choices) > 0 {
			text += chunk.Choices[0].Delta.Content
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if text == "" || usage == nil {
		t.Fatal("dashboard stream omitted output text or terminal usage")
	}
	return text, *usage
}

func readLiveModels(t *testing.T, handler http.Handler, secret string) []string {
	t.Helper()
	response := performLiveRequest(t, handler, secret, http.MethodGet, "/v1/models", "")
	if response.Code != http.StatusOK {
		t.Fatalf("GET /v1/models returned %d", response.Code)
	}
	var body modelListBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode live model list: %v", err)
	}
	models := make([]string, 0, len(body.Data))
	for _, model := range body.Data {
		models = append(models, model.ID)
	}
	return models
}

func streamLiveResponse(t *testing.T, handler http.Handler, secret, model, effort string) responseUsage {
	t.Helper()
	body := fmt.Sprintf(
		`{"model":%q,"input":%q,"reasoning":{"effort":%q},"stream":true}`,
		model, liveMatrixPrompt, effort,
	)
	response := performLiveRequest(t, handler, secret, http.MethodPost, "/v1/responses", body)
	if response.Code != http.StatusOK {
		t.Fatalf("Responses stream for %s/%s returned %d", model, effort, response.Code)
	}
	stream := response.Body.String()
	assertOrderedStreamMarkers(t, stream,
		"event: response.created",
		"event: response.output_text.delta",
		"event: response.output_text.done",
		"event: response.completed",
	)
	if strings.Contains(stream, "event: response.failed") {
		t.Fatalf("Responses stream for %s/%s reported failure", model, effort)
	}
	for _, block := range strings.Split(stream, "\n\n") {
		if !strings.HasPrefix(block, "event: response.completed\n") {
			continue
		}
		parts := strings.SplitN(block, "\ndata: ", 2)
		if len(parts) != 2 {
			t.Fatalf("Responses stream for %s/%s omitted terminal data", model, effort)
		}
		payload := parts[1]
		if !strings.Contains(payload, `"cached_tokens":`) || !strings.Contains(payload, `"reasoning_tokens":`) {
			t.Fatalf("Responses stream for %s/%s omitted detailed usage", model, effort)
		}
		var completed struct {
			Response responseBody `json:"response"`
		}
		if err := json.Unmarshal([]byte(payload), &completed); err != nil {
			t.Fatalf("decode Responses terminal usage for %s/%s: %v", model, effort, err)
		}
		assertLiveTokenCounts(t, model, effort, completed.Response.Usage.InputTokens, completed.Response.Usage.OutputTokens, completed.Response.Usage.TotalTokens)
		return completed.Response.Usage
	}
	t.Fatalf("Responses stream for %s/%s omitted completion event", model, effort)
	return responseUsage{}
}

func streamLiveChatCompletion(t *testing.T, handler http.Handler, secret, model, effort string) chatUsage {
	t.Helper()
	body := fmt.Sprintf(
		`{"model":%q,"messages":[{"role":"user","content":%q}],"reasoning_effort":%q,"stream":true,"stream_options":{"include_usage":true}}`,
		model, liveMatrixPrompt, effort,
	)
	response := performLiveRequest(t, handler, secret, http.MethodPost, "/v1/chat/completions", body)
	if response.Code != http.StatusOK {
		t.Fatalf("Chat Completions stream for %s/%s returned %d", model, effort, response.Code)
	}
	stream := response.Body.String()
	assertOrderedStreamMarkers(t, stream,
		`"role":"assistant"`,
		`"content":"`,
		`"finish_reason":"stop"`,
		`"usage":`,
		"data: [DONE]",
	)
	if strings.Contains(stream, `"error":`) {
		t.Fatalf("Chat Completions stream for %s/%s reported failure", model, effort)
	}
	for _, block := range strings.Split(stream, "\n\n") {
		payload, found := strings.CutPrefix(block, "data: ")
		if !found || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			Usage *chatUsage `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil || chunk.Usage == nil {
			continue
		}
		if !strings.Contains(payload, `"cached_tokens":`) || !strings.Contains(payload, `"reasoning_tokens":`) {
			t.Fatalf("Chat Completions stream for %s/%s omitted detailed usage", model, effort)
		}
		assertLiveTokenCounts(t, model, effort, chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, chunk.Usage.TotalTokens)
		return *chunk.Usage
	}
	t.Fatalf("Chat Completions stream for %s/%s omitted terminal usage chunk", model, effort)
	return chatUsage{}
}

type liveUsage struct {
	requests              int64
	inputTokens           int64
	cachedInputTokens     int64
	outputTokens          int64
	reasoningOutputTokens int64
	totalTokens           int64
}

func (usage *liveUsage) addResponse(item responseUsage) {
	usage.requests++
	usage.inputTokens += item.InputTokens
	usage.cachedInputTokens += item.InputTokenDetails.CachedTokens
	usage.outputTokens += item.OutputTokens
	usage.reasoningOutputTokens += item.OutputTokenDetails.ReasoningTokens
	usage.totalTokens += item.TotalTokens
}

func (usage *liveUsage) addChat(item chatUsage) {
	usage.requests++
	usage.inputTokens += item.PromptTokens
	usage.cachedInputTokens += item.PromptTokenDetails.CachedTokens
	usage.outputTokens += item.CompletionTokens
	usage.reasoningOutputTokens += item.CompletionTokenDetails.ReasoningTokens
	usage.totalTokens += item.TotalTokens
}

func assertLiveTokenCounts(t *testing.T, model, effort string, inputTokens, outputTokens, totalTokens int64) {
	t.Helper()
	if inputTokens <= 0 || outputTokens <= 0 || totalTokens != inputTokens+outputTokens {
		t.Fatalf("stream for %s/%s returned inconsistent terminal token totals", model, effort)
	}
}

func assertLiveUsagePersistence(t *testing.T, service *projects.Service, projectID, apiKeyID string, expected liveUsage) {
	t.Helper()
	actual, err := service.UsageSummary(context.Background())
	if err != nil {
		t.Fatalf("read live usage summary: %v", err)
	}
	if actual.Requests != expected.requests || actual.InputTokens != expected.inputTokens ||
		actual.CachedInputTokens != expected.cachedInputTokens || actual.OutputTokens != expected.outputTokens ||
		actual.ReasoningOutputTokens != expected.reasoningOutputTokens || actual.TotalTokens != expected.totalTokens {
		t.Fatal("persisted live usage totals did not match terminal stream usage")
	}
	if len(actual.Projects) != 1 || actual.Projects[0].ProjectID != projectID || actual.Projects[0].TotalTokens != expected.totalTokens {
		t.Fatal("live usage was not scoped to the authenticated project")
	}
	if len(actual.APIKeys) != 1 || actual.APIKeys[0].APIKeyID != apiKeyID || actual.APIKeys[0].ProjectID != projectID || actual.APIKeys[0].TotalTokens != expected.totalTokens {
		t.Fatal("live usage was not scoped to the authenticated API key")
	}
}

func performLiveRequest(t *testing.T, handler http.Handler, secret, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	requestContext, cancelRequest := context.WithTimeout(context.Background(), 3*time.Minute)
	request := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(requestContext)
	request.Header.Set("Authorization", "Bearer "+secret)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	cancelRequest()
	return response
}

func assertOrderedStreamMarkers(t *testing.T, stream string, markers ...string) {
	t.Helper()
	position := 0
	for _, marker := range markers {
		next := strings.Index(stream[position:], marker)
		if next < 0 {
			t.Fatalf("live stream omitted expected marker %q", marker)
		}
		position += next + len(marker)
	}
}
