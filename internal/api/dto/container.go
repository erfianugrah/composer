package dto

type ContainerIDInput struct {
	ID string `path:"id" maxLength:"128" doc:"Container ID (short 12-char or full 64-char)"`
}

type ContainerListOutput struct {
	Body struct {
		Containers []ContainerOutput `json:"containers"`
	}
}

type ContainerDetailOutput struct {
	Body ContainerOutput
}

// ContainerLogsInput configures a snapshot-style log fetch.
// For streaming logs use GET /api/v1/sse/containers/{id}/logs.
type ContainerLogsInput struct {
	ID    string `path:"id" maxLength:"128" doc:"Container ID"`
	Tail  string `query:"tail" default:"100" doc:"Number of lines from the end ('all' for full history)"`
	Since string `query:"since" default:"" doc:"Show logs since RFC3339 timestamp or Go duration (e.g. 5m, 2h)"`
}

// ContainerLogsOutput is the response body for containerLogs.
type ContainerLogsOutput struct {
	Body struct {
		Lines []string `json:"lines"`
	}
}
