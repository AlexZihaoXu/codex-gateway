package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"codex-gateway/internal/auth"

	"github.com/danielgtaylor/huma/v2"
)

const administratorSessionCookie = "codex_gateway_session"

type Authentication interface {
	Status(ctx context.Context, token string) (auth.Status, error)
	Authenticate(ctx context.Context, identity auth.Identity) (auth.Session, error)
	Logout(ctx context.Context, token string) error
}

type FirebaseWebConfig struct {
	APIKey     string `json:"api_key" doc:"Public Firebase web API key"`
	AuthDomain string `json:"auth_domain" doc:"Firebase Authentication domain"`
	ProjectID  string `json:"project_id" doc:"Firebase project identifier"`
	AppID      string `json:"app_id" doc:"Firebase web application identifier"`
}

type firebaseWebConfiguration = FirebaseWebConfig

type authenticationCookieInput struct {
	SessionToken string `cookie:"codex_gateway_session"`
}

type firebaseAuthenticationInput struct {
	Body struct {
		IDToken string `json:"id_token" minLength:"1" maxLength:"16384" doc:"Firebase ID token returned after Google sign-in"`
	}
}

type authenticationStatusBody struct {
	Authenticated bool      `json:"authenticated" doc:"Whether the request has a valid approved-user session"`
	Email         string    `json:"email,omitempty" format:"email" doc:"Verified Google email for the active session"`
	Role          auth.Role `json:"role,omitempty" enum:"administrator,operator" doc:"Authorization role for the active session"`
}

type authenticationStatusOutput struct {
	Body authenticationStatusBody
}

type firebaseConfigurationOutput struct {
	Body FirebaseWebConfig
}

type authenticationSessionBody struct {
	Authenticated bool      `json:"authenticated" doc:"Whether an approved-user session is active"`
	ExpiresAt     time.Time `json:"expires_at,omitempty" format:"date-time" doc:"Session expiration time"`
	Email         string    `json:"email,omitempty" format:"email" doc:"Verified Google email for the active session"`
	Role          auth.Role `json:"role,omitempty" enum:"administrator,operator" doc:"Authorization role for the active session"`
}

type authenticationSessionOutput struct {
	SetCookie string `header:"Set-Cookie"`
	Body      authenticationSessionBody
}

func registerAuthentication(api huma.API, authentication Authentication, verifier auth.IdentityVerifier, firebase FirebaseWebConfig) {
	huma.Register(api, huma.Operation{
		OperationID: "get-firebase-configuration",
		Method:      http.MethodGet,
		Path:        "/api/auth/config",
		Summary:     "Read the public Firebase settings required for Google sign-in.",
		Tags:        []string{"Administration"},
	}, func(context.Context, *struct{}) (*firebaseConfigurationOutput, error) {
		return &firebaseConfigurationOutput{Body: firebase}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-authentication-status",
		Method:      http.MethodGet,
		Path:        "/api/auth/status",
		Summary:     "Read the current approved-user session status.",
		Tags:        []string{"Administration"},
	}, func(ctx context.Context, input *authenticationCookieInput) (*authenticationStatusOutput, error) {
		status, err := authentication.Status(ctx, input.SessionToken)
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to read authentication status.")
		}
		return &authenticationStatusOutput{Body: authenticationStatusBody{
			Authenticated: status.Authenticated,
			Email:         status.Email,
			Role:          status.Role,
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "authenticate-with-google",
		Method:      http.MethodPost,
		Path:        "/api/auth/google",
		Summary:     "Exchange a verified Firebase Google identity for a local session.",
		Tags:        []string{"Administration"},
	}, func(ctx context.Context, input *firebaseAuthenticationInput) (*authenticationSessionOutput, error) {
		identity, err := verifier.Verify(ctx, input.Body.IDToken)
		if err != nil {
			if errors.Is(err, auth.ErrFirebaseKeysUnavailable) {
				return nil, huma.Error503ServiceUnavailable("Google sign-in verification is temporarily unavailable.")
			}
			return nil, huma.Error401Unauthorized("Google sign-in could not be verified.")
		}
		session, err := authentication.Authenticate(ctx, identity)
		if err != nil {
			if errors.Is(err, auth.ErrEmailNotAllowed) {
				return nil, huma.Error403Forbidden("This Google account is not approved for this gateway.")
			}
			return nil, huma.Error500InternalServerError("Unable to start the local session.")
		}
		return sessionOutput(session), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "logout-administrator",
		Method:      http.MethodPost,
		Path:        "/api/auth/logout",
		Summary:     "Revoke the current local session.",
		Tags:        []string{"Administration"},
	}, func(ctx context.Context, input *authenticationCookieInput) (*authenticationSessionOutput, error) {
		if err := authentication.Logout(ctx, input.SessionToken); err != nil {
			return nil, huma.Error500InternalServerError("Unable to end the local session.")
		}
		return &authenticationSessionOutput{
			SetCookie: (&http.Cookie{
				Name:     administratorSessionCookie,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				MaxAge:   -1,
				Expires:  time.Unix(1, 0).UTC(),
			}).String(),
			Body: authenticationSessionBody{Authenticated: false},
		}, nil
	})
}

func sessionOutput(session auth.Session) *authenticationSessionOutput {
	return &authenticationSessionOutput{
		SetCookie: (&http.Cookie{
			Name:     administratorSessionCookie,
			Value:    session.Token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Expires:  session.ExpiresAt,
		}).String(),
		Body: authenticationSessionBody{
			Authenticated: true,
			ExpiresAt:     session.ExpiresAt,
			Email:         session.Email,
			Role:          session.Role,
		},
	}
}
