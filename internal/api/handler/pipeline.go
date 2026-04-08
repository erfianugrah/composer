package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/pipeline"
)

type PipelineHandler struct {
	svc *app.PipelineService
}

func NewPipelineHandler(svc *app.PipelineService) *PipelineHandler {
	return &PipelineHandler{svc: svc}
}

func (h *PipelineHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listPipelines", Method: http.MethodGet,
		Path: "/api/v1/pipelines", Summary: "List pipelines", Tags: []string{"pipelines"},
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "createPipeline", Method: http.MethodPost,
		Path: "/api/v1/pipelines", Summary: "Create pipeline", Tags: []string{"pipelines"},
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "getPipeline", Method: http.MethodGet,
		Path: "/api/v1/pipelines/{id}", Summary: "Get pipeline detail", Tags: []string{"pipelines"},
	}, h.Get)
	huma.Register(api, huma.Operation{
		OperationID: "deletePipeline", Method: http.MethodDelete,
		Path: "/api/v1/pipelines/{id}", Summary: "Delete pipeline", Tags: []string{"pipelines"},
	}, h.Delete)
	huma.Register(api, huma.Operation{
		OperationID: "runPipeline", Method: http.MethodPost,
		Path: "/api/v1/pipelines/{id}/run", Summary: "Trigger pipeline run", Tags: []string{"pipelines"},
	}, h.Run)
	huma.Register(api, huma.Operation{
		OperationID: "listPipelineRuns", Method: http.MethodGet,
		Path: "/api/v1/pipelines/{id}/runs", Summary: "List runs for pipeline", Tags: []string{"pipelines"},
	}, h.ListRuns)
	huma.Register(api, huma.Operation{
		OperationID: "getPipelineRun", Method: http.MethodGet,
		Path: "/api/v1/pipelines/{id}/runs/{runId}", Summary: "Get run detail", Tags: []string{"pipelines"},
	}, h.GetRun)
	huma.Register(api, huma.Operation{
		OperationID: "updatePipeline", Method: http.MethodPut,
		Path: "/api/v1/pipelines/{id}", Summary: "Update pipeline", Tags: []string{"pipelines"},
	}, h.Update)
	huma.Register(api, huma.Operation{
		OperationID: "cancelPipelineRun", Method: http.MethodPost,
		Path: "/api/v1/pipelines/{id}/cancel", Summary: "Cancel running pipeline", Tags: []string{"pipelines"},
	}, h.Cancel)
}

func (h *PipelineHandler) List(ctx context.Context, input *struct{}) (*dto.PipelineListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	pipelines, err := h.svc.List(ctx)
	if err != nil {
		return nil, internalError()
	}

	out := &dto.PipelineListOutput{}
	out.Body.Pipelines = make([]dto.PipelineSummary, 0, len(pipelines))
	for _, p := range pipelines {
		out.Body.Pipelines = append(out.Body.Pipelines, dto.PipelineSummary{
			ID: p.ID, Name: p.Name, Description: p.Description,
			StepCount: len(p.Steps), CreatedAt: p.CreatedAt,
		})
	}
	return out, nil
}

func (h *PipelineHandler) Create(ctx context.Context, input *dto.CreatePipelineInput) (*dto.PipelineCreatedOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	callerID := authmw.UserIDFromContext(ctx)

	// Convert DTO steps to domain steps
	steps := make([]pipeline.Step, 0, len(input.Body.Steps))
	for _, s := range input.Body.Steps {
		step := pipeline.Step{
			ID: s.ID, Name: s.Name, Type: pipeline.StepType(s.Type),
			Config: s.Config, ContinueOnError: s.ContinueOnError,
			DependsOn: s.DependsOn,
		}
		if s.Timeout != "" {
			d, err := time.ParseDuration(s.Timeout)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("invalid timeout for step " + s.ID)
			}
			step.Timeout = d
		}
		steps = append(steps, step)
	}

	triggers := make([]pipeline.Trigger, 0, len(input.Body.Triggers))
	for _, t := range input.Body.Triggers {
		triggers = append(triggers, pipeline.Trigger{
			Type: pipeline.TriggerType(t.Type), Config: t.Config,
		})
	}

	p, err := h.svc.Create(ctx, input.Body.Name, input.Body.Description, callerID, steps, triggers)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	out := &dto.PipelineCreatedOutput{}
	out.Body.ID = p.ID
	out.Body.Name = p.Name
	return out, nil
}

func (h *PipelineHandler) Get(ctx context.Context, input *dto.PipelineIDInput) (*dto.PipelineDetailOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	p, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("pipeline not found")
		}
		return nil, internalError()
	}

	out := &dto.PipelineDetailOutput{}
	out.Body.ID = p.ID
	out.Body.Name = p.Name
	out.Body.Description = p.Description
	out.Body.CreatedBy = p.CreatedBy
	out.Body.CreatedAt = p.CreatedAt
	out.Body.UpdatedAt = p.UpdatedAt

	out.Body.Steps = make([]dto.PipelineStepDTO, 0, len(p.Steps))
	for _, s := range p.Steps {
		out.Body.Steps = append(out.Body.Steps, dto.PipelineStepDTO{
			ID: s.ID, Name: s.Name, Type: string(s.Type),
			Config: s.Config, Timeout: s.Timeout.String(),
			ContinueOnError: s.ContinueOnError, DependsOn: s.DependsOn,
		})
	}

	out.Body.Triggers = make([]dto.TriggerDTO, 0, len(p.Triggers))
	for _, t := range p.Triggers {
		out.Body.Triggers = append(out.Body.Triggers, dto.TriggerDTO{
			Type: string(t.Type), Config: t.Config,
		})
	}

	return out, nil
}

