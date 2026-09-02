package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codex-gateway/internal/auth"
	"codex-gateway/internal/projects"
)

type projectServiceStub struct {
	projects             []projects.Project
	project              projects.Project
	createProjectName    string
	createProjectLimits  []projects.UsageLimit
	updateProjectID      string
	updateProjectName    string
	updateProjectLimits  []projects.UsageLimit
	updatedProject       projects.Project
	deleteProjectID      string
	deleteProjectError   error
	projectError         error
	keys                 []projects.APIKey
	createdKey           projects.CreatedAPIKey
	createKeyProjectID   string
	createKeyName        string
	keyError             error
	revokedKeyID         string
	revokeError          error
	usage                projects.Usage
	usageError           error
	listProjectCalls     int
	access               projects.ProjectAccess
	authenticateError    error
	authenticatedKey     string
	dashboardProjectID   string
	dashboardAccess      projects.ProjectAccess
	dashboardAccessError error
	recordedUsage        projects.UsageRecord
	recordUsageError     error
}

func (stub *projectServiceStub) ListProjects(context.Context) ([]projects.Project, error) {
	stub.listProjectCalls++
	return stub.projects, stub.projectError
}

func (stub *projectServiceStub) CreateProject(_ context.Context, name string, limits []projects.UsageLimit) (projects.Project, error) {
	stub.createProjectName = name
	stub.createProjectLimits = limits
	return stub.project, stub.projectError
}

func (stub *projectServiceStub) UpdateProject(_ context.Context, id, name string, limits []projects.UsageLimit) (projects.Project, error) {
	stub.updateProjectID = id
	stub.updateProjectName = name
	stub.updateProjectLimits = limits
	return stub.updatedProject, stub.projectError
}

func (stub *projectServiceStub) DeleteProject(_ context.Context, id string) error {
	stub.deleteProjectID = id
	return stub.deleteProjectError
}

func (stub *projectServiceStub) ListAPIKeys(context.Context) ([]projects.APIKey, error) {
	return stub.keys, stub.keyError
}

func (stub *projectServiceStub) CreateAPIKey(_ context.Context, projectID, name string) (projects.CreatedAPIKey, error) {
	stub.createKeyProjectID = projectID
	stub.createKeyName = name
	return stub.createdKey, stub.keyError
}

func (stub *projectServiceStub) RevokeAPIKey(_ context.Context, id string) error {
	stub.revokedKeyID = id
	return stub.revokeError
}

func (stub *projectServiceStub) UsageSummary(context.Context) (projects.Usage, error) {
	return stub.usage, stub.usageError
}

func (stub *projectServiceStub) AuthenticateAPIKey(_ context.Context, secret string) (projects.ProjectAccess, error) {
	stub.authenticatedKey = secret
	return stub.access, stub.authenticateError
}

func (stub *projectServiceStub) AuthorizeDashboardChat(_ context.Context, projectID string) (projects.ProjectAccess, error) {
	stub.dashboardProjectID = projectID
	return stub.dashboardAccess, stub.dashboardAccessError
}

func (stub *projectServiceStub) RecordUsage(_ context.Context, usage projects.UsageRecord) error {
	stub.recordedUsage = usage
	return stub.recordUsageError
}

func TestProjectRoutesRequireAdministratorSession(t *testing.T) {
	projectService := &projectServiceStub{}
	handler := newTestHandlerWithProjectService(
		t,
		&authenticationStub{status: auth.Status{Authenticated: false}},
		projectService,
	)
	request := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
	if projectService.listProjectCalls != 0 {
		t.Fatal("unauthenticated request reached the project service")
	}
}

func TestCreateAndListProjectsEndpoints(t *testing.T) {
	createdAt := time.Date(2026, time.September, 2, 15, 0, 0, 0, time.UTC)
	limit := projects.UsageLimit{TokenLimit: 250_000, IntervalCount: 6, IntervalUnit: projects.IntervalHour}
	projectService := &projectServiceStub{
		project:  projects.Project{ID: "prj_123", Name: "OpenCode", Limits: []projects.UsageLimit{limit}, CreatedAt: createdAt},
		projects: []projects.Project{{ID: "prj_123", Name: "OpenCode", Limits: []projects.UsageLimit{limit}, CreatedAt: createdAt}},
	}
	handler := newTestHandlerWithProjectService(t, authenticatedAdministrator(), projectService)

	createRequest := authenticatedRequest(http.MethodPost, "/api/projects", `{"name":"OpenCode","limits":[{"token_limit":250000,"interval_count":6,"interval_unit":"hour"}]}`)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, createResponse.Code, createResponse.Body.String())
	}
	if projectService.createProjectName != "OpenCode" || len(projectService.createProjectLimits) != 1 || projectService.createProjectLimits[0] != limit {
		t.Fatalf("unexpected create input: %q %#v", projectService.createProjectName, projectService.createProjectLimits)
	}
	var created projectBody
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created project: %v", err)
	}
	if created.ID != "prj_123" || created.Name != "OpenCode" || len(created.Limits) != 1 || created.Limits[0].TokenLimit != limit.TokenLimit || created.Limits[0].IntervalCount != limit.IntervalCount || created.Limits[0].IntervalUnit != string(limit.IntervalUnit) {
		t.Fatalf("unexpected created project: %#v", created)
	}

	listRequest := authenticatedRequest(http.MethodGet, "/api/projects", "")
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, listResponse.Code, listResponse.Body.String())
	}
	var listed projectListBody
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode project list: %v", err)
	}
	if len(listed.Projects) != 1 || listed.Projects[0].ID != "prj_123" {
		t.Fatalf("unexpected project list: %#v", listed)
	}
}

