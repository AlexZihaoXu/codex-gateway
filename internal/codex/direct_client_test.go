package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDirectClientCompletesDeviceLoginAndPersistsAccount(t *testing.T) {
	t.Parallel()
	var pollCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			assertJSONBody(t, request, map[string]any{"client_id": "test-client"})
			writeJSON(t, writer, map[string]any{
				"device_auth_id": "device-123",
				"user_code":      "ABCD-1234",
				"interval":       "1",
			})
		case "/api/accounts/deviceauth/token":
			assertJSONBody(t, request, map[string]any{
				"device_auth_id": "device-123",
				"user_code":      "ABCD-1234",
			})
			if pollCalls.Add(1) == 1 {
				writer.WriteHeader(http.StatusForbidden)
				return
			}
			writeJSON(t, writer, map[string]any{
				"authorization_code": "authorization-code",
				"code_verifier":      "code-verifier",
			})
		case "/oauth/token":
			if err := request.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			want := url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {"authorization-code"},
				"redirect_uri":  {server.URL + "/deviceauth/callback"},
				"client_id":     {"test-client"},
				"code_verifier": {"code-verifier"},
			}
			if request.Form.Encode() != want.Encode() {
				t.Errorf("unexpected token form keys: got %v want %v", formKeys(request.Form), formKeys(want))
			}
			writeJSON(t, writer, map[string]any{
				"id_token": jwtForTest(map[string]any{
					"email":              "alex@example.test",
					"chatgpt_account_id": "account-123",
					"chatgpt_plan_type":  "pro",
				}),
				"access_token":  jwtForTest(map[string]any{}),
				"refresh_token": "refresh-secret",
				"expires_in":    3600,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	dataDirectory := t.TempDir()
	client, err := New(Options{
		DataDirectory:     dataDirectory,
		Version:           "test",
		HTTPClient:        server.Client(),
		Issuer:            server.URL,
		ResponsesEndpoint: server.URL + "/backend-api/codex/responses",
		ClientID:          "test-client",
		NewID:             func() string { return "login-123" },
		PollWait:          func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatalf("create direct client: %v", err)
	}
	defer client.Close()

	login, err := client.StartDeviceLogin(context.Background())
	if err != nil {
		t.Fatalf("start device login: %v", err)
	}
	if login.ID != "login-123" || login.UserCode != "ABCD-1234" || login.VerificationURL != server.URL+"/codex/device" {
		t.Fatalf("unexpected device login: %#v", login)
	}

	status := waitForConnectedAccount(t, client)
	if status.Account == nil || status.Account.Email != "alex@example.test" || status.Account.PlanType != "pro" {
		t.Fatalf("unexpected connected account: %#v", status)
	}
	if !status.RequiresOpenAIAuth {
		t.Fatal("Codex remains an OpenAI-authenticated provider after connection")
	}

	restarted, err := New(Options{
		DataDirectory: dataDirectory,
		Version:       "test",
		HTTPClient:    server.Client(),
		Issuer:        server.URL,
		ClientID:      "test-client",
	})
	if err != nil {
		t.Fatalf("restart direct client: %v", err)
	}
	defer restarted.Close()
	restartedStatus, err := restarted.Account(context.Background())
	if err != nil || restartedStatus.Account == nil || restartedStatus.Account.Email != "alex@example.test" {
		t.Fatalf("persisted account was not restored: %#v, %v", restartedStatus, err)
	}

	if runtime.GOOS != "windows" {
		information, statErr := os.Stat(filepath.Join(dataDirectory, credentialsFileName))
		if statErr != nil {
			t.Fatalf("stat credential store: %v", statErr)
		}
		if information.Mode().Perm()&0o077 != 0 {
			t.Fatalf("credential store is accessible outside its owner: %v", information.Mode().Perm())
		}
	}
}

func TestDirectClientRefreshesOnceForConcurrentRequests(t *testing.T) {
	t.Parallel()
	var refreshCalls atomic.Int32
	releaseRefresh := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth/token":
			refreshCalls.Add(1)
			<-releaseRefresh
			writeJSON(t, writer, map[string]any{
				"id_token": jwtForTest(map[string]any{"chatgpt_account_id": "account-refreshed"}),
				"access_token": jwtForTest(map[string]any{
					"chatgpt_compute_residency": "eu",
				}),
				"refresh_token": "refresh-new",
				"expires_in":    3600,
			})
		case "/backend-api/codex/responses":
			if got := request.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
				t.Errorf("missing bearer authorization")
			}
			if request.Header.Get("ChatGPT-Account-Id") != "account-refreshed" {
				t.Errorf("missing refreshed account header")
			}
			if request.Header.Get("x-openai-internal-codex-residency") != "eu" {
				t.Errorf("missing compute residency header")
			}
			writeCompletedSSE(writer, "response-refresh", "ok")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := newAuthenticatedTestClient(t, server, storedCredentials{
		AccessToken:  "expired-access",
		RefreshToken: "refresh-old",
		AccountID:    "account-old",
		ExpiresAt:    time.Now().Add(-time.Hour).UnixMilli(),
	})

	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			events, err := client.Generate(context.Background(), GenerateRequest{Model: "gpt-5.6-sol", Input: "hi"})
			if err != nil {
				t.Errorf("start generation: %v", err)
				return
			}
			if _, err := collectTestGeneration(events); err != nil {
				t.Errorf("collect generation: %v", err)
			}
		}()
	}
	waitUntil(t, func() bool { return refreshCalls.Load() == 1 })
	close(releaseRefresh)
	wait.Wait()
	if refreshCalls.Load() != 1 {
		t.Fatalf("expected one refresh, got %d", refreshCalls.Load())
	}
}

