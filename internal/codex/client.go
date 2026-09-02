package codex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	defaultIssuer            = "https://auth.openai.com"
	defaultResponsesEndpoint = "https://chatgpt.com/backend-api/codex/responses"
	defaultClientID          = "app_EMoamEEZ73f0CkXaXp7hrann"
	credentialsFileName      = "codex-auth.json"
	maximumJSONBytes         = 1024 * 1024
	maximumSSELineBytes      = 16 * 1024 * 1024
	refreshSafetyWindow      = 30 * time.Second
	devicePollingMargin      = 3 * time.Second
)

const generationSafetyInstructions = "Return text only. Do not call tools, access the network, execute commands, or modify files."

var ErrNotAuthenticated = errors.New("Codex account is not authenticated")

type Account struct {
	Type     string
	Email    string
	PlanType string
}

type AccountStatus struct {
	Account            *Account
	RequiresOpenAIAuth bool
}

type DeviceLogin struct {
	ID              string
	VerificationURL string
	UserCode        string
}

type Model struct {
	ID                        string
	DisplayName               string
	Description               string
	Default                   bool
	DefaultReasoningEffort    string
	SupportedReasoningEfforts []string
}

type GenerationEventType string

const (
	GenerationStarted        GenerationEventType = "started"
	GenerationReasoningDelta GenerationEventType = "reasoning_delta"
	GenerationDelta          GenerationEventType = "delta"
	GenerationCompleted      GenerationEventType = "completed"
)

type GenerateRequest struct {
	ConversationID      string
	Model               string
	Input               string
	ContinuationInput   string
	Instructions        string
	ReasoningEffort     string
	Messages            []Message
	RestartConversation bool
}

type Message struct {
	Role    string
	Content string
}

type GenerationResult struct {
	ID                    string
	ThreadID              string
	Model                 string
	Text                  string
	ReasoningSummary      string
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
}

type GenerationEvent struct {
	Type   GenerationEventType
	ID     string
	Delta  string
	Result *GenerationResult
	Err    error
}

type Options struct {
	DataDirectory     string
	Version           string
	HTTPClient        *http.Client
	DisableWebSockets bool
	Issuer            string
	ResponsesEndpoint string
	ClientID          string
	Models            []Model
	Now               func() time.Time
	NewID             func() string
	PollWait          func(context.Context, time.Duration) error
}

type StartOptions = Options

type storedCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id,omitempty"`
	Email        string `json:"email,omitempty"`
	PlanType     string `json:"plan_type,omitempty"`
	Residency    string `json:"residency,omitempty"`
	ExpiresAt    int64  `json:"expires_at"`
}

type conversationState struct {
	active       bool
	model        string
	instructions string
	items        []json.RawMessage
}

type preparedGeneration struct {
	id       string
	cacheKey string
	threadID string
	input    []json.RawMessage
	release  func(bool, []json.RawMessage)
}

type Client struct {
	httpClient        *http.Client
	disableWebSockets bool
	issuer            string
	responsesEndpoint string
	clientID          string
	version           string
	credentialsPath   string
	models            []Model
	now               func() time.Time
	newID             func() string
	pollWait          func(context.Context, time.Duration) error

	mutex         sync.Mutex
	credentials   *storedCredentials
	refreshing    chan struct{}
	refreshError  error
	pendingLogins map[string]context.CancelFunc
	conversations map[string]*conversationState
	websockets    map[string]*websocket.Conn
	closed        bool
}

func Start(_ context.Context, options StartOptions) (*Client, error) {
	return New(options)
}

func New(options Options) (*Client, error) {
	if strings.TrimSpace(options.DataDirectory) == "" {
		return nil, errors.New("Codex data directory is required")
	}
	if strings.TrimSpace(options.Version) == "" {
		return nil, errors.New("Codex Gateway version is required")
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{}
	}
	if options.Issuer == "" {
		options.Issuer = defaultIssuer
	}
	if options.ResponsesEndpoint == "" {
		options.ResponsesEndpoint = defaultResponsesEndpoint
	}
	if options.ClientID == "" {
		options.ClientID = defaultClientID
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = randomID
	}
	if options.PollWait == nil {
		options.PollWait = waitForContext
	}
	if len(options.Models) == 0 {
		options.Models = auditedModels()
	}
	issuer, err := validateBaseURL(options.Issuer)
	if err != nil {
		return nil, fmt.Errorf("invalid Codex OAuth issuer: %w", err)
	}
	endpoint, err := validateBaseURL(options.ResponsesEndpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid Codex Responses endpoint: %w", err)
	}
	if strings.TrimSpace(options.ClientID) == "" {
		return nil, errors.New("Codex OAuth client identifier is required")
	}
	if err := os.MkdirAll(options.DataDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create Codex data directory: %w", err)
	}
	client := &Client{
		httpClient:        options.HTTPClient,
		disableWebSockets: options.DisableWebSockets,
		issuer:            strings.TrimRight(issuer.String(), "/"),
		responsesEndpoint: endpoint.String(),
		clientID:          options.ClientID,
		version:           options.Version,
		credentialsPath:   filepath.Join(options.DataDirectory, credentialsFileName),
		models:            append([]Model(nil), options.Models...),
		now:               options.Now,
		newID:             options.NewID,
		pollWait:          options.PollWait,
		pendingLogins:     make(map[string]context.CancelFunc),
		conversations:     make(map[string]*conversationState),
		websockets:        make(map[string]*websocket.Conn),
	}
	credentials, err := readCredentials(client.credentialsPath)
	if err != nil {
		return nil, err
	}
	client.credentials = credentials
	return client, nil
}

