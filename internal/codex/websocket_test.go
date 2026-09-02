package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDirectClientReusesConversationWebSocketAndReportsCachedUsage(t *testing.T) {
	var connections atomic.Int32
	var mutex sync.Mutex
	requests := make([]map[string]any, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := websocket.Accept(writer, request, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		connections.Add(1)
		defer connection.CloseNow()
		for index := 1; index <= 2; index++ {
			_, data, readErr := connection.Read(request.Context())
			if readErr != nil {
				return
			}
			var body map[string]any
			if json.Unmarshal(data, &body) != nil {
				t.Error("decode websocket request")
				return
			}
			mutex.Lock()
			requests = append(requests, body)
			mutex.Unlock()
			cached := 0
			if index == 2 {
				cached = 8
			}
			for _, event := range []string{
				fmt.Sprintf(`{"type":"response.output_text.delta","delta":"turn %d"}`, index),
				fmt.Sprintf(`{"type":"response.completed","response":{"id":"response-%d","model":"gpt-5.6-luna","output":[{"id":"reasoning-%d","type":"reasoning","encrypted_content":"opaque-%d","summary":[]},{"id":"message-%d","type":"message","role":"assistant","content":[{"type":"output_text","text":"turn %d"}]}],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":%d},"output_tokens":3,"total_tokens":15}}}`, index, index, index, index, index, cached),
			} {
				if writeErr := connection.Write(request.Context(), websocket.MessageText, []byte(event)); writeErr != nil {
					t.Errorf("write websocket event: %v", writeErr)
					return
				}
			}
		}
	}))
	defer server.Close()

	client, err := New(Options{
		DataDirectory: t.TempDir(), Version: "test", HTTPClient: server.Client(),
		Issuer: server.URL, ResponsesEndpoint: server.URL, ClientID: "test-client",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := client.saveCredentials(storedCredentials{
		AccessToken: "access", RefreshToken: "refresh", AccountID: "account-123",
		ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	defer client.Close()

	firstEvents, err := client.Generate(context.Background(), GenerateRequest{
		ConversationID: "conversation-1", Model: "gpt-5.6-luna", Input: "first",
		ContinuationInput: "first", ReasoningEffort: "medium",
	})
	if err != nil {
		t.Fatalf("start first turn: %v", err)
	}
	if _, err := collectTestGeneration(firstEvents); err != nil {
		t.Fatalf("collect first turn: %v", err)
	}
	secondEvents, err := client.Generate(context.Background(), GenerateRequest{
		ConversationID: "conversation-1", Model: "gpt-5.6-luna", Input: "second",
		ContinuationInput: "second", ReasoningEffort: "medium",
	})
	if err != nil {
		t.Fatalf("start second turn: %v", err)
	}
	second, err := collectTestGeneration(secondEvents)
	if err != nil {
		t.Fatalf("collect second turn: %v", err)
	}

	if connections.Load() != 1 {
		t.Fatalf("opened %d websocket connections, want one", connections.Load())
	}
	if second.CachedInputTokens != 8 {
		t.Fatalf("cached input=%d, want 8", second.CachedInputTokens)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(requests) != 2 || requests[0]["type"] != "response.create" || requests[1]["type"] != "response.create" {
		t.Fatalf("unexpected websocket requests: %#v", requests)
	}
	if _, exists := requests[0]["stream"]; exists {
		t.Fatalf("websocket request retained HTTP-only stream flag: %#v", requests[0])
	}
}

func TestDirectClientRetriesConversationWebSocketBeforeHTTPFallback(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			http.Error(writer, "temporary websocket setup failure", http.StatusServiceUnavailable)
			return
		}
		connection, err := websocket.Accept(writer, request, nil)
		if err != nil {
			t.Errorf("accept retried websocket: %v", err)
			return
		}
		defer connection.CloseNow()
		if _, _, err := connection.Read(request.Context()); err != nil {
			return
		}
		terminal := `{"type":"response.completed","response":{"id":"response-1","model":"gpt-5.6-luna","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`
		if err := connection.Write(request.Context(), websocket.MessageText, []byte(terminal)); err != nil {
			t.Errorf("write retried websocket response: %v", err)
		}
	}))
	defer server.Close()

	client, err := New(Options{
		DataDirectory: t.TempDir(), Version: "test", HTTPClient: server.Client(),
		Issuer: server.URL, ResponsesEndpoint: server.URL, ClientID: "test-client",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := client.saveCredentials(storedCredentials{
		AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	defer client.Close()

	events, err := client.Generate(context.Background(), GenerateRequest{
		ConversationID: "conversation-retry", Model: "gpt-5.6-luna", Input: "hello",
		ContinuationInput: "hello", ReasoningEffort: "medium",
	})
	if err != nil {
		t.Fatalf("start generation: %v", err)
	}
	result, err := collectTestGeneration(events)
	if err != nil {
		t.Fatalf("collect generation: %v", err)
	}
	if result.Text != "OK" || attempts.Load() != 2 {
		t.Fatalf("result=%#v attempts=%d, want a successful second websocket attempt", result, attempts.Load())
	}
}
