package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

const administratorTestEmail = "administrator@example.test"

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }

func TestAuthenticateCreatesAdministratorSession(t *testing.T) {
	service := newTestService(t, time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	session, err := service.Authenticate(ctx, Identity{UID: "firebase-admin-uid", Email: "Administrator@Example.Test"})
	if err != nil {
		t.Fatalf("authenticate administrator: %v", err)
	}
	if session.Token == "" || session.Email != administratorTestEmail || session.Role != RoleAdministrator {
		t.Fatalf("unexpected administrator session: %#v", session)
	}

	status, err := service.Status(ctx, session.Token)
	if err != nil {
		t.Fatalf("read administrator status: %v", err)
	}
	if !status.Authenticated || status.Email != administratorTestEmail || status.Role != RoleAdministrator {
		t.Fatalf("unexpected authenticated status: %#v", status)
	}
}

func TestAuthenticateAllowsOnlyConfiguredGoogleEmails(t *testing.T) {
	service := newTestService(t, time.Now())
	ctx := context.Background()

	operator, err := service.Authenticate(ctx, Identity{UID: "work-uid", Email: "work@example.com"})
	if err != nil {
		t.Fatalf("authenticate approved operator: %v", err)
	}
	if operator.Role != RoleOperator || operator.Email != "work@example.com" {
		t.Fatalf("unexpected operator session: %#v", operator)
	}

	for _, identity := range []Identity{
		{UID: "unknown", Email: "unknown@example.com"},
		{UID: "missing-email"},
		{Email: administratorTestEmail},
	} {
		if _, err := service.Authenticate(ctx, identity); !errors.Is(err, ErrEmailNotAllowed) {
			t.Fatalf("expected identity %#v to be denied, got %v", identity, err)
		}
	}
}

func TestStatusRechecksCurrentAllowlist(t *testing.T) {
	service := newTestService(t, time.Now())
	ctx := context.Background()
	session, err := service.Authenticate(ctx, Identity{UID: "work-uid", Email: "work@example.com"})
	if err != nil {
		t.Fatalf("authenticate approved operator: %v", err)
	}

	delete(service.allowedEmails, "work@example.com")
	status, err := service.Status(ctx, session.Token)
	if err != nil {
		t.Fatalf("read status after allowlist change: %v", err)
	}
	if status.Authenticated {
		t.Fatal("expected a removed allowlist entry to invalidate its existing session")
	}
}

func TestExpiredAndLoggedOutSessionsAreRejected(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	service := newTestServiceWithClock(t, func() time.Time { return now })
	ctx := context.Background()
	session, err := service.Authenticate(ctx, Identity{UID: "admin", Email: administratorTestEmail})
	if err != nil {
		t.Fatalf("authenticate administrator: %v", err)
	}

	now = now.Add(defaultSessionDuration + time.Second)
	status, err := service.Status(ctx, session.Token)
	if err != nil {
		t.Fatalf("read expired session: %v", err)
	}
	if status.Authenticated {
		t.Fatal("expected expired session to be rejected")
	}

	now = time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	second, err := service.Authenticate(ctx, Identity{UID: "admin", Email: administratorTestEmail})
	if err != nil {
		t.Fatalf("authenticate second session: %v", err)
	}
	if err := service.Logout(ctx, second.Token); err != nil {
		t.Fatalf("log out: %v", err)
	}
	status, err = service.Status(ctx, second.Token)
	if err != nil {
		t.Fatalf("read logged-out session: %v", err)
	}
	if status.Authenticated {
		t.Fatal("expected logged-out session to be rejected")
	}
	if err := service.Logout(ctx, ""); err != nil {
		t.Fatalf("empty logout should be idempotent: %v", err)
	}
}

func TestAuthenticationOptionsAreValidated(t *testing.T) {
	for name, options := range map[string]Options{
		"missing database":      {},
		"missing administrator": {DataSourceName: ":memory:"},
		"invalid administrator": {DataSourceName: ":memory:", AdministratorEmail: "not an email"},
		"short session": {
			DataSourceName:     ":memory:",
			AdministratorEmail: administratorTestEmail,
			SessionDuration:    time.Second,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Open(options); err == nil {
				t.Fatal("expected invalid options to be rejected")
			}
		})
	}
	if _, err := Open(Options{
		DataSourceName:     "file:missing-auth-db?mode=ro",
		AdministratorEmail: administratorTestEmail,
	}); err == nil {
		t.Fatal("expected a read-only missing database to fail initialization")
	}
}

func TestAuthenticationReportsEntropyAndDatabaseFailures(t *testing.T) {
	entropyFailure := errors.New("entropy failed")
	service := openTestService(t, Options{
		DataSourceName:     ":memory:",
		AdministratorEmail: administratorTestEmail,
		Random:             errorReader{err: entropyFailure},
	})
	if _, err := service.Authenticate(context.Background(), Identity{UID: "admin", Email: administratorTestEmail}); !errors.Is(err, entropyFailure) {
		t.Fatalf("expected entropy failure, got %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}

	service = openTestService(t, Options{
		DataSourceName:     ":memory:",
		AdministratorEmail: administratorTestEmail,
	})
	if err := service.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}
	ctx := context.Background()
	if _, err := service.Status(ctx, "session"); err == nil {
		t.Fatal("expected status on a closed database to fail")
	}
	if _, err := service.Authenticate(ctx, Identity{UID: "admin", Email: administratorTestEmail}); err == nil {
		t.Fatal("expected authentication on a closed database to fail")
	}
	if err := service.Logout(ctx, "session"); err == nil {
		t.Fatal("expected logout on a closed database to fail")
	}
}

func newTestService(t *testing.T, now time.Time) *Service {
	t.Helper()
	return newTestServiceWithClock(t, func() time.Time { return now })
}

func newTestServiceWithClock(t *testing.T, clock func() time.Time) *Service {
	t.Helper()
	entropy := make([]byte, 0, 32*8)
	for value := byte(1); value <= 8; value++ {
		entropy = append(entropy, bytes.Repeat([]byte{value}, 32)...)
	}
	service := openTestService(t, Options{
		DataSourceName:     ":memory:",
		AdministratorEmail: administratorTestEmail,
		AllowedEmails:      []string{"work@example.com", " WORK@example.com "},
		Clock:              clock,
		Random:             bytes.NewReader(entropy),
	})
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("close authentication service: %v", err)
		}
	})
	return service
}

func openTestService(t *testing.T, options Options) *Service {
	t.Helper()
	service, err := Open(options)
	if err != nil {
		t.Fatalf("open authentication service: %v", err)
	}
	return service
}
