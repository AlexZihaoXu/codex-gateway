package projects

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

const (
	maximumProjectNameRunes  = 80
	maximumAPIKeyNameRunes   = 80
	identifierBytes          = 16
	apiKeySecretBytes        = 32
	dashboardChatKeyPrefix   = "key_dashboard_"
	dashboardChatKeyName     = "Dashboard Chat"
	dashboardChatKeyDisplay  = "session"
	dashboardChatProjectID   = "prj_dashboard_chat"
	dashboardChatProjectName = "Dashboard Chat"
)

var (
	ErrInvalidName        = errors.New("project name must contain between 1 and 80 characters")
	ErrInvalidLimit       = errors.New("usage limits must contain unique valid windows and positive safe-integer values")
	ErrDuplicateName      = errors.New("a project with this name already exists")
	ErrProjectNotFound    = errors.New("project was not found")
	ErrInvalidAPIKeyName  = errors.New("API key name must contain between 1 and 80 characters")
	ErrAPIKeyNotFound     = errors.New("API key was not found or is already revoked")
	ErrInvalidAPIKey      = errors.New("API key is invalid or revoked")
	ErrTokenLimitExceeded = errors.New("a project usage limit has been reached")
	ErrInvalidUsage       = errors.New("usage requires a request identifier and non-negative token counts")
	ErrDuplicateUsage     = errors.New("usage has already been recorded for this request")
)

type Options struct {
	DataSourceName string
	Clock          func() time.Time
	Random         io.Reader
}

type Project struct {
	ID        string
	Name      string
	Limits    []UsageLimit
	CreatedAt time.Time
}

