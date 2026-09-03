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
	"codex-gateway/internal/chats"
	"codex-gateway/internal/codex"
)

const administratorTestEmail = "administrator@example.test"

type authenticationStub struct {
	status                 auth.Status
	statusError            error
	statusToken            string
	authenticationSession  auth.Session
	authenticationIdentity auth.Identity
	authenticationError    error
	logoutToken            string
	logoutError            error
}

func (stub *authenticationStub) Status(_ context.Context, token string) (auth.Status, error) {
	stub.statusToken = token
	return stub.status, stub.statusError
}

type codexStub struct {
	account       codex.AccountStatus
	accountCalls  int
	accountError  error
	login         codex.DeviceLogin
	loginCalls    int
	loginError    error
	cancelLoginID string
	cancelError   error
	logoutCalls   int
	logoutError   error
}

type chatHistoryStub struct {
	sessions     []chats.Session
	listOwner    string
	listError    error
	savedOwner   string
	savedSession chats.Session
	saveError    error
	deletedOwner string
	deletedID    string
	deleteError  error
	renamedOwner string
	renamedID    string
	renamedTitle string
	renameError  error
}

func (stub *chatHistoryStub) ListSessions(_ context.Context, owner string) ([]chats.Session, error) {
	stub.listOwner = owner
	return stub.sessions, stub.listError
}

func (stub *chatHistoryStub) SaveSession(_ context.Context, owner string, session chats.Session) error {
	stub.savedOwner = owner
	stub.savedSession = session
	return stub.saveError
}

func (stub *chatHistoryStub) DeleteSession(_ context.Context, owner, id string) error {
	stub.deletedOwner = owner
	stub.deletedID = id
	return stub.deleteError
}

func (stub *chatHistoryStub) RenameSession(_ context.Context, owner, id, title string) error {
	stub.renamedOwner = owner
	stub.renamedID = id
	stub.renamedTitle = title
	return stub.renameError
}

func (stub *codexStub) Account(context.Context) (codex.AccountStatus, error) {
	stub.accountCalls++
	return stub.account, stub.accountError
}

func (stub *codexStub) StartDeviceLogin(context.Context) (codex.DeviceLogin, error) {
	stub.loginCalls++
	return stub.login, stub.loginError
}

func (stub *codexStub) CancelLogin(_ context.Context, loginID string) error {
	stub.cancelLoginID = loginID
	return stub.cancelError
}

func (stub *codexStub) Logout(context.Context) error {
	stub.logoutCalls++
	return stub.logoutError
}

func (stub *authenticationStub) Authenticate(_ context.Context, identity auth.Identity) (auth.Session, error) {
	stub.authenticationIdentity = identity
	return stub.authenticationSession, stub.authenticationError
}

func (stub *authenticationStub) Logout(_ context.Context, token string) error {
	stub.logoutToken = token
	return stub.logoutError
}

type identityVerifierStub struct {
	identity auth.Identity
	err      error
	token    string
}

func (stub *identityVerifierStub) Verify(_ context.Context, token string) (auth.Identity, error) {
	stub.token = token
	return stub.identity, stub.err
}

func TestHealthEndpoint(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}

	var body struct {
		Service string `json:"service"`
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body.Service != "codex-gateway" || body.Status != "ok" || body.Version != "test" {
		t.Fatalf("unexpected health response: %#v", body)
	}
}

