package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"codex-gateway/internal/projects"

	"github.com/danielgtaylor/huma/v2"
)

type searchInput struct{ raw json.RawMessage }

func (input *searchInput) UnmarshalJSON(data []byte) error {
	var text string
	if json.Unmarshal(data, &text) == nil {
		if text == "" {
			return errors.New("search input text cannot be empty")
		}
		input.raw = append(input.raw[:0], data...)
		return nil
	}
	var items []map[string]any
	if json.Unmarshal(data, &items) != nil || len(items) == 0 {
		return errors.New("search input must be text or a non-empty array of Responses input items")
	}
	input.raw = append(input.raw[:0], data...)
	return nil
}

func (input searchInput) MarshalJSON() ([]byte, error) {
	if len(input.raw) == 0 {
		return []byte("null"), nil
	}
	return append([]byte(nil), input.raw...), nil
}

func (searchInput) Schema(huma.Registry) *huma.Schema {
	minLength, maxLength, minItems, maxItems := 1, 1_000_000, 1, 1_000
	return &huma.Schema{
		Description: "Search context as plain text or Responses API input items.",
		OneOf: []*huma.Schema{
			{Type: huma.TypeString, MinLength: &minLength, MaxLength: &maxLength},
			{Type: huma.TypeArray, MinItems: &minItems, MaxItems: &maxItems, Items: &huma.Schema{Type: huma.TypeObject, AdditionalProperties: true}},
		},
	}
}

type externalWebAccess struct{ raw json.RawMessage }

func (access *externalWebAccess) UnmarshalJSON(data []byte) error {
	var enabled bool
	if json.Unmarshal(data, &enabled) == nil {
		access.raw = append(access.raw[:0], data...)
		return nil
	}
	var mode string
	if json.Unmarshal(data, &mode) != nil || (mode != "cached" && mode != "indexed" && mode != "live") {
		return errors.New("external_web_access must be a boolean or cached, indexed, or live")
	}
	access.raw = append(access.raw[:0], data...)
	return nil
}

func (access externalWebAccess) MarshalJSON() ([]byte, error) {
	if len(access.raw) == 0 {
		return []byte("null"), nil
	}
	return append([]byte(nil), access.raw...), nil
}

func (externalWebAccess) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{
		Description: "Whether and how the search service may access the external web.",
		OneOf: []*huma.Schema{
			{Type: huma.TypeBoolean},
			{Type: huma.TypeString, Enum: []any{"cached", "indexed", "live"}},
		},
	}
}

type searchReasoning struct {
	Effort  string `json:"effort,omitempty" enum:"none,low,medium,high,xhigh,max,ultra" doc:"Reasoning effort for search synthesis"`
	Summary string `json:"summary,omitempty" enum:"auto,concise,detailed" doc:"Requested reasoning-summary mode"`
	Context string `json:"context,omitempty" enum:"auto,current_turn,all_turns" doc:"Reasoning context retention mode"`
}

type searchQuery struct {
	Query   string   `json:"q" minLength:"1" maxLength:"2048" doc:"Search query"`
	Recency *uint64  `json:"recency,omitempty" minimum:"1" maximum:"36500" doc:"Only include results from this many recent days"`
	Domains []string `json:"domains,omitempty" maxItems:"20" doc:"Optional domain allow-list for this query"`
}

type searchOpenOperation struct {
	ReferenceID string  `json:"ref_id" minLength:"1" maxLength:"4096" doc:"Search reference identifier or URL"`
	LineNumber  *uint64 `json:"lineno,omitempty" doc:"Line number at which to open the page"`
}

type searchClickOperation struct {
	ReferenceID string `json:"ref_id" minLength:"1" maxLength:"256" doc:"Reference identifier containing the link"`
	ID          uint64 `json:"id" doc:"Numbered link identifier"`
}

type searchFindOperation struct {
	ReferenceID string `json:"ref_id" minLength:"1" maxLength:"4096" doc:"Search reference identifier or URL"`
	Pattern     string `json:"pattern" minLength:"1" maxLength:"2048" doc:"Text pattern to locate"`
}