func (client *Client) Account(context.Context) (AccountStatus, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.closed {
		return AccountStatus{}, errors.New("Codex client is closed")
	}
	if client.credentials == nil {
		return AccountStatus{RequiresOpenAIAuth: true}, nil
	}
	return AccountStatus{RequiresOpenAIAuth: true, Account: &Account{
		Type: "chatgpt", Email: client.credentials.Email, PlanType: client.credentials.PlanType,
	}}, nil
}

func (client *Client) StartDeviceLogin(ctx context.Context) (DeviceLogin, error) {
	body, err := json.Marshal(map[string]string{"client_id": client.clientID})
	if err != nil {
		return DeviceLogin{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.issuer+"/api/accounts/deviceauth/usercode", bytes.NewReader(body))
	if err != nil {
		return DeviceLogin{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", client.userAgent())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return DeviceLogin{}, errors.New("start Codex device authorization")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DeviceLogin{}, fmt.Errorf("start Codex device authorization: upstream status %d", response.StatusCode)
	}
	var payload struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		Interval     string `json:"interval"`
	}
	if err := decodeBoundedJSON(response.Body, &payload); err != nil {
		return DeviceLogin{}, fmt.Errorf("decode Codex device authorization: %w", err)
	}
	if payload.DeviceAuthID == "" || payload.UserCode == "" {
		return DeviceLogin{}, errors.New("Codex device authorization returned incomplete data")
	}
	interval, err := time.ParseDuration(payload.Interval + "s")
	if err != nil || interval < time.Second {
		interval = 5 * time.Second
	}
	loginID := client.newID()
	if loginID == "" {
		return DeviceLogin{}, errors.New("create Codex login identifier")
	}
	pollContext, cancel := context.WithCancel(context.Background())
	client.mutex.Lock()
	if client.closed {
		client.mutex.Unlock()
		cancel()
		return DeviceLogin{}, errors.New("Codex client is closed")
	}
	client.pendingLogins[loginID] = cancel
	client.mutex.Unlock()
	go client.pollDeviceLogin(pollContext, loginID, payload.DeviceAuthID, payload.UserCode, interval)
	return DeviceLogin{ID: loginID, VerificationURL: client.issuer + "/codex/device", UserCode: payload.UserCode}, nil
}

func (client *Client) CancelLogin(_ context.Context, loginID string) error {
	if strings.TrimSpace(loginID) == "" {
		return errors.New("Codex login identifier is required")
	}
	client.mutex.Lock()
	cancel := client.pendingLogins[loginID]
	delete(client.pendingLogins, loginID)
	client.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (client *Client) Logout(context.Context) error {
	client.mutex.Lock()
	client.credentials = nil
	client.conversations = make(map[string]*conversationState)
	connections := client.takeWebSocketsLocked()
	cancellations := make([]context.CancelFunc, 0, len(client.pendingLogins))
	for _, cancel := range client.pendingLogins {
		cancellations = append(cancellations, cancel)
	}
	client.pendingLogins = make(map[string]context.CancelFunc)
	client.mutex.Unlock()
	closeWebSockets(connections)
	for _, cancel := range cancellations {
		cancel()
	}
	if err := os.Remove(client.credentialsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Codex credentials: %w", err)
	}
	return nil
}

func (client *Client) Models(context.Context) ([]Model, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.closed {
		return nil, errors.New("Codex client is closed")
	}
	return append([]Model(nil), client.models...), nil
}

func (client *Client) Generate(ctx context.Context, request GenerateRequest) (<-chan GenerationEvent, error) {
	if strings.TrimSpace(request.Model) == "" {
		return nil, errors.New("generation model is required")
	}
	if strings.TrimSpace(request.Input) == "" {
		return nil, errors.New("generation input is required")
	}
	if request.ReasoningEffort != "" && !isReasoningEffort(request.ReasoningEffort) {
		return nil, errors.New("generation reasoning effort is unsupported")
	}
	credentials, err := client.authorize(ctx)
	if err != nil {
		return nil, err
	}
	prepared, err := client.prepare(request)
	if err != nil {
		return nil, err
	}
	events := make(chan GenerationEvent, 64)
	go client.runGeneration(ctx, request, credentials, prepared, events)
	return events, nil
}

func (client *Client) Close() error {
	client.mutex.Lock()
	if client.closed {
		client.mutex.Unlock()
		return nil
	}
	client.closed = true
	cancellations := make([]context.CancelFunc, 0, len(client.pendingLogins))
	for _, cancel := range client.pendingLogins {
		cancellations = append(cancellations, cancel)
	}
	client.pendingLogins = make(map[string]context.CancelFunc)
	client.conversations = make(map[string]*conversationState)
	connections := client.takeWebSocketsLocked()
	client.mutex.Unlock()
	closeWebSockets(connections)
	for _, cancel := range cancellations {
		cancel()
	}
	return nil
}

func (client *Client) pollDeviceLogin(ctx context.Context, loginID, deviceAuthID, userCode string, interval time.Duration) {
	defer func() {
		client.mutex.Lock()
		delete(client.pendingLogins, loginID)
		client.mutex.Unlock()
	}()
	for {
		code, verifier, pending, err := client.pollDeviceToken(ctx, deviceAuthID, userCode)
		if err != nil {
			return
		}
		if !pending {
			credentials, exchangeErr := client.exchangeAuthorizationCode(ctx, code, verifier)
			if exchangeErr == nil && ctx.Err() == nil {
				_ = client.completeDeviceLogin(loginID, credentials)
			}
			return
		}
		if client.pollWait(ctx, interval+devicePollingMargin) != nil {
			return
		}
	}
}

func (client *Client) completeDeviceLogin(loginID string, credentials storedCredentials) error {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.closed || client.pendingLogins[loginID] == nil {
		return context.Canceled
	}
	if err := writeCredentials(client.credentialsPath, credentials); err != nil {
		return err
	}
	client.credentials = &credentials
	delete(client.pendingLogins, loginID)
	return nil
}

func (client *Client) pollDeviceToken(ctx context.Context, deviceAuthID, userCode string) (string, string, bool, error) {
	body, err := json.Marshal(map[string]string{"device_auth_id": deviceAuthID, "user_code": userCode})
	if err != nil {
		return "", "", false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.issuer+"/api/accounts/deviceauth/token", bytes.NewReader(body))
	if err != nil {
		return "", "", false, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", client.userAgent())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", "", false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound {
		return "", "", true, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", false, fmt.Errorf("poll Codex device authorization: upstream status %d", response.StatusCode)
	}
	var payload struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if err := decodeBoundedJSON(response.Body, &payload); err != nil {
		return "", "", false, err
	}
	if payload.AuthorizationCode == "" || payload.CodeVerifier == "" {
		return "", "", false, errors.New("Codex device authorization returned incomplete token data")
	}
	return payload.AuthorizationCode, payload.CodeVerifier, false, nil
}

func (client *Client) exchangeAuthorizationCode(ctx context.Context, code, verifier string) (storedCredentials, error) {
	values := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {client.issuer + "/deviceauth/callback"},
		"client_id":    {client.clientID}, "code_verifier": {verifier},
	}
	return client.exchangeToken(ctx, values, "")
}

func (client *Client) refresh(ctx context.Context, refreshToken string) (storedCredentials, error) {
	values := url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {client.clientID},
	}
	return client.exchangeToken(ctx, values, refreshToken)
}

func (client *Client) exchangeToken(ctx context.Context, values url.Values, priorRefreshToken string) (storedCredentials, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.issuer+"/oauth/token", strings.NewReader(values.Encode()))
	if err != nil {
		return storedCredentials{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", client.userAgent())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return storedCredentials{}, errors.New("exchange Codex OAuth token")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return storedCredentials{}, fmt.Errorf("exchange Codex OAuth token: upstream status %d", response.StatusCode)
	}
	var payload struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := decodeBoundedJSON(response.Body, &payload); err != nil {
		return storedCredentials{}, fmt.Errorf("decode Codex OAuth token: %w", err)
	}
	if payload.RefreshToken == "" {
		payload.RefreshToken = priorRefreshToken
	}
	if payload.AccessToken == "" || payload.RefreshToken == "" {
		return storedCredentials{}, errors.New("Codex OAuth token response was incomplete")
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 3600
	}
	credentials := storedCredentials{
		AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken,
		ExpiresAt: client.now().Add(time.Duration(payload.ExpiresIn) * time.Second).UnixMilli(),
		Residency: extractResidency(payload.AccessToken),
	}
	metadata := mergeAccountMetadata(
		parseAccountMetadata(payload.IDToken),
		parseAccountMetadata(payload.AccessToken),
	)
	credentials.AccountID, credentials.Email, credentials.PlanType = metadata.AccountID, metadata.Email, metadata.PlanType
	return credentials, nil
}

func (client *Client) authorize(ctx context.Context) (storedCredentials, error) {
	for {
		client.mutex.Lock()
		if client.closed {
			client.mutex.Unlock()
			return storedCredentials{}, errors.New("Codex client is closed")
		}
		if client.credentials == nil {
			client.mutex.Unlock()
			return storedCredentials{}, ErrNotAuthenticated
		}
		credentials := *client.credentials
		if credentials.ExpiresAt > client.now().Add(refreshSafetyWindow).UnixMilli() {
			client.mutex.Unlock()
			return credentials, nil
		}
		if client.refreshing != nil {
			waiting := client.refreshing
			client.mutex.Unlock()
			select {
			case <-waiting:
				client.mutex.Lock()
				err := client.refreshError
				client.mutex.Unlock()
				if err != nil {
					return storedCredentials{}, err
				}
				continue
			case <-ctx.Done():
				return storedCredentials{}, ctx.Err()
			}
		}
		waiting := make(chan struct{})
		client.refreshing = waiting
		client.refreshError = nil
		client.mutex.Unlock()

		refreshed, err := client.refresh(ctx, credentials.RefreshToken)
		if err == nil {
			if refreshed.AccountID == "" {
				refreshed.AccountID = credentials.AccountID
			}
			if refreshed.Email == "" {
				refreshed.Email = credentials.Email
			}
			if refreshed.PlanType == "" {
				refreshed.PlanType = credentials.PlanType
			}
			err = writeCredentials(client.credentialsPath, refreshed)
		}
		client.mutex.Lock()
		if err == nil {
			client.credentials = &refreshed
		}
		client.refreshError = err
		client.refreshing = nil
		close(waiting)
		client.mutex.Unlock()
		if err != nil {
			return storedCredentials{}, err
		}
		return refreshed, nil
	}
}

func (client *Client) prepare(request GenerateRequest) (preparedGeneration, error) {
	id := client.newID()
	if id == "" {
		return preparedGeneration{}, errors.New("create generation identifier")
	}
	userText := request.Input
	if request.ConversationID != "" && request.ContinuationInput != "" {
		userText = request.ContinuationInput
	}
	userItem, err := marshalInputMessage(Message{Role: "user", Content: userText})
	if err != nil {
		return preparedGeneration{}, err
	}
	if request.ConversationID == "" {
		input, inputErr := marshalInputMessages(request.Messages)
		if inputErr != nil {
			return preparedGeneration{}, inputErr
		}
		if len(input) == 0 {
			input = []json.RawMessage{userItem}
		}
		return preparedGeneration{id: id, cacheKey: id, input: input, release: func(bool, []json.RawMessage) {}}, nil
	}

	client.mutex.Lock()
	conversation := client.conversations[request.ConversationID]
	if request.RestartConversation && conversation != nil {
		if conversation.active {
			client.mutex.Unlock()
			return preparedGeneration{}, errors.New("conversation already has an active generation")
		}
		delete(client.conversations, request.ConversationID)
		conversation = nil
	}
	if conversation != nil && (conversation.model != request.Model || conversation.instructions != request.Instructions) {
		if conversation.active {
			client.mutex.Unlock()
			return preparedGeneration{}, errors.New("conversation already has an active generation")
		}
		delete(client.conversations, request.ConversationID)
		conversation = nil
	}
	if conversation == nil {
		conversation = &conversationState{model: request.Model, instructions: request.Instructions}
		client.conversations[request.ConversationID] = conversation
	}
	if conversation.active {
		client.mutex.Unlock()
		return preparedGeneration{}, errors.New("conversation already has an active generation")
	}
	conversation.active = true
	input := append([]json.RawMessage(nil), conversation.items...)
	if len(input) == 0 && len(request.Messages) > 0 {
		input, err = marshalInputMessages(request.Messages)
		if err != nil {
			conversation.active = false
			client.mutex.Unlock()
			return preparedGeneration{}, err
		}
	}
	if len(input) == 0 || len(conversation.items) > 0 {
		input = append(input, userItem)
	}
	client.mutex.Unlock()

	release := func(success bool, output []json.RawMessage) {
		client.mutex.Lock()
		defer client.mutex.Unlock()
		if client.conversations[request.ConversationID] != conversation {
			return
		}
		if success {
			conversation.items = append(append([]json.RawMessage(nil), input...), output...)
		}
		conversation.active = false
	}
	return preparedGeneration{id: id, cacheKey: request.ConversationID, threadID: request.ConversationID, input: input, release: release}, nil
}

func marshalInputMessages(messages []Message) ([]json.RawMessage, error) {
	input := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			return nil, errors.New("generation message role is unsupported")
		}
		if strings.TrimSpace(message.Content) == "" {
			return nil, errors.New("generation message content is required")
		}
		encoded, err := marshalInputMessage(message)
		if err != nil {
			return nil, err
		}
		input = append(input, encoded)
	}
	return input, nil
}

