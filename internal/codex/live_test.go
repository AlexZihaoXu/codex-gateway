//go:build integration

package codex

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveModelListAndEphemeralGeneration(t *testing.T) {
	if os.Getenv("CODEX_GATEWAY_LIVE_TEST") != "1" {
		t.Skip("set CODEX_GATEWAY_LIVE_TEST=1 to use the locally authenticated Codex account")
	}
	dataDirectory := requireLiveDataDirectory(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := Start(ctx, StartOptions{Version: "integration-test", DataDirectory: dataDirectory})
	if err != nil {
		t.Fatalf("start Codex transport: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Codex transport: %v", err)
		}
	}()

	models, err := client.Models(ctx)
	if err != nil {
		t.Fatalf("list live Codex models: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("live Codex model list was empty")
	}
	model := models[0].ID
	for _, candidate := range models {
		if candidate.Default {
			model = candidate.ID
			break
		}
	}

	events, err := client.Generate(ctx, GenerateRequest{Model: model, Input: "Reply with exactly OK."})
	if err != nil {
		t.Fatalf("start live ephemeral generation: %v", err)
	}
	var result *GenerationResult
	for event := range events {
		if event.Err != nil {
			t.Fatalf("live generation event failed: %v", event.Err)
		}
		if event.Type == GenerationCompleted {
			result = event.Result
		}
	}
	if result == nil || result.Text == "" {
		t.Fatal("live generation did not return text")
	}
	if result.InputTokens < 0 || result.OutputTokens < 0 {
		t.Fatal("live generation returned invalid token usage")
	}
}

func TestLiveLunaConversationThreadReuse(t *testing.T) {
	if os.Getenv("CODEX_GATEWAY_LIVE_TEST") != "1" {
		t.Skip("set CODEX_GATEWAY_LIVE_TEST=1 to use the locally authenticated Codex account")
	}
	dataDirectory := requireLiveDataDirectory(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client, err := Start(ctx, StartOptions{Version: "integration-test", DataDirectory: dataDirectory})
	if err != nil {
		t.Fatalf("start Codex transport: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Codex transport: %v", err)
		}
	}()

	models, err := client.Models(ctx)
	if err != nil {
		t.Fatalf("list live Codex models: %v", err)
	}
	if !hasModel(models, "gpt-5.6-luna") {
		t.Fatal("the authenticated Codex account did not advertise gpt-5.6-luna")
	}
	marker := fmt.Sprintf("cache-probe-%d ", time.Now().UnixNano())
	firstInput := strings.Repeat(marker, 320) + "\nReply with exactly LUNA."
	first := collectLiveGeneration(t, client, ctx, GenerateRequest{
		ConversationID: "live-luna-cache", Model: "gpt-5.6-luna", Input: "User: " + firstInput,
		ContinuationInput: firstInput, ReasoningEffort: "medium",
	})
	secondInput := "What single word did I ask you to return? Reply with only that word."
	second := collectLiveGeneration(t, client, ctx, GenerateRequest{
		ConversationID: "live-luna-cache", Model: "gpt-5.6-luna",
		Input:             "User: " + firstInput + "\n\nAssistant: " + first.Text + "\n\nUser: " + secondInput,
		ContinuationInput: secondInput, ReasoningEffort: "medium",
	})

	if first.ThreadID == "" || second.ThreadID != first.ThreadID {
		t.Fatalf("Luna conversation did not reuse its ephemeral thread: first=%q second=%q", first.ThreadID, second.ThreadID)
	}
	for index, result := range []GenerationResult{first, second} {
		if result.Text == "" || result.InputTokens <= 0 || result.OutputTokens <= 0 || result.TotalTokens != result.InputTokens+result.OutputTokens {
			t.Fatalf("Luna turn %d returned invalid output or token totals: %#v", index+1, result)
		}
		if result.CachedInputTokens < 0 || result.CachedInputTokens > result.InputTokens {
			t.Fatalf("Luna turn %d returned invalid cached input usage: %#v", index+1, result)
		}
	}
	t.Logf(
		"Luna usage: first input=%d cached=%d total=%d; second input=%d cached=%d total=%d",
		first.InputTokens, first.CachedInputTokens, first.TotalTokens,
		second.InputTokens, second.CachedInputTokens, second.TotalTokens,
	)
	if second.CachedInputTokens <= first.CachedInputTokens {
		t.Fatalf(
			"continued Luna turn did not reuse the cache-eligible prompt prefix: first cached=%d second cached=%d",
			first.CachedInputTokens, second.CachedInputTokens,
		)
	}
}

func TestLiveLunaWebSearchToolLoop(t *testing.T) {
	if os.Getenv("CODEX_GATEWAY_LIVE_TEST") != "1" {
		t.Skip("set CODEX_GATEWAY_LIVE_TEST=1 to use the locally authenticated Codex account")
	}
	dataDirectory := requireLiveDataDirectory(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	client, err := Start(ctx, StartOptions{Version: "integration-test", DataDirectory: dataDirectory})
	if err != nil {
		t.Fatalf("start Codex transport: %v", err)
	}
	defer func() { _ = client.Close() }()

	events, err := client.Generate(ctx, GenerateRequest{
		ConversationID: "live-luna-web-search-" + fmt.Sprint(time.Now().UnixNano()),
		Model:          "gpt-5.6-luna", Input: "Search the web for the official OpenAI Codex product page, then answer with its page title and URL.",
		ContinuationInput: "Search the web for the official OpenAI Codex product page, then answer with its page title and URL.",
		ReasoningEffort:   "medium", EnableWebSearch: true,
	})
	if err != nil {
		t.Fatalf("start live web search generation: %v", err)
	}
	var result *GenerationResult
	started := 0
	completed := 0
	for event := range events {
		if event.Err != nil {
			t.Fatalf("live web search generation failed: %v", event.Err)
		}
		switch event.Type {
		case GenerationWebSearchStarted:
			started++
		case GenerationWebSearchCompleted:
			completed++
			if event.WebSearch == nil || strings.TrimSpace(event.WebSearch.Query) == "" {
				t.Fatal("live web search completion omitted its query")
			}
		case GenerationCompleted:
			result = event.Result
		}
	}
	if started == 0 || completed != started {
		t.Fatalf("live tool lifecycle was incomplete: started=%d completed=%d", started, completed)
	}
	if result == nil || strings.TrimSpace(result.Text) == "" || len(result.WebSearches) != completed {
		t.Fatalf("live tool loop omitted the final answer or searches: %#v", result)
	}
	if result.InputTokens <= 0 || result.OutputTokens <= 0 || result.TotalTokens != result.InputTokens+result.OutputTokens {
		t.Fatalf("live tool loop returned invalid aggregate usage: %#v", result)
	}
	t.Logf("verified Luna web tool loop with %d search call(s), input=%d cached=%d output=%d total=%d", completed, result.InputTokens, result.CachedInputTokens, result.OutputTokens, result.TotalTokens)
}

func TestLiveLunaThreeTurnDashboardScenarioMaintainsUsage(t *testing.T) {
	if os.Getenv("CODEX_GATEWAY_LIVE_TEST") != "1" {
		t.Skip("set CODEX_GATEWAY_LIVE_TEST=1 to use the locally authenticated Codex account")
	}
	dataDirectory := requireLiveDataDirectory(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	client, err := Start(ctx, StartOptions{Version: "integration-test", DataDirectory: dataDirectory})
	if err != nil {
		t.Fatalf("start Codex transport: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Codex transport: %v", err)
		}
	}()

	prompts := []string{
		"What is quantization in an LLM?",
		"Code a Flappy Bird demo in HTML.",
		"How do I play?",
	}
	results := make([]GenerationResult, 0, len(prompts))
	for _, prompt := range prompts {
		results = append(results, collectLiveGeneration(t, client, ctx, GenerateRequest{
			ConversationID: "live-luna-three-turn-cache", Model: "gpt-5.6-luna",
			Input: prompt, ContinuationInput: prompt, ReasoningEffort: "medium",
		}))
	}

	for index, result := range results {
		if result.Text == "" || result.InputTokens <= 0 || result.OutputTokens <= 0 || result.TotalTokens != result.InputTokens+result.OutputTokens {
			t.Fatalf("Luna turn %d returned invalid output or token totals: %#v", index+1, result)
		}
		if result.CachedInputTokens < 0 || result.CachedInputTokens > result.InputTokens {
			t.Fatalf("Luna turn %d returned invalid cached input usage: %#v", index+1, result)
		}
	}
	t.Logf(
		"Luna three-turn usage: first=%d/%d, second=%d/%d, third=%d/%d (input/cached)",
		results[0].InputTokens, results[0].CachedInputTokens,
		results[1].InputTokens, results[1].CachedInputTokens,
		results[2].InputTokens, results[2].CachedInputTokens,
	)
}

func requireLiveDataDirectory(t *testing.T) string {
	t.Helper()
	directory := os.Getenv("CODEX_GATEWAY_LIVE_DATA_DIR")
	if directory == "" {
		t.Fatal("CODEX_GATEWAY_LIVE_DATA_DIR must identify a gateway data directory with an authenticated Codex account")
	}
	return directory
}

func hasModel(models []Model, modelID string) bool {
	for _, model := range models {
		if model.ID == modelID {
			return true
		}
	}
	return false
}

func collectLiveGeneration(t *testing.T, client *Client, ctx context.Context, request GenerateRequest) GenerationResult {
	t.Helper()
	events, err := client.Generate(ctx, request)
	if err != nil {
		t.Fatalf("start live generation: %v", err)
	}
	var result *GenerationResult
	for event := range events {
		if event.Err != nil {
			t.Fatalf("live generation event failed: %v", event.Err)
		}
		if event.Type == GenerationCompleted {
			result = event.Result
		}
	}
	if result == nil {
		t.Fatal("live generation did not complete")
	}
	return *result
}