func (h *PipelineHandler) Delete(ctx context.Context, input *dto.PipelineIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	return nil, h.svc.Delete(ctx, input.ID)
}

func (h *PipelineHandler) Run(ctx context.Context, input *dto.RunPipelineInput) (*dto.RunOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	callerID := authmw.UserIDFromContext(ctx)
	run, err := h.svc.Run(ctx, input.ID, callerID)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("pipeline not found")
		}
		return nil, internalError()
	}

	return runToOutput(run), nil
}

func (h *PipelineHandler) ListRuns(ctx context.Context, input *dto.PipelineIDInput) (*dto.RunListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	runs, err := h.svc.ListRuns(ctx, input.ID)
	if err != nil {
		return nil, internalError()
	}

	out := &dto.RunListOutput{}
	out.Body.Runs = make([]dto.RunSummary, 0, len(runs))
	for _, r := range runs {
		out.Body.Runs = append(out.Body.Runs, dto.RunSummary{
			ID: r.ID, Status: string(r.Status), TriggeredBy: r.TriggeredBy,
			StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func (h *PipelineHandler) GetRun(ctx context.Context, input *dto.RunIDInput) (*dto.RunOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	run, err := h.svc.GetRun(ctx, input.RunID)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("run not found")
		}
		return nil, internalError()
	}

	return runToOutput(run), nil
}

// UpdatePipelineInput combines the path ID with the create body fields.
type UpdatePipelineInput struct {
	ID   string `path:"id" doc:"Pipeline ID"`
	Body struct {
		Name        string                `json:"name" minLength:"1" doc:"Pipeline name"`
		Description string                `json:"description,omitempty" doc:"Pipeline description"`
		Steps       []dto.PipelineStepDTO `json:"steps" minItems:"1" doc:"Pipeline steps"`
		Triggers    []dto.TriggerDTO      `json:"triggers,omitempty" doc:"Pipeline triggers"`
	}
}

func (h *PipelineHandler) Update(ctx context.Context, input *UpdatePipelineInput) (*dto.PipelineCreatedOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	existing, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("pipeline not found")
		}
		return nil, internalError()
	}

	// Update fields from the request body
	existing.Name = input.Body.Name
	existing.Description = input.Body.Description
	existing.Steps = make([]pipeline.Step, 0, len(input.Body.Steps))
	for _, s := range input.Body.Steps {
		step := pipeline.Step{
			ID:              s.ID,
			Name:            s.Name,
			Type:            pipeline.StepType(s.Type),
			Config:          s.Config,
			DependsOn:       s.DependsOn,
			ContinueOnError: s.ContinueOnError,
		}
		if s.Timeout != "" {
			if d, err := time.ParseDuration(s.Timeout); err == nil {
				step.Timeout = d
			}
		}
		existing.Steps = append(existing.Steps, step)
	}
	existing.Triggers = make([]pipeline.Trigger, 0, len(input.Body.Triggers))
	for _, t := range input.Body.Triggers {
		existing.Triggers = append(existing.Triggers, pipeline.Trigger{
			Type:   pipeline.TriggerType(t.Type),
			Config: t.Config,
		})
	}

	if err := h.svc.Update(ctx, existing); err != nil {
		return nil, internalError()
	}

	out := &dto.PipelineCreatedOutput{}
	out.Body.ID = existing.ID
	out.Body.Name = existing.Name
	return out, nil
}

func (h *PipelineHandler) Cancel(ctx context.Context, input *dto.PipelineIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	// Get the latest running run for this pipeline and cancel it
	runs, err := h.svc.ListRuns(ctx, input.ID)
	if err != nil {
		return nil, internalError()
	}

	for _, run := range runs {
		if run.Status == pipeline.RunRunning || run.Status == pipeline.RunPending {
			run.Cancel()
			// Persist the cancellation to the database
			if err := h.svc.UpdateRun(ctx, run); err != nil {
				return nil, internalError()
			}
			return nil, nil
		}
	}

	return nil, huma.Error404NotFound("no running pipeline to cancel")
}

func runToOutput(r *pipeline.Run) *dto.RunOutput {
	out := &dto.RunOutput{}
	out.Body.ID = r.ID
	out.Body.PipelineID = r.PipelineID
	out.Body.Status = string(r.Status)
	out.Body.TriggeredBy = r.TriggeredBy
	out.Body.StartedAt = r.StartedAt
	out.Body.FinishedAt = r.FinishedAt
	out.Body.CreatedAt = r.CreatedAt
	return out
}