func marshalInputMessage(message Message) (json.RawMessage, error) {
	contentType := "input_text"
	if message.Role == "assistant" {
		contentType = "output_text"
	}
	encoded, err := json.Marshal(map[string]any{
		"role": message.Role, "content": []map[string]string{{"type": contentType, "text": message.Content}},
	})
	return json.RawMessage(encoded), err
}

func (client *Client) runGeneration(ctx context.Context, request GenerateRequest, credentials storedCredentials, prepared preparedGeneration, events chan<- GenerationEvent) {
	defer close(events)
	released := false
	defer func() {
		if !released {
			prepared.release(false, nil)
		}
	}()
	effort := request.ReasoningEffort
	if effort == "" {
		effort = "medium"
	}
	instructions := generationSafetyInstructions
	if strings.TrimSpace(request.Instructions) != "" {
		instructions += "\n\n" + strings.TrimSpace(request.Instructions)
	}
	payload := struct {
		Model          string            `json:"model"`
		Input          []json.RawMessage `json:"input"`
		Instructions   string            `json:"instructions"`
		Stream         bool              `json:"stream"`
		Store          bool              `json:"store"`
		PromptCacheKey string            `json:"prompt_cache_key"`
		Include        []string          `json:"include"`
		Reasoning      map[string]string `json:"reasoning"`
		Text           map[string]string `json:"text"`
	}{
		Model: request.Model, Input: prepared.input, Instructions: instructions,
		Stream: true, Store: false, PromptCacheKey: prepared.cacheKey,
		Include:   []string{"reasoning.encrypted_content"},
		Reasoning: map[string]string{"effort": effort, "summary": "auto"},
		Text:      map[string]string{"verbosity": "low"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		trySendGenerationEvent(events, GenerationEvent{ID: prepared.id, Err: err})
		return
	}
	responseBody, websocketErr := client.openWebSocketResponse(ctx, credentials, prepared, body)
	if websocketErr != nil || responseBody == nil {
		upstreamRequest, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, client.responsesEndpoint, bytes.NewReader(body))
		if requestErr != nil {
			trySendGenerationEvent(events, GenerationEvent{ID: prepared.id, Err: requestErr})
			return
		}
		setResponseHeaders(upstreamRequest.Header, credentials, prepared.cacheKey, client.userAgent())
		upstreamRequest.Header.Set("Accept", "text/event-stream")
		upstreamRequest.Header.Set("Content-Type", "application/json")
		response, requestErr := client.httpClient.Do(upstreamRequest)
		if requestErr != nil {
			trySendGenerationEvent(events, GenerationEvent{ID: prepared.id, Err: errors.New("request Codex response")})
			return
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumJSONBytes))
			_ = response.Body.Close()
			trySendGenerationEvent(events, GenerationEvent{ID: prepared.id, Err: fmt.Errorf("Codex upstream rejected request with status %d", response.StatusCode)})
			return
		}
		responseBody = response.Body
	}
	defer responseBody.Close()
	if !emitGenerationEvent(ctx, events, GenerationEvent{Type: GenerationStarted, ID: prepared.id}) {
		return
	}
	result, completedOutput, err := parseResponseStream(ctx, responseBody, prepared.id, prepared.threadID, request.Model, events)
	if err != nil {
		trySendGenerationEvent(events, GenerationEvent{ID: prepared.id, Err: err})
		return
	}
	prepared.release(true, completedOutput)
	released = true
	emitGenerationEvent(ctx, events, GenerationEvent{Type: GenerationCompleted, ID: prepared.id, Result: &result})
}