type searchScreenshotOperation struct {
	ReferenceID string `json:"ref_id" minLength:"1" maxLength:"4096" doc:"PDF reference identifier or URL"`
	PageNumber  uint64 `json:"pageno" doc:"Zero-indexed PDF page number"`
}

type searchFinanceOperation struct {
	Ticker string `json:"ticker" minLength:"1" maxLength:"32" doc:"Ticker symbol"`
	Type   string `json:"type" enum:"equity,fund,crypto,index" doc:"Asset type"`
	Market string `json:"market,omitempty" maxLength:"8" doc:"ISO 3166-1 alpha-3 market, OTC, or empty for cryptocurrency"`
}

type searchWeatherOperation struct {
	Location string  `json:"location" minLength:"1" maxLength:"256" doc:"Location in Country, Area, City form"`
	Start    string  `json:"start,omitempty" format:"date" doc:"Forecast start date"`
	Duration *uint64 `json:"duration,omitempty" minimum:"1" maximum:"14" doc:"Forecast duration in days"`
}

type searchSportsOperation struct {
	Tool     string  `json:"tool,omitempty" enum:"sports" doc:"Optional sports tool discriminator"`
	Function string  `json:"fn" enum:"schedule,standings" doc:"Sports operation"`
	League   string  `json:"league" enum:"nba,wnba,nfl,nhl,mlb,epl,ncaamb,ncaawb,ipl" doc:"League identifier"`
	Team     string  `json:"team,omitempty" maxLength:"16" doc:"Broadcast team alias"`
	Opponent string  `json:"opponent,omitempty" maxLength:"16" doc:"Opponent broadcast alias"`
	DateFrom string  `json:"date_from,omitempty" format:"date" doc:"Schedule range start"`
	DateTo   string  `json:"date_to,omitempty" format:"date" doc:"Schedule range end"`
	NumGames *uint64 `json:"num_games,omitempty" minimum:"1" maximum:"100" doc:"Maximum games to return"`
	Locale   string  `json:"locale,omitempty" maxLength:"32" doc:"Result locale"`
}

type searchTimeOperation struct {
	UTCOffset string `json:"utc_offset" pattern:"^[+-](0[0-9]|1[0-4]):[0-5][0-9]$" doc:"UTC offset such as +03:00"`
}

type searchCommands struct {
	SearchQuery    []searchQuery               `json:"search_query,omitempty" maxItems:"4" doc:"Internet search queries"`
	ImageQuery     []searchQuery               `json:"image_query,omitempty" maxItems:"4" doc:"Image search queries"`
	Open           []searchOpenOperation       `json:"open,omitempty" maxItems:"20" doc:"Pages to open"`
	Click          []searchClickOperation      `json:"click,omitempty" maxItems:"20" doc:"Numbered links to open"`
	Find           []searchFindOperation       `json:"find,omitempty" maxItems:"20" doc:"Patterns to find in pages"`
	Screenshot     []searchScreenshotOperation `json:"screenshot,omitempty" maxItems:"20" doc:"PDF pages to capture"`
	Finance        []searchFinanceOperation    `json:"finance,omitempty" maxItems:"20" doc:"Market quotes to retrieve"`
	Weather        []searchWeatherOperation    `json:"weather,omitempty" maxItems:"20" doc:"Weather forecasts to retrieve"`
	Sports         []searchSportsOperation     `json:"sports,omitempty" maxItems:"20" doc:"Sports schedules or standings to retrieve"`
	Time           []searchTimeOperation       `json:"time,omitempty" maxItems:"20" doc:"Current times to retrieve"`
	ResponseLength string                      `json:"response_length,omitempty" enum:"short,medium,long" doc:"Requested response detail"`
}

type approximateSearchLocation struct {
	Type     string `json:"type" enum:"approximate" doc:"Location precision"`
	Country  string `json:"country,omitempty" maxLength:"2" doc:"ISO 3166-1 alpha-2 country code"`
	Region   string `json:"region,omitempty" maxLength:"128" doc:"Region name"`
	City     string `json:"city,omitempty" maxLength:"128" doc:"City name"`
	Timezone string `json:"timezone,omitempty" maxLength:"64" doc:"IANA timezone"`
}

