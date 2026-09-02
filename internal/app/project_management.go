package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"codex-gateway/internal/projects"

	"github.com/danielgtaylor/huma/v2"
)

type ProjectService interface {
	ListProjects(ctx context.Context) ([]projects.Project, error)
	CreateProject(ctx context.Context, name string, limits []projects.UsageLimit) (projects.Project, error)
	UpdateProject(ctx context.Context, id, name string, limits []projects.UsageLimit) (projects.Project, error)
	DeleteProject(ctx context.Context, id string) error
	ListAPIKeys(ctx context.Context) ([]projects.APIKey, error)
	CreateAPIKey(ctx context.Context, projectID, name string) (projects.CreatedAPIKey, error)
	RevokeAPIKey(ctx context.Context, id string) error
	UsageSummary(ctx context.Context) (projects.Usage, error)
	AuthenticateAPIKey(ctx context.Context, secret string) (projects.ProjectAccess, error)
	AuthorizeDashboardChat(ctx context.Context, projectID string) (projects.ProjectAccess, error)
	RecordUsage(ctx context.Context, usage projects.UsageRecord) error
}

type usageLimitBody struct {
	TokenLimit    int64  `json:"token_limit" minimum:"1" maximum:"9007199254740991" doc:"Maximum total tokens allowed in the fixed window" example:"250000"`
	IntervalCount int64  `json:"interval_count" minimum:"1" maximum:"10000" doc:"Number of interval units in the fixed window" example:"6"`
	IntervalUnit  string `json:"interval_unit" enum:"minute,hour,day,week,month" doc:"UTC-aligned fixed-window unit" example:"hour"`
}

type projectBody struct {
	ID        string           `json:"id" doc:"Opaque project identifier" example:"prj_example"`
	Name      string           `json:"name" doc:"Human-readable project name" example:"OpenCode"`
	Limits    []usageLimitBody `json:"limits" maxItems:"8" doc:"Fixed UTC usage limits; every configured limit is enforced"`
	CreatedAt time.Time        `json:"created_at" format:"date-time" doc:"Project creation time"`
}

type projectListBody struct {
	Projects []projectBody `json:"projects" doc:"Projects ordered by creation time"`
}

type projectListOutput struct{ Body projectListBody }

type createProjectInput struct {
	SessionToken string `cookie:"codex_gateway_session"`
	Body         struct {
		Name   string           `json:"name" minLength:"1" maxLength:"80" doc:"Human-readable project name"`
		Limits []usageLimitBody `json:"limits,omitempty" maxItems:"8" doc:"Fixed UTC usage limits; omit for unlimited"`
	}
}

type projectOutput struct{ Body projectBody }

type updateProjectInput struct {
	SessionToken string `cookie:"codex_gateway_session"`
	ID           string `path:"id" maxLength:"128" doc:"Project identifier"`
	Body         struct {
		Name   string           `json:"name" minLength:"1" maxLength:"80" doc:"Human-readable project name"`
		Limits []usageLimitBody `json:"limits,omitempty" maxItems:"8" doc:"Replacement fixed UTC usage limits; omit for unlimited"`
	}
}

type deleteProjectInput struct {
	SessionToken string `cookie:"codex_gateway_session"`
	ID           string `path:"id" maxLength:"128" doc:"Project identifier"`
}

type apiKeyBody struct {
	ID         string     `json:"id" doc:"Opaque API key identifier" example:"key_example"`
	ProjectID  string     `json:"project_id" doc:"Owning project identifier" example:"prj_example"`
	Name       string     `json:"name" doc:"Human-readable API key name" example:"Development"`
	Prefix     string     `json:"prefix" doc:"Non-secret prefix used to identify the key" example:"cg_example"`
	CreatedAt  time.Time  `json:"created_at" format:"date-time" doc:"API key creation time"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" format:"date-time" doc:"Most recent authenticated request time"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty" format:"date-time" doc:"Revocation time, when revoked"`
}

type apiKeyListBody struct {
	APIKeys []apiKeyBody `json:"api_keys" doc:"API keys without plaintext secret material"`
}

type apiKeyListOutput struct{ Body apiKeyListBody }

