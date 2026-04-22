package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
)

// JobHandler exposes background job status.
type JobHandler struct {
	jobs *app.JobManager
}

func NewJobHandler(jobs *app.JobManager) *JobHandler {
	return &JobHandler{jobs: jobs}
}

func (h *JobHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listJobs", Method: http.MethodGet,
		Path:        "/api/v1/jobs",
		Summary:     "List background jobs",
		Description: "Returns up to 100 background jobs, newest first. Jobs are created by async compose operations (?async=true) and webhook-triggered GitOps syncs. Completed/failed jobs are pruned after 1 hour.",
		Tags:        []string{"jobs"},
		Errors:      errsViewer,
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "getJob", Method: http.MethodGet,
		Path:        "/api/v1/jobs/{id}",
		Summary:     "Get job status and output",
		Description: "Returns the status, captured output, and timestamps for a single job. Poll this endpoint to track async compose operations.",
		Tags:        []string{"jobs"},
		Errors:      errsViewerNotFound,
	}, h.Get)
}

// jobToDTO projects the internal app.Job type to the API DTO.
// Keeping this boundary isolates the OpenAPI schema from domain changes.
func jobToDTO(j *app.Job) dto.JobSummary {
	return dto.JobSummary{
		ID:         j.ID,
		Type:       j.Type,
		Target:     j.Target,
		Status:     string(j.Status),
		Output:     j.Output,
		Error:      j.Error,
		CreatedAt:  j.CreatedAt,
		StartedAt:  j.StartedAt,
		FinishedAt: j.FinishedAt,
	}
}

func (h *JobHandler) List(ctx context.Context, input *struct{}) (*dto.JobListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	jobs := h.jobs.List()
	out := &dto.JobListOutput{}
	out.Body.Jobs = make([]dto.JobSummary, 0, len(jobs))
	for _, j := range jobs {
		out.Body.Jobs = append(out.Body.Jobs, jobToDTO(j))
	}
	return out, nil
}

func (h *JobHandler) Get(ctx context.Context, input *dto.JobIDInput) (*dto.JobOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	job := h.jobs.Get(input.ID)
	if job == nil {
		return nil, huma.Error404NotFound("job not found")
	}
	out := &dto.JobOutput{}
	out.Body = jobToDTO(job)
	return out, nil
}
