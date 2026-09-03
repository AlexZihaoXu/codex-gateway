package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearchForwardsAnAuthenticatedCodexSearchRequest(t *testing.T) {
	t.Parallel()
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/backend-api/codex/alpha/search" {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer access-secret" || request.Header.Get("ChatGPT-Account-Id") != "account-123" || request.Header.Get("originator") != "codex_gateway" {
			t.Errorf("unexpected search request metadata: method=%s authorization=%t account=%q originator=%q", request.Method, request.Header.Get("Authorization") != "", request.Header.Get("ChatGPT-Account-Id"), request.Header.Get("originator"))
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Errorf("decode search request: %v", err)
		}
		writeJSON(t, writer, map[string]any{
			"encrypted_output": "opaque",
			"output":           "Search result",
			"results": []any{map[string]any{
				"type": "text_result", "ref_id": "turn0search0",
				"url": "https://example.test/result", "future_field": map[string]any{"preserved": true},
			}},
		})
	}))
	defer server.Close()

	client, err := New(Options{
		DataDirectory: t.TempDir(), Version: "test", HTTPClient: server.Client(),
		Issuer: server.URL, ResponsesEndpoint: server.URL + "/backend-api/codex/responses",
		ClientID: "test-client", NewID: func() string { return "search-request-1" },
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := client.saveCredentials(storedCredentials{
		AccessToken: "access-secret", AccountID: "account-123", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	payload := json.RawMessage(`{"id":"search-session","model":"gpt-5.6-luna","commands":{"search_query":[{"q":"Codex Gateway"}]},"settings":{"allowed_callers":["direct"]}}`)
	result, err := client.Search(context.Background(), payload)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.RequestID != "search-request-1" || result.Output != "Search result" || result.EncryptedOutput == nil || *result.EncryptedOutput != "opaque" || len(result.Results) != 1 {
		t.Fatalf("unexpected search result: %#v", result)
	}
	if received["id"] != "search-session" || received["model"] != "gpt-5.6-luna" {
		t.Fatalf("search request was not preserved: %#v", received)
	}
	encoded, err := json.Marshal(result.Results[0])
	if err != nil || string(encoded) != `{"future_field":{"preserved":true},"ref_id":"turn0search0","type":"text_result","url":"https://example.test/result"}` {
		t.Fatalf("opaque result fields were not preserved: %s, %v", encoded, err)
	}
}

func TestSearchRejectsInvalidPayloadBeforeCallingUpstream(t *testing.T) {
	t.Parallel()
	client := &Client{newID: func() string { return "search-request-1" }}
	for _, payload := range []json.RawMessage{nil, json.RawMessage(`[]`), json.RawMessage(`{"model":`)} {
		if _, err := client.Search(context.Background(), payload); err == nil {
			t.Fatalf("expected invalid search payload to fail: %s", payload)
		}
	}
}
