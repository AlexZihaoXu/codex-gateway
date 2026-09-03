package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"codex-gateway/internal/auth"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const serviceName = "codex-gateway"

type Options struct {
	Version          string
	Dashboard        http.Handler
	Authentication   Authentication
	IdentityVerifier auth.IdentityVerifier
	Firebase         FirebaseWebConfig
	Codex            Codex
	Projects         ProjectService
	ChatHistory      ChatHistory
	Inference        Inference
	Clock            func() time.Time
}

type serviceStatus struct {
	Service string `json:"service" doc:"Stable service identifier" example:"codex-gateway"`
	Status  string `json:"status" doc:"Current service state" example:"ok"`
	Version string `json:"version" doc:"Gateway version" example:"0.1.0"`
}

type serviceStatusOutput struct {
	Body serviceStatus
}

func New(options Options) (http.Handler, error) {
	if options.Dashboard == nil {
		return nil, errors.New("dashboard handler is required")
	}
	if options.Authentication == nil {
		return nil, errors.New("authentication service is required")
	}
	if options.IdentityVerifier == nil {
		return nil, errors.New("Firebase identity verifier is required")
	}
	if options.Codex == nil {
		return nil, errors.New("Codex service is required")
	}
	if options.Projects == nil {
		return nil, errors.New("project service is required")
	}
	if options.ChatHistory == nil {
		return nil, errors.New("chat history service is required")
	}
	if options.Inference == nil {
		return nil, errors.New("inference service is required")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}

	mux := http.NewServeMux()
	config := huma.DefaultConfig("Codex Gateway API", options.Version)
	config.DocsPath = ""
	config.OpenAPIPath = "/openapi"
	config.SchemasPath = "/schemas"
	config.Info.Description = "A local OpenAI-compatible gateway backed by an authenticated Codex account."
	api := humago.New(mux, config)

	registerServiceStatus(api, "/healthz", "get-health", "Reports whether the gateway process is alive.", "ok")
	registerServiceStatus(api, "/readyz", "get-readiness", "Reports whether the gateway can receive requests.", "ready")
	registerAuthentication(api, options.Authentication, options.IdentityVerifier, options.Firebase)
	registerCodexAccount(api, options.Authentication, options.Codex)
	registerProjectManagement(api, options.Authentication, options.Projects)
	registerOpenAICompatibility(api, options.Projects, options.Inference, options.Clock)
	registerStandaloneSearch(api, options.Projects, options.Inference)
	registerDashboardChat(api, options.Authentication, options.Projects, options.ChatHistory, options.Inference, options.Clock)

	mux.HandleFunc("/docs", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, "/doc", http.StatusPermanentRedirect)
	})
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/v1/", http.NotFoundHandler())
	mux.Handle("/", options.Dashboard)
	return withSecurityHeaders(mux), nil
}

func registerServiceStatus(api huma.API, path, operationID, summary, status string) {
	huma.Register(api, huma.Operation{
		OperationID: operationID,
		Method:      http.MethodGet,
		Path:        path,
		Summary:     summary,
		Tags:        []string{"System"},
	}, func(_ context.Context, _ *struct{}) (*serviceStatusOutput, error) {
		return &serviceStatusOutput{Body: serviceStatus{
			Service: serviceName,
			Status:  status,
			Version: api.OpenAPI().Info.Version,
		}}, nil
	})
}