func (client *Client) openWebSocketResponse(ctx context.Context, credentials storedCredentials, prepared preparedGeneration, body []byte) (io.ReadCloser, error) {
	if client.disableWebSockets || prepared.threadID == "" {
		return nil, nil
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return nil, errors.New("decode Codex websocket request")
	}
	delete(payload, "stream")
	payload["type"] = "response.create"
	message, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("encode Codex websocket request")
	}
	var connection *websocket.Conn
	var connectionErr error
	for range 2 {
		connection, connectionErr = client.webSocket(ctx, credentials, prepared.cacheKey)
		if connectionErr != nil {
			continue
		}
		if connectionErr = connection.Write(ctx, websocket.MessageText, message); connectionErr == nil {
			break
		}
		client.invalidateWebSocket(prepared.cacheKey, connection)
	}
	if connectionErr != nil {
		return nil, errors.New("open Codex websocket response")
	}

	reader, writer := io.Pipe()
	go client.pipeWebSocketResponse(ctx, prepared.cacheKey, connection, writer)
	return reader, nil
}

func (client *Client) webSocket(ctx context.Context, credentials storedCredentials, cacheKey string) (*websocket.Conn, error) {
	client.mutex.Lock()
	if client.closed {
		client.mutex.Unlock()
		return nil, errors.New("Codex client is closed")
	}
	if connection := client.websockets[cacheKey]; connection != nil {
		client.mutex.Unlock()
		return connection, nil
	}
	client.mutex.Unlock()

	endpoint, err := url.Parse(client.responsesEndpoint)
	if err != nil {
		return nil, errors.New("parse Codex websocket endpoint")
	}
	switch endpoint.Scheme {
	case "https":
		endpoint.Scheme = "wss"
	case "http":
		endpoint.Scheme = "ws"
	default:
		return nil, errors.New("Codex websocket endpoint is unsupported")
	}
	headers := make(http.Header)
	setResponseHeaders(headers, credentials, cacheKey, client.userAgent())
	headers.Set("openai-beta", "responses_websockets=2026-02-06")
	connection, response, err := websocket.Dial(ctx, endpoint.String(), &websocket.DialOptions{
		HTTPClient: client.httpClient, HTTPHeader: headers,
	})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, errors.New("connect Codex websocket")
	}
	connection.SetReadLimit(maximumSSELineBytes)

	client.mutex.Lock()
	if client.closed {
		client.mutex.Unlock()
		_ = connection.CloseNow()
		return nil, errors.New("Codex client is closed")
	}
	if existing := client.websockets[cacheKey]; existing != nil {
		client.mutex.Unlock()
		_ = connection.CloseNow()
		return existing, nil
	}
	client.websockets[cacheKey] = connection
	client.mutex.Unlock()
	return connection, nil
}

