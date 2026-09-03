package chats

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionsPersistAcrossRestartAndRemainOwnerScoped(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 2, 20, 0, 0, 0, time.UTC)
	dsn := filepath.Join(t.TempDir(), "gateway.db")
	service, err := Open(Options{DataSourceName: dsn, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("open chat history: %v", err)
	}
	session := Session{
		ID: "chat-session-1", Title: "First prompt", Model: "gpt-5.6-sol",
		ProjectID: "prj_1", Endpoint: EndpointChatCompletions,
		ReasoningEffort: ReasoningMedium, Instructions: "Be concise.", WebSearch: true,
		Messages: []Message{
			{Role: RoleUser, Content: "First prompt"},
			{Role: RoleAssistant, Content: "First answer", Reasoning: "Checked the request.", WebSearches: []WebSearch{{
				CallID: "web-call-1", Query: "current release", Results: []map[string]any{{"type": "text_result", "url": "https://example.test/release", "title": "Release"}},
			}}, Usage: &Usage{
				InputTokens: 10, CachedInputTokens: 4, OutputTokens: 3,
				ReasoningOutputTokens: 1, TotalTokens: 13,
			}},
		},
	}
	if err := service.SaveSession(context.Background(), "Alex@Example.Test", session); err != nil {
		t.Fatalf("save chat session: %v", err)
	}

	now = now.Add(time.Minute)
	session.Messages[1].Usage = nil
	session.Messages = append(session.Messages,
		Message{Role: RoleUser, Content: "Follow up"},
		Message{Role: RoleAssistant, Content: "Second answer", Usage: &Usage{
			InputTokens: 18, CachedInputTokens: 10, OutputTokens: 4,
			ReasoningOutputTokens: 2, TotalTokens: 22,
		}},
	)
	if err := service.SaveSession(context.Background(), "alex@example.test", session); err != nil {
		t.Fatalf("update chat session: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close chat history: %v", err)
	}

	service, err = Open(Options{DataSourceName: dsn, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("reopen chat history: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	sessions, err := service.ListSessions(context.Background(), "alex@example.test")
	if err != nil {
		t.Fatalf("list chat sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %#v, want one", sessions)
	}
	stored := sessions[0]
	if stored.ID != session.ID || stored.Title != session.Title || stored.Model != session.Model || stored.Endpoint != session.Endpoint || stored.ReasoningEffort != session.ReasoningEffort || stored.Instructions != session.Instructions || stored.ProjectID != session.ProjectID || !stored.WebSearch {
		t.Fatalf("stored metadata changed: %#v", stored)
	}
	if !stored.CreatedAt.Equal(now.Add(-time.Minute)) || !stored.UpdatedAt.Equal(now) {
		t.Fatalf("stored timestamps = %s, %s", stored.CreatedAt, stored.UpdatedAt)
	}
	if len(stored.Messages) != 4 || stored.Messages[1].Reasoning != "Checked the request." || len(stored.Messages[1].WebSearches) != 1 || stored.Messages[1].WebSearches[0].Query != "current release" || stored.Messages[1].Usage == nil || stored.Messages[1].Usage.CachedInputTokens != 4 || stored.Messages[3].Content != "Second answer" || stored.Messages[3].Usage == nil || stored.Messages[3].Usage.CachedInputTokens != 10 {
		t.Fatalf("stored messages changed: %#v", stored.Messages)
	}
	if err := service.RenameSession(context.Background(), "other@example.test", session.ID, "Hidden"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("cross-owner rename = %v, want ErrSessionNotFound", err)
	}
	if err := service.RenameSession(context.Background(), "alex@example.test", session.ID, "  Renamed chat  "); err != nil {
		t.Fatalf("rename chat session: %v", err)
	}
	sessions, err = service.ListSessions(context.Background(), "alex@example.test")
	if err != nil || len(sessions) != 1 || sessions[0].Title != "Renamed chat" || !sessions[0].UpdatedAt.Equal(now) {
		t.Fatalf("renamed session = %#v, %v", sessions, err)
	}
	other, err := service.ListSessions(context.Background(), "other@example.test")
	if err != nil || len(other) != 0 {
		t.Fatalf("other owner saw sessions: %#v, %v", other, err)
	}
	if err := service.DeleteSession(context.Background(), "other@example.test", session.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("cross-owner delete = %v, want ErrSessionNotFound", err)
	}
	if err := service.DeleteSession(context.Background(), "alex@example.test", session.ID); err != nil {
		t.Fatalf("delete chat session: %v", err)
	}
	sessions, err = service.ListSessions(context.Background(), "alex@example.test")
	if err != nil || len(sessions) != 0 {
		t.Fatalf("deleted session remained: %#v, %v", sessions, err)
	}
}

func TestChatHistoryRejectsInvalidState(t *testing.T) {
	t.Parallel()
	if _, err := Open(Options{}); err == nil {
		t.Fatal("missing data source was accepted")
	}
	service, err := Open(Options{DataSourceName: ":memory:"})
	if err != nil {
		t.Fatalf("open chat history: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	valid := Session{
		ID: "chat-valid", Title: "Valid", Model: "gpt-5.6-sol", Endpoint: EndpointResponses,
		ReasoningEffort: ReasoningMedium,
		Messages:        []Message{{Role: RoleUser, Content: "Hello"}},
	}
	for _, test := range []struct {
		name    string
		owner   string
		mutate  func(*Session)
		wantErr error
	}{
		{name: "owner", owner: "not-an-email", wantErr: ErrInvalidOwner},
		{name: "identifier", owner: "alex@example.test", mutate: func(value *Session) { value.ID = "bad id" }, wantErr: ErrInvalidSession},
		{name: "endpoint", owner: "alex@example.test", mutate: func(value *Session) { value.Endpoint = "unknown" }, wantErr: ErrInvalidSession},
		{name: "messages", owner: "alex@example.test", mutate: func(value *Session) { value.Messages = nil }, wantErr: ErrInvalidSession},
		{name: "usage total", owner: "alex@example.test", mutate: func(value *Session) {
			value.Messages = []Message{{Role: RoleAssistant, Content: "Hello", Usage: &Usage{InputTokens: 2, OutputTokens: 3, TotalTokens: 6}}}
		}, wantErr: ErrInvalidSession},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			candidate.Messages = append([]Message(nil), valid.Messages...)
			if test.mutate != nil {
				test.mutate(&candidate)
			}
			if err := service.SaveSession(context.Background(), test.owner, candidate); !errors.Is(err, test.wantErr) {
				t.Fatalf("SaveSession() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestRenameSessionRejectsInvalidTitle(t *testing.T) {
	t.Parallel()
	service, err := Open(Options{DataSourceName: ":memory:"})
	if err != nil {
		t.Fatalf("open chat history: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	for _, title := range []string{"", "   ", strings.Repeat("x", maximumTitleRunes+1)} {
		if err := service.RenameSession(context.Background(), "alex@example.test", "chat-valid", title); !errors.Is(err, ErrInvalidSession) {
			t.Fatalf("RenameSession(%q) error = %v, want ErrInvalidSession", title, err)
		}
	}
}