type APIKey struct {
	ID         string
	ProjectID  string
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

type CreatedAPIKey struct {
	Key    APIKey
	Secret string
}

type ProjectAccess struct {
	APIKeyID  string
	ProjectID string
	Limits    []UsageLimitUsage
}

type ProjectUsage struct {
	ProjectID             string
	Name                  string
	Limits                []UsageLimitUsage
	Requests              int64
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
}

type APIKeyUsage struct {
	APIKeyID              string
	ProjectID             string
	Name                  string
	Prefix                string
	Requests              int64
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
}

type UsageRecord struct {
	RequestID             string
	ProjectID             string
	APIKeyID              string
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
}

type Usage struct {
	PeriodStart           time.Time
	PeriodEnd             time.Time
	Projects              []ProjectUsage
	APIKeys               []APIKeyUsage
	Requests              int64
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
}

type Service struct {
	database *sql.DB
	clock    func() time.Time
	random   io.Reader
}

func Open(options Options) (*Service, error) {
	if options.DataSourceName == "" {
		return nil, errors.New("project data source name is required")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	database, err := sql.Open("sqlite", options.DataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open project database: %w", err)
	}
	database.SetMaxOpenConns(1)
	service := &Service{database: database, clock: options.Clock, random: options.Random}
	if err := service.initialize(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	return service, nil
}

func (service *Service) Close() error {
	return service.database.Close()
}

func (service *Service) CreateProject(ctx context.Context, name string, limits []UsageLimit) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maximumProjectNameRunes {
		return Project{}, ErrInvalidName
	}
	if err := validateUsageLimits(limits); err != nil {
		return Project{}, ErrInvalidLimit
	}
	id, err := service.randomIdentifier("prj_")
	if err != nil {
		return Project{}, fmt.Errorf("generate project identifier: %w", err)
	}
	createdAt := service.clock().UTC()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project creation: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO project (id, name, monthly_token_limit, created_at)
		VALUES (?, ?, NULL, ?)
		ON CONFLICT(name) DO NOTHING
	`, id, name, createdAt.Unix())
	if err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Project{}, fmt.Errorf("inspect project creation: %w", err)
	}
	if rowsAffected == 0 {
		return Project{}, ErrDuplicateName
	}
	if err := insertUsageLimits(ctx, transaction, id, limits, createdAt); err != nil {
		return Project{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project creation: %w", err)
	}
	return Project{ID: id, Name: name, Limits: copyUsageLimits(limits), CreatedAt: createdAt}, nil
}

func (service *Service) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := service.database.QueryContext(ctx, `
		SELECT id, name, created_at
		FROM project
		WHERE id <> ?
		ORDER BY created_at, id
	`, dashboardChatProjectID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	result := make([]Project, 0)
	for rows.Next() {
		var project Project
		var createdAt int64
		if err := rows.Scan(&project.ID, &project.Name, &createdAt); err != nil {
			return nil, fmt.Errorf("decode project: %w", err)
		}
		project.CreatedAt = time.Unix(createdAt, 0).UTC()
		result = append(result, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close project rows: %w", err)
	}
	limits, err := service.listUsageLimits(ctx)
	if err != nil {
		return nil, err
	}
	for index := range result {
		result[index].Limits = limits[result[index].ID]
	}
	return result, nil
}

func (service *Service) UpdateProject(ctx context.Context, id, name string, limits []UsageLimit) (Project, error) {
	if id == dashboardChatProjectID {
		return Project{}, ErrProjectNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maximumProjectNameRunes {
		return Project{}, ErrInvalidName
	}
	if err := validateUsageLimits(limits); err != nil {
		return Project{}, ErrInvalidLimit
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project update: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	result, err := transaction.ExecContext(ctx, `
		UPDATE project SET name = ?, monthly_token_limit = NULL
		WHERE id = ? AND NOT EXISTS (
			SELECT 1 FROM project AS conflicting WHERE conflicting.name = ? AND conflicting.id <> ?
		)
	`, name, id, name, id)
	if err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Project{}, fmt.Errorf("inspect project update: %w", err)
	}
	if rowsAffected == 0 {
		var exists bool
		err := transaction.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM project WHERE id = ?)", id).Scan(&exists)
		if err != nil {
			return Project{}, fmt.Errorf("read project: %w", err)
		}
		if !exists {
			return Project{}, ErrProjectNotFound
		}
		return Project{}, ErrDuplicateName
	}
	var createdAt int64
	if err := transaction.QueryRowContext(ctx, "SELECT created_at FROM project WHERE id = ?", id).Scan(&createdAt); err != nil {
		return Project{}, fmt.Errorf("read updated project: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM project_usage_limit WHERE project_id = ?", id); err != nil {
		return Project{}, fmt.Errorf("replace project usage limits: %w", err)
	}
	if err := insertUsageLimits(ctx, transaction, id, limits, service.clock().UTC()); err != nil {
		return Project{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project update: %w", err)
	}
	return Project{
		ID: id, Name: name, Limits: copyUsageLimits(limits),
		CreatedAt: time.Unix(createdAt, 0).UTC(),
	}, nil
}

func (service *Service) DeleteProject(ctx context.Context, id string) error {
	if id == dashboardChatProjectID {
		return ErrProjectNotFound
	}
	result, err := service.database.ExecContext(ctx, "DELETE FROM project WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect project deletion: %w", err)
	}
	if rowsAffected == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func (service *Service) CreateAPIKey(ctx context.Context, projectID, name string) (CreatedAPIKey, error) {
	if projectID == dashboardChatProjectID {
		return CreatedAPIKey{}, ErrProjectNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maximumAPIKeyNameRunes {
		return CreatedAPIKey{}, ErrInvalidAPIKeyName
	}
	exists, err := service.projectExists(ctx, projectID)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	if !exists {
		return CreatedAPIKey{}, ErrProjectNotFound
	}
	id, err := service.randomIdentifier("key_")
	if err != nil {
		return CreatedAPIKey{}, fmt.Errorf("generate API key identifier: %w", err)
	}
	secretBytes := make([]byte, apiKeySecretBytes)
	if _, err := io.ReadFull(service.random, secretBytes); err != nil {
		return CreatedAPIKey{}, fmt.Errorf("generate API key secret: %w", err)
	}
	secret := "cg_" + base64.RawURLEncoding.EncodeToString(secretBytes)
	prefix := secret[:11]
	secretHash := sha256.Sum256([]byte(secret))
	createdAt := service.clock().UTC()
	if _, err := service.database.ExecContext(ctx, `
		INSERT INTO project_api_key (id, project_id, name, key_hash, prefix, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, projectID, name, secretHash[:], prefix, createdAt.Unix()); err != nil {
		return CreatedAPIKey{}, fmt.Errorf("create API key: %w", err)
	}
	return CreatedAPIKey{
		Key:    APIKey{ID: id, ProjectID: projectID, Name: name, Prefix: prefix, CreatedAt: createdAt},
		Secret: secret,
	}, nil
}

func (service *Service) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := service.database.QueryContext(ctx, `
		SELECT id, project_id, name, prefix, created_at, last_used_at, revoked_at
		FROM project_api_key
		WHERE revoked_at IS NULL AND id NOT GLOB ?
		ORDER BY created_at, id
	`, dashboardChatKeyPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}
	defer rows.Close()

	result := make([]APIKey, 0)
	for rows.Next() {
		var key APIKey
		var createdAt int64
		var lastUsedAt, revokedAt sql.NullInt64
		if err := rows.Scan(&key.ID, &key.ProjectID, &key.Name, &key.Prefix, &createdAt, &lastUsedAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("decode API key: %w", err)
		}
		key.CreatedAt = time.Unix(createdAt, 0).UTC()
		key.LastUsedAt = timePointer(lastUsedAt)
		key.RevokedAt = timePointer(revokedAt)
		result = append(result, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate API keys: %w", err)
	}
	return result, nil
}

func (service *Service) AuthenticateAPIKey(ctx context.Context, secret string) (ProjectAccess, error) {
	secretHash := sha256.Sum256([]byte(secret))
	now := service.clock().UTC()

	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ProjectAccess{}, fmt.Errorf("begin API key authentication: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	var access ProjectAccess
	err = transaction.QueryRowContext(ctx, `
		SELECT k.id, k.project_id
		FROM project_api_key k
		WHERE k.key_hash = ? AND k.revoked_at IS NULL AND k.id NOT GLOB ?
	`, secretHash[:], dashboardChatKeyPrefix+"*").Scan(
		&access.APIKeyID,
		&access.ProjectID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectAccess{}, ErrInvalidAPIKey
	}
	if err != nil {
		return ProjectAccess{}, fmt.Errorf("authenticate API key: %w", err)
	}
	limits, err := readUsageLimitsForProject(ctx, transaction, access.ProjectID)
	if err != nil {
		return ProjectAccess{}, err
	}
	exceeded := false
	for _, limit := range limits {
		usage, err := readUsageForLimit(ctx, transaction, access.ProjectID, limit, now)
		if err != nil {
			return ProjectAccess{}, err
		}
		access.Limits = append(access.Limits, usage)
		if usage.TotalTokens >= usage.TokenLimit {
			exceeded = true
		}
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE project_api_key SET last_used_at = ? WHERE id = ?", now.Unix(), access.APIKeyID); err != nil {
		return ProjectAccess{}, fmt.Errorf("track API key use: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return ProjectAccess{}, fmt.Errorf("commit API key authentication: %w", err)
	}
	if exceeded {
		return ProjectAccess{}, ErrTokenLimitExceeded
	}
	return access, nil
}

func (service *Service) AuthorizeDashboardChat(ctx context.Context, projectID string) (ProjectAccess, error) {
	projectID = strings.TrimSpace(projectID)
	now := service.clock().UTC()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return ProjectAccess{}, fmt.Errorf("begin dashboard chat authorization: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if projectID == "" {
		projectID, err = ensureDashboardChatProject(ctx, transaction, now)
		if err != nil {
			return ProjectAccess{}, err
		}
	}

	var exists bool
	if err := transaction.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM project WHERE id = ?)", projectID).Scan(&exists); err != nil {
		return ProjectAccess{}, fmt.Errorf("read dashboard chat project: %w", err)
	}
	if !exists {
		return ProjectAccess{}, ErrProjectNotFound
	}
	access := ProjectAccess{ProjectID: projectID, APIKeyID: dashboardChatAPIKeyID(projectID)}
	limits, err := readUsageLimitsForProject(ctx, transaction, projectID)
	if err != nil {
		return ProjectAccess{}, err
	}
	for _, limit := range limits {
		usage, err := readUsageForLimit(ctx, transaction, projectID, limit, now)
		if err != nil {
			return ProjectAccess{}, err
		}
		access.Limits = append(access.Limits, usage)
		if usage.TotalTokens >= usage.TokenLimit {
			return ProjectAccess{}, ErrTokenLimitExceeded
		}
	}
	keyHash := sha256.Sum256([]byte("dashboard-chat:" + access.APIKeyID))
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO project_api_key (id, project_id, name, key_hash, prefix, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, access.APIKeyID, projectID, dashboardChatKeyName, keyHash[:], dashboardChatKeyDisplay, now.Unix()); err != nil {
		return ProjectAccess{}, fmt.Errorf("create dashboard chat usage identity: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return ProjectAccess{}, fmt.Errorf("commit dashboard chat authorization: %w", err)
	}
	return access, nil
}

func ensureDashboardChatProject(ctx context.Context, transaction *sql.Tx, now time.Time) (string, error) {
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO project (id, name, monthly_token_limit, created_at)
		VALUES (?, ?, NULL, ?)
		ON CONFLICT DO NOTHING
	`, dashboardChatProjectID, dashboardChatProjectName, now.Unix()); err != nil {
		return "", fmt.Errorf("create dashboard chat project: %w", err)
	}
	var projectID string
	if err := transaction.QueryRowContext(ctx,
		"SELECT id FROM project WHERE name = ? COLLATE NOCASE",
		dashboardChatProjectName,
	).Scan(&projectID); err != nil {
		return "", fmt.Errorf("read dashboard chat project: %w", err)
	}
	return projectID, nil
}

func dashboardChatAPIKeyID(projectID string) string {
	digest := sha256.Sum256([]byte(projectID))
	return dashboardChatKeyPrefix + base64.RawURLEncoding.EncodeToString(digest[:12])
}

func (service *Service) RevokeAPIKey(ctx context.Context, id string) error {
	result, err := service.database.ExecContext(ctx, `
		UPDATE project_api_key SET revoked_at = ?
		WHERE id = ? AND revoked_at IS NULL AND id NOT GLOB ?
	`, service.clock().UTC().Unix(), id, dashboardChatKeyPrefix+"*")
	if err != nil {
		return fmt.Errorf("revoke API key: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect API key revocation: %w", err)
	}
	if rowsAffected == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (service *Service) RecordUsage(ctx context.Context, record UsageRecord) error {
	if record.RequestID == "" || record.ProjectID == "" || record.APIKeyID == "" ||
		record.InputTokens < 0 || record.CachedInputTokens < 0 || record.OutputTokens < 0 ||
		record.ReasoningOutputTokens < 0 || record.TotalTokens < 0 ||
		record.CachedInputTokens > record.InputTokens || record.ReasoningOutputTokens > record.OutputTokens ||
		record.TotalTokens != record.InputTokens+record.OutputTokens {
		return ErrInvalidUsage
	}
	exists, err := service.projectExists(ctx, record.ProjectID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrProjectNotFound
	}
	var keyExists bool
	if err := service.database.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM project_api_key WHERE id = ? AND project_id = ?)",
		record.APIKeyID, record.ProjectID,
	).Scan(&keyExists); err != nil {
		return fmt.Errorf("read API key: %w", err)
	}
	if !keyExists {
		return ErrAPIKeyNotFound
	}
	result, err := service.database.ExecContext(ctx, `
		INSERT INTO project_usage (
			request_id, project_id, api_key_id, input_tokens, cached_input_tokens,
			output_tokens, reasoning_output_tokens, total_tokens, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(request_id) DO NOTHING
	`, record.RequestID, record.ProjectID, record.APIKeyID, record.InputTokens, record.CachedInputTokens,
		record.OutputTokens, record.ReasoningOutputTokens, record.TotalTokens, service.clock().UTC().Unix())
	if err != nil {
		return fmt.Errorf("record project usage: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect project usage: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDuplicateUsage
	}
	return nil
}

func (service *Service) UsageSummary(ctx context.Context) (Usage, error) {
	at := service.clock().UTC()
	periodStart := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)
	rows, err := service.database.QueryContext(ctx, `
		SELECT p.id, p.name,
			COUNT(u.request_id), COALESCE(SUM(u.input_tokens), 0),
			COALESCE(SUM(u.cached_input_tokens), 0), COALESCE(SUM(u.output_tokens), 0),
			COALESCE(SUM(u.reasoning_output_tokens), 0), COALESCE(SUM(u.total_tokens), 0)
		FROM project p
		LEFT JOIN project_usage u
			ON u.project_id = p.id AND u.created_at >= ? AND u.created_at < ?
		GROUP BY p.id, p.name, p.created_at
		ORDER BY p.created_at, p.id
	`, periodStart.Unix(), periodEnd.Unix())
	if err != nil {
		return Usage{}, fmt.Errorf("read project usage: %w", err)
	}
	defer rows.Close()

	usage := Usage{PeriodStart: periodStart, PeriodEnd: periodEnd, Projects: make([]ProjectUsage, 0), APIKeys: make([]APIKeyUsage, 0)}
	for rows.Next() {
		var project ProjectUsage
		if err := rows.Scan(
			&project.ProjectID, &project.Name, &project.Requests, &project.InputTokens,
			&project.CachedInputTokens, &project.OutputTokens, &project.ReasoningOutputTokens, &project.TotalTokens,
		); err != nil {
			return Usage{}, fmt.Errorf("decode project usage: %w", err)
		}
		usage.Projects = append(usage.Projects, project)
		usage.Requests += project.Requests
		usage.InputTokens += project.InputTokens
		usage.CachedInputTokens += project.CachedInputTokens
		usage.OutputTokens += project.OutputTokens
		usage.ReasoningOutputTokens += project.ReasoningOutputTokens
		usage.TotalTokens += project.TotalTokens
	}
	if err := rows.Err(); err != nil {
		return Usage{}, fmt.Errorf("iterate project usage: %w", err)
	}
	if err := rows.Close(); err != nil {
		return Usage{}, fmt.Errorf("close project usage rows: %w", err)
	}
	for index := range usage.Projects {
		limits, err := readUsageLimitsForProject(ctx, service.database, usage.Projects[index].ProjectID)
		if err != nil {
			return Usage{}, err
		}
		usage.Projects[index].Limits = make([]UsageLimitUsage, 0, len(limits))
		for _, limit := range limits {
			limitUsage, err := readUsageForLimit(ctx, service.database, usage.Projects[index].ProjectID, limit, at)
			if err != nil {
				return Usage{}, err
			}
			usage.Projects[index].Limits = append(usage.Projects[index].Limits, limitUsage)
		}
	}
	keyRows, err := service.database.QueryContext(ctx, `
		SELECT k.id, k.project_id, k.name, k.prefix,
			COUNT(u.request_id), COALESCE(SUM(u.input_tokens), 0),
			COALESCE(SUM(u.cached_input_tokens), 0), COALESCE(SUM(u.output_tokens), 0),
			COALESCE(SUM(u.reasoning_output_tokens), 0), COALESCE(SUM(u.total_tokens), 0)
		FROM project_api_key k
		LEFT JOIN project_usage u
			ON u.api_key_id = k.id AND u.created_at >= ? AND u.created_at < ?
		GROUP BY k.id, k.project_id, k.name, k.prefix, k.created_at
		ORDER BY k.created_at, k.id
	`, periodStart.Unix(), periodEnd.Unix())
	if err != nil {
		return Usage{}, fmt.Errorf("read API key usage: %w", err)
	}
	defer keyRows.Close()
	for keyRows.Next() {
		var key APIKeyUsage
		if err := keyRows.Scan(
			&key.APIKeyID, &key.ProjectID, &key.Name, &key.Prefix, &key.Requests,
			&key.InputTokens, &key.CachedInputTokens, &key.OutputTokens,
			&key.ReasoningOutputTokens, &key.TotalTokens,
		); err != nil {
			return Usage{}, fmt.Errorf("decode API key usage: %w", err)
		}
		usage.APIKeys = append(usage.APIKeys, key)
	}
	if err := keyRows.Err(); err != nil {
		return Usage{}, fmt.Errorf("iterate API key usage: %w", err)
	}
	return usage, nil
}

func (service *Service) projectExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	if err := service.database.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM project WHERE id = ?)", id).Scan(&exists); err != nil {
		return false, fmt.Errorf("read project: %w", err)
	}
	return exists, nil
}

func (service *Service) randomIdentifier(prefix string) (string, error) {
	identifier := make([]byte, identifierBytes)
	if _, err := io.ReadFull(service.random, identifier); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(identifier), nil
}

func (service *Service) initialize(ctx context.Context) error {
	statements := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		`CREATE TABLE IF NOT EXISTS project (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL COLLATE NOCASE UNIQUE,
			monthly_token_limit INTEGER CHECK (monthly_token_limit IS NULL OR monthly_token_limit > 0),
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS project_usage_limit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
			token_limit INTEGER NOT NULL CHECK (token_limit > 0 AND token_limit <= 9007199254740991),
			interval_count INTEGER NOT NULL CHECK (interval_count > 0 AND interval_count <= 10000),
			interval_unit TEXT NOT NULL CHECK (interval_unit IN ('minute', 'hour', 'day', 'week', 'month')),
			created_at INTEGER NOT NULL,
			UNIQUE (project_id, interval_unit, interval_count)
		)`,
		`CREATE TABLE IF NOT EXISTS project_api_key (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			key_hash BLOB NOT NULL UNIQUE,
			prefix TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_used_at INTEGER,
			revoked_at INTEGER
		)`,
		"CREATE INDEX IF NOT EXISTS project_api_key_project ON project_api_key (project_id)",
		`CREATE TABLE IF NOT EXISTS project_usage (
			request_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
			api_key_id TEXT NOT NULL REFERENCES project_api_key(id) ON DELETE CASCADE,
			input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
			cached_input_tokens INTEGER NOT NULL CHECK (cached_input_tokens >= 0),
			output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
			reasoning_output_tokens INTEGER NOT NULL CHECK (reasoning_output_tokens >= 0),
			total_tokens INTEGER NOT NULL CHECK (total_tokens >= 0),
			created_at INTEGER NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := service.database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize project database: %w", err)
		}
	}
	if err := service.ensureUsageColumns(ctx); err != nil {
		return err
	}
	if err := service.migrateLegacyLimits(ctx); err != nil {
		return err
	}
	for _, statement := range []string{
		"CREATE INDEX IF NOT EXISTS project_usage_period ON project_usage (project_id, created_at)",
		"CREATE INDEX IF NOT EXISTS project_usage_api_key_period ON project_usage (api_key_id, created_at)",
	} {
		if _, err := service.database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize project database indexes: %w", err)
		}
	}
	return nil
}

func (service *Service) migrateLegacyLimits(ctx context.Context) error {
	if _, err := service.database.ExecContext(ctx, `
		INSERT OR IGNORE INTO project_usage_limit (
			project_id, token_limit, interval_count, interval_unit, created_at
		)
		SELECT id, monthly_token_limit, 1, 'month', created_at
		FROM project
		WHERE monthly_token_limit IS NOT NULL
	`); err != nil {
		return fmt.Errorf("migrate monthly project limits: %w", err)
	}
	if _, err := service.database.ExecContext(ctx, `
		UPDATE project SET monthly_token_limit = NULL
		WHERE monthly_token_limit IS NOT NULL
	`); err != nil {
		return fmt.Errorf("finalize monthly project limit migration: %w", err)
	}
	return nil
}

func (service *Service) listUsageLimits(ctx context.Context) (map[string][]UsageLimit, error) {
	rows, err := service.database.QueryContext(ctx, `
		SELECT project_id, token_limit, interval_count, interval_unit
		FROM project_usage_limit
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("list project usage limits: %w", err)
	}
	defer rows.Close()
	limits := make(map[string][]UsageLimit)
	for rows.Next() {
		var projectID string
		var limit UsageLimit
		if err := rows.Scan(&projectID, &limit.TokenLimit, &limit.IntervalCount, &limit.IntervalUnit); err != nil {
			return nil, fmt.Errorf("decode project usage limit: %w", err)
		}
		limits[projectID] = append(limits[projectID], limit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project usage limits: %w", err)
	}
	for _, projectLimits := range limits {
		if err := validateUsageLimits(projectLimits); err != nil {
			return nil, fmt.Errorf("validate stored project usage limits: %w", err)
		}
	}
	return limits, nil
}

type usageQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readUsageLimitsForProject(ctx context.Context, queryer usageQueryer, projectID string) ([]UsageLimit, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT token_limit, interval_count, interval_unit
		FROM project_usage_limit
		WHERE project_id = ?
		ORDER BY id
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project usage limits: %w", err)
	}
	defer rows.Close()
	limits := make([]UsageLimit, 0)
	for rows.Next() {
		var limit UsageLimit
		if err := rows.Scan(&limit.TokenLimit, &limit.IntervalCount, &limit.IntervalUnit); err != nil {
			return nil, fmt.Errorf("decode project usage limit: %w", err)
		}
		limits = append(limits, limit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project usage limits: %w", err)
	}
	if err := validateUsageLimits(limits); err != nil {
		return nil, fmt.Errorf("validate stored project usage limits: %w", err)
	}
	return limits, nil
}

func readUsageForLimit(ctx context.Context, queryer usageQueryer, projectID string, limit UsageLimit, at time.Time) (UsageLimitUsage, error) {
	periodStart, periodEnd, err := usageLimitWindow(at, limit)
	if err != nil {
		return UsageLimitUsage{}, err
	}
	usage := UsageLimitUsage{UsageLimit: limit, PeriodStart: periodStart, PeriodEnd: periodEnd}
	if err := queryer.QueryRowContext(ctx, `
		SELECT COUNT(request_id), COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(cached_input_tokens), 0), COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_output_tokens), 0), COALESCE(SUM(total_tokens), 0)
		FROM project_usage
		WHERE project_id = ? AND created_at >= ? AND created_at < ?
	`, projectID, periodStart.Unix(), periodEnd.Unix()).Scan(
		&usage.Requests, &usage.InputTokens, &usage.CachedInputTokens, &usage.OutputTokens,
		&usage.ReasoningOutputTokens, &usage.TotalTokens,
	); err != nil {
		return UsageLimitUsage{}, fmt.Errorf("read project usage limit: %w", err)
	}
	return usage, nil
}

func insertUsageLimits(ctx context.Context, transaction *sql.Tx, projectID string, limits []UsageLimit, createdAt time.Time) error {
	for _, limit := range limits {
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO project_usage_limit (
				project_id, token_limit, interval_count, interval_unit, created_at
			)
			VALUES (?, ?, ?, ?, ?)
		`, projectID, limit.TokenLimit, limit.IntervalCount, limit.IntervalUnit, createdAt.Unix()); err != nil {
			return fmt.Errorf("create project usage limit: %w", err)
		}
	}
	return nil
}