type createAPIKeyInput struct {
	SessionToken string `cookie:"codex_gateway_session"`
	Body         struct {
		ProjectID string `json:"project_id" minLength:"1" maxLength:"128" doc:"Owning project identifier"`
		Name      string `json:"name" minLength:"1" maxLength:"80" doc:"Human-readable API key name"`
	}
}

type apiKeyCreatedBody struct {
	APIKey apiKeyBody `json:"api_key"`
	Secret string     `json:"secret" doc:"Plaintext API key shown only in this creation response"`
}

type apiKeyCreatedOutput struct{ Body apiKeyCreatedBody }

type revokeAPIKeyInput struct {
	SessionToken string `cookie:"codex_gateway_session"`
	ID           string `path:"id" maxLength:"128" doc:"API key identifier"`
}

type projectUsageBody struct {
	ProjectID             string                `json:"project_id"`
	Name                  string                `json:"name"`
	Limits                []usageLimitUsageBody `json:"limits" maxItems:"8"`
	Requests              int64                 `json:"requests" minimum:"0"`
	InputTokens           int64                 `json:"input_tokens" minimum:"0"`
	CachedInputTokens     int64                 `json:"cached_input_tokens" minimum:"0"`
	OutputTokens          int64                 `json:"output_tokens" minimum:"0"`
	ReasoningOutputTokens int64                 `json:"reasoning_output_tokens" minimum:"0"`
	TotalTokens           int64                 `json:"total_tokens" minimum:"0"`
}

type usageLimitUsageBody struct {
	usageLimitBody
	PeriodStart           time.Time `json:"period_start" format:"date-time" doc:"Start of the active fixed UTC window"`
	PeriodEnd             time.Time `json:"period_end" format:"date-time" doc:"Exclusive end and reset time of the active fixed UTC window"`
	Requests              int64     `json:"requests" minimum:"0"`
	InputTokens           int64     `json:"input_tokens" minimum:"0"`
	CachedInputTokens     int64     `json:"cached_input_tokens" minimum:"0"`
	OutputTokens          int64     `json:"output_tokens" minimum:"0"`
	ReasoningOutputTokens int64     `json:"reasoning_output_tokens" minimum:"0"`
	TotalTokens           int64     `json:"total_tokens" minimum:"0"`
}

type apiKeyUsageBody struct {
	APIKeyID              string `json:"api_key_id"`
	ProjectID             string `json:"project_id"`
	Name                  string `json:"name"`
	Prefix                string `json:"prefix"`
	Requests              int64  `json:"requests" minimum:"0"`
	InputTokens           int64  `json:"input_tokens" minimum:"0"`
	CachedInputTokens     int64  `json:"cached_input_tokens" minimum:"0"`
	OutputTokens          int64  `json:"output_tokens" minimum:"0"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens" minimum:"0"`
	TotalTokens           int64  `json:"total_tokens" minimum:"0"`
}

type usageSummaryBody struct {
	PeriodStart           time.Time          `json:"period_start" format:"date-time" doc:"Start of the current UTC month"`
	PeriodEnd             time.Time          `json:"period_end" format:"date-time" doc:"Exclusive end of the current UTC month"`
	Projects              []projectUsageBody `json:"projects"`
	APIKeys               []apiKeyUsageBody  `json:"api_keys"`
	Requests              int64              `json:"requests" minimum:"0"`
	InputTokens           int64              `json:"input_tokens" minimum:"0"`
	CachedInputTokens     int64              `json:"cached_input_tokens" minimum:"0"`
	OutputTokens          int64              `json:"output_tokens" minimum:"0"`
	ReasoningOutputTokens int64              `json:"reasoning_output_tokens" minimum:"0"`
	TotalTokens           int64              `json:"total_tokens" minimum:"0"`
}

type usageSummaryOutput struct{ Body usageSummaryBody }