func TestOpenAPIDocument(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}

	var document struct {
		OpenAPI string                     `json:"openapi"`
		Info    map[string]any             `json:"info"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode OpenAPI document: %v", err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("expected OpenAPI 3.1.0, got %q", document.OpenAPI)
	}
	if document.Info["title"] != "Codex Gateway API" {
		t.Fatalf("unexpected API title: %v", document.Info["title"])
	}
	if _, ok := document.Paths["/healthz"]; !ok {
		t.Fatal("OpenAPI document does not contain /healthz")
	}
	if _, ok := document.Paths["/readyz"]; !ok {
		t.Fatal("OpenAPI document does not contain /readyz")
	}
	for _, path := range []string{
		"/api/auth/config",
		"/api/auth/status",
		"/api/auth/google",
		"/api/auth/logout",
		"/api/codex/account",
		"/api/codex/login/device",
		"/api/codex/login/cancel",
		"/api/codex/logout",
		"/api/projects",
		"/api/api-keys",
		"/api/api-keys/{id}",
		"/api/usage",
		"/api/chat/sessions",
		"/api/chat/sessions/{id}",
		"/api/chat/models",
		"/api/chat/stream",
		"/v1/models",
		"/v1/responses",
		"/v1/chat/completions",
		"/v1/alpha/search",
	} {
		if _, ok := document.Paths[path]; !ok {
			t.Fatalf("OpenAPI document does not contain %s", path)
		}
	}
	for _, removedPath := range []string{"/api/auth/setup", "/api/auth/login"} {
		if _, ok := document.Paths[removedPath]; ok {
			t.Fatalf("OpenAPI document still contains removed password route %s", removedPath)
		}
	}
	for _, schemaField := range []string{
		`"limits"`, `"token_limit"`, `"interval_count"`, `"interval_unit"`,
		`"minute"`, `"hour"`, `"day"`, `"week"`, `"month"`, `"period_end"`,
	} {
		if !strings.Contains(response.Body.String(), schemaField) {
			t.Fatalf("OpenAPI document does not contain usage-limit schema field %s", schemaField)
		}
	}
}

func TestCodexAccountEndpointRequiresAdministratorSession(t *testing.T) {
	authentication := &authenticationStub{status: auth.Status{Authenticated: false}}
	codexService := &codexStub{}
	handler := newTestHandlerWithServices(t, authentication, codexService)
	request := httptest.NewRequest(http.MethodGet, "/api/codex/account", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
	if codexService.accountCalls != 0 {
		t.Fatal("unauthenticated request reached the Codex transport")
	}
}

func TestCodexAccountEndpointReturnsConnectedAccount(t *testing.T) {
	authentication := &authenticationStub{status: auth.Status{Authenticated: true}}
	codexService := &codexStub{account: codex.AccountStatus{
		Account:            &codex.Account{Type: "chatgpt", Email: "alex@example.test", PlanType: "plus"},
		RequiresOpenAIAuth: true,
	}}
	handler := newTestHandlerWithServices(t, authentication, codexService)
	request := httptest.NewRequest(http.MethodGet, "/api/codex/account", nil)
	request.AddCookie(&http.Cookie{Name: administratorSessionCookie, Value: "session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var body codexAccountBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode Codex account: %v", err)
	}
	if !body.Connected || body.Type != "chatgpt" || body.Email != "alex@example.test" || body.PlanType != "plus" || !body.RequiresOpenAIAuth {
		t.Fatalf("unexpected Codex account: %#v", body)
	}
}

func TestCodexDeviceLoginEndpointReturnsBrowserInstructions(t *testing.T) {
	authentication := &authenticationStub{status: auth.Status{Authenticated: true}}
	codexService := &codexStub{login: codex.DeviceLogin{
		ID:              "login-123",
		VerificationURL: "https://auth.openai.com/codex/device",
		UserCode:        "ABCD-1234",
	}}
	handler := newTestHandlerWithServices(t, authentication, codexService)
	request := httptest.NewRequest(http.MethodPost, "/api/codex/login/device", nil)
	request.AddCookie(&http.Cookie{Name: administratorSessionCookie, Value: "session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	var body codexDeviceLoginBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode device login: %v", err)
	}
	if body.LoginID != "login-123" || body.VerificationURL != "https://auth.openai.com/codex/device" || body.UserCode != "ABCD-1234" {
		t.Fatalf("unexpected device login: %#v", body)
	}
}

func TestCodexLoginCancellationPassesOpaqueIdentifier(t *testing.T) {
	authentication := &authenticationStub{status: auth.Status{Authenticated: true}}
	codexService := &codexStub{}
	handler := newTestHandlerWithServices(t, authentication, codexService)
	request := httptest.NewRequest(http.MethodPost, "/api/codex/login/cancel", strings.NewReader(`{"login_id":"login-123"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: administratorSessionCookie, Value: "session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if codexService.cancelLoginID != "login-123" {
		t.Fatalf("unexpected login identifier: %q", codexService.cancelLoginID)
	}
}