func (client *Client) pipeWebSocketResponse(ctx context.Context, cacheKey string, connection *websocket.Conn, writer *io.PipeWriter) {
	defer writer.Close()
	for {
		messageType, data, err := connection.Read(ctx)
		if err != nil {
			client.invalidateWebSocket(cacheKey, connection)
			_ = writer.CloseWithError(errors.New("read Codex websocket response"))
			return
		}
		if messageType != websocket.MessageText {
			client.invalidateWebSocket(cacheKey, connection)
			_ = writer.CloseWithError(errors.New("Codex websocket returned a non-text message"))
			return
		}
		var envelope struct {
			Type string `json:"type"`
		}
		decodeErr := json.Unmarshal(data, &envelope)
		if _, err := writer.Write(append(append([]byte("data: "), data...), []byte("\n\n")...)); err != nil {
			return
		}
		if decodeErr != nil {
			client.invalidateWebSocket(cacheKey, connection)
			return
		}
		switch envelope.Type {
		case "response.completed":
			return
		case "response.failed", "response.incomplete", "error":
			client.invalidateWebSocket(cacheKey, connection)
			return
		}
	}
}

func setResponseHeaders(headers http.Header, credentials storedCredentials, cacheKey, userAgent string) {
	headers.Set("Authorization", "Bearer "+credentials.AccessToken)
	headers.Set("originator", "codex_gateway")
	headers.Set("User-Agent", userAgent)
	headers.Set("session-id", cacheKey)
	if credentials.AccountID != "" {
		headers.Set("ChatGPT-Account-Id", credentials.AccountID)
	}
	if credentials.Residency != "" {
		headers.Set("x-openai-internal-codex-residency", credentials.Residency)
	}
}