func TestDirectClientStreamsResponsesAndCarriesConversationState(t *testing.T) {
	t.Parallel()
	var requests []map[string]any
	var requestHeaders []http.Header
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/backend-api/codex/responses" {
			http.NotFound(writer, request)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		requests = append(requests, body)
		requestHeaders = append(requestHeaders, request.Header.Clone())
		index := len(requests)
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := writer.(http.Flusher)
		fmt.Fprintf(writer, "event: response.created\r\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"response-%d\"}}\r\n\r\n", index)
		flusher.Flush()
		fmt.Fprint(writer, "event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"Checked the request.\"}\n\n")
		flusher.Flush()
		fmt.Fprint(writer, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"O\"}\n\n")
		flusher.Flush()
		fmt.Fprint(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"K\"}\n\n")
		fmt.Fprintf(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-%d\",\"model\":\"gpt-5.6-sol-versioned\",\"output\":[{\"id\":\"reasoning-%d\",\"type\":\"reasoning\",\"encrypted_content\":\"opaque-%d\",\"summary\":[]},{\"id\":\"message-%d\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}],\"usage\":{\"input_tokens\":12,\"input_tokens_details\":{\"cached_tokens\":8},\"output_tokens\":3,\"output_tokens_details\":{\"reasoning_tokens\":1},\"total_tokens\":999}}}\n\n", index, index, index, index)
		flusher.Flush()
	}))
	defer server.Close()

	client := newAuthenticatedTestClient(t, server, storedCredentials{
		AccessToken:  jwtForTest(map[string]any{}),
		RefreshToken: "refresh-secret",
		AccountID:    "account-123",
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
	})
	first, err := client.Generate(context.Background(), GenerateRequest{
		ConversationID: "session-123", Model: "gpt-5.6-sol", Input: "User: Hi", ContinuationInput: "Hi",
		Instructions: "Be concise.", ReasoningEffort: "medium",
	})
	if err != nil {
		t.Fatalf("start first request: %v", err)
	}
	firstResult, err := collectTestGeneration(first)
	if err != nil {
		t.Fatalf("collect first request: %v", err)
	}
	if firstResult.Text != "OK" || firstResult.ReasoningSummary != "Checked the request." || firstResult.InputTokens != 12 || firstResult.CachedInputTokens != 8 || firstResult.OutputTokens != 3 || firstResult.ReasoningOutputTokens != 1 || firstResult.TotalTokens != 15 {
		t.Fatalf("unexpected first result: %#v", firstResult)
	}

	second, err := client.Generate(context.Background(), GenerateRequest{
		ConversationID: "session-123", Model: "gpt-5.6-sol", Input: "ignored full transcript", ContinuationInput: "Again",
		Instructions: "Be concise.", ReasoningEffort: "medium",
	})
	if err != nil {
		t.Fatalf("start second request: %v", err)
	}
	if _, err := collectTestGeneration(second); err != nil {
		t.Fatalf("collect second request: %v", err)
	}

	restarted, err := client.Generate(context.Background(), GenerateRequest{
		ConversationID: "session-123", Model: "gpt-5.6-sol", Input: "changed full transcript", ContinuationInput: "Changed",
		Instructions: "Be concise.", ReasoningEffort: "medium", RestartConversation: true,
		Messages: []Message{{Role: "user", Content: "Changed"}},
	})
	if err != nil {
		t.Fatalf("restart conversation: %v", err)
	}
	if _, err := collectTestGeneration(restarted); err != nil {
		t.Fatalf("collect restarted request: %v", err)
	}

	if len(requests) != 3 {
		t.Fatalf("expected three upstream requests, got %d", len(requests))
	}
	for index, body := range requests {
		if body["stream"] != true || body["store"] != false || body["prompt_cache_key"] != "session-123" {
			t.Fatalf("request %d omitted streaming/cache invariants: %#v", index+1, body)
		}
		if fmt.Sprint(body["instructions"]) != generationSafetyInstructions+"\n\nBe concise." {
			t.Fatalf("request %d omitted explicit instructions", index+1)
		}
		if fmt.Sprint(body["include"]) != "[reasoning.encrypted_content]" {
			t.Fatalf("request %d omitted encrypted reasoning include: %#v", index+1, body["include"])
		}
	}
	firstInput := requests[0]["input"].([]any)
	if len(firstInput) != 1 || nestedString(firstInput[0], "role") != "user" || nestedInputText(firstInput[0]) != "Hi" {
		t.Fatalf("unexpected first input: %#v", firstInput)
	}
	secondInput := requests[1]["input"].([]any)
	if len(secondInput) != 4 || nestedString(secondInput[1], "type") != "reasoning" || nestedString(secondInput[1], "encrypted_content") != "opaque-1" || nestedString(secondInput[2], "role") != "assistant" || nestedInputText(secondInput[3]) != "Again" {
		t.Fatalf("conversation state was not carried forward: %#v", secondInput)
	}
	restartedInput := requests[2]["input"].([]any)
	if len(restartedInput) != 1 || nestedString(restartedInput[0], "role") != "user" || nestedInputText(restartedInput[0]) != "Changed" {
		t.Fatalf("restarted conversation retained superseded turns: %#v", restartedInput)
	}
	if requestHeaders[0].Get("ChatGPT-Account-Id") != "account-123" || requestHeaders[0].Get("originator") != "codex_gateway" || requestHeaders[0].Get("session-id") != "session-123" {
		t.Fatalf("unexpected transport headers: %#v", requestHeaders[0])
	}
}