func (service *Service) ensureUsageColumns(ctx context.Context) error {
	columns := map[string]string{
		"api_key_id":              "TEXT",
		"cached_input_tokens":     "INTEGER NOT NULL DEFAULT 0 CHECK (cached_input_tokens >= 0)",
		"reasoning_output_tokens": "INTEGER NOT NULL DEFAULT 0 CHECK (reasoning_output_tokens >= 0)",
		"total_tokens":            "INTEGER NOT NULL DEFAULT 0 CHECK (total_tokens >= 0)",
	}
	for name, definition := range columns {
		exists, err := service.columnExists(ctx, "project_usage", name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := service.database.ExecContext(ctx, "ALTER TABLE project_usage ADD COLUMN "+name+" "+definition); err != nil {
			return fmt.Errorf("add project usage column %s: %w", name, err)
		}
	}
	if _, err := service.database.ExecContext(ctx, `
		UPDATE project_usage SET total_tokens = input_tokens + output_tokens
		WHERE total_tokens = 0 AND input_tokens + output_tokens > 0
	`); err != nil {
		return fmt.Errorf("backfill project usage totals: %w", err)
	}
	return nil
}

func (service *Service) columnExists(ctx context.Context, table, column string) (bool, error) {
	rows, err := service.database.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var sequence int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&sequence, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, fmt.Errorf("decode %s column: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate %s columns: %w", table, err)
	}
	return false, nil
}

func timePointer(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := time.Unix(value.Int64, 0).UTC()
	return &result
}