func (client *Client) invalidateWebSocket(cacheKey string, connection *websocket.Conn) {
	client.mutex.Lock()
	if client.websockets[cacheKey] == connection {
		delete(client.websockets, cacheKey)
	}
	client.mutex.Unlock()
	_ = connection.CloseNow()
}

func (client *Client) takeWebSocketsLocked() []*websocket.Conn {
	connections := make([]*websocket.Conn, 0, len(client.websockets))
	for _, connection := range client.websockets {
		connections = append(connections, connection)
	}
	client.websockets = make(map[string]*websocket.Conn)
	return connections
}

func closeWebSockets(connections []*websocket.Conn) {
	for _, connection := range connections {
		_ = connection.CloseNow()
	}
}

type responseEvent struct {
	Type         string          `json:"type"`
	Delta        string          `json:"delta,omitempty"`
	Code         string          `json:"code,omitempty"`
	Message      string          `json:"message,omitempty"`
	ItemID       string          `json:"item_id,omitempty"`
	SummaryIndex *int            `json:"summary_index,omitempty"`
	Item         json.RawMessage `json:"item,omitempty"`
	Response     *struct {
		ID     string            `json:"id"`
		Model  string            `json:"model"`
		Output []json.RawMessage `json:"output"`
		Usage  *responseUsage    `json:"usage"`
		Error  *responseError    `json:"error"`
	} `json:"response,omitempty"`
}

type responseUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	TotalTokens        int64 `json:"total_tokens"`
	InputTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func parseResponseStream(ctx context.Context, reader io.Reader, id, threadID, requestedModel string, events chan<- GenerationEvent) (GenerationResult, []json.RawMessage, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maximumSSELineBytes)
	dataLines := make([]string, 0, 1)
	result := GenerationResult{ID: id, ThreadID: threadID, Model: requestedModel}
	var output []json.RawMessage
	terminal := false
	lastReasoningPart := ""
	hasReasoningPart := false
	process := func(data string) error {
		if data == "[DONE]" {
			if terminal {
				return nil
			}
			return errors.New("Codex response ended before a terminal event")
		}
		var event responseEvent
		if json.Unmarshal([]byte(data), &event) != nil {
			return errors.New("decode Codex response stream event")
		}
		switch event.Type {
		case "response.reasoning_text.delta", "response.reasoning_summary.delta":
			if event.Delta != "" {
				result.ReasoningSummary += event.Delta
				if !emitGenerationEvent(ctx, events, GenerationEvent{Type: GenerationReasoningDelta, ID: id, Delta: event.Delta}) {
					return ctx.Err()
				}
			}
		case "response.reasoning_summary_text.delta":
			if event.Delta == "" {
				break
			}
			if event.SummaryIndex != nil {
				part := event.ItemID + ":" + strconv.Itoa(*event.SummaryIndex)
				if hasReasoningPart && part != lastReasoningPart && result.ReasoningSummary != "" {
					separator := reasoningSummaryPartSeparator(result.ReasoningSummary, event.Delta)
					result.ReasoningSummary += separator
					if separator != "" && !emitGenerationEvent(ctx, events, GenerationEvent{Type: GenerationReasoningDelta, ID: id, Delta: separator}) {
						return ctx.Err()
					}
				}
				lastReasoningPart = part
				hasReasoningPart = true
			}
			result.ReasoningSummary += event.Delta
			if !emitGenerationEvent(ctx, events, GenerationEvent{Type: GenerationReasoningDelta, ID: id, Delta: event.Delta}) {
				return ctx.Err()
			}
		case "response.output_text.delta":
			if event.Delta != "" {
				result.Text += event.Delta
				if !emitGenerationEvent(ctx, events, GenerationEvent{Type: GenerationDelta, ID: id, Delta: event.Delta}) {
					return ctx.Err()
				}
			}
		case "response.completed", "response.incomplete":
			if event.Response == nil {
				return errors.New("Codex terminal response omitted response data")
			}
			if event.Response.Model != "" {
				result.Model = event.Response.Model
			}
			if event.Response.Usage == nil {
				return errors.New("Codex terminal response omitted usage")
			}
			usage := event.Response.Usage
			result.InputTokens = nonNegative(usage.InputTokens)
			result.OutputTokens = nonNegative(usage.OutputTokens)
			result.CachedInputTokens = boundedSubset(usage.InputTokensDetails.CachedTokens, result.InputTokens)
			result.ReasoningOutputTokens = boundedSubset(usage.OutputTokensDetails.ReasoningTokens, result.OutputTokens)
			result.TotalTokens = result.InputTokens + result.OutputTokens
			output = append([]json.RawMessage(nil), event.Response.Output...)
			if result.Text == "" {
				result.Text = completedOutputText(output)
			}
			if result.ReasoningSummary == "" {
				result.ReasoningSummary = completedReasoningSummary(output)
				if result.ReasoningSummary != "" && !emitGenerationEvent(ctx, events, GenerationEvent{Type: GenerationReasoningDelta, ID: id, Delta: result.ReasoningSummary}) {
					return ctx.Err()
				}
			}
			terminal = true
		case "response.failed", "error":
			code, message := event.Code, event.Message
			if event.Response != nil && event.Response.Error != nil {
				if code == "" {
					code = event.Response.Error.Code
				}
				if message == "" {
					message = event.Response.Error.Message
				}
			}
			if message == "" {
				message = "Codex response failed"
			}
			if code != "" {
				message = code + ": " + message
			}
			return errors.New(message)
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line != "" {
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
			continue
		}
		if len(dataLines) == 0 {
			continue
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if err := process(data); err != nil {
			return GenerationResult{}, nil, err
		}
		if terminal {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return GenerationResult{}, nil, errors.New("read Codex response stream")
	}
	if !terminal && len(dataLines) > 0 {
		if err := process(strings.Join(dataLines, "\n")); err != nil {
			return GenerationResult{}, nil, err
		}
	}
	if !terminal {
		return GenerationResult{}, nil, errors.New("Codex response ended before a terminal event")
	}
	return result, output, nil
}

func completedOutputText(output []json.RawMessage) string {
	var text strings.Builder
	for _, raw := range output {
		var item struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Type != "message" || item.Role != "assistant" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" {
				text.WriteString(content.Text)
			}
		}
	}
	return text.String()
}

func completedReasoningSummary(output []json.RawMessage) string {
	var summary strings.Builder
	for _, raw := range output {
		var item struct {
			Type    string `json:"type"`
			Summary []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"summary"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Type != "reasoning" {
			continue
		}
		for _, part := range item.Summary {
			if part.Type == "summary_text" && part.Text != "" {
				if summary.Len() > 0 {
					summary.WriteString(reasoningSummaryPartSeparator(summary.String(), part.Text))
				}
				summary.WriteString(part.Text)
			}
		}
	}
	return summary.String()
}

func reasoningSummaryPartSeparator(previous, next string) string {
	newlines := 0
	for index := len(previous) - 1; index >= 0 && previous[index] == '\n' && newlines < 2; index-- {
		newlines++
	}
	for index := 0; index < len(next) && next[index] == '\n' && newlines < 2; index++ {
		newlines++
	}
	return strings.Repeat("\n", 2-newlines)
}

func (client *Client) saveCredentials(credentials storedCredentials) error {
	metadata := parseAccountMetadata(credentials.AccessToken)
	if credentials.AccountID == "" {
		credentials.AccountID = metadata.AccountID
	}
	if credentials.Email == "" {
		credentials.Email = metadata.Email
	}
	if credentials.PlanType == "" {
		credentials.PlanType = metadata.PlanType
	}
	if credentials.Residency == "" {
		credentials.Residency = extractResidency(credentials.AccessToken)
	}
	if err := writeCredentials(client.credentialsPath, credentials); err != nil {
		return err
	}
	client.mutex.Lock()
	client.credentials = &credentials
	client.mutex.Unlock()
	return nil
}

func readCredentials(path string) (*storedCredentials, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open Codex credentials: %w", err)
	}
	defer file.Close()
	var credentials storedCredentials
	if err := decodeBoundedJSON(file, &credentials); err != nil {
		return nil, fmt.Errorf("decode Codex credentials: %w", err)
	}
	if credentials.AccessToken == "" || credentials.RefreshToken == "" {
		return nil, errors.New("stored Codex credentials are incomplete")
	}
	return &credentials, nil
}

