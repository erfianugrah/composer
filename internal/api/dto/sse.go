package dto

// ContainerLogInput defines the path/query params for container log streaming.
type ContainerLogInput struct {
	ID    string `path:"id" maxLength:"128" doc:"Container ID"`
	Tail  string `query:"tail" default:"100" doc:"Number of lines from the end"`
	Since string `query:"since" default:"" doc:"Show logs since RFC3339 timestamp or Go duration"`
}

// StackLogInput defines the path/query params for stack-level log streaming.
type StackLogInput struct {
	Name  string `path:"name" maxLength:"128" doc:"Stack name"`
	Tail  string `query:"tail" default:"50" doc:"Lines per container"`
	Since string `query:"since" default:"" doc:"Since timestamp"`
}

// PipelineRunSSEInput defines the path params for pipeline run streaming.
type PipelineRunSSEInput struct {
	ID    string `path:"id" maxLength:"128" doc:"Pipeline ID"`
	RunID string `path:"runId" maxLength:"128" doc:"Run ID"`
}

// ContainerStatsInput defines the path params for stats streaming.
type ContainerStatsInput struct {
	ID string `path:"id" maxLength:"128" doc:"Container ID"`
}
