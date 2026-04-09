package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

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
		Path: "/api/v1/jobs", Summary: "List background jobs", Tags: []string{"jobs"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "getJob", Method: http.MethodGet,
		Path: "/api/v1/jobs/{id}", Summary: "Get job status and output", Tags: []string{"jobs"},
	}, h.Get)
}

type JobOutput struct {
	Body app.Job
}

type JobListOutput struct {
	Body struct {
		Jobs []*app.Job `json:"jobs"`
	}
}

type JobIDInput struct {
	ID string `path:"id" doc:"Job ID"`
}

func (h *JobHandler) List(ctx context.Context, input *struct{}) (*JobListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	out := &JobListOutput{}
	out.Body.Jobs = h.jobs.List()
	return out, nil
}

func (h *JobHandler) Get(ctx context.Context, input *JobIDInput) (*JobOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	job := h.jobs.Get(input.ID)
	if job == nil {
		return nil, huma.Error404NotFound("job not found")
	}
	out := &JobOutput{}
	out.Body = *job
	return out, nil
}