func registerProjectManagement(api huma.API, authentication Authentication, service ProjectService) {
	huma.Register(api, huma.Operation{
		OperationID: "list-projects", Method: http.MethodGet, Path: "/api/projects",
		Summary: "List API consumer projects.", Tags: []string{"Projects"},
	}, func(ctx context.Context, input *authenticationCookieInput) (*projectListOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		items, err := service.ListProjects(ctx)
		if err != nil {
			return nil, mapProjectError(err)
		}
		body := projectListBody{Projects: make([]projectBody, 0, len(items))}
		for _, item := range items {
			body.Projects = append(body.Projects, projectResponse(item))
		}
		return &projectListOutput{Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-project", Method: http.MethodPost, Path: "/api/projects",
		Summary: "Create an API consumer project.", Tags: []string{"Projects"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createProjectInput) (*projectOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		project, err := service.CreateProject(ctx, input.Body.Name, projectUsageLimits(input.Body.Limits))
		if err != nil {
			return nil, mapProjectError(err)
		}
		return &projectOutput{Body: projectResponse(project)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-project", Method: http.MethodPut, Path: "/api/projects/{id}",
		Summary: "Replace a project's name and fixed-window usage limits.", Tags: []string{"Projects"},
	}, func(ctx context.Context, input *updateProjectInput) (*projectOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		project, err := service.UpdateProject(ctx, input.ID, input.Body.Name, projectUsageLimits(input.Body.Limits))
		if err != nil {
			return nil, mapProjectError(err)
		}
		return &projectOutput{Body: projectResponse(project)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-project", Method: http.MethodDelete, Path: "/api/projects/{id}",
		Summary: "Delete a project and its API keys and usage records.", Tags: []string{"Projects"},
	}, func(ctx context.Context, input *deleteProjectInput) (*operationStatusOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		if err := service.DeleteProject(ctx, input.ID); err != nil {
			return nil, mapProjectError(err)
		}
		return &operationStatusOutput{Body: operationStatusBody{Status: "ok"}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-api-keys", Method: http.MethodGet, Path: "/api/api-keys",
		Summary: "List project API keys without secrets.", Tags: []string{"API keys"},
	}, func(ctx context.Context, input *authenticationCookieInput) (*apiKeyListOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		items, err := service.ListAPIKeys(ctx)
		if err != nil {
			return nil, mapProjectError(err)
		}
		body := apiKeyListBody{APIKeys: make([]apiKeyBody, 0, len(items))}
		for _, item := range items {
			if item.RevokedAt != nil {
				continue
			}
			body.APIKeys = append(body.APIKeys, apiKeyResponse(item))
		}
		return &apiKeyListOutput{Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-api-key", Method: http.MethodPost, Path: "/api/api-keys",
		Summary: "Create a project API key and return its plaintext once.", Tags: []string{"API keys"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createAPIKeyInput) (*apiKeyCreatedOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		created, err := service.CreateAPIKey(ctx, input.Body.ProjectID, input.Body.Name)
		if err != nil {
			return nil, mapProjectError(err)
		}
		return &apiKeyCreatedOutput{Body: apiKeyCreatedBody{
			APIKey: apiKeyResponse(created.Key), Secret: created.Secret,
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "revoke-api-key", Method: http.MethodDelete, Path: "/api/api-keys/{id}",
		Summary: "Revoke a project API key.", Tags: []string{"API keys"},
	}, func(ctx context.Context, input *revokeAPIKeyInput) (*operationStatusOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		if err := service.RevokeAPIKey(ctx, input.ID); err != nil {
			return nil, mapProjectError(err)
		}
		return &operationStatusOutput{Body: operationStatusBody{Status: "ok"}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-usage-summary", Method: http.MethodGet, Path: "/api/usage",
		Summary: "Read current-month token usage by project and API key.", Tags: []string{"Usage"},
	}, func(ctx context.Context, input *authenticationCookieInput) (*usageSummaryOutput, error) {
		if err := requireAdministrator(ctx, authentication, input.SessionToken); err != nil {
			return nil, err
		}
		usage, err := service.UsageSummary(ctx)
		if err != nil {
			return nil, mapProjectError(err)
		}
		body := usageSummaryBody{
			PeriodStart: usage.PeriodStart, PeriodEnd: usage.PeriodEnd, Projects: make([]projectUsageBody, 0, len(usage.Projects)),
			APIKeys: make([]apiKeyUsageBody, 0, len(usage.APIKeys)), Requests: usage.Requests,
			InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens,
			OutputTokens: usage.OutputTokens, ReasoningOutputTokens: usage.ReasoningOutputTokens,
			TotalTokens: usage.TotalTokens,
		}
		for _, project := range usage.Projects {
			body.Projects = append(body.Projects, projectUsageBody{
				ProjectID: project.ProjectID, Name: project.Name, Limits: usageLimitUsageResponses(project.Limits),
				Requests: project.Requests, InputTokens: project.InputTokens, CachedInputTokens: project.CachedInputTokens,
				OutputTokens: project.OutputTokens, ReasoningOutputTokens: project.ReasoningOutputTokens,
				TotalTokens: project.TotalTokens,
			})
		}
		for _, key := range usage.APIKeys {
			body.APIKeys = append(body.APIKeys, apiKeyUsageBody{
				APIKeyID: key.APIKeyID, ProjectID: key.ProjectID, Name: key.Name, Prefix: key.Prefix,
				Requests: key.Requests, InputTokens: key.InputTokens, CachedInputTokens: key.CachedInputTokens,
				OutputTokens: key.OutputTokens, ReasoningOutputTokens: key.ReasoningOutputTokens,
				TotalTokens: key.TotalTokens,
			})
		}
		return &usageSummaryOutput{Body: body}, nil
	})
}

func projectResponse(project projects.Project) projectBody {
	return projectBody{
		ID: project.ID, Name: project.Name, Limits: usageLimitResponses(project.Limits), CreatedAt: project.CreatedAt,
	}
}

func projectUsageLimits(limits []usageLimitBody) []projects.UsageLimit {
	result := make([]projects.UsageLimit, 0, len(limits))
	for _, limit := range limits {
		result = append(result, projects.UsageLimit{
			TokenLimit: limit.TokenLimit, IntervalCount: limit.IntervalCount,
			IntervalUnit: projects.IntervalUnit(limit.IntervalUnit),
		})
	}
	return result
}

func usageLimitResponses(limits []projects.UsageLimit) []usageLimitBody {
	result := make([]usageLimitBody, 0, len(limits))
	for _, limit := range limits {
		result = append(result, usageLimitBody{
			TokenLimit: limit.TokenLimit, IntervalCount: limit.IntervalCount, IntervalUnit: string(limit.IntervalUnit),
		})
	}
	return result
}

func usageLimitUsageResponses(limits []projects.UsageLimitUsage) []usageLimitUsageBody {
	result := make([]usageLimitUsageBody, 0, len(limits))
	for _, limit := range limits {
		result = append(result, usageLimitUsageBody{
			usageLimitBody: usageLimitBody{
				TokenLimit: limit.TokenLimit, IntervalCount: limit.IntervalCount, IntervalUnit: string(limit.IntervalUnit),
			},
			PeriodStart: limit.PeriodStart, PeriodEnd: limit.PeriodEnd, Requests: limit.Requests,
			InputTokens: limit.InputTokens, CachedInputTokens: limit.CachedInputTokens,
			OutputTokens: limit.OutputTokens, ReasoningOutputTokens: limit.ReasoningOutputTokens,
			TotalTokens: limit.TotalTokens,
		})
	}
	return result
}

func apiKeyResponse(key projects.APIKey) apiKeyBody {
	return apiKeyBody{
		ID: key.ID, ProjectID: key.ProjectID, Name: key.Name, Prefix: key.Prefix,
		CreatedAt: key.CreatedAt, LastUsedAt: key.LastUsedAt, RevokedAt: key.RevokedAt,
	}
}

func mapProjectError(err error) error {
	switch {
	case errors.Is(err, projects.ErrInvalidName),
		errors.Is(err, projects.ErrInvalidLimit),
		errors.Is(err, projects.ErrInvalidAPIKeyName):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, projects.ErrDuplicateName):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, projects.ErrProjectNotFound), errors.Is(err, projects.ErrAPIKeyNotFound):
		return huma.Error404NotFound(err.Error())
	default:
		return huma.Error500InternalServerError("Unable to update project access configuration.")
	}
}
