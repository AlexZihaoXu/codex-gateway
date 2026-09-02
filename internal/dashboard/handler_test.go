package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesEmbeddedDashboard(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("build dashboard handler: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if !strings.Contains(response.Body.String(), "<title>Codex Gateway</title>") {
		t.Fatal("dashboard response does not contain the application title")
	}
}

func TestHandlerFallsBackToIndexForClientRoute(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("build dashboard handler: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/projects/example", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if !strings.Contains(response.Body.String(), "<title>Codex Gateway</title>") {
		t.Fatal("client route did not fall back to the dashboard index")
	}
}

func TestHandlerReturnsNotFoundForMissingAsset(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("build dashboard handler: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
}
