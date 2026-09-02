package app

import (
	"context"
	"net/http"

	"codex-gateway/internal/auth"
	"codex-gateway/internal/codex"

	"github.com/danielgtaylor/huma/v2"
)

type Codex interface {
	Account(ctx context.Context) (codex.AccountStatus, error)
	StartDeviceLogin(ctx context.Context) (codex.DeviceLogin, error)
	CancelLogin(ctx context.Context, loginID string) error
	Logout(ctx context.Context) error
}

type codexAccountBody struct {
	Connected          bool   `json:"connected" doc:"Whether Codex has an authenticated account"`
	Type               string `json:"type,omitempty" doc:"Codex account authentication type" example:"chatgpt"`
	Email              string `json:"email,omitempty" format:"email" doc:"ChatGPT account email when available"`
	PlanType           string `json:"plan_type,omitempty" doc:"ChatGPT plan reported by Codex" example:"plus"`
	RequiresOpenAIAuth bool   `json:"requires_openai_auth" doc:"Whether the active model provider requires OpenAI authentication"`
}

type codexAccountOutput struct {
	Body codexAccountBody
}

type codexDeviceLoginBody struct {
	LoginID         string `json:"login_id" doc:"Opaque identifier for this login attempt"`
	VerificationURL string `json:"verification_url" format:"uri" doc:"OpenAI page where the device login is completed"`
	UserCode        string `json:"user_code" doc:"Short-lived code to enter on the verification page"`
}

type codexDeviceLoginOutput struct {
	Body codexDeviceLoginBody
}

type codexCancelLoginInput struct {
	SessionToken string `cookie:"codex_gateway_session"`
	Body         struct {
		LoginID string `json:"login_id" minLength:"1" maxLength:"256" doc:"Opaque login identifier returned by the device login endpoint"`
	}
}

type operationStatusBody struct {
	Status string `json:"status" doc:"Operation result" example:"ok"`
}

type operationStatusOutput struct {
	Body operationStatusBody
}

func registerCodexAccount(api huma.API, authentication Authentication, codexService Codex) {
	huma.Register(api, huma.Operation{
		OperationID: "get-codex-account",
		Method:      http.MethodGet,
		Path:        "/api/codex/account",
		Summary:     "Read the Codex-managed account status.",
		Tags:        []string{"Codex account"},
	}, func(ctx context.Context, input *authenticationCookieInput) (*codexAccountOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		status, err := codexService.Account(ctx)
		if err != nil {
			return nil, codexUnavailable()
		}
		body := codexAccountBody{RequiresOpenAIAuth: status.RequiresOpenAIAuth}
		if status.Account != nil {
			body.Connected = true
			body.Type = status.Account.Type
			body.Email = status.Account.Email
			body.PlanType = status.Account.PlanType
		}
		return &codexAccountOutput{Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "start-codex-device-login",
		Method:        http.MethodPost,
		Path:          "/api/codex/login/device",
		Summary:       "Start a container-safe ChatGPT device login through Codex.",
		Tags:          []string{"Codex account"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *authenticationCookieInput) (*codexDeviceLoginOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		login, err := codexService.StartDeviceLogin(ctx)
		if err != nil {
			return nil, codexUnavailable()
		}
		return &codexDeviceLoginOutput{Body: codexDeviceLoginBody{
			LoginID:         login.ID,
			VerificationURL: login.VerificationURL,
			UserCode:        login.UserCode,
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cancel-codex-login",
		Method:      http.MethodPost,
		Path:        "/api/codex/login/cancel",
		Summary:     "Cancel a pending Codex device login.",
		Tags:        []string{"Codex account"},
	}, func(ctx context.Context, input *codexCancelLoginInput) (*operationStatusOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		if err := codexService.CancelLogin(ctx, input.Body.LoginID); err != nil {
			return nil, codexUnavailable()
		}
		return &operationStatusOutput{Body: operationStatusBody{Status: "ok"}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "logout-codex-account",
		Method:      http.MethodPost,
		Path:        "/api/codex/logout",
		Summary:     "Remove the account credentials managed by Codex.",
		Tags:        []string{"Codex account"},
	}, func(ctx context.Context, input *authenticationCookieInput) (*operationStatusOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		if err := codexService.Logout(ctx); err != nil {
			return nil, codexUnavailable()
		}
		return &operationStatusOutput{Body: operationStatusBody{Status: "ok"}}, nil
	})
}

func requireAdministrator(ctx context.Context, authentication Authentication, token string) error {
	_, err := administratorStatus(ctx, authentication, token)
	return err
}

func administratorStatus(ctx context.Context, authentication Authentication, token string) (auth.Status, error) {
	status, err := authentication.Status(ctx, token)
	if err != nil {
		return auth.Status{}, huma.Error500InternalServerError("Unable to verify the administrator session.")
	}
	if !status.Authenticated {
		return auth.Status{}, huma.Error401Unauthorized("Administrator authentication is required.")
	}
	return status, nil
}

func codexUnavailable() error {
	return huma.Error502BadGateway("The Codex transport is unavailable or rejected the request.")
}
