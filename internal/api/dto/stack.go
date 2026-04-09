package dto

import "time"

// --- Request types ---

type CreateStackInput struct {
	Body struct {
		Name    string `json:"name" minLength:"1" maxLength:"128" doc:"Stack name (filesystem-safe, no spaces/slashes)"`
		Compose string `json:"compose" minLength:"1" doc:"compose.yaml content"`
	}
}

type CreateGitStackInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" maxLength:"128" doc:"Stack name"`
		RepoURL     string `json:"repo_url" minLength:"1" doc:"Git repository URL (HTTPS or SSH)"`
		Branch      string `json:"branch,omitempty" doc:"Branch to track (default: main)"`
		ComposePath string `json:"compose_path,omitempty" doc:"Path to compose file in repo (default: compose.yaml)"`
		AuthMethod  string `json:"auth_method,omitempty" doc:"Auth method: none, token, ssh_key, basic"`
		Token       string `json:"token,omitempty" doc:"Access token for token auth"`
		SSHKey      string `json:"ssh_key,omitempty" doc:"PEM-encoded SSH private key"`
		Username    string `json:"username,omitempty" doc:"Username for basic auth"`
		Password    string `json:"password,omitempty" doc:"Password for basic auth"`
	}
}

type GetStackInput struct {
	Name string `path:"name" doc:"Stack name"`
}

type UpdateStackInput struct {
	Name string `path:"name" doc:"Stack name"`
	Body struct {
		Compose string `json:"compose" minLength:"1" doc:"Updated compose.yaml content"`
	}
}

type DeleteStackInput struct {
	Name          string `path:"name" doc:"Stack name"`
	RemoveVolumes bool   `query:"remove_volumes" default:"false" doc:"Also remove named volumes"`
}

type StackNameInput struct {
	Name string `path:"name" doc:"Stack name"`
}

// --- Response types ---

type StackSummary struct {
	Name      string    `json:"name" doc:"Stack name"`
	Source    string    `json:"source" doc:"local or git"`
	Status    string    `json:"status" doc:"running, stopped, partial, unknown"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type StackListOutput struct {
	Body struct {
		Stacks []StackSummary `json:"stacks"`
	}
}

type StackDetailOutput struct {
	Body struct {
		Name           string            `json:"name"`
		Path           string            `json:"path"`
		Source         string            `json:"source"`
		Status         string            `json:"status"`
		ComposeContent string            `json:"compose_content"`
		GitConfig      *GitSourceOutput  `json:"git_config,omitempty"`
		Containers     []ContainerOutput `json:"containers"`
		CreatedAt      time.Time         `json:"created_at"`
		UpdatedAt      time.Time         `json:"updated_at"`
	}
}

type GitSourceOutput struct {
	RepoURL       string     `json:"repo_url"`
	Branch        string     `json:"branch"`
	ComposePath   string     `json:"compose_path"`
	AutoSync      bool       `json:"auto_sync"`
	AuthMethod    string     `json:"auth_method"`
	LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
	LastCommitSHA string     `json:"last_commit_sha,omitempty"`
	SyncStatus    string     `json:"sync_status"`
}

type ContainerOutput struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ServiceName string `json:"service_name"`
	Image       string `json:"image"`
	Status      string `json:"status"`
	Health      string `json:"health"`
}

type StackCreatedOutput struct {
	Body struct {
		Name   string `json:"name"`
		Source string `json:"source"`
		Path   string `json:"path"`
	}
}

type ComposeOpOutput struct {
	Body struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
}
