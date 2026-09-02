package chats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

const (
	maximumOwnerLength        = 320
	maximumSessionIDLength    = 128
	maximumTitleRunes         = 200
	maximumModelLength        = 128
	maximumProjectIDLength    = 128
	maximumInstructionsLength = 4 * 1024 * 1024
	maximumMessageLength      = 1024 * 1024
	maximumMessages           = 1025
)

var (
	ErrInvalidOwner    = errors.New("chat history owner must be a valid email address")
	ErrInvalidSession  = errors.New("chat session is invalid")
	ErrSessionNotFound = errors.New("chat session was not found")
	sessionIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
)

type Endpoint string

const (
	EndpointChatCompletions Endpoint = "chat-completions"
	EndpointResponses       Endpoint = "responses"
)

type ReasoningEffort string

const (
	ReasoningNone   ReasoningEffort = "none"
	ReasoningLow    ReasoningEffort = "low"
	ReasoningMedium ReasoningEffort = "medium"
	ReasoningHigh   ReasoningEffort = "high"
	ReasoningXHigh  ReasoningEffort = "xhigh"
	ReasoningMax    ReasoningEffort = "max"
	ReasoningUltra  ReasoningEffort = "ultra"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Usage struct {
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
}

type Message struct {
	Role      Role
	Content   string
	Reasoning string
	Usage     *Usage
}

type Session struct {
	ID              string
	Title           string
	Model           string
	ProjectID       string
	Endpoint        Endpoint
	ReasoningEffort ReasoningEffort
	Instructions    string
	Messages        []Message
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Options struct {
	DataSourceName string
	Clock          func() time.Time
}

type Service struct {
	database *sql.DB
	clock    func() time.Time
}

func Open(options Options) (*Service, error) {
	if strings.TrimSpace(options.DataSourceName) == "" {
		return nil, errors.New("chat history data source name is required")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	database, err := sql.Open("sqlite", options.DataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open chat history database: %w", err)
	}
	database.SetMaxOpenConns(1)
	service := &Service{database: database, clock: options.Clock}
	if err := service.initialize(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	return service, nil
}

func (service *Service) Close() error {
	return service.database.Close()
}

func (service *Service) SaveSession(ctx context.Context, owner string, session Session) error {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return err
	}
	normalizeSession(&session)
	if !validSession(session) {
		return ErrInvalidSession
	}
	now := service.clock().UTC()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin chat session update: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO dashboard_chat_session (
			owner_email, id, title, model, project_id, endpoint, reasoning_effort,
			instructions, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (owner_email, id) DO UPDATE SET
			title = excluded.title,
			model = excluded.model,
			project_id = excluded.project_id,
			endpoint = excluded.endpoint,
			reasoning_effort = excluded.reasoning_effort,
			instructions = excluded.instructions,
			updated_at = excluded.updated_at
	`, owner, session.ID, session.Title, session.Model, session.ProjectID, session.Endpoint,
		session.ReasoningEffort, session.Instructions, now.Unix(), now.Unix()); err != nil {
		return fmt.Errorf("store chat session: %w", err)
	}
	if err := preserveStoredUsage(ctx, transaction, owner, &session); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx,
		"DELETE FROM dashboard_chat_message WHERE owner_email = ? AND session_id = ?",
		owner, session.ID,
	); err != nil {
		return fmt.Errorf("replace chat session messages: %w", err)
	}
	for position, message := range session.Messages {
		hasUsage := 0
		usage := Usage{}
		if message.Usage != nil {
			hasUsage = 1
			usage = *message.Usage
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO dashboard_chat_message (
				owner_email, session_id, position, role, content, reasoning, has_usage,
				input_tokens, cached_input_tokens, output_tokens,
				reasoning_output_tokens, total_tokens
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, owner, session.ID, position, message.Role, message.Content, message.Reasoning, hasUsage,
			usage.InputTokens, usage.CachedInputTokens, usage.OutputTokens,
			usage.ReasoningOutputTokens, usage.TotalTokens); err != nil {
			return fmt.Errorf("store chat session message: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit chat session update: %w", err)
	}
	return nil
}

func preserveStoredUsage(ctx context.Context, transaction *sql.Tx, owner string, session *Session) error {
	rows, err := transaction.QueryContext(ctx, `
		SELECT position, role, content, reasoning, input_tokens, cached_input_tokens,
		       output_tokens, reasoning_output_tokens, total_tokens
		FROM dashboard_chat_message
		WHERE owner_email = ? AND session_id = ? AND has_usage = 1
	`, owner, session.ID)
	if err != nil {
		return fmt.Errorf("read existing chat usage: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var position int
		var role string
		var content string
		var usage Usage
		var reasoning string
		if err := rows.Scan(&position, &role, &content, &reasoning, &usage.InputTokens,
			&usage.CachedInputTokens, &usage.OutputTokens, &usage.ReasoningOutputTokens,
			&usage.TotalTokens); err != nil {
			return fmt.Errorf("scan existing chat usage: %w", err)
		}
		if position < 0 || position >= len(session.Messages) {
			continue
		}
		message := &session.Messages[position]
		if message.Usage == nil && message.Role == Role(role) && message.Content == content && message.Reasoning == reasoning {
			message.Usage = &usage
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate existing chat usage: %w", err)
	}
	return nil
}

func (service *Service) ListSessions(ctx context.Context, owner string) ([]Session, error) {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return nil, err
	}
	rows, err := service.database.QueryContext(ctx, `
		SELECT id, title, model, project_id, endpoint, reasoning_effort,
		       instructions, created_at, updated_at
		FROM dashboard_chat_session
		WHERE owner_email = ?
		ORDER BY updated_at DESC, id ASC
	`, owner)
	if err != nil {
		return nil, fmt.Errorf("list chat sessions: %w", err)
	}
	sessions := make([]Session, 0)
	for rows.Next() {
		var session Session
		var endpoint string
		var effort string
		var createdAt int64
		var updatedAt int64
		if err := rows.Scan(&session.ID, &session.Title, &session.Model, &session.ProjectID,
			&endpoint, &effort, &session.Instructions, &createdAt, &updatedAt); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan chat session: %w", err)
		}
		session.Endpoint = Endpoint(endpoint)
		session.ReasoningEffort = ReasoningEffort(effort)
		session.CreatedAt = time.Unix(createdAt, 0).UTC()
		session.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate chat sessions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close chat sessions: %w", err)
	}
	for index := range sessions {
		messages, err := service.listMessages(ctx, owner, sessions[index].ID)
		if err != nil {
			return nil, err
		}
		sessions[index].Messages = messages
		if !validSession(sessions[index]) {
			return nil, errors.New("stored chat session is invalid")
		}
	}
	return sessions, nil
}

func (service *Service) DeleteSession(ctx context.Context, owner, id string) error {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if !validSessionID(id) {
		return ErrInvalidSession
	}
	result, err := service.database.ExecContext(ctx,
		"DELETE FROM dashboard_chat_session WHERE owner_email = ? AND id = ?", owner, id)
	if err != nil {
		return fmt.Errorf("delete chat session: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted chat session count: %w", err)
	}
	if deleted == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (service *Service) RenameSession(ctx context.Context, owner, id, title string) error {
	owner, err := normalizeOwner(owner)
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	title = strings.TrimSpace(title)
	if !validSessionID(id) || title == "" || utf8.RuneCountInString(title) > maximumTitleRunes {
		return ErrInvalidSession
	}
	result, err := service.database.ExecContext(ctx, `
		UPDATE dashboard_chat_session
		SET title = ?, updated_at = ?
		WHERE owner_email = ? AND id = ?
	`, title, service.clock().UTC().Unix(), owner, id)
	if err != nil {
		return fmt.Errorf("rename chat session: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read renamed chat session count: %w", err)
	}
	if updated == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (service *Service) listMessages(ctx context.Context, owner, sessionID string) ([]Message, error) {
	rows, err := service.database.QueryContext(ctx, `
		SELECT role, content, reasoning, has_usage, input_tokens, cached_input_tokens,
		       output_tokens, reasoning_output_tokens, total_tokens
		FROM dashboard_chat_message
		WHERE owner_email = ? AND session_id = ?
		ORDER BY position ASC
	`, owner, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chat session messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		var role string
		var hasUsage int
		var usage Usage
		if err := rows.Scan(&role, &message.Content, &message.Reasoning, &hasUsage, &usage.InputTokens,
			&usage.CachedInputTokens, &usage.OutputTokens, &usage.ReasoningOutputTokens,
			&usage.TotalTokens); err != nil {
			return nil, fmt.Errorf("scan chat session message: %w", err)
		}
		message.Role = Role(role)
		if hasUsage == 1 {
			message.Usage = &usage
		} else if hasUsage != 0 {
			return nil, errors.New("stored chat message usage marker is invalid")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chat session messages: %w", err)
	}
	return messages, nil
}

func (service *Service) initialize(ctx context.Context) error {
	statements := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		`CREATE TABLE IF NOT EXISTS dashboard_chat_session (
			owner_email TEXT NOT NULL,
			id TEXT NOT NULL,
			title TEXT NOT NULL,
			model TEXT NOT NULL,
			project_id TEXT NOT NULL,
			endpoint TEXT NOT NULL CHECK (endpoint IN ('chat-completions', 'responses')),
			reasoning_effort TEXT NOT NULL CHECK (reasoning_effort IN ('none', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra')),
			instructions TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (owner_email, id)
		)`,
		`CREATE TABLE IF NOT EXISTS dashboard_chat_message (
			owner_email TEXT NOT NULL,
			session_id TEXT NOT NULL,
			position INTEGER NOT NULL CHECK (position >= 0),
			role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			reasoning TEXT NOT NULL DEFAULT '',
			has_usage INTEGER NOT NULL CHECK (has_usage IN (0, 1)),
			input_tokens INTEGER NOT NULL CHECK (input_tokens >= 0),
			cached_input_tokens INTEGER NOT NULL CHECK (cached_input_tokens >= 0),
			output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
			reasoning_output_tokens INTEGER NOT NULL CHECK (reasoning_output_tokens >= 0),
			total_tokens INTEGER NOT NULL CHECK (total_tokens >= 0),
			PRIMARY KEY (owner_email, session_id, position),
			FOREIGN KEY (owner_email, session_id)
				REFERENCES dashboard_chat_session(owner_email, id) ON DELETE CASCADE
		)`,
		"CREATE INDEX IF NOT EXISTS dashboard_chat_session_owner_updated ON dashboard_chat_session (owner_email, updated_at DESC)",
	}
	for _, statement := range statements {
		if _, err := service.database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize chat history database: %w", err)
		}
	}
	if err := service.ensureMessageReasoningColumn(ctx); err != nil {
		return err
	}
	return nil
}

func (service *Service) ensureMessageReasoningColumn(ctx context.Context) error {
	rows, err := service.database.QueryContext(ctx, "PRAGMA table_info(dashboard_chat_message)")
	if err != nil {
		return fmt.Errorf("inspect chat message schema: %w", err)
	}
	found := false
	for rows.Next() {
		var columnID int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&columnID, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan chat message schema: %w", err)
		}
		if name == "reasoning" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate chat message schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close chat message schema: %w", err)
	}
	if found {
		return nil
	}
	if _, err := service.database.ExecContext(ctx, "ALTER TABLE dashboard_chat_message ADD COLUMN reasoning TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add chat message reasoning column: %w", err)
	}
	return nil
}

func normalizeOwner(owner string) (string, error) {
	owner = strings.ToLower(strings.TrimSpace(owner))
	if owner == "" || len(owner) > maximumOwnerLength {
		return "", ErrInvalidOwner
	}
	parsed, err := mail.ParseAddress(owner)
	if err != nil || parsed.Address != owner {
		return "", ErrInvalidOwner
	}
	return owner, nil
}

func normalizeSession(session *Session) {
	session.ID = strings.TrimSpace(session.ID)
	session.Title = strings.TrimSpace(session.Title)
	session.Model = strings.TrimSpace(session.Model)
	session.ProjectID = strings.TrimSpace(session.ProjectID)
}

func validSession(session Session) bool {
	if !validSessionID(session.ID) || session.Title == "" || utf8.RuneCountInString(session.Title) > maximumTitleRunes ||
		session.Model == "" || len(session.Model) > maximumModelLength || len(session.ProjectID) > maximumProjectIDLength ||
		len(session.Instructions) > maximumInstructionsLength || len(session.Messages) == 0 || len(session.Messages) > maximumMessages ||
		!validEndpoint(session.Endpoint) || !validReasoningEffort(session.ReasoningEffort) {
		return false
	}
	for _, message := range session.Messages {
		if len(message.Content) > maximumMessageLength || len(message.Reasoning) > maximumMessageLength || (message.Role != RoleAssistant && strings.TrimSpace(message.Content) == "") ||
			(message.Role != RoleAssistant && message.Reasoning != "") ||
			(message.Role != RoleUser && message.Role != RoleAssistant) || (message.Role == RoleUser && message.Usage != nil) {
			return false
		}
		if message.Usage != nil && !validUsage(*message.Usage) {
			return false
		}
	}
	return true
}

func validSessionID(id string) bool {
	return id != "" && len(id) <= maximumSessionIDLength && sessionIDPattern.MatchString(id)
}

func validEndpoint(endpoint Endpoint) bool {
	return endpoint == EndpointChatCompletions || endpoint == EndpointResponses
}

func validReasoningEffort(effort ReasoningEffort) bool {
	switch effort {
	case ReasoningNone, ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningXHigh, ReasoningMax, ReasoningUltra:
		return true
	default:
		return false
	}
}

func validUsage(usage Usage) bool {
	return usage.InputTokens >= 0 && usage.CachedInputTokens >= 0 && usage.CachedInputTokens <= usage.InputTokens &&
		usage.OutputTokens >= 0 && usage.ReasoningOutputTokens >= 0 && usage.ReasoningOutputTokens <= usage.OutputTokens &&
		usage.TotalTokens == usage.InputTokens+usage.OutputTokens
}