type searchFilters struct {
	AllowedDomains []string `json:"allowed_domains,omitempty" maxItems:"100" doc:"Domains search may access"`
	BlockedDomains []string `json:"blocked_domains,omitempty" maxItems:"100" doc:"Domains search must exclude"`
}

type searchImageSettings struct {
	MaxResults *uint64 `json:"max_results,omitempty" minimum:"1" maximum:"100" doc:"Maximum image results"`
	Caption    *bool   `json:"caption,omitempty" doc:"Whether image results should include captions"`
}

type searchSettings struct {
	UserLocation      *approximateSearchLocation `json:"user_location,omitempty" doc:"Approximate user location for localized results"`
	SearchContextSize string                     `json:"search_context_size,omitempty" enum:"low,medium,high" doc:"Search context budget"`
	Filters           *searchFilters             `json:"filters,omitempty"`
	ImageSettings     *searchImageSettings       `json:"image_settings,omitempty"`
	AllowedCallers    []string                   `json:"allowed_callers,omitempty" maxItems:"3" enum:"direct,shell,code_interpreter" doc:"Allowed search caller classes"`
	ExternalWebAccess *externalWebAccess         `json:"external_web_access,omitempty"`
}

type standaloneSearchRequestBody struct {
	ID              string           `json:"id" minLength:"1" maxLength:"128" doc:"Stable search-session identifier" example:"search-session"`
	Model           string           `json:"model" minLength:"1" maxLength:"128" doc:"Codex model identifier" example:"gpt-5.6-luna"`
	Reasoning       *searchReasoning `json:"reasoning,omitempty"`
	Input           *searchInput     `json:"input,omitempty"`
	Commands        *searchCommands  `json:"commands,omitempty"`
	Settings        *searchSettings  `json:"settings,omitempty"`
	MaxOutputTokens *uint64          `json:"max_output_tokens,omitempty" minimum:"1" maximum:"1000000" doc:"Maximum search synthesis output tokens"`
}

type standaloneSearchInput struct {
	Authorization string `header:"Authorization" doc:"Project API key using the Bearer scheme" example:"Bearer cg_example"`
	Body          standaloneSearchRequestBody
}

type standaloneSearchResponseBody struct {
	EncryptedOutput *string          `json:"encrypted_output" doc:"Opaque continuation state returned by Codex"`
	Output          string           `json:"output" doc:"Search result text supplied to the caller or model"`
	Results         []map[string]any `json:"results,omitempty" doc:"Forward-compatible structured result objects"`
}

type standaloneSearchOutput struct{ Body standaloneSearchResponseBody }

func registerStandaloneSearch(api huma.API, projectService ProjectService, inference Inference) {
	huma.Register(api, huma.Operation{
		OperationID: "create-standalone-search", Method: http.MethodPost, Path: "/v1/alpha/search",
		Summary: "Run the standalone Codex search API.", Tags: []string{"Codex search"},
		Security: []map[string][]string{{projectAPIKeySecurityScheme: {}}},
	}, func(ctx context.Context, input *standaloneSearchInput) (*standaloneSearchOutput, error) {
		access, err := requireProjectAPIKey(ctx, projectService, input.Authorization)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(input.Body)
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to encode the search request.")
		}
		result, err := inference.Search(ctx, bytes.Clone(payload))
		if err != nil {
			return nil, codexUnavailable()
		}
		if err := projectService.RecordUsage(ctx, projects.UsageRecord{
			RequestID: result.RequestID, ProjectID: access.ProjectID, APIKeyID: access.APIKeyID,
		}); err != nil {
			return nil, huma.Error500InternalServerError("Unable to record search usage.")
		}
		return &standaloneSearchOutput{Body: standaloneSearchResponseBody{
			EncryptedOutput: result.EncryptedOutput, Output: result.Output, Results: result.Results,
		}}, nil
	})
}
