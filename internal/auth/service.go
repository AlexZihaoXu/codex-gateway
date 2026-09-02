package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	defaultSessionDuration = 24 * time.Hour
	sessionTokenBytes      = 32
)

var ErrEmailNotAllowed = errors.New("Google account is not approved")

type Role string

const (
	RoleAdministrator Role = "administrator"
	RoleOperator      Role = "operator"
)

type Identity struct {
	UID   string
	Email string
}

type Options struct {
	DataSourceName     string
	AdministratorEmail string
	AllowedEmails      []string
	Clock              func() time.Time
	Random             io.Reader
	SessionDuration    time.Duration
}

type Status struct {
	Authenticated bool
	Email         string
	Role          Role
}

type Session struct {
	Token     string
	ExpiresAt time.Time
	Email     string
	Role      Role
}

type Service struct {
	database           *sql.DB
	administratorEmail string
	allowedEmails      map[string]struct{}
	clock              func() time.Time
	random             io.Reader
	sessionDuration    time.Duration
}

func Open(options Options) (*Service, error) {
	if options.DataSourceName == "" {
		return nil, errors.New("authentication data source name is required")
	}
	administratorEmail, err := validatedEmail(options.AdministratorEmail)
	if err != nil {
		return nil, fmt.Errorf("administrator email: %w", err)
	}
	allowedEmails := make(map[string]struct{}, len(options.AllowedEmails))
	for _, candidate := range options.AllowedEmails {
		email, validationErr := validatedEmail(candidate)
		if validationErr != nil {
			return nil, fmt.Errorf("allowed email: %w", validationErr)
		}
		if email != administratorEmail {
			allowedEmails[email] = struct{}{}
		}
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.SessionDuration == 0 {
		options.SessionDuration = defaultSessionDuration
	}
	if options.SessionDuration < time.Minute {
		return nil, errors.New("administrator session duration must be at least one minute")
	}

	database, err := sql.Open("sqlite", options.DataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open authentication database: %w", err)
	}
	database.SetMaxOpenConns(1)
	service := &Service{
		database:           database,
		administratorEmail: administratorEmail,
		allowedEmails:      allowedEmails,
		clock:              options.Clock,
		random:             options.Random,
		sessionDuration:    options.SessionDuration,
	}
	if err := service.initialize(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	return service, nil
}

func (service *Service) Close() error {
	return service.database.Close()
}

func (service *Service) Status(ctx context.Context, token string) (Status, error) {
	if token == "" {
		return Status{}, nil
	}
	tokenHash := sha256.Sum256([]byte(token))
	var email string
	if err := service.database.QueryRowContext(
		ctx,
		"SELECT email FROM firebase_session WHERE token_hash = ? AND expires_at > ?",
		tokenHash[:],
		service.clock().UTC().Unix(),
	).Scan(&email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Status{}, nil
		}
		return Status{}, fmt.Errorf("read administrator session: %w", err)
	}
	role, allowed := service.roleForEmail(email)
	if !allowed {
		return Status{}, nil
	}
	return Status{Authenticated: true, Email: email, Role: role}, nil
}

func (service *Service) Authenticate(ctx context.Context, identity Identity) (Session, error) {
	if strings.TrimSpace(identity.UID) == "" {
		return Session{}, ErrEmailNotAllowed
	}
	email, err := validatedEmail(identity.Email)
	if err != nil {
		return Session{}, ErrEmailNotAllowed
	}
	role, allowed := service.roleForEmail(email)
	if !allowed {
		return Session{}, ErrEmailNotAllowed
	}
	return service.createSession(ctx, service.clock().UTC(), email, role)
}

func (service *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	tokenHash := sha256.Sum256([]byte(token))
	if _, err := service.database.ExecContext(ctx, "DELETE FROM firebase_session WHERE token_hash = ?", tokenHash[:]); err != nil {
		return fmt.Errorf("delete administrator session: %w", err)
	}
	return nil
}

func (service *Service) initialize(ctx context.Context) error {
	statements := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		`CREATE TABLE IF NOT EXISTS firebase_session (
			token_hash BLOB PRIMARY KEY,
			email TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		"CREATE INDEX IF NOT EXISTS firebase_session_expiry ON firebase_session (expires_at)",
	}
	for _, statement := range statements {
		if _, err := service.database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize authentication database: %w", err)
		}
	}
	return nil
}

func (service *Service) createSession(ctx context.Context, now time.Time, email string, role Role) (Session, error) {
	if _, err := service.database.ExecContext(ctx, "DELETE FROM firebase_session WHERE expires_at <= ?", now.Unix()); err != nil {
		return Session{}, fmt.Errorf("clean expired administrator sessions: %w", err)
	}

	tokenBytes := make([]byte, sessionTokenBytes)
	if _, err := io.ReadFull(service.random, tokenBytes); err != nil {
		return Session{}, fmt.Errorf("generate administrator session: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	tokenHash := sha256.Sum256([]byte(token))
	expiresAt := now.Add(service.sessionDuration)
	if _, err := service.database.ExecContext(
		ctx,
		"INSERT INTO firebase_session (token_hash, email, expires_at, created_at) VALUES (?, ?, ?, ?)",
		tokenHash[:],
		email,
		expiresAt.Unix(),
		now.Unix(),
	); err != nil {
		return Session{}, fmt.Errorf("store administrator session: %w", err)
	}
	return Session{Token: token, ExpiresAt: expiresAt, Email: email, Role: role}, nil
}

func (service *Service) roleForEmail(email string) (Role, bool) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == service.administratorEmail {
		return RoleAdministrator, true
	}
	_, allowed := service.allowedEmails[normalized]
	return RoleOperator, allowed
}

func validatedEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || !strings.Contains(normalized, "@") {
		return "", errors.New("must be a valid email address")
	}
	return normalized, nil
}
