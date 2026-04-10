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
		AuthMethod  string `json:"auth_method,omitempty" doc:"Auth method: none, token, ssh_key, ssh_file, basic"`
		Token       string `json:"token,omitempty" doc:"Access token for token auth"`
		SSHKey      string `json:"ssh_key,omitempty" doc:"PEM-encoded SSH private key (inline)"`
		SSHKeyFile  string `json:"ssh_key_file,omitempty" doc:"Path to SSH key file on server (per-stack override)"`
		Username    string `json:"username,omitempty" doc:"Username for basic auth"`
		Password    string `json:"password,omitempty" doc:"Password for basic auth"`
		AgeKey      string `json:"age_key,omitempty" doc:"Per-stack age private key for SOPS decryption (overrides global)"`
	}
}

type GetStackInput struct {
	Name       string `path:"name" doc:"Stack name"`
	DecryptEnv bool   `query:"decrypt_env" default:"false" doc:"Decrypt SOPS-encrypted .env content for display"`
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
	Name  string `path:"name" doc:"Stack name"`
	Async bool   `query:"async" default:"false" doc:"Run asynchronously and return a job ID instead of blocking"`
}

// --- Response types ---

type StackSummary struct {
	Name           string    `json:"name" doc:"Stack name"`
	Source         string    `json:"source" doc:"local or git"`
	Status         string    `json:"status" doc:"running, stopped, partial, unknown"`
	ContainerCount int       `json:"container_count" doc:"Number of containers in this stack"`
	RunningCount   int       `json:"running_count" doc:"Number of running containers"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type StackListOutput struct {
	Body struct {
		Stacks []StackSummary `json:"stacks"`
	}
}

type StackDetailOutput struct {
	Body struct {
		Name             string            `json:"name"`
		Path             string            `json:"path"`
		Source           string            `json:"source"`
		Status           string            `json:"status"`
		ComposeContent   string            `json:"compose_content"`
		EnvContent       string            `json:"env_content,omitempty"`
		EnvSopsEncrypted bool              `json:"env_sops_encrypted,omitempty" doc:"True when .env is SOPS-encrypted"`
		Dockerfiles      []StackFile       `json:"dockerfiles,omitempty" doc:"Dockerfiles found in the stack directory"`
		GitConfig        *GitSourceOutput  `json:"git_config,omitempty"`
		Containers       []ContainerOutput `json:"containers"`
		CreatedAt        time.Time         `json:"created_at"`
		UpdatedAt        time.Time         `json:"updated_at"`
	}
}

// StackFile represents a file in the stack directory.
type StackFile struct {
	Name    string `json:"name" doc:"Relative path within stack directory"`
	Content string `json:"content" doc:"File content"`
}

type UpdateEnvInput struct {
	Name string `path:"name" doc:"Stack name"`
	Body struct {
		Env string `json:"env" doc:".env file content"`
	}
}

type GitSourceOutput struct {
	RepoURL          string     `json:"repo_url"`
	Branch           string     `json:"branch"`
	ComposePath      string     `json:"compose_path"`
	AutoSync         bool       `json:"auto_sync"`
	AuthMethod       string     `json:"auth_method"`
	LastSyncAt       *time.Time `json:"last_sync_at,omitempty"`
	LastCommitSHA    string     `json:"last_commit_sha,omitempty"`
	SyncStatus       string     `json:"sync_status"`
	WorkingTreeDirty bool       `json:"working_tree_dirty" doc:"True when local edits diverge from git HEAD"`
}

type ContainerOutput struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ServiceName     string `json:"service_name"`
	Image           string `json:"image"`
	Status          string `json:"status"`
	Health          string `json:"health"`
	ExitCode        int    `json:"exit_code,omitempty"`
	RestartPolicy   string `json:"restart_policy,omitempty"`
	CompletedOneOff bool   `json:"completed_one_off,omitempty" doc:"True if this is an init/one-off container that exited successfully"`
}

type StackCreatedOutput struct {
	Body struct {
		Name   string `json:"name"`
		Source string `json:"source"`
		Path   string `json:"path"`
	}
}

type ExecComposeInput struct {
	Name string `path:"name" doc:"Stack name"`
	Body struct {
		Command string `json:"command" minLength:"1" doc:"Docker compose subcommand (e.g. 'ps', 'logs --tail 50', 'exec web sh -c env')"`
	}
}

type ExecComposeOutput struct {
	Body struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}
}

type ImportStacksInput struct {
	Body struct {
		SourceDir string `json:"source_dir" minLength:"1" doc:"Absolute path to directory containing stack subdirectories (e.g. /import/dockge)"`
	}
}

type ImportStacksOutput struct {
	Body struct {
		Imported []string `json:"imported"`
		Skipped  []string `json:"skipped"`
		Errors   []string `json:"errors"`
	}
}

type ConvertToGitInput struct {
	Name string `path:"name" doc:"Stack name"`
	Body struct {
		RepoURL    string `json:"repo_url" minLength:"1" doc:"Git repository URL"`
		Branch     string `json:"branch,omitempty" doc:"Branch (default: main)"`
		Token      string `json:"token,omitempty" doc:"Access token for auth"`
		SSHKey     string `json:"ssh_key,omitempty" doc:"PEM-encoded SSH private key"`
		SSHKeyFile string `json:"ssh_key_file,omitempty" doc:"Path to SSH key file on server"`
		Username   string `json:"username,omitempty" doc:"Username for basic auth"`
		Password   string `json:"password,omitempty" doc:"Password for basic auth"`
		AgeKey     string `json:"age_key,omitempty" doc:"Per-stack age key for SOPS decryption"`
	}
}

type ConvertToLocalInput struct {
	Name string `path:"name" doc:"Stack name"`
}

// --- Credentials ---

type StackCredentialsOutput struct {
	Body struct {
		AuthMethod string `json:"auth_method" doc:"none, token, ssh_key, ssh_file, basic"`
		PerStack   struct {
			TokenSet     bool   `json:"token_set"`
			TokenPreview string `json:"token_preview,omitempty"`
			SSHKeySet    bool   `json:"ssh_key_set" doc:"Inline PEM key in DB"`
			SSHKeyFile   string `json:"ssh_key_file,omitempty" doc:"Per-stack key file path"`
			AgeKeySet    bool   `json:"age_key_set"`
			UsernameSet  bool   `json:"username_set"`
		} `json:"per_stack"`
		Resolved struct {
			SSHSource   string `json:"ssh_source" doc:"Where SSH auth comes from (per-stack, global file, none)"`
			TokenSource string `json:"token_source" doc:"Where token comes from (per-stack, global, none)"`
			AgeSource   string `json:"age_source" doc:"Where age key comes from (per-stack, env, file, none)"`
		} `json:"resolved"`
	}
}

type UpdateStackCredentialsInput struct {
	Name string `path:"name" doc:"Stack name"`
	Body struct {
		Token      string `json:"token,omitempty" doc:"Git access token (empty to remove)"`
		SSHKey     string `json:"ssh_key,omitempty" doc:"PEM SSH key (empty to remove)"`
		SSHKeyFile string `json:"ssh_key_file,omitempty" doc:"SSH key file path (empty to remove)"`
		AgeKey     string `json:"age_key,omitempty" doc:"Age private key for SOPS (empty to remove)"`
		Username   string `json:"username,omitempty" doc:"Basic auth username (empty to remove)"`
		Password   string `json:"password,omitempty" doc:"Basic auth password (empty to remove)"`
	}
}

type ComposeOpOutput struct {
	Body struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
		JobID  string `json:"job_id,omitempty" doc:"Background job ID (present when async=true)"`
	}
}
