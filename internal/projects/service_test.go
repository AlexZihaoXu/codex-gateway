package projects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

var projectTestNow = time.Date(2026, time.September, 2, 15, 0, 0, 0, time.UTC)

func TestCreateAndListProjects(t *testing.T) {
	service := newTestService(t)
	limit := UsageLimit{TokenLimit: 250_000, IntervalCount: 6, IntervalUnit: IntervalHour}
	created, err := service.CreateProject(context.Background(), "  OpenCode  ", []UsageLimit{limit})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if created.ID == "" || created.Name != "OpenCode" || len(created.Limits) != 1 || created.Limits[0] != limit {
		t.Fatalf("unexpected project: %#v", created)
	}
	if !created.CreatedAt.Equal(projectTestNow) {
		t.Fatalf("unexpected creation time: %s", created.CreatedAt)
	}

	projects, err := service.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != created.ID {
		t.Fatalf("unexpected projects: %#v", projects)
	}
}

func TestProjectValidationAndDuplicateNames(t *testing.T) {
	service := newTestService(t)
	invalidLimits := [][]UsageLimit{
		{{TokenLimit: 0, IntervalCount: 1, IntervalUnit: IntervalHour}},
		{{TokenLimit: 1000, IntervalCount: 0, IntervalUnit: IntervalHour}},
	}
	for _, limits := range invalidLimits {
		if _, err := service.CreateProject(context.Background(), "Project", limits); !errors.Is(err, ErrInvalidLimit) {
			t.Fatalf("expected ErrInvalidLimit for %#v, got %v", limits, err)
		}
	}
	for _, name := range []string{"   ", string(bytes.Repeat([]byte{'a'}, maximumProjectNameRunes+1))} {
		if _, err := service.CreateProject(context.Background(), name, nil); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("expected ErrInvalidName for %q, got %v", name, err)
		}
	}
	if _, err := service.CreateProject(context.Background(), "OpenCode", nil); err != nil {
		t.Fatalf("create first project: %v", err)
	}
	if _, err := service.CreateProject(context.Background(), "opencode", nil); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got %v", err)
	}
}

