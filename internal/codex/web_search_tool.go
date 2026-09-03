package codex

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

//go:embed web_run_description.md
var webRunDescription string

const (
	maximumWebSearchToolCalls = 8
	maximumToolArgumentsBytes = 64 * 1024
)

type webSearchToolCall struct {
	CallID    string `json:"call_id"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func webSearchToolDefinition() map[string]any {
	stringProperty := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	integerProperty := func(description string) map[string]any {
		return map[string]any{"type": "integer", "description": description}
	}
	arrayOfObject := func(description string, properties map[string]any, required ...string) map[string]any {
		return map[string]any{
			"type": "array", "description": description,
			"items": map[string]any{"type": "object", "properties": properties, "required": required},
		}
	}
	queryProperties := map[string]any{
		"q":       stringProperty("Search query."),
		"recency": integerProperty("Whether to filter by a specific number of recent days."),
		"domains": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Whether to filter by a specific list of domains."},
	}
	parameters := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"search_query": arrayOfObject("Query the internet search engine for a given list of queries.", queryProperties, "q"),
			"image_query":  arrayOfObject("Query the image search engine for a given list of queries.", queryProperties, "q"),
			"open": arrayOfObject("Open pages by reference id or URL.", map[string]any{
				"ref_id": stringProperty("Reference id or URL to open."), "lineno": integerProperty("Line number to position the page at."),
			}, "ref_id"),
			"click": arrayOfObject("Open links from previously opened pages.", map[string]any{
				"ref_id": stringProperty("Reference id containing the numbered link."), "id": integerProperty("Numbered link id to open."),
			}, "id", "ref_id"),
			"find": arrayOfObject("Find text patterns in pages.", map[string]any{
				"ref_id": stringProperty("Reference id or URL to search within."), "pattern": stringProperty("Text pattern to find."),
			}, "pattern", "ref_id"),
			"screenshot": arrayOfObject("Take screenshots of PDF pages.", map[string]any{
				"ref_id": stringProperty("Reference id or URL to screenshot."), "pageno": integerProperty("Zero-indexed PDF page number."),
			}, "pageno", "ref_id"),
			"finance": arrayOfObject("Look up prices for the given stock symbols.", map[string]any{
				"ticker": stringProperty("Ticker symbol."),
				"type":   map[string]any{"type": "string", "description": "Asset type to look up.", "enum": []string{"equity", "fund", "crypto", "index"}},
				"market": stringProperty("ISO 3166-1 alpha-3 country code, \"OTC\", or \"\" for cryptocurrency."),
			}, "ticker", "type"),
			"weather": arrayOfObject("Look up weather forecasts.", map[string]any{
				"location": stringProperty("Location in \"Country, Area, City\" format."),
				"start":    stringProperty("Start date in YYYY-MM-DD format. Defaults to today."),
				"duration": integerProperty("Number of days to return. Defaults to 7."),
			}, "location"),
			"sports": arrayOfObject("Look up sports schedules and standings.", map[string]any{
				"tool":   map[string]any{"type": "string", "description": "Tool name for sports requests.", "enum": []string{"sports"}},
				"fn":     map[string]any{"type": "string", "description": "Sports function to call.", "enum": []string{"schedule", "standings"}},
				"league": map[string]any{"type": "string", "description": "League to look up.", "enum": []string{"nba", "wnba", "nfl", "nhl", "mlb", "epl", "ncaamb", "ncaawb", "ipl"}},
				"team":   stringProperty("Team to look up, using the common 3 or 4 letter alias used in broadcasts."), "opponent": stringProperty("Opponent to use with `team` when narrowing the lookup."),
				"date_from": stringProperty("Start date in YYYY-MM-DD format."), "date_to": stringProperty("End date in YYYY-MM-DD format."),
				"num_games": integerProperty("Number of games to return."), "locale": stringProperty("Locale for the lookup."),
			}, "fn", "league"),
			"time": arrayOfObject("Get the current time for UTC offsets.", map[string]any{
				"utc_offset": stringProperty("UTC offset formatted like \"+03:00\"."),
			}, "utc_offset"),
			"response_length": map[string]any{"type": "string", "description": "Set the length of the response to be returned.", "enum": []string{"short", "medium", "long"}},
		},
	}
	return map[string]any{
		"type": "namespace", "name": "web", "description": "Tools in the web namespace.",
		"tools": []any{map[string]any{
			"type": "function", "name": "run", "strict": false,
			"description": webRunDescription,
			"parameters":  parameters,
		}},
	}
}

func parseWebSearchToolCalls(output []json.RawMessage) ([]webSearchToolCall, error) {
	calls := make([]webSearchToolCall, 0)
	for _, raw := range output {
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &envelope) != nil || envelope.Type != "function_call" {
			continue
		}
		var call webSearchToolCall
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, errors.New("decode Codex web search tool call")
		}
		if call.Namespace != "web" || call.Name != "run" || strings.TrimSpace(call.CallID) == "" {
			return nil, errors.New("Codex requested an unsupported tool")
		}
		if len(call.Arguments) == 0 || len(call.Arguments) > maximumToolArgumentsBytes {
			return nil, errors.New("Codex web search arguments are invalid")
		}
		var commands map[string]json.RawMessage
		if err := json.Unmarshal([]byte(call.Arguments), &commands); err != nil || commands == nil {
			return nil, errors.New("Codex web search arguments are invalid")
		}
		calls = append(calls, call)
	}
	return calls, nil
}

func webSearchQuery(arguments string) string {
	var commands struct {
		SearchQuery []struct {
			Q string `json:"q"`
		} `json:"search_query"`
		ImageQuery []struct {
			Q string `json:"q"`
		} `json:"image_query"`
		Open []struct {
			RefID string `json:"ref_id"`
		} `json:"open"`
		Find []struct {
			RefID   string `json:"ref_id"`
			Pattern string `json:"pattern"`
		} `json:"find"`
	}
	if json.Unmarshal([]byte(arguments), &commands) != nil {
		return "Web search"
	}
	if len(commands.SearchQuery) > 0 && strings.TrimSpace(commands.SearchQuery[0].Q) != "" {
		return strings.TrimSpace(commands.SearchQuery[0].Q)
	}
	if len(commands.ImageQuery) > 0 && strings.TrimSpace(commands.ImageQuery[0].Q) != "" {
		return strings.TrimSpace(commands.ImageQuery[0].Q)
	}
	if len(commands.Find) > 0 {
		return strings.TrimSpace(commands.Find[0].Pattern)
	}
	if len(commands.Open) > 0 {
		return strings.TrimSpace(commands.Open[0].RefID)
	}
	return "Web search"
}

func webSearchPayload(id, model string, input []json.RawMessage, arguments string) (json.RawMessage, error) {
	var commands map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arguments), &commands); err != nil || commands == nil {
		return nil, errors.New("Codex web search arguments are invalid")
	}
	payload, err := json.Marshal(map[string]any{
		"id": id, "model": model, "input": input, "commands": commands,
		"settings":          map[string]any{"allowed_callers": []string{"direct"}, "external_web_access": true, "search_context_size": "medium"},
		"max_output_tokens": 4096,
	})
	if err != nil {
		return nil, fmt.Errorf("encode Codex web search request: %w", err)
	}
	return payload, nil
}

func functionCallOutput(callID, output string) (json.RawMessage, error) {
	encoded, err := json.Marshal(map[string]any{
		"type": "function_call_output", "call_id": callID,
		"output": []map[string]string{{"type": "input_text", "text": output}},
	})
	return json.RawMessage(encoded), err
}
