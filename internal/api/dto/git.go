package dto

import "time"

// --- Git Operations ---

type GitSyncInput struct {
	Name string `path:"name" doc:"Stack name"`
}

type GitSyncOutput struct {
	Body struct {
		Changed bool   `json:"changed" doc:"Whether compose file changed"`
		NewSHA  string `json:"new_sha" doc:"New HEAD commit SHA"`
	}
}

type GitLogInput struct {
	Name  string `path:"name" doc:"Stack name"`
	Limit int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Max commits to return"`
}

type GitLogOutput struct {
	Body struct {
		Commits []GitCommitOutput `json:"commits"`
	}
}

type GitCommitOutput struct {
	SHA      string    `json:"sha"`
	ShortSHA string    `json:"short_sha"`
	Message  string    `json:"message"`
	Author   string    `json:"author"`
	Date     time.Time `json:"date"`
}

type GitStatusInput struct {
	Name string `path:"name" doc:"Stack name"`
}

type GitStatusOutput struct {
	Body struct {
		RepoURL       string     `json:"repo_url"`
		Branch        string     `json:"branch"`
		ComposePath   string     `json:"compose_path"`
		AutoSync      bool       `json:"auto_sync"`
		LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
		LastCommitSHA string     `json:"last_commit_sha,omitempty"`
		SyncStatus    string     `json:"sync_status"`
	}
}

// --- Webhook Management ---

type CreateWebhookInput struct {
	Body struct {
		StackName    string `json:"stack_name" minLength:"1" doc:"Stack to trigger on push"`
		Provider     string `json:"provider" enum:"github,gitlab,gitea,generic" doc:"Git hosting provider"`
		BranchFilter string `json:"branch_filter,omitempty" doc:"Only trigger on this branch (empty = any)"`
		AutoRedeploy bool   `json:"auto_redeploy" doc:"Auto-redeploy on compose change"`
	}
}

type WebhookIDInput struct {
	ID string `path:"id" doc:"Webhook ID"`
}

type WebhookOutput struct {
	Body struct {
		ID           string `json:"id"`
		StackName    string `json:"stack_name"`
		Provider     string `json:"provider"`
		Secret       string `json:"secret" doc:"HMAC secret (configure in your git provider)"`
		URL          string `json:"url" doc:"Webhook URL to configure in your git provider"`
		BranchFilter string `json:"branch_filter,omitempty"`
		AutoRedeploy bool   `json:"auto_redeploy"`
	}
}

type WebhookListOutput struct {
	Body struct {
		Webhooks []WebhookSummary `json:"webhooks"`
	}
}

type WebhookSummary struct {
	ID           string `json:"id"`
	StackName    string `json:"stack_name"`
	Provider     string `json:"provider"`
	BranchFilter string `json:"branch_filter,omitempty"`
	AutoRedeploy bool   `json:"auto_redeploy"`
	URL          string `json:"url"`
}