func TestUpdateProjectChangesNameAndLimits(t *testing.T) {
	service := newTestService(t)
	project, err := service.CreateProject(context.Background(), "Editor", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	limits := []UsageLimit{
		{TokenLimit: 25_000, IntervalCount: 1, IntervalUnit: IntervalHour},
		{TokenLimit: 250_000, IntervalCount: 1, IntervalUnit: IntervalDay},
	}
	updated, err := service.UpdateProject(context.Background(), project.ID, "  OpenCode  ", limits)
	if err != nil {
		t.Fatalf("update project: %v", err)
	}
	if updated.ID != project.ID || updated.Name != "OpenCode" || len(updated.Limits) != 2 || updated.Limits[0] != limits[0] || updated.Limits[1] != limits[1] || !updated.CreatedAt.Equal(project.CreatedAt) {
		t.Fatalf("unexpected updated project: %#v", updated)
	}
	updated, err = service.UpdateProject(context.Background(), project.ID, "OpenCode", nil)
	if err != nil || len(updated.Limits) != 0 {
		t.Fatalf("remove project limits: project=%#v err=%v", updated, err)
	}
}

func TestUpdateProjectValidatesAndReportsConflicts(t *testing.T) {
	service := newTestService(t)
	project, err := service.CreateProject(context.Background(), "Editor", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := service.CreateProject(context.Background(), "OpenCode", nil); err != nil {
		t.Fatalf("create conflicting project: %v", err)
	}
	if _, err := service.UpdateProject(context.Background(), project.ID, "OpenCode", nil); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got %v", err)
	}
	if _, err := service.UpdateProject(context.Background(), project.ID, " ", nil); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
	if _, err := service.UpdateProject(context.Background(), project.ID, "Editor", []UsageLimit{{IntervalCount: 1, IntervalUnit: IntervalHour}}); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("expected ErrInvalidLimit, got %v", err)
	}
	if _, err := service.UpdateProject(context.Background(), "missing", "Editor", nil); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestDeleteProjectCascadesAPIKeysAndUsage(t *testing.T) {
	service := newTestService(t)
	project, err := service.CreateProject(context.Background(), "Editor", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := service.CreateAPIKey(context.Background(), project.ID, "Development")
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}
	if err := service.RecordUsage(context.Background(), UsageRecord{
		RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID,
		InputTokens: 8, OutputTokens: 2, TotalTokens: 10,
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if err := service.DeleteProject(context.Background(), project.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	projects, err := service.ListProjects(context.Background())
	if err != nil || len(projects) != 0 {
		t.Fatalf("projects remained after deletion: projects=%#v err=%v", projects, err)
	}
	keys, err := service.ListAPIKeys(context.Background())
	if err != nil || len(keys) != 0 {
		t.Fatalf("API keys remained after deletion: keys=%#v err=%v", keys, err)
	}
	var usageRecords int
	if err := service.database.QueryRow("SELECT COUNT(*) FROM project_usage").Scan(&usageRecords); err != nil || usageRecords != 0 {
		t.Fatalf("usage remained after deletion: count=%d err=%v", usageRecords, err)
	}
	var usageLimits int
	if err := service.database.QueryRow("SELECT COUNT(*) FROM project_usage_limit").Scan(&usageLimits); err != nil || usageLimits != 0 {
		t.Fatalf("usage limits remained after deletion: count=%d err=%v", usageLimits, err)
	}
	if err := service.DeleteProject(context.Background(), project.ID); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestCreateListAndRevokeAPIKey(t *testing.T) {
	service := newTestService(t)
	project, err := service.CreateProject(context.Background(), "Editor", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := service.CreateAPIKey(context.Background(), project.ID, "Development")
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}
	if created.Secret == "" || created.Key.ID == "" || created.Key.ProjectID != project.ID || created.Key.Name != "Development" {
		t.Fatalf("unexpected created API key: %#v", created)
	}
	if created.Key.Prefix == "" || len(created.Key.Prefix) >= len(created.Secret) {
		t.Fatalf("unexpected API key prefix %q", created.Key.Prefix)
	}

	var storedHash []byte
	var storedPrefix string
	if err := service.database.QueryRow("SELECT key_hash, prefix FROM project_api_key WHERE id = ?", created.Key.ID).Scan(&storedHash, &storedPrefix); err != nil {
		t.Fatalf("read stored API key: %v", err)
	}
	expectedHash := sha256.Sum256([]byte(created.Secret))
	if !bytes.Equal(storedHash, expectedHash[:]) || storedPrefix != created.Key.Prefix {
		t.Fatal("API key was not stored as its SHA-256 hash and display prefix")
	}
	var plaintextMatches int
	if err := service.database.QueryRow("SELECT COUNT(*) FROM project_api_key WHERE CAST(key_hash AS TEXT) = ?", created.Secret).Scan(&plaintextMatches); err != nil {
		t.Fatalf("search for plaintext API key: %v", err)
	}
	if plaintextMatches != 0 {
		t.Fatal("plaintext API key was persisted")
	}

	keys, err := service.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("list API keys: %v", err)
	}
	if len(keys) != 1 || keys[0].ID != created.Key.ID || keys[0].RevokedAt != nil {
		t.Fatalf("unexpected API keys: %#v", keys)
	}
	if err := service.RevokeAPIKey(context.Background(), created.Key.ID); err != nil {
		t.Fatalf("revoke API key: %v", err)
	}
	keys, err = service.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("list revoked API keys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected revoked API key to be omitted, got %#v", keys)
	}
	if err := service.RevokeAPIKey(context.Background(), created.Key.ID); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("expected second revocation to return ErrAPIKeyNotFound, got %v", err)
	}
}

func TestAPIKeyCreationValidatesProjectAndName(t *testing.T) {
	service := newTestService(t)
	if _, err := service.CreateAPIKey(context.Background(), "missing", "Development"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
	project, err := service.CreateProject(context.Background(), "Editor", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	for _, name := range []string{" ", string(bytes.Repeat([]byte{'a'}, maximumAPIKeyNameRunes+1))} {
		if _, err := service.CreateAPIKey(context.Background(), project.ID, name); !errors.Is(err, ErrInvalidAPIKeyName) {
			t.Fatalf("expected ErrInvalidAPIKeyName for %q, got %v", name, err)
		}
	}
}

func TestAuthenticateAPIKeyReturnsProjectAccessAndTracksLastUse(t *testing.T) {
	service := newTestService(t)
	limit := UsageLimit{TokenLimit: 10_000, IntervalCount: 6, IntervalUnit: IntervalHour}
	project, err := service.CreateProject(context.Background(), "Editor", []UsageLimit{limit})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := service.CreateAPIKey(context.Background(), project.ID, "Development")
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}

	access, err := service.AuthenticateAPIKey(context.Background(), created.Secret)
	if err != nil {
		t.Fatalf("authenticate API key: %v", err)
	}
	if access.ProjectID != project.ID || access.APIKeyID != created.Key.ID || len(access.Limits) != 1 || access.Limits[0].UsageLimit != limit || access.Limits[0].TotalTokens != 0 {
		t.Fatalf("unexpected project access: %#v", access)
	}
	keys, err := service.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("list API keys: %v", err)
	}
	if len(keys) != 1 || keys[0].LastUsedAt == nil || !keys[0].LastUsedAt.Equal(projectTestNow) {
		t.Fatalf("successful authentication did not track last use: %#v", keys)
	}
}

func TestAuthenticateAPIKeyRejectsUnknownRevokedAndExhaustedKeys(t *testing.T) {
	service := newTestService(t)
	limit := UsageLimit{TokenLimit: 100, IntervalCount: 1, IntervalUnit: IntervalHour}
	project, err := service.CreateProject(context.Background(), "Editor", []UsageLimit{limit})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := service.CreateAPIKey(context.Background(), project.ID, "Development")
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}

	if _, err := service.AuthenticateAPIKey(context.Background(), "cg_unknown"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected unknown key to return ErrInvalidAPIKey, got %v", err)
	}
	if err := service.RecordUsage(context.Background(), UsageRecord{
		RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID,
		InputTokens: 80, OutputTokens: 20, TotalTokens: 100,
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if _, err := service.AuthenticateAPIKey(context.Background(), created.Secret); !errors.Is(err, ErrTokenLimitExceeded) {
		t.Fatalf("expected exhausted project to return ErrTokenLimitExceeded, got %v", err)
	}
	if err := service.RevokeAPIKey(context.Background(), created.Key.ID); err != nil {
		t.Fatalf("revoke API key: %v", err)
	}
	if _, err := service.AuthenticateAPIKey(context.Background(), created.Secret); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected revoked key to return ErrInvalidAPIKey, got %v", err)
	}
}

func TestAuthenticateAPIKeyEnforcesEveryWindowAndResetsOnBoundary(t *testing.T) {
	now := projectTestNow
	service := newTestServiceWithClock(t, func() time.Time { return now })
	project, err := service.CreateProject(context.Background(), "Editor", []UsageLimit{
		{TokenLimit: 100, IntervalCount: 1, IntervalUnit: IntervalHour},
		{TokenLimit: 1000, IntervalCount: 1, IntervalUnit: IntervalDay},
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := service.CreateAPIKey(context.Background(), project.ID, "Development")
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}
	record := func(id string, total int64) {
		t.Helper()
		if err := service.RecordUsage(context.Background(), UsageRecord{
			RequestID: id, ProjectID: project.ID, APIKeyID: created.Key.ID,
			InputTokens: total, TotalTokens: total,
		}); err != nil {
			t.Fatalf("record usage: %v", err)
		}
	}
	record("hour-one", 100)
	if _, err := service.AuthenticateAPIKey(context.Background(), created.Secret); !errors.Is(err, ErrTokenLimitExceeded) {
		t.Fatalf("expected hourly limit to reject access, got %v", err)
	}
	now = projectTestNow.Add(time.Hour)
	if _, err := service.AuthenticateAPIKey(context.Background(), created.Secret); err != nil {
		t.Fatalf("hourly window did not reset: %v", err)
	}
	record("rest-of-day", 900)
	if _, err := service.AuthenticateAPIKey(context.Background(), created.Secret); !errors.Is(err, ErrTokenLimitExceeded) {
		t.Fatalf("expected daily limit to reject access, got %v", err)
	}
}

func TestAuthorizeDashboardChatUsesHiddenProjectIdentityAndRecordsUsage(t *testing.T) {
	service := newTestService(t)
	project, err := service.CreateProject(context.Background(), "Editor", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	access, err := service.AuthorizeDashboardChat(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("authorize dashboard chat: %v", err)
	}
	if access.ProjectID != project.ID || access.APIKeyID == "" {
		t.Fatalf("unexpected dashboard chat access: %#v", access)
	}
	repeated, err := service.AuthorizeDashboardChat(context.Background(), project.ID)
	if err != nil || repeated.APIKeyID != access.APIKeyID {
		t.Fatalf("dashboard chat identity was not stable: access=%#v err=%v", repeated, err)
	}
	keys, err := service.ListAPIKeys(context.Background())
	if err != nil || len(keys) != 0 {
		t.Fatalf("dashboard chat identity leaked into API key management: keys=%#v err=%v", keys, err)
	}
	if _, err := service.AuthenticateAPIKey(context.Background(), access.APIKeyID); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("dashboard chat identity authenticated as a public API key: %v", err)
	}
	if err := service.RecordUsage(context.Background(), UsageRecord{
		RequestID: "dashboard-request", ProjectID: project.ID, APIKeyID: access.APIKeyID,
		InputTokens: 8, CachedInputTokens: 2, OutputTokens: 2, ReasoningOutputTokens: 1, TotalTokens: 10,
	}); err != nil {
		t.Fatalf("record dashboard chat usage: %v", err)
	}
	summary, err := service.UsageSummary(context.Background())
	if err != nil {
		t.Fatalf("read usage summary: %v", err)
	}
	if summary.TotalTokens != 10 || len(summary.Projects) != 1 || summary.Projects[0].TotalTokens != 10 {
		t.Fatalf("dashboard chat was not included in project totals: %#v", summary)
	}
	if len(summary.APIKeys) != 1 || summary.APIKeys[0].Name != "Dashboard Chat" || summary.APIKeys[0].Prefix != "session" || summary.APIKeys[0].TotalTokens != 10 {
		t.Fatalf("dashboard chat usage source was not identified: %#v", summary.APIKeys)
	}
	if _, err := service.AuthorizeDashboardChat(context.Background(), "missing"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected missing project to return ErrProjectNotFound, got %v", err)
	}
}

func TestAuthorizeDashboardChatCreatesHiddenDefaultUsageProject(t *testing.T) {
	service := newTestService(t)

	access, err := service.AuthorizeDashboardChat(context.Background(), "")
	if err != nil {
		t.Fatalf("authorize default dashboard chat: %v", err)
	}
	if access.ProjectID == "" || access.APIKeyID == "" {
		t.Fatalf("unexpected default dashboard chat access: %#v", access)
	}
	listed, err := service.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("default dashboard chat project leaked into project management: %#v", listed)
	}
	if err := service.RecordUsage(context.Background(), UsageRecord{
		RequestID: "dashboard-default", ProjectID: access.ProjectID, APIKeyID: access.APIKeyID,
		InputTokens: 6, OutputTokens: 4, TotalTokens: 10,
	}); err != nil {
		t.Fatalf("record default dashboard chat usage: %v", err)
	}
	summary, err := service.UsageSummary(context.Background())
	if err != nil {
		t.Fatalf("read usage summary: %v", err)
	}
	if summary.TotalTokens != 10 || len(summary.Projects) != 1 || summary.Projects[0].Name != "Dashboard Chat" || summary.Projects[0].TotalTokens != 10 {
		t.Fatalf("default dashboard chat was not included in dashboard totals: %#v", summary)
	}
	if len(summary.APIKeys) != 1 || summary.APIKeys[0].Name != "Dashboard Chat" || summary.APIKeys[0].TotalTokens != 10 {
		t.Fatalf("default dashboard chat source was not included in dashboard totals: %#v", summary.APIKeys)
	}
	if _, err := service.UpdateProject(context.Background(), access.ProjectID, "Renamed", nil); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected hidden dashboard project update to return ErrProjectNotFound, got %v", err)
	}
	if err := service.DeleteProject(context.Background(), access.ProjectID); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected hidden dashboard project deletion to return ErrProjectNotFound, got %v", err)
	}
	if _, err := service.CreateAPIKey(context.Background(), access.ProjectID, "Hidden"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected hidden dashboard project key creation to return ErrProjectNotFound, got %v", err)
	}
}