func TestDirectClientExecutesNamespacedWebSearchToolAndContinuesGeneration(t *testing.T) {
	t.Parallel()
	var responseBodies []map[string]any
	var searchBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/backend-api/codex/responses":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode response request: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			responseBodies = append(responseBodies, body)
			writer.Header().Set("Content-Type", "text/event-stream")
			if len(responseBodies) == 1 {
				_, _ = io.WriteString(writer, `data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"web-call-1","namespace":"web","name":"run","arguments":"{\"search_query\":[{\"q\":\"current Codex Gateway release\"}]}"}}

data: {"type":"response.completed","response":{"id":"response-tool","model":"gpt-5.6-luna","output":[],"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":4},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":12}}}

`)
				return
			}
			_, _ = io.WriteString(writer, `data: {"type":"response.output_text.delta","delta":"The current release is verified."}

data: {"type":"response.completed","response":{"id":"response-answer","model":"gpt-5.6-luna","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"The current release is verified."}]}],"usage":{"input_tokens":15,"input_tokens_details":{"cached_tokens":8},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":19}}}

`)
		case "/backend-api/codex/alpha/search":
			if err := json.NewDecoder(request.Body).Decode(&searchBody); err != nil {
				t.Errorf("decode search request: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, writer, map[string]any{
				"output": "Search result", "results": []map[string]any{{
					"type": "text_result", "url": "https://example.test/release", "title": "Release",
				}},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := newAuthenticatedTestClient(t, server, storedCredentials{
		AccessToken: "access", RefreshToken: "refresh", AccountID: "account",
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	})
	events, err := client.Generate(context.Background(), GenerateRequest{
		ConversationID: "dashboard-chat", Model: "gpt-5.6-luna", Input: "Search for the current release.",
		ContinuationInput: "Search for the current release.", EnableWebSearch: true,
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}
	var result GenerationResult
	var started, completed *WebSearchEvent
	for event := range events {
		if event.Err != nil {
			t.Fatalf("generation event: %v", event.Err)
		}
		switch event.Type {
		case GenerationWebSearchStarted:
			started = event.WebSearch
		case GenerationWebSearchCompleted:
			completed = event.WebSearch
		case GenerationCompleted:
			if event.Result != nil {
				result = *event.Result
			}
		}
	}

	if result.Text != "The current release is verified." || result.InputTokens != 25 || result.CachedInputTokens != 12 || result.OutputTokens != 6 || result.ReasoningOutputTokens != 2 || result.TotalTokens != 31 || len(result.WebSearches) != 1 || result.WebSearches[0].Query != "current Codex Gateway release" {
		t.Fatalf("unexpected aggregate result: %#v", result)
	}
	if started == nil || started.CallID != "web-call-1" || started.Query != "current Codex Gateway release" {
		t.Fatalf("unexpected started event: %#v", started)
	}
	if completed == nil || completed.CallID != "web-call-1" || completed.Output != "Search result" || len(completed.Results) != 1 {
		t.Fatalf("unexpected completed event: %#v", completed)
	}
	if len(responseBodies) != 2 {
		t.Fatalf("expected two model requests, got %d", len(responseBodies))
	}
	tools, _ := responseBodies[0]["tools"].([]any)
	if len(tools) != 1 || nestedString(tools[0], "type") != "namespace" || nestedString(tools[0], "name") != "web" {
		t.Fatalf("web namespace tool was not exposed: %#v", tools)
	}
	if nestedString(tools[0], "description") != "Tools in the web namespace." {
		t.Fatalf("web namespace description drifted from the reserved tool contract: %#v", tools[0])
	}
	tool := tools[0].(map[string]any)["tools"].([]any)[0]
	parameters := tool.(map[string]any)["parameters"].(map[string]any)
	searchQuery := parameters["properties"].(map[string]any)["search_query"].(map[string]any)
	queryProperties := searchQuery["items"].(map[string]any)["properties"].(map[string]any)
	if searchQuery["description"] != "Query the internet search engine for a given list of queries." ||
		queryProperties["domains"].(map[string]any)["description"] != "Whether to filter by a specific list of domains." ||
		queryProperties["recency"].(map[string]any)["format"] != nil ||
		queryProperties["recency"].(map[string]any)["minimum"] != nil ||
		parameters["additionalProperties"] != nil || searchQuery["items"].(map[string]any)["additionalProperties"] != nil {
		t.Fatalf("web.run parameters drifted from the reserved tool schema: %#v", parameters)
	}
	if responseBodies[0]["tool_choice"] != "auto" {
		t.Fatalf("web search request omitted automatic tool selection: %#v", responseBodies[0]["tool_choice"])
	}
	secondInput, _ := responseBodies[1]["input"].([]any)
	if len(secondInput) != 3 || nestedString(secondInput[1], "type") != "function_call" || nestedString(secondInput[2], "type") != "function_call_output" || nestedString(secondInput[2], "call_id") != "web-call-1" {
		t.Fatalf("tool output was not continued into the model: %#v", secondInput)
	}
	if searchBody["id"] != "dashboard-chat" || searchBody["model"] != "gpt-5.6-luna" {
		t.Fatalf("unexpected search request identity: %#v", searchBody)
	}
	commands, _ := searchBody["commands"].(map[string]any)
	queries, _ := commands["search_query"].([]any)
	if len(queries) != 1 || nestedString(queries[0], "q") != "current Codex Gateway release" {
		t.Fatalf("unexpected search commands: %#v", commands)
	}
}

func TestDirectClientEmitsReasoningSummaryDeltasSeparatelyFromAnswerText(t *testing.T) {
	t.Parallel()
	stream := strings.NewReader(
		"data: {\"type\":\"response.reasoning_text.delta\",\"delta\":\"First\"}\n\n" +
			"data: {\"type\":\"response.reasoning_summary.delta\",\"delta\":\" second\"}\n\n" +
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Answer\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-1\",\"model\":\"gpt-5.6-luna\",\"output\":[],\"usage\":{\"input_tokens\":4,\"input_tokens_details\":{\"cached_tokens\":0},\"output_tokens\":3,\"output_tokens_details\":{\"reasoning_tokens\":2},\"total_tokens\":7}}}\n\n",
	)
	events := make(chan GenerationEvent, 8)
	result, _, err := parseResponseStream(context.Background(), stream, "turn-1", "thread-1", "gpt-5.6-luna", events)
	close(events)
	if err != nil {
		t.Fatalf("parse response stream: %v", err)
	}
	if result.ReasoningSummary != "First second" || result.Text != "Answer" {
		t.Fatalf("unexpected parsed result: %#v", result)
	}
	var reasoning, answer strings.Builder
	for event := range events {
		switch event.Type {
		case GenerationReasoningDelta:
			reasoning.WriteString(event.Delta)
		case GenerationDelta:
			answer.WriteString(event.Delta)
		}
	}
	if reasoning.String() != "First second" || answer.String() != "Answer" {
		t.Fatalf("reasoning=%q answer=%q", reasoning.String(), answer.String())
	}
}

func TestDirectClientPreservesReasoningSummaryPartBoundaries(t *testing.T) {
	t.Parallel()
	stream := strings.NewReader(
		"data: {\"type\":\"response.reasoning_summary_part.added\",\"summary_index\":0,\"part\":{\"type\":\"summary_text\",\"text\":\"\"}}\n\n" +
			"data: {\"type\":\"response.reasoning_summary_text.delta\",\"summary_index\":0,\"delta\":\"**Planning the demo**\"}\n\n" +
			"data: {\"type\":\"response.reasoning_summary_part.done\",\"summary_index\":0,\"part\":{\"type\":\"summary_text\",\"text\":\"**Planning the demo**\"}}\n\n" +
			"data: {\"type\":\"response.reasoning_summary_part.added\",\"summary_index\":1,\"part\":{\"type\":\"summary_text\",\"text\":\"\"}}\n\n" +
			"data: {\"type\":\"response.reasoning_summary_text.delta\",\"summary_index\":1,\"delta\":\"**Designing game mechanics**\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-1\",\"model\":\"gpt-5.6-luna\",\"output\":[],\"usage\":{\"input_tokens\":4,\"output_tokens\":3,\"total_tokens\":7}}}\n\n",
	)
	events := make(chan GenerationEvent, 8)
	result, _, err := parseResponseStream(context.Background(), stream, "turn-1", "thread-1", "gpt-5.6-luna", events)
	close(events)
	if err != nil {
		t.Fatalf("parse response stream: %v", err)
	}
	const expected = "**Planning the demo**\n\n**Designing game mechanics**"
	if result.ReasoningSummary != expected {
		t.Fatalf("reasoning summary=%q, want %q", result.ReasoningSummary, expected)
	}
	var reasoning strings.Builder
	for event := range events {
		if event.Type == GenerationReasoningDelta {
			reasoning.WriteString(event.Delta)
		}
	}
	if reasoning.String() != expected {
		t.Fatalf("reasoning event=%q, want %q", reasoning.String(), expected)
	}
}

func TestDirectClientRejectsFailedAndTruncatedStreams(t *testing.T) {
	t.Parallel()
	responses := []string{
		"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"slow down\"}}}\n\n",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n",
	}
	var call atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, responses[int(call.Add(1))-1])
	}))
	defer server.Close()
	client := newAuthenticatedTestClient(t, server, storedCredentials{
		AccessToken: "access", RefreshToken: "refresh", AccountID: "account", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	})

	for index, expected := range []string{"rate_limit_exceeded: slow down", "ended before a terminal event"} {
		events, err := client.Generate(context.Background(), GenerateRequest{Model: "gpt-5.6-sol", Input: "hi"})
		if err != nil {
			t.Fatalf("request %d start: %v", index+1, err)
		}
		_, err = collectTestGeneration(events)
		if err == nil || !strings.Contains(err.Error(), expected) {
			t.Fatalf("request %d: expected %q, got %v", index+1, expected, err)
		}
	}
}