func TestCodexFailureIsReturnedAsBadGateway(t *testing.T) {
	authentication := &authenticationStub{status: auth.Status{Authenticated: true}}
	codexService := &codexStub{accountError: errors.New("transport exited")}
	handler := newTestHandlerWithServices(t, authentication, codexService)
	request := httptest.NewRequest(http.MethodGet, "/api/codex/account", nil)
	request.AddCookie(&http.Cookie{Name: administratorSessionCookie, Value: "session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, response.Code)
	}
	if strings.Contains(response.Body.String(), "transport exited") {
		t.Fatal("internal Codex error leaked through the public API")
	}
}

func TestAuthenticationStatusEndpointReadsSessionCookie(t *testing.T) {
	authentication := &authenticationStub{status: auth.Status{
		Authenticated: true,
		Email:         administratorTestEmail,
		Role:          auth.RoleAdministrator,
	}}
	handler := newTestHandlerWithAuthentication(t, authentication)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	request.AddCookie(&http.Cookie{Name: administratorSessionCookie, Value: "session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if authentication.statusToken != "session-token" {
		t.Fatalf("expected session token to reach authentication service, got %q", authentication.statusToken)
	}
	var body authenticationStatusBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode authentication status: %v", err)
	}
	if !body.Authenticated || body.Email != administratorTestEmail || body.Role != auth.RoleAdministrator {
		t.Fatalf("unexpected authentication status: %#v", body)
	}
}

func TestFirebaseConfigurationEndpointReturnsOnlyPublicClientSettings(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var body firebaseWebConfiguration
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode Firebase configuration: %v", err)
	}
	if body.APIKey != "test-api-key" || body.AuthDomain != "test.firebaseapp.com" || body.ProjectID != "test-project" || body.AppID != "test-app-id" {
		t.Fatalf("unexpected Firebase configuration: %#v", body)
	}
	if strings.Contains(response.Body.String(), administratorTestEmail) {
		t.Fatal("authentication configuration exposed the administrator allowlist")
	}
}

func TestGoogleAuthenticationCreatesStrictHTTPOnlySession(t *testing.T) {
	expiresAt := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	authentication := &authenticationStub{authenticationSession: auth.Session{
		Token:     "one-time-session-token",
		ExpiresAt: expiresAt,
		Email:     administratorTestEmail,
		Role:      auth.RoleAdministrator,
	}}
	verifier := &identityVerifierStub{identity: auth.Identity{UID: "firebase-uid", Email: administratorTestEmail}}
	handler := newTestHandlerWithAuthenticationAndVerifier(t, authentication, verifier)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/google", strings.NewReader(`{"id_token":"signed-firebase-token"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if verifier.token != "signed-firebase-token" || authentication.authenticationIdentity != verifier.identity {
		t.Fatalf("identity was not verified before authentication: verifier=%q identity=%#v", verifier.token, authentication.authenticationIdentity)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != administratorSessionCookie || cookie.Value != "one-time-session-token" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("unexpected session cookie: %#v", cookie)
	}
	if !cookie.Expires.Equal(expiresAt) {
		t.Fatalf("expected cookie to expire at %s, got %s", expiresAt, cookie.Expires)
	}
}

