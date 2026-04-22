package dto

import "time"

type CreatePipelineInput struct {
	Body PipelineBody
}

// UpdatePipelineInput combines the path ID with the full pipeline body.
// Pipelines are edited as a whole (replace) rather than merged.
type UpdatePipelineInput struct {
	ID   string `path:"id" maxLength:"128" doc:"Pipeline ID"`
	Body PipelineBody
}

// PipelineBody is the editable subset of a pipeline, shared by create/update.
type PipelineBody struct {
	Name        string            `json:"name" minLength:"1" maxLength:"128" doc:"Pipeline name"`
	Description string            `json:"description,omitempty" maxLength:"512" doc:"Pipeline description"`
	Steps       []PipelineStepDTO `json:"steps" minItems:"1" doc:"Pipeline steps"`
	Triggers    []TriggerDTO      `json:"triggers,omitempty" doc:"Pipeline triggers"`
}

type PipelineStepDTO struct {
	ID              string         `json:"id" minLength:"1" maxLength:"64" doc:"Step ID (unique within pipeline)"`
	Name            string         `json:"name" minLength:"1" maxLength:"128" doc:"Step name"`
	Type            string         `json:"type" enum:"compose_up,compose_down,compose_pull,compose_restart,shell_command,http_request,wait,notify" doc:"Step kind"`
	Config          map[string]any `json:"config,omitempty" doc:"Step config (shape varies by type)"`
	Timeout         string         `json:"timeout,omitempty" doc:"Step timeout as Go duration (e.g. 5m, 30s). Empty uses the step type's default."`
	ContinueOnError bool           `json:"continue_on_error,omitempty" doc:"Continue pipeline on step failure"`
	DependsOn       []string       `json:"depends_on,omitempty" doc:"Step IDs this step depends on"`
}

type TriggerDTO struct {
	Type   string         `json:"type" enum:"manual,webhook,schedule" doc:"Trigger kind"`
	Config map[string]any `json:"config,omitempty" doc:"Trigger config (shape varies by type)"`
}

type PipelineIDInput struct {
	ID string `path:"id" maxLength:"128" doc:"Pipeline ID"`
}

type RunPipelineInput struct {
	ID string `path:"id" maxLength:"128" doc:"Pipeline ID"`
}

type RunIDInput struct {
	ID    string `path:"id" maxLength:"128" doc:"Pipeline ID"`
	RunID string `path:"runId" maxLength:"128" doc:"Run ID"`
}

type PipelineListOutput struct {
	Body struct {
		Pipelines []PipelineSummary `json:"pipelines"`
	}
}

type PipelineSummary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	StepCount   int       `json:"step_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type PipelineDetailOutput struct {
	Body struct {
		ID          string            `json:"id"`
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Steps       []PipelineStepDTO `json:"steps"`
		Triggers    []TriggerDTO      `json:"triggers"`
		CreatedBy   string            `json:"created_by"`
		CreatedAt   time.Time         `json:"created_at"`
		UpdatedAt   time.Time         `json:"updated_at"`
	}
}

type PipelineCreatedOutput struct {
	Body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
}

type StepResultDTO struct {
	StepID     string     `json:"step_id"`
	StepName   string     `json:"step_name"`
	Status     string     `json:"status" enum:"pending,running,success,failed,cancelled,skipped"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	DurationMs int64      `json:"duration_ms"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type RunOutput struct {
	Body struct {
		ID          string          `json:"id"`
		PipelineID  string          `json:"pipeline_id"`
		Status      string          `json:"status" enum:"pending,running,success,failed,cancelled"`
		TriggeredBy string          `json:"triggered_by"`
		StepResults []StepResultDTO `json:"step_results,omitempty"`
		StartedAt   *time.Time      `json:"started_at,omitempty"`
		FinishedAt  *time.Time      `json:"finished_at,omitempty"`
		CreatedAt   time.Time       `json:"created_at"`
	}
}

type RunListOutput struct {
	Body struct {
		Runs []RunSummary `json:"runs"`
	}
}

type RunSummary struct {
	ID          string     `json:"id"`
	Status      string     `json:"status" enum:"pending,running,success,failed,cancelled"`
	TriggeredBy string     `json:"triggered_by"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}