func TestAuthorizeDashboardChatEnforcesProjectLimits(t *testing.T) {
	service := newTestService(t)
	project, err := service.CreateProject(context.Background(), "Editor", []UsageLimit{{
		TokenLimit: 10, IntervalCount: 1, IntervalUnit: IntervalHour,
	}})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	access, err := service.AuthorizeDashboardChat(context.Background(), project.ID)
	if err != nil {
		t.Fatalf("authorize dashboard chat: %v", err)
	}
	if err := service.RecordUsage(context.Background(), UsageRecord{
		RequestID: "dashboard-request", ProjectID: project.ID, APIKeyID: access.APIKeyID,
		InputTokens: 8, OutputTokens: 2, TotalTokens: 10,
	}); err != nil {
		t.Fatalf("record dashboard chat usage: %v", err)
	}
	if _, err := service.AuthorizeDashboardChat(context.Background(), project.ID); !errors.Is(err, ErrTokenLimitExceeded) {
		t.Fatalf("expected exhausted project to reject dashboard chat, got %v", err)
	}
}

func TestUsageSummaryAggregatesProjectsWithoutContent(t *testing.T) {
	service := newTestService(t)
	limit := UsageLimit{TokenLimit: 10_000, IntervalCount: 6, IntervalUnit: IntervalHour}
	first, err := service.CreateProject(context.Background(), "First", []UsageLimit{limit})
	if err != nil {
		t.Fatalf("create first project: %v", err)
	}
	if _, err := service.CreateProject(context.Background(), "Second", nil); err != nil {
		t.Fatalf("create second project: %v", err)
	}
	firstKey, err := service.CreateAPIKey(context.Background(), first.ID, "Development")
	if err != nil {
		t.Fatalf("create first API key: %v", err)
	}
	secondKey, err := service.CreateAPIKey(context.Background(), first.ID, "CI")
	if err != nil {
		t.Fatalf("create second API key: %v", err)
	}
	if err := service.RecordUsage(context.Background(), UsageRecord{
		RequestID: "request-1", ProjectID: first.ID, APIKeyID: firstKey.Key.ID,
		InputTokens: 120, CachedInputTokens: 25, OutputTokens: 30, ReasoningOutputTokens: 9, TotalTokens: 150,
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if err := service.RecordUsage(context.Background(), UsageRecord{
		RequestID: "request-2", ProjectID: first.ID, APIKeyID: secondKey.Key.ID,
		InputTokens: 80, CachedInputTokens: 10, OutputTokens: 20, ReasoningOutputTokens: 4, TotalTokens: 100,
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}

	summary, err := service.UsageSummary(context.Background())
	if err != nil {
		t.Fatalf("read usage summary: %v", err)
	}
	if summary.Requests != 2 || summary.InputTokens != 200 || summary.CachedInputTokens != 35 || summary.OutputTokens != 50 || summary.ReasoningOutputTokens != 13 || summary.TotalTokens != 250 {
		t.Fatalf("unexpected usage totals: %#v", summary)
	}
	if len(summary.Projects) != 2 || summary.Projects[0].Name != "First" || summary.Projects[0].CachedInputTokens != 35 || summary.Projects[0].ReasoningOutputTokens != 13 || summary.Projects[0].TotalTokens != 250 || summary.Projects[1].Name != "Second" {
		t.Fatalf("unexpected project usage: %#v", summary.Projects)
	}
	if len(summary.Projects[0].Limits) != 1 || summary.Projects[0].Limits[0].UsageLimit != limit || summary.Projects[0].Limits[0].TotalTokens != 250 || !summary.Projects[0].Limits[0].PeriodStart.Equal(time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)) || !summary.Projects[0].Limits[0].PeriodEnd.Equal(time.Date(2026, time.September, 2, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected limit usage: %#v", summary.Projects[0].Limits)
	}
	if len(summary.APIKeys) != 2 || summary.APIKeys[0].APIKeyID != firstKey.Key.ID || summary.APIKeys[0].TotalTokens != 150 || summary.APIKeys[1].APIKeyID != secondKey.Key.ID || summary.APIKeys[1].TotalTokens != 100 {
		t.Fatalf("unexpected API key usage: %#v", summary.APIKeys)
	}
}

func TestUsageRejectsInvalidOrDuplicateRecords(t *testing.T) {
	service := newTestService(t)
	project, err := service.CreateProject(context.Background(), "Editor", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	created, err := service.CreateAPIKey(context.Background(), project.ID, "Development")
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}
	if err := service.RecordUsage(context.Background(), UsageRecord{RequestID: "request-1", ProjectID: "missing", APIKeyID: created.Key.ID, InputTokens: 1, OutputTokens: 1, TotalTokens: 2}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
	if err := service.RecordUsage(context.Background(), UsageRecord{RequestID: "request-1", ProjectID: project.ID, APIKeyID: "missing", InputTokens: 1, OutputTokens: 1, TotalTokens: 2}); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("expected ErrAPIKeyNotFound, got %v", err)
	}
	for _, record := range []UsageRecord{
		{ProjectID: project.ID, APIKeyID: created.Key.ID, InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		{RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID, InputTokens: -1, OutputTokens: 1},
		{RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID, InputTokens: 1, CachedInputTokens: -1, OutputTokens: 1},
		{RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID, InputTokens: 1, OutputTokens: -1},
		{RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID, InputTokens: 1, OutputTokens: 1, ReasoningOutputTokens: -1},
		{RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID, InputTokens: 1, OutputTokens: 1, TotalTokens: -1},
		{RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID, InputTokens: 1, CachedInputTokens: 2, OutputTokens: 1, TotalTokens: 2},
		{RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID, InputTokens: 1, OutputTokens: 1, ReasoningOutputTokens: 2, TotalTokens: 2},
		{RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID, InputTokens: 1, OutputTokens: 1, TotalTokens: 3},
	} {
		if err := service.RecordUsage(context.Background(), record); !errors.Is(err, ErrInvalidUsage) {
			t.Fatalf("expected ErrInvalidUsage, got %v", err)
		}
	}
	record := UsageRecord{RequestID: "request-1", ProjectID: project.ID, APIKeyID: created.Key.ID, InputTokens: 1, OutputTokens: 1, TotalTokens: 2}
	if err := service.RecordUsage(context.Background(), record); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if err := service.RecordUsage(context.Background(), record); !errors.Is(err, ErrDuplicateUsage) {
		t.Fatalf("expected ErrDuplicateUsage, got %v", err)
	}
}

func TestOpenValidatesDataSourceAndClosedDatabaseErrors(t *testing.T) {
	if _, err := Open(Options{}); err == nil {
		t.Fatal("expected missing data source name to fail")
	}
	service := newTestServiceWithoutCleanup(t)
	if err := service.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}
	ctx := context.Background()
	if _, err := service.ListProjects(ctx); err == nil {
		t.Fatal("expected project list on closed database to fail")
	}
	if _, err := service.ListAPIKeys(ctx); err == nil {
		t.Fatal("expected API key list on closed database to fail")
	}
	if _, err := service.UsageSummary(ctx); err == nil {
		t.Fatal("expected usage summary on closed database to fail")
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	return newTestServiceWithClock(t, func() time.Time { return projectTestNow })
}

func newTestServiceWithClock(t *testing.T, clock func() time.Time) *Service {
	t.Helper()
	service := newTestServiceWithoutCleanupAt(t, clock)
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("close project service: %v", err)
		}
	})
	return service
}

func newTestServiceWithoutCleanup(t *testing.T) *Service {
	t.Helper()
	return newTestServiceWithoutCleanupAt(t, func() time.Time { return projectTestNow })
}

func newTestServiceWithoutCleanupAt(t *testing.T, clock func() time.Time) *Service {
	t.Helper()
	entropy := make([]byte, 0, 1024)
	for value := byte(1); value <= 64; value++ {
		entropy = append(entropy, bytes.Repeat([]byte{value}, 16)...)
	}
	service, err := Open(Options{
		DataSourceName: ":memory:",
		Clock:          clock,
		Random:         bytes.NewReader(entropy),
	})
	if err != nil {
		t.Fatalf("open project service: %v", err)
	}
	return service
}

func pointer[T any](value T) *T { return &value }