func TestDirectClientUsesCompletedMessageWhenNoTextDeltaArrives(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, `data: {"type":"response.completed","response":{"id":"response-fallback","output":[{"id":"message-1","type":"message","role":"assistant","content":[{"type":"output_text","text":"fallback text"}]}],"usage":{"input_tokens":2,"output_tokens":2,"total_tokens":4}}}

`)
	}))
	defer server.Close()
	client := newAuthenticatedTestClient(t, server, storedCredentials{
		AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	})
	events, err := client.Generate(context.Background(), GenerateRequest{Model: "gpt-5.6-sol", Input: "hi"})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}
	result, err := collectTestGeneration(events)
	if err != nil {
		t.Fatalf("collect generation: %v", err)
	}
	if result.Text != "fallback text" {
		t.Fatalf("expected completed output fallback, got %q", result.Text)
	}
}

func TestDirectClientUsesCompletedReasoningSummaryWhenNoSummaryDeltaArrives(t *testing.T) {
	t.Parallel()
	stream := strings.NewReader(`data: {"type":"response.completed","response":{"id":"response-fallback","output":[{"id":"reasoning-1","type":"reasoning","summary":[{"type":"summary_text","text":"**Inspecting the request**\n\nChecked the route."},{"type":"summary_text","text":"**Preparing the answer**\n\nWrote the response."}]}],"usage":{"input_tokens":2,"output_tokens":2,"total_tokens":4}}}

`)
	events := make(chan GenerationEvent, 4)
	result, _, err := parseResponseStream(context.Background(), stream, "turn-1", "thread-1", "gpt-5.6-sol", events)
	close(events)
	if err != nil {
		t.Fatalf("parse response stream: %v", err)
	}
	if result.ReasoningSummary != "**Inspecting the request**\n\nChecked the route.\n\n**Preparing the answer**\n\nWrote the response." {
		t.Fatalf("unexpected completed reasoning fallback: %q", result.ReasoningSummary)
	}
	var summary strings.Builder
	for event := range events {
		if event.Type == GenerationReasoningDelta {
			summary.WriteString(event.Delta)
		}
	}
	if summary.String() != result.ReasoningSummary {
		t.Fatalf("reasoning event=%q result=%q", summary.String(), result.ReasoningSummary)
	}
}

