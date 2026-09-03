package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// SearchResult is the non-streaming response returned by Codex standalone web
// search. Result objects intentionally remain opaque so the gateway preserves
// new upstream result variants and fields.
type SearchResult struct {
	RequestID       string
	EncryptedOutput *string
	Output          string
	Results         []map[string]any
}

func (client *Client) Search(ctx context.Context, payload json.RawMessage) (SearchResult, error) {
	if err := validateSearchPayload(payload); err != nil {
		return SearchResult{}, err
	}
	credentials, err := client.authorize(ctx)
	if err != nil {
		return SearchResult{}, err
	}
	requestID := client.newID()
	if requestID == "" {
		return SearchResult{}, errors.New("create search request identifier")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.searchEndpoint, bytes.NewReader(payload))
	if err != nil {
		return SearchResult{}, errors.New("create Codex search request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("originator", "codex_gateway")
	request.Header.Set("User-Agent", client.userAgent())
	if credentials.AccountID != "" {
		request.Header.Set("ChatGPT-Account-Id", credentials.AccountID)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return SearchResult{}, errors.New("request Codex search")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return SearchResult{}, fmt.Errorf("Codex search upstream rejected request with status %d", response.StatusCode)
	}
	var body struct {
		EncryptedOutput *string          `json:"encrypted_output"`
		Output          string           `json:"output"`
		Results         []map[string]any `json:"results"`
	}
	if err := decodeBoundedJSON(response.Body, &body); err != nil {
		return SearchResult{}, fmt.Errorf("decode Codex search response: %w", err)
	}
	return SearchResult{
		RequestID: requestID, EncryptedOutput: body.EncryptedOutput,
		Output: body.Output, Results: body.Results,
	}, nil
}

func validateSearchPayload(payload json.RawMessage) error {
	if len(payload) == 0 || len(payload) > maximumJSONBytes {
		return errors.New("Codex search payload is invalid")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil || object == nil {
		return errors.New("Codex search payload is invalid")
	}
	return nil
}

func siblingSearchEndpoint(responsesEndpoint string) string {
	parsed, err := url.Parse(responsesEndpoint)
	if err != nil {
		return responsesEndpoint
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/responses") {
		path = strings.TrimSuffix(path, "/responses")
	}
	parsed.Path = strings.TrimRight(path, "/") + "/alpha/search"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