func TestGoogleAuthenticationRejectsInvalidAndUnapprovedIdentities(t *testing.T) {
	tests := map[string]struct {
		authenticationError error
		verificationError   error
		wantStatus          int
	}{
		"invalid token":      {verificationError: auth.ErrInvalidFirebaseToken, wantStatus: http.StatusUnauthorized},
		"keys unavailable":   {verificationError: auth.ErrFirebaseKeysUnavailable, wantStatus: http.StatusServiceUnavailable},
		"email not approved": {authenticationError: auth.ErrEmailNotAllowed, wantStatus: http.StatusForbidden},
		"database failure":   {authenticationError: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			authentication := &authenticationStub{authenticationError: test.authenticationError}
			verifier := &identityVerifierStub{
				identity: auth.Identity{UID: "firebase-uid", Email: "unknown@example.com"},
				err:      test.verificationError,
			}
			handler := newTestHandlerWithAuthenticationAndVerifier(t, authentication, verifier)
			request := httptest.NewRequest(http.MethodPost, "/api/auth/google", strings.NewReader(`{"id_token":"signed-firebase-token"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", test.wantStatus, response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "database unavailable") {
				t.Fatal("internal authentication error leaked through the public API")
			}
		})
	}
}

func TestAdministratorLogoutRevokesAndExpiresSession(t *testing.T) {
	authentication := &authenticationStub{}
	handler := newTestHandlerWithAuthentication(t, authentication)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: administratorSessionCookie, Value: "session-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if authentication.logoutToken != "session-token" {
		t.Fatalf("expected session to be revoked, got %q", authentication.logoutToken)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != administratorSessionCookie || cookies[0].MaxAge >= 0 {
		t.Fatalf("expected an expired session cookie, got %#v", cookies)
	}
}

func TestAuthenticationServiceIsRequired(t *testing.T) {
	dashboard := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("dashboard"))
	})
	if _, err := New(Options{Version: "test", Dashboard: dashboard}); err == nil {
		t.Fatal("expected missing authentication service to be rejected")
	}
}

func TestCodexServiceIsRequired(t *testing.T) {
	dashboard := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("dashboard"))
	})
	if _, err := New(Options{
		Version:        "test",
		Dashboard:      dashboard,
		Authentication: &authenticationStub{},
	}); err == nil {
		t.Fatal("expected missing Codex service to be rejected")
	}
}

func TestChatHistoryServiceIsRequired(t *testing.T) {
	dashboard := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("dashboard"))
	})
	if _, err := New(Options{
		Version:          "test",
		Dashboard:        dashboard,
		Authentication:   &authenticationStub{},
		IdentityVerifier: &identityVerifierStub{},
		Codex:            &codexStub{},
		Projects:         &projectServiceStub{},
	}); err == nil || !strings.Contains(err.Error(), "chat history") {
		t.Fatalf("expected missing chat history service to be rejected, got %v", err)
	}
}

func TestDashboardFallback(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if response.Body.String() != "dashboard" {
		t.Fatalf("expected dashboard response, got %q", response.Body.String())
	}
}

func TestDashboardFallbackSupportsClientSideTabPaths(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/usage", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "dashboard" {
		t.Fatalf("expected dashboard at client route, got %d: %q", response.Code, response.Body.String())
	}
}

func TestDocsAliasRedirectsToCanonicalPath(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/docs", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected status %d, got %d", http.StatusPermanentRedirect, response.Code)
	}
	if location := response.Header().Get("Location"); location != "/doc" {
		t.Fatalf("expected redirect to /doc, got %q", location)
	}
}

func TestDocsRouteUsesEmbeddedDashboard(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/doc", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if response.Body.String() != "dashboard" {
		t.Fatalf("expected embedded dashboard response, got %q", response.Body.String())
	}
}

func TestUnknownAPIPathDoesNotReturnDashboard(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/unknown", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
	if response.Body.String() == "dashboard" {
		t.Fatal("unknown API route returned the dashboard")
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return newTestHandlerWithServices(
		t,
		&authenticationStub{},
		&codexStub{},
	)
}

func newTestHandlerWithAuthentication(t *testing.T, authentication Authentication) http.Handler {
	t.Helper()
	return newTestHandlerWithServices(t, authentication, &codexStub{})
}

func newTestHandlerWithAuthenticationAndVerifier(t *testing.T, authentication Authentication, verifier auth.IdentityVerifier) http.Handler {
	t.Helper()
	return newTestHandlerWithAllServices(t, authentication, verifier, &codexStub{})
}

func newTestHandlerWithServices(t *testing.T, authentication Authentication, codexService Codex) http.Handler {
	t.Helper()
	return newTestHandlerWithAllServices(t, authentication, &identityVerifierStub{
		identity: auth.Identity{UID: "firebase-uid", Email: administratorTestEmail},
	}, codexService)
}

func newTestHandlerWithAllServices(t *testing.T, authentication Authentication, verifier auth.IdentityVerifier, codexService Codex) http.Handler {
	t.Helper()

	dashboard := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("dashboard"))
	})
	handler, err := New(Options{
		Version:          "test",
		Dashboard:        dashboard,
		Authentication:   authentication,
		IdentityVerifier: verifier,
		Firebase: firebaseWebConfiguration{
			APIKey:     "test-api-key",
			AuthDomain: "test.firebaseapp.com",
			ProjectID:  "test-project",
			AppID:      "test-app-id",
		},
		Codex:       codexService,
		Projects:    &projectServiceStub{},
		ChatHistory: &chatHistoryStub{},
		Inference:   &inferenceStub{},
	})
	if err != nil {
		t.Fatalf("build application: %v", err)
	}
	return handler
}
