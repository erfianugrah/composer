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
		Path:        "/api/v1/pipelines",
		Summary:     "List pipelines",
		Description: "Returns every pipeline with step count and creation metadata. Operator+.",
		Tags:        []string{"pipelines"},
		Errors:      errsViewer,
	}, h.List)
	huma.Register(api, huma.Operation{
		OperationID: "createPipeline", Method: http.MethodPost,
		Path:        "/api/v1/pipelines",
		Summary:     "Create pipeline",
		Description: "Creates a new pipeline with steps and triggers. Admin only — pipelines can execute shell commands and docker operations on the host.",
		Tags:        []string{"pipelines"},
		Errors:      errsAdminMutation,
	}, h.Create)
	huma.Register(api, huma.Operation{
		OperationID: "getPipeline", Method: http.MethodGet,
		Path:        "/api/v1/pipelines/{id}",
		Summary:     "Get pipeline detail",
		Description: "Returns the full pipeline definition including steps, triggers, and timestamps. Operator+.",
		Tags:        []string{"pipelines"},
		Errors:      errsViewerNotFound,
	}, h.Get)
	huma.Register(api, huma.Operation{
		OperationID: "deletePipeline", Method: http.MethodDelete,
		Path:        "/api/v1/pipelines/{id}",
		Summary:     "Delete pipeline",
		Description: "Deletes a pipeline and its run history. Active runs continue until they finish. Operator+.",
		Tags:        []string{"pipelines"},
		Errors:      errsOperatorMutation,
	}, h.Delete)
	huma.Register(api, huma.Operation{
		OperationID: "runPipeline", Method: http.MethodPost,
		Path:        "/api/v1/pipelines/{id}/run",
		Summary:     "Trigger pipeline run",
		Description: "Starts a new pipeline run asynchronously. Returns the run ID immediately; poll `getPipelineRun` or stream `/api/v1/sse/pipelines/{id}/runs/{runId}` for status.",
		Tags:        []string{"pipelines"},
		Errors:      errsOperatorMutation,
	}, h.Run)
	huma.Register(api, huma.Operation{
		OperationID: "listPipelineRuns", Method: http.MethodGet,
		Path:        "/api/v1/pipelines/{id}/runs",
		Summary:     "List runs for pipeline",
		Description: "Returns a pipeline's run history, newest first. Operator+.",
		Tags:        []string{"pipelines"},
		Errors:      errsViewer,
	}, h.ListRuns)
	huma.Register(api, huma.Operation{
		OperationID: "getPipelineRun", Method: http.MethodGet,
		Path:        "/api/v1/pipelines/{id}/runs/{runId}",
		Summary:     "Get run detail",
		Description: "Returns the run status plus step-by-step results (output, duration, error).",
		Tags:        []string{"pipelines"},
		Errors:      errsViewerNotFound,
	}, h.GetRun)
	huma.Register(api, huma.Operation{
		OperationID: "updatePipeline", Method: http.MethodPut,
		Path:        "/api/v1/pipelines/{id}",
		Summary:     "Update pipeline",
		Description: "Replaces the pipeline definition. When the updated steps include `shell_command` or `docker_exec` types the caller must be admin (prevents privilege escalation via pipeline edit).",
		Tags:        []string{"pipelines"},
		Errors:      errsOperatorMutation,
	}, h.Update)
	huma.Register(api, huma.Operation{
		OperationID: "cancelPipelineRun", Method: http.MethodPost,
		Path:        "/api/v1/pipelines/{id}/cancel",
		Summary:     "Cancel running pipeline",
		Description: "Cancels the most recent pending or running run of this pipeline by cancelling its executor context. Completed runs are left untouched.",
		Tags:        []string{"pipelines"},
		Errors:      errsOperatorMutation,
	}, h.Cancel)
}

func (h *PipelineHandler) List(ctx context.Context, input *struct{}) (*dto.PipelineListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	pipelines, err := h.svc.List(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
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
	// Admin-only: pipelines can execute shell commands on the host
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
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
		return nil, serverError(ctx, err)
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
	if err := h.svc.Delete(ctx, input.ID); err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("pipeline not found")
		}
		return nil, serverError(ctx, err)
	}
	return nil, nil
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
		return nil, serverError(ctx, err)
	}

	return runToOutput(run), nil
}

func (h *PipelineHandler) ListRuns(ctx context.Context, input *dto.PipelineIDInput) (*dto.RunListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	runs, err := h.svc.ListRuns(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
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
		return nil, serverError(ctx, err)
	}

	return runToOutput(run), nil
}

func (h *PipelineHandler) Update(ctx context.Context, input *dto.UpdatePipelineInput) (*dto.PipelineCreatedOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	// Shell/docker steps require admin (prevents operator privilege escalation via pipeline update)
	for _, s := range input.Body.Steps {
		if s.Type == "shell_command" || s.Type == "docker_exec" {
			if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
				return nil, huma.Error403Forbidden("shell_command and docker_exec steps require admin role")
			}
			break
		}
	}

	existing, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("pipeline not found")
		}
		return nil, serverError(ctx, err)
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
		return nil, serverError(ctx, err)
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
		return nil, serverError(ctx, err)
	}

	for _, run := range runs {
		if run.Status == pipeline.RunRunning || run.Status == pipeline.RunPending {
			// Cancel the goroutine's context + persist status
			if err := h.svc.CancelRun(ctx, run); err != nil {
				return nil, serverError(ctx, err)
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

	// Include step results if available
	if len(r.StepResults) > 0 {
		out.Body.StepResults = make([]dto.StepResultDTO, 0, len(r.StepResults))
		for _, sr := range r.StepResults {
			out.Body.StepResults = append(out.Body.StepResults, dto.StepResultDTO{
				StepID:     sr.StepID,
				StepName:   sr.StepName,
				Status:     string(sr.Status),
				Output:     sr.Output,
				Error:      sr.Error,
				DurationMs: sr.Duration.Milliseconds(),
				StartedAt:  sr.StartedAt,
				FinishedAt: sr.FinishedAt,
			})
		}
	}

	return out
}
