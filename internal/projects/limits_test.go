package projects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestUsageLimitWindowUsesFixedUTCBoundaries(t *testing.T) {
	now := time.Date(2026, time.September, 2, 15, 47, 23, 0, time.FixedZone("EDT", -4*60*60))
	tests := []struct {
		name      string
		limit     UsageLimit
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "minute",
			limit:     UsageLimit{TokenLimit: 1, IntervalCount: 1, IntervalUnit: IntervalMinute},
			wantStart: time.Date(2026, time.September, 2, 19, 47, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, time.September, 2, 19, 48, 0, 0, time.UTC),
		},
		{
			name:      "six hours",
			limit:     UsageLimit{TokenLimit: 1, IntervalCount: 6, IntervalUnit: IntervalHour},
			wantStart: time.Date(2026, time.September, 2, 18, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "day",
			limit:     UsageLimit{TokenLimit: 1, IntervalCount: 1, IntervalUnit: IntervalDay},
			wantStart: time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "week",
			limit:     UsageLimit{TokenLimit: 1, IntervalCount: 1, IntervalUnit: IntervalWeek},
			wantStart: time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "three months",
			limit:     UsageLimit{TokenLimit: 1, IntervalCount: 3, IntervalUnit: IntervalMonth},
			wantStart: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, err := usageLimitWindow(now, test.limit)
			if err != nil {
				t.Fatalf("calculate window: %v", err)
			}
			if !start.Equal(test.wantStart) || !end.Equal(test.wantEnd) {
				t.Fatalf("window = %s to %s, want %s to %s", start, end, test.wantStart, test.wantEnd)
			}
		})
	}
}

func TestUsageLimitValidationRejectsInvalidAndDuplicatePolicies(t *testing.T) {
	valid := UsageLimit{TokenLimit: 1000, IntervalCount: 1, IntervalUnit: IntervalHour}
	tests := []struct {
		name   string
		limits []UsageLimit
	}{
		{name: "zero tokens", limits: []UsageLimit{{IntervalCount: 1, IntervalUnit: IntervalHour}}},
		{name: "unsafe token integer", limits: []UsageLimit{{TokenLimit: maximumSafeInteger + 1, IntervalCount: 1, IntervalUnit: IntervalHour}}},
		{name: "zero interval", limits: []UsageLimit{{TokenLimit: 1, IntervalUnit: IntervalHour}}},
		{name: "large interval", limits: []UsageLimit{{TokenLimit: 1, IntervalCount: maximumIntervalCount + 1, IntervalUnit: IntervalHour}}},
		{name: "invalid unit", limits: []UsageLimit{{TokenLimit: 1, IntervalCount: 1, IntervalUnit: "fortnight"}}},
		{name: "duplicate window", limits: []UsageLimit{valid, {TokenLimit: 2000, IntervalCount: 1, IntervalUnit: IntervalHour}}},
	}
	tooMany := make([]UsageLimit, maximumProjectLimits+1)
	for index := range tooMany {
		tooMany[index] = UsageLimit{TokenLimit: int64(index + 1), IntervalCount: int64(index + 1), IntervalUnit: IntervalMinute}
	}
	tests = append(tests, struct {
		name   string
		limits []UsageLimit
	}{name: "too many policies", limits: tooMany})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateUsageLimits(test.limits); !errors.Is(err, ErrInvalidLimit) {
				t.Fatalf("expected ErrInvalidLimit, got %v", err)
			}
		})
	}
	if err := validateUsageLimits([]UsageLimit{valid}); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
}

