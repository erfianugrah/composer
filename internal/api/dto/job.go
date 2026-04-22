package dto

import "time"

// JobSummary is the API representation of a background job.
// Kept decoupled from the internal app.Job type so the OpenAPI schema is
// not coupled to domain package changes.
type JobSummary struct {
	ID         string     `json:"id"`
	Type       string     `json:"type" doc:"Job kind: deploy, build, pull, sync_redeploy, etc."`
	Target     string     `json:"target" doc:"Stack name or image ref the job operates on"`
	Status     string     `json:"status" enum:"pending,running,completed,failed"`
	Output     string     `json:"output" doc:"Captured stdout"`
	Error      string     `json:"error" doc:"Captured stderr or error message"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// JobIDInput is a path parameter for a job lookup.
type JobIDInput struct {
	ID string `path:"id" doc:"Job ID (format: job_<hex>)"`
}

// JobOutput is the response body for getJob.
type JobOutput struct {
	Body JobSummary
}

// JobListOutput is the response body for listJobs.
type JobListOutput struct {
	Body struct {
		Jobs []JobSummary `json:"jobs"`
	}
}
