package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codex-gateway/internal/codex"
	"codex-gateway/internal/projects"
)

func TestStandaloneSearchRequiresAProjectAPIKey(t *testing.T) {
	projectService := &projectServiceStub{authenticateError: projects.ErrInvalidAPIKey}
	inference := &inferenceStub{}
	handler := newCompatibilityHandler(t, projectService, inference)
	request := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"id":"search-session","model":"gpt-5.6-luna","commands":{"search_query":[{"q":"Codex Gateway"}]}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, response.Code, response.Body.String())
	}
	if inference.searchPayload != nil {
		t.Fatal("unauthenticated search reached Codex")
	}
}

func TestStandaloneSearchForwardsTheCodexContractAndRecordsTheRequest(t *testing.T) {
	projectService := &projectServiceStub{access: projects.ProjectAccess{ProjectID: "prj_1", APIKeyID: "key_1"}}
	encrypted := "opaque"
	inference := &inferenceStub{searchResult: codex.SearchResult{
		RequestID: "search-request-1", EncryptedOutput: &encrypted, Output: "Search result",
		Results: []map[string]any{{"type": "text_result", "ref_id": "turn0search0", "url": "https://example.test/result", "future_field": map[string]any{"preserved": true}}},
	}}
	handler := newCompatibilityHandler(t, projectService, inference)
	payload := `{
		"id":"search-session","model":"gpt-5.6-luna",
		"reasoning":{"effort":"medium"},
		"input":"Use this context",
		"commands":{
			"search_query":[{"q":"Codex Gateway","recency":7,"domains":["openai.com"]}],
			"open":[{"ref_id":"turn0search0","lineno":12}],
			"response_length":"short"
		},
		"settings":{
			"user_location":{"type":"approximate","country":"CA","city":"Toronto","timezone":"America/Toronto"},
			"search_context_size":"high",
			"filters":{"allowed_domains":["openai.com"],"blocked_domains":["example.com"]},
			"image_settings":{"max_results":4,"caption":true},
			"allowed_callers":["direct"],
			"external_web_access":"live"
		},
		"max_output_tokens":2500
	}`
	request := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(payload))
	request.Header.Set("Authorization", "Bearer cg_secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if projectService.authenticatedKey != "cg_secret" {
		t.Fatalf("search used the wrong project API key: %q", projectService.authenticatedKey)
	}
	var forwarded map[string]any
	if err := json.Unmarshal(inference.searchPayload, &forwarded); err != nil {
		t.Fatalf("decode forwarded search: %v", err)
	}
	if forwarded["id"] != "search-session" || forwarded["model"] != "gpt-5.6-luna" {
		t.Fatalf("search contract was not forwarded: %#v", forwarded)
	}
	if projectService.recordedUsage.RequestID != "search-request-1" || projectService.recordedUsage.ProjectID != "prj_1" || projectService.recordedUsage.APIKeyID != "key_1" || projectService.recordedUsage.TotalTokens != 0 {
		t.Fatalf("search request usage was not recorded: %#v", projectService.recordedUsage)
	}
	var body struct {
		EncryptedOutput *string          `json:"encrypted_output"`
		Output          string           `json:"output"`
		Results         []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if body.EncryptedOutput == nil || *body.EncryptedOutput != "opaque" || body.Output != "Search result" || len(body.Results) != 1 || body.Results[0]["future_field"] == nil {
		t.Fatalf("unexpected search response: %#v", body)
	}
}

func TestStandaloneSearchSchemaDocumentsEveryCodexCommandFamily(t *testing.T) {
	handler := newCompatibilityHandler(t, &projectServiceStub{}, &inferenceStub{})
	request := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected OpenAPI status %d, got %d", http.StatusOK, response.Code)
	}
	document := response.Body.String()
	for _, expected := range []string{
		`"/v1/alpha/search"`, `"search_query"`, `"image_query"`, `"open"`, `"click"`, `"find"`, `"screenshot"`,
		`"finance"`, `"weather"`, `"sports"`, `"time"`, `"response_length"`, `"search_context_size"`,
		`"external_web_access"`, `"allowed_callers"`, `"encrypted_output"`, `"results"`,
	} {
		if !strings.Contains(document, expected) {
			t.Fatalf("OpenAPI document does not expose %s", expected)
		}
	}
}