func TestOpenMigratesLegacyMonthlyLimitExactlyOnce(t *testing.T) {
	dsn := fmt.Sprintf("file:legacy-limits-%d?mode=memory&cache=shared", time.Now().UnixNano())
	legacy, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	if _, err := legacy.Exec(`CREATE TABLE project (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL COLLATE NOCASE UNIQUE,
		monthly_token_limit INTEGER,
		created_at INTEGER NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy project table: %v", err)
	}
	if _, err := legacy.Exec(
		"INSERT INTO project (id, name, monthly_token_limit, created_at) VALUES (?, ?, ?, ?)",
		"prj_legacy", "Legacy", 250_000, projectTestNow.Unix(),
	); err != nil {
		t.Fatalf("insert legacy project: %v", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		service, err := Open(Options{DataSourceName: dsn, Clock: func() time.Time { return projectTestNow }})
		if err != nil {
			t.Fatalf("open migrated service: %v", err)
		}
		projects, err := service.ListProjects(context.Background())
		if err != nil {
			_ = service.Close()
			t.Fatalf("list migrated projects: %v", err)
		}
		if len(projects) != 1 || len(projects[0].Limits) != 1 || projects[0].Limits[0] != (UsageLimit{
			TokenLimit: 250_000, IntervalCount: 1, IntervalUnit: IntervalMonth,
		}) {
			_ = service.Close()
			t.Fatalf("unexpected migrated project: %#v", projects)
		}
		if err := service.Close(); err != nil {
			t.Fatalf("close migrated service: %v", err)
		}
	}
}

func TestOpenBackfillsLegacyUsageWithUnknownModel(t *testing.T) {
	dsn := fmt.Sprintf("file:legacy-usage-%d?mode=memory&cache=shared", time.Now().UnixNano())
	legacy, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	for _, statement := range []string{
		`CREATE TABLE project (
			id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE UNIQUE,
			monthly_token_limit INTEGER, created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE project_api_key (
			id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
			name TEXT NOT NULL, key_hash BLOB NOT NULL UNIQUE, prefix TEXT NOT NULL,
			created_at INTEGER NOT NULL, last_used_at INTEGER, revoked_at INTEGER
		)`,
		`CREATE TABLE project_usage (
			request_id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
			api_key_id TEXT, input_tokens INTEGER NOT NULL, cached_input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL, reasoning_output_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL
		)`,
	} {
		if _, err := legacy.Exec(statement); err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}
	if _, err := legacy.Exec("INSERT INTO project VALUES (?, ?, NULL, ?)", "prj_legacy", "Legacy", projectTestNow.Unix()); err != nil {
		t.Fatalf("insert legacy project: %v", err)
	}
	if _, err := legacy.Exec("INSERT INTO project_api_key VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)", "key_legacy", "prj_legacy", "Legacy", []byte("hash"), "cg_legacy", projectTestNow.Unix()); err != nil {
		t.Fatalf("insert legacy key: %v", err)
	}
	if _, err := legacy.Exec("INSERT INTO project_usage VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", "req_legacy", "prj_legacy", "key_legacy", 8, 2, 3, 1, 11, projectTestNow.Unix()); err != nil {
		t.Fatalf("insert legacy usage: %v", err)
	}

	service, err := Open(Options{DataSourceName: dsn, Clock: func() time.Time { return projectTestNow }})
	if err != nil {
		t.Fatalf("open migrated service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	summary, err := service.UsageSummary(context.Background())
	if err != nil {
		t.Fatalf("read migrated usage: %v", err)
	}
	if len(summary.Models) != 1 || summary.Models[0].Model != "unknown" || summary.Models[0].TotalTokens != 11 {
		t.Fatalf("legacy usage was not grouped under unknown: %#v", summary.Models)
	}
}

func TestStoredUsageLimitsAreValidatedAtTheDatabaseBoundary(t *testing.T) {
	service := newTestService(t)
	project, err := service.CreateProject(context.Background(), "Boundary", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := service.database.Exec("PRAGMA ignore_check_constraints = ON"); err != nil {
		t.Fatalf("disable check constraints for boundary test: %v", err)
	}
	if _, err := service.database.Exec(`
		INSERT INTO project_usage_limit (project_id, token_limit, interval_count, interval_unit, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, project.ID, maximumSafeInteger+1, 1, IntervalHour, projectTestNow.Unix()); err != nil {
		t.Fatalf("insert invalid stored usage limit: %v", err)
	}
	if _, err := service.database.Exec("PRAGMA ignore_check_constraints = OFF"); err != nil {
		t.Fatalf("restore check constraints: %v", err)
	}

	if _, err := service.ListProjects(context.Background()); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("expected stored limit validation failure, got %v", err)
	}
}