func writeCredentials(path string, credentials storedCredentials) error {
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create Codex credential store: %w", err)
	}
	err = json.NewEncoder(file).Encode(credentials)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("write Codex credential store: %w", err)
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("secure Codex credential store: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace Codex credential store: %w", err)
	}
	return nil
}

type accountMetadata struct{ AccountID, Email, PlanType string }

func mergeAccountMetadata(primary, fallback accountMetadata) accountMetadata {
	if primary.AccountID == "" {
		primary.AccountID = fallback.AccountID
	}
	if primary.Email == "" {
		primary.Email = fallback.Email
	}
	if primary.PlanType == "" {
		primary.PlanType = fallback.PlanType
	}
	return primary
}

func parseAccountMetadata(token string) accountMetadata {
	claims := parseJWTClaims(token)
	metadata := accountMetadata{AccountID: claimString(claims, "chatgpt_account_id"), Email: claimString(claims, "email"), PlanType: claimString(claims, "chatgpt_plan_type")}
	if namespace, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if metadata.AccountID == "" {
			metadata.AccountID = claimString(namespace, "chatgpt_account_id")
		}
		if metadata.PlanType == "" {
			metadata.PlanType = claimString(namespace, "chatgpt_plan_type")
		}
	}
	if metadata.AccountID == "" {
		if organizations, ok := claims["organizations"].([]any); ok && len(organizations) > 0 {
			if organization, ok := organizations[0].(map[string]any); ok {
				metadata.AccountID = claimString(organization, "id")
			}
		}
	}
	return metadata
}

func extractResidency(token string) string {
	claims := parseJWTClaims(token)
	residency := ""
	if namespace, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		residency = claimString(namespace, "chatgpt_compute_residency")
	}
	if residency == "" {
		residency = claimString(claims, "chatgpt_compute_residency")
	}
	if residency == "no_constraint" {
		return ""
	}
	return residency
}

func parseJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return nil
	}
	return claims
}

func claimString(claims map[string]any, key string) string {
	value, _ := claims[key].(string)
	return value
}

func auditedModels() []Model {
	efforts := []string{"none", "low", "medium", "high", "xhigh", "max", "ultra"}
	description := "GPT model for general reasoning, writing, coding, and tool-assisted tasks"
	return []Model{
		{ID: "gpt-5.6-sol", DisplayName: "GPT 5.6 Sol", Description: description, Default: true, DefaultReasoningEffort: "medium", SupportedReasoningEfforts: append([]string(nil), efforts...)},
		{ID: "gpt-5.6-terra", DisplayName: "GPT 5.6 Terra", Description: description, DefaultReasoningEffort: "medium", SupportedReasoningEfforts: append([]string(nil), efforts...)},
		{ID: "gpt-5.6-luna", DisplayName: "GPT 5.6 Luna", Description: description, DefaultReasoningEffort: "medium", SupportedReasoningEfforts: append([]string(nil), efforts...)},
	}
}

func isReasoningEffort(value string) bool {
	switch value {
	case "none", "low", "medium", "high", "xhigh", "max", "ultra":
		return true
	default:
		return false
	}
}

func validateBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("absolute URL without credentials or fragments is required")
	}
	loopback := parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1"
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback) {
		return nil, errors.New("HTTPS is required except for loopback tests")
	}
	return parsed, nil
}

func decodeBoundedJSON(reader io.Reader, output any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maximumJSONBytes+1))
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("JSON response exceeded one value")
	}
	return nil
}

func randomID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func waitForContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func boundedSubset(value, total int64) int64 {
	if value < 0 {
		return 0
	}
	if value > total {
		return total
	}
	return value
}
func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
func (client *Client) userAgent() string { return "codex-gateway/" + client.version }

func trySendGenerationEvent(events chan<- GenerationEvent, event GenerationEvent) {
	select {
	case events <- event:
	default:
	}
}
func emitGenerationEvent(ctx context.Context, events chan<- GenerationEvent, event GenerationEvent) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