func TestDirectClientListsAuditedModelsWithoutNetwork(t *testing.T) {
	t.Parallel()
	client, err := New(Options{DataDirectory: t.TempDir(), Version: "test"})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 3 || models[0].ID != "gpt-5.6-sol" || models[1].ID != "gpt-5.6-terra" || models[2].ID != "gpt-5.6-luna" {
		t.Fatalf("unexpected audited model catalog: %#v", models)
	}
	for _, model := range models {
		if model.DefaultReasoningEffort != "medium" || len(model.SupportedReasoningEfforts) != 7 {
			t.Fatalf("unexpected model reasoning metadata: %#v", model)
		}
	}
}

func TestDirectClientRetainsAdvertisedUltraReasoningCompatibility(t *testing.T) {
	t.Parallel()
	if !isReasoningEffort("ultra") {
		t.Fatal("ultra reasoning was previously advertised and must remain accepted")
	}
}

func TestDirectClientPreservesStructuredChatMessages(t *testing.T) {
	t.Parallel()
	var input []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Input []map[string]any `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		input = body.Input
		writeCompletedSSE(writer, "response-structured", "ok")
	}))
	defer server.Close()
	client := newAuthenticatedTestClient(t, server, storedCredentials{
		AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	})
	events, err := client.Generate(context.Background(), GenerateRequest{
		Model: "gpt-5.6-sol", Input: "fallback transcript",
		Messages: []Message{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "answer"},
			{Role: "user", Content: "follow-up"},
		},
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}
	if _, err := collectTestGeneration(events); err != nil {
		t.Fatalf("collect generation: %v", err)
	}
	if len(input) != 3 || nestedString(input[0], "role") != "user" || nestedInputText(input[0]) != "first" || nestedString(input[1], "role") != "assistant" || nestedInputText(input[1]) != "answer" || nestedInputText(input[2]) != "follow-up" {
		t.Fatalf("structured messages were not preserved: %#v", input)
	}
}

func TestDirectClientCancelsLoginAndLogsOutIdempotently(t *testing.T) {
	t.Parallel()
	client, err := New(Options{DataDirectory: t.TempDir(), Version: "test"})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := client.CancelLogin(context.Background(), ""); err == nil {
		t.Fatal("expected an empty login identifier to fail")
	}
	cancelled := false
	client.pendingLogins["login-1"] = func() { cancelled = true }
	if err := client.CancelLogin(context.Background(), "login-1"); err != nil || !cancelled {
		t.Fatalf("cancel pending login: cancelled=%v err=%v", cancelled, err)
	}
	if err := client.saveCredentials(storedCredentials{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}
	logoutCancelled := false
	client.pendingLogins["login-2"] = func() { logoutCancelled = true }
	if err := client.Logout(context.Background()); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if !logoutCancelled {
		t.Fatal("logout did not cancel the pending device login")
	}
	status, err := client.Account(context.Background())
	if err != nil || status.Account != nil || !status.RequiresOpenAIAuth {
		t.Fatalf("unexpected logged-out status: %#v, %v", status, err)
	}
	if err := client.Logout(context.Background()); err != nil {
		t.Fatalf("idempotent logout: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
}

func TestTokenMetadataUsesIDIdentityAndAccessTransportClaims(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(t, writer, map[string]any{
			"id_token": jwtForTest(map[string]any{
				"email": "alex@example.test",
			}),
			"access_token": jwtForTest(map[string]any{
				"chatgpt_account_id":        "account-from-access",
				"chatgpt_plan_type":         "plus",
				"chatgpt_compute_residency": "ca",
			}),
			"refresh_token": "refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()
	client, err := New(Options{DataDirectory: t.TempDir(), Version: "test", HTTPClient: server.Client(), Issuer: server.URL})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	credentials, err := client.exchangeAuthorizationCode(context.Background(), "code", "verifier")
	if err != nil {
		t.Fatalf("exchange token: %v", err)
	}
	if credentials.Email != "alex@example.test" || credentials.AccountID != "account-from-access" || credentials.PlanType != "plus" || credentials.Residency != "ca" {
		t.Fatalf("metadata was not merged across tokens: %#v", credentials)
	}
}

func TestResidencyFallsBackToRootClaimWhenNamespaceDoesNotSpecifyOne(t *testing.T) {
	t.Parallel()
	token := jwtForTest(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "account-from-namespace",
		},
		"chatgpt_compute_residency": "ca",
	})
	if got := extractResidency(token); got != "ca" {
		t.Fatalf("residency = %q, want root fallback %q", got, "ca")
	}
}

func TestDirectClientValidatesConfigurationAndGeneration(t *testing.T) {
	t.Parallel()
	for _, options := range []Options{
		{Version: "test"},
		{DataDirectory: t.TempDir()},
		{DataDirectory: t.TempDir(), Version: "test", Issuer: "http://example.com"},
		{DataDirectory: t.TempDir(), Version: "test", ResponsesEndpoint: "relative"},
	} {
		if _, err := New(options); err == nil {
			t.Fatalf("expected invalid options to fail: %#v", options)
		}
	}
	client, err := New(Options{DataDirectory: t.TempDir(), Version: "test"})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	for _, request := range []GenerateRequest{
		{Input: "hi"},
		{Model: "gpt-5.6-sol"},
		{Model: "gpt-5.6-sol", Input: "hi", ReasoningEffort: "infinite"},
	} {
		if _, err := client.Generate(context.Background(), request); err == nil {
			t.Fatalf("expected invalid generation to fail: %#v", request)
		}
	}
}

func newAuthenticatedTestClient(t *testing.T, server *httptest.Server, credentials storedCredentials) *Client {
	t.Helper()
	client, err := New(Options{
		DataDirectory:     t.TempDir(),
		Version:           "test",
		HTTPClient:        server.Client(),
		DisableWebSockets: true,
		Issuer:            server.URL,
		ResponsesEndpoint: server.URL + "/backend-api/codex/responses",
		ClientID:          "test-client",
	})
	if err != nil {
		t.Fatalf("create direct client: %v", err)
	}
	if err := client.saveCredentials(credentials); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func waitForConnectedAccount(t *testing.T, client *Client) AccountStatus {
	t.Helper()
	var status AccountStatus
	waitUntil(t, func() bool {
		var err error
		status, err = client.Account(context.Background())
		return err == nil && status.Account != nil
	})
	return status
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(time.Millisecond)
	}
}

func collectTestGeneration(events <-chan GenerationEvent) (GenerationResult, error) {
	var result GenerationResult
	for event := range events {
		if event.Err != nil {
			return GenerationResult{}, event.Err
		}
		if event.Type == GenerationCompleted && event.Result != nil {
			result = *event.Result
		}
	}
	if result.ID == "" {
		return GenerationResult{}, fmt.Errorf("missing completed result")
	}
	return result, nil
}

func assertJSONBody(t *testing.T, request *http.Request, expected map[string]any) {
	t.Helper()
	var actual map[string]any
	if err := json.NewDecoder(request.Body).Decode(&actual); err != nil {
		t.Errorf("decode JSON body: %v", err)
		return
	}
	if fmt.Sprint(actual) != fmt.Sprint(expected) {
		t.Errorf("unexpected JSON body: got %#v want %#v", actual, expected)
	}
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func writeCompletedSSE(writer http.ResponseWriter, id, text string) {
	writer.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\n", text)
	fmt.Fprintf(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n", id)
}

func jwtForTest(claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "none"})
	payload, _ := json.Marshal(claims)
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func formKeys(values url.Values) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func nestedString(value any, key string) string {
	object, _ := value.(map[string]any)
	text, _ := object[key].(string)
	return text
}

func nestedInputText(value any) string {
	object, _ := value.(map[string]any)
	content, _ := object["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	part, _ := content[0].(map[string]any)
	text, _ := part["text"].(string)
	return text
}