func TestProjectEndpointRejectsInvalidLimitSchemaBeforeService(t *testing.T) {
	projectService := &projectServiceStub{}
	handler := newTestHandlerWithProjectService(t, authenticatedAdministrator(), projectService)
	request := authenticatedRequest(
		http.MethodPost,
		"/api/projects",
		`{"name":"OpenCode","limits":[{"token_limit":250000,"interval_count":1,"interval_unit":"fortnight"}]}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnprocessableEntity, response.Code, response.Body.String())
	}
	if projectService.createProjectName != "" {
		t.Fatal("invalid usage limit reached the project service")
	}
}

func TestUpdateAndDeleteProjectEndpoints(t *testing.T) {
	createdAt := time.Date(2026, time.September, 2, 15, 0, 0, 0, time.UTC)
	limit := projects.UsageLimit{TokenLimit: 500_000, IntervalCount: 1, IntervalUnit: projects.IntervalDay}
	projectService := &projectServiceStub{updatedProject: projects.Project{
		ID: "prj_123", Name: "OpenCode Desktop", Limits: []projects.UsageLimit{limit}, CreatedAt: createdAt,
	}}
	handler := newTestHandlerWithProjectService(t, authenticatedAdministrator(), projectService)

	updateRequest := authenticatedRequest(http.MethodPut, "/api/projects/prj_123", `{"name":"OpenCode Desktop","limits":[{"token_limit":500000,"interval_count":1,"interval_unit":"day"}]}`)
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, updateResponse.Code, updateResponse.Body.String())
	}
	if projectService.updateProjectID != "prj_123" || projectService.updateProjectName != "OpenCode Desktop" || len(projectService.updateProjectLimits) != 1 || projectService.updateProjectLimits[0] != limit {
		t.Fatalf("unexpected update input: %#v", projectService)
	}
	var updated projectBody
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updated); err != nil || updated.ID != "prj_123" || updated.Name != "OpenCode Desktop" {
		t.Fatalf("unexpected update response: body=%#v err=%v", updated, err)
	}

	deleteRequest := authenticatedRequest(http.MethodDelete, "/api/projects/prj_123", "")
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK || projectService.deleteProjectID != "prj_123" {
		t.Fatalf("unexpected delete response: status=%d id=%q body=%s", deleteResponse.Code, projectService.deleteProjectID, deleteResponse.Body.String())
	}
}

func TestCreateListAndRevokeAPIKeyEndpoints(t *testing.T) {
	createdAt := time.Date(2026, time.September, 2, 15, 0, 0, 0, time.UTC)
	revokedAt := createdAt.Add(time.Hour)
	key := projects.APIKey{ID: "key_123", ProjectID: "prj_123", Name: "Development", Prefix: "cg_example", CreatedAt: createdAt}
	projectService := &projectServiceStub{
		createdKey: projects.CreatedAPIKey{Key: key, Secret: "cg_plaintext_shown_once"},
		keys: []projects.APIKey{key, {
			ID: "key_revoked", ProjectID: "prj_123", Name: "Old key", Prefix: "cg_old_key",
			CreatedAt: createdAt, RevokedAt: &revokedAt,
		}},
	}
	handler := newTestHandlerWithProjectService(t, authenticatedAdministrator(), projectService)

	createRequest := authenticatedRequest(http.MethodPost, "/api/api-keys", `{"project_id":"prj_123","name":"Development"}`)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, createResponse.Code, createResponse.Body.String())
	}
	if projectService.createKeyProjectID != "prj_123" || projectService.createKeyName != "Development" {
		t.Fatalf("unexpected API key input: %q %q", projectService.createKeyProjectID, projectService.createKeyName)
	}
	var created apiKeyCreatedBody
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created API key: %v", err)
	}
	if created.Secret != "cg_plaintext_shown_once" || created.APIKey.ID != "key_123" {
		t.Fatalf("unexpected created API key: %#v", created)
	}

	listRequest := authenticatedRequest(http.MethodGet, "/api/api-keys", "")
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, listResponse.Code)
	}
	if strings.Contains(listResponse.Body.String(), "cg_plaintext_shown_once") || strings.Contains(listResponse.Body.String(), `"secret"`) {
		t.Fatal("API key list exposed plaintext secret material")
	}
	var listed apiKeyListBody
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil || len(listed.APIKeys) != 1 || listed.APIKeys[0].ID != "key_123" {
		t.Fatalf("revoked key appeared in active key list: body=%#v err=%v", listed, err)
	}

	revokeRequest := authenticatedRequest(http.MethodDelete, "/api/api-keys/key_123", "")
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, revokeResponse.Code, revokeResponse.Body.String())
	}
	if projectService.revokedKeyID != "key_123" {
		t.Fatalf("unexpected revoked key ID %q", projectService.revokedKeyID)
	}
}

func TestUsageSummaryEndpoint(t *testing.T) {
	periodStart := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)
	limit := projects.UsageLimit{TokenLimit: 1_000, IntervalCount: 1, IntervalUnit: projects.IntervalDay}
	limitStart := time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC)
	projectService := &projectServiceStub{usage: projects.Usage{
		PeriodStart: periodStart, PeriodEnd: periodEnd, Requests: 2, InputTokens: 200, CachedInputTokens: 35,
		OutputTokens: 50, ReasoningOutputTokens: 13, TotalTokens: 250,
		Projects: []projects.ProjectUsage{{
			ProjectID: "prj_123", Name: "OpenCode", Limits: []projects.UsageLimitUsage{{
				UsageLimit: limit, PeriodStart: limitStart, PeriodEnd: limitStart.AddDate(0, 0, 1),
				Requests: 2, InputTokens: 200, CachedInputTokens: 35, OutputTokens: 50,
				ReasoningOutputTokens: 13, TotalTokens: 250,
			}},
			Requests: 2, InputTokens: 200, CachedInputTokens: 35, OutputTokens: 50,
			ReasoningOutputTokens: 13, TotalTokens: 250,
		}},
		APIKeys: []projects.APIKeyUsage{{
			APIKeyID: "key_123", ProjectID: "prj_123", Name: "Development", Prefix: "cg_example",
			Requests: 2, InputTokens: 200, CachedInputTokens: 35, OutputTokens: 50,
			ReasoningOutputTokens: 13, TotalTokens: 250,
		}},
	}}
	handler := newTestHandlerWithProjectService(t, authenticatedAdministrator(), projectService)
	request := authenticatedRequest(http.MethodGet, "/api/usage", "")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var body usageSummaryBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode usage summary: %v", err)
	}
	if body.TotalTokens != 250 || body.CachedInputTokens != 35 || body.ReasoningOutputTokens != 13 ||
		!body.PeriodEnd.Equal(periodEnd) ||
		len(body.Projects) != 1 || body.Projects[0].ProjectID != "prj_123" ||
		body.Projects[0].CachedInputTokens != 35 || body.Projects[0].ReasoningOutputTokens != 13 ||
		len(body.Projects[0].Limits) != 1 || body.Projects[0].Limits[0].TokenLimit != 1_000 ||
		!body.Projects[0].Limits[0].PeriodStart.Equal(limitStart) ||
		len(body.APIKeys) != 1 || body.APIKeys[0].APIKeyID != "key_123" || body.APIKeys[0].ProjectID != "prj_123" || body.APIKeys[0].TotalTokens != 250 {
		t.Fatalf("unexpected usage summary: %#v", body)
	}
}

func TestProjectErrorsMapToPublicStatuses(t *testing.T) {
	tests := []struct {
		name       string
		serviceErr error
		status     int
	}{
		{name: "invalid name", serviceErr: projects.ErrInvalidName, status: http.StatusUnprocessableEntity},
		{name: "invalid limit", serviceErr: projects.ErrInvalidLimit, status: http.StatusUnprocessableEntity},
		{name: "duplicate", serviceErr: projects.ErrDuplicateName, status: http.StatusConflict},
		{name: "project missing", serviceErr: projects.ErrProjectNotFound, status: http.StatusNotFound},
		{name: "key missing", serviceErr: projects.ErrAPIKeyNotFound, status: http.StatusNotFound},
		{name: "internal", serviceErr: errors.New("database unavailable"), status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projectService := &projectServiceStub{projectError: test.serviceErr}
			handler := newTestHandlerWithProjectService(t, authenticatedAdministrator(), projectService)
			request := authenticatedRequest(http.MethodPost, "/api/projects", `{"name":"OpenCode"}`)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("expected status %d, got %d: %s", test.status, response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "database unavailable") {
				t.Fatal("internal project error leaked to the public API")
			}
		})
	}
}

func authenticatedAdministrator() *authenticationStub {
	return &authenticationStub{status: auth.Status{Authenticated: true, Email: administratorTestEmail, Role: auth.RoleAdministrator}}
}

func authenticatedRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.AddCookie(&http.Cookie{Name: administratorSessionCookie, Value: "session-token"})
	return request
}

func newTestHandlerWithProjectService(t *testing.T, authentication Authentication, projectService ProjectService) http.Handler {
	t.Helper()
	dashboard := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("dashboard"))
	})
	handler, err := New(Options{
		Version:          "test",
		Dashboard:        dashboard,
		Authentication:   authentication,
		IdentityVerifier: &identityVerifierStub{},
		Codex:            &codexStub{},
		Projects:         projectService,
		ChatHistory:      &chatHistoryStub{},
		Inference:        &inferenceStub{},
	})
	if err != nil {
		t.Fatalf("build application: %v", err)
	}
	return handler
}
