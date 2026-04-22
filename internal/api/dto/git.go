package dto

import "time"

// --- Git Operations ---

type GitSyncInput struct {
	Name string `path:"name" maxLength:"128" doc:"Stack name"`
}

type GitSyncOutput struct {
	Body struct {
		Changed bool   `json:"changed" doc:"Whether compose file changed"`
		NewSHA  string `json:"new_sha" doc:"New HEAD commit SHA"`
	}
}

type GitLogInput struct {
	Name  string `path:"name" maxLength:"128" doc:"Stack name"`
	Limit int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Max commits to return"`
}

type GitLogOutput struct {
	Body struct {
		Commits []GitCommitOutput `json:"commits"`
	}
}

type GitCommitOutput struct {
	SHA            string    `json:"sha"`
	ShortSHA       string    `json:"short_sha"`
	Message        string    `json:"message"`
	Author         string    `json:"author"`
	Date           time.Time `json:"date"`
	ChangedCompose bool      `json:"changed_compose" doc:"True if this commit modified the compose file"`
}

type GitStatusInput struct {
	Name string `path:"name" maxLength:"128" doc:"Stack name"`
}

type GitRollbackInput struct {
	Name string `path:"name" maxLength:"128" doc:"Stack name"`
	Body struct {
		CommitSHA string `json:"commit_sha" minLength:"7" maxLength:"40" pattern:"^[0-9a-fA-F]+$" doc:"Commit SHA to rollback to (7-40 hex chars)"`
	}
}

type GitDiffInput struct {
	Name string `path:"name" maxLength:"128" doc:"Stack name"`
}

type GitStatusOutput struct {
	Body struct {
		RepoURL       string     `json:"repo_url"`
		Branch        string     `json:"branch"`
		ComposePath   string     `json:"compose_path"`
		AutoSync      bool       `json:"auto_sync"`
		LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
		LastCommitSHA string     `json:"last_commit_sha,omitempty"`
		SyncStatus    string     `json:"sync_status" enum:"synced,behind,diverged,dirty,error,syncing"`
	}
}

type CreateWebhookInput struct {
	Body struct {
		StackName    string `json:"stack_name" minLength:"1" maxLength:"128" doc:"Stack to trigger on push"`
		Provider     string `json:"provider" enum:"github,gitlab,gitea,generic" doc:"Git hosting provider"`
		BranchFilter string `json:"branch_filter,omitempty" maxLength:"255" doc:"Only trigger on this branch (empty = any)"`
		AutoRedeploy bool   `json:"auto_redeploy" doc:"Auto-redeploy on compose change"`
	}
}

// UpdateWebhookInput is the request body for PUT /api/v1/webhooks/{id}.
// Fields use pointer semantics so omitted fields preserve existing values
// and explicit nulls clear them.
type UpdateWebhookInput struct {
	ID   string `path:"id" maxLength:"64" doc:"Webhook ID"`
	Body struct {
		BranchFilter *string `json:"branch_filter,omitempty" doc:"Branch filter; empty string clears, omit to keep current"`
		AutoRedeploy *bool   `json:"auto_redeploy,omitempty" doc:"Auto-redeploy on push"`
		Provider     *string `json:"provider,omitempty" enum:"github,gitlab,gitea,generic" doc:"Webhook provider"`
	}
}

// DeliverySummary is a single webhook delivery attempt.
type DeliverySummary struct {
	ID        string `json:"id"`
	Event     string `json:"event"`
	Branch    string `json:"branch"`
	CommitSHA string `json:"commit_sha"`
	Status    string `json:"status" enum:"received,skipped,processing,success,failed"`
	Action    string `json:"action,omitempty" doc:"Deploy action taken when status=success"`
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"created_at" format:"date-time"`
}

// DeliveryListOutput is the response for listWebhookDeliveries.
type DeliveryListOutput struct {
	Body struct {
		Deliveries []DeliverySummary `json:"deliveries"`
	}
}

// WebhookCreatedOutput is returned on webhook creation.
// Secret is the full plaintext HMAC secret, shown ONCE; subsequent reads
// return WebhookOutput with a redacted secret.
type WebhookCreatedOutput struct {
	Body struct {
		ID           string `json:"id"`
		StackName    string `json:"stack_name"`
		Provider     string `json:"provider"`
		Secret       string `json:"secret" doc:"HMAC secret — shown ONCE on creation. Configure this in your git provider and store securely; GET returns a redacted form."`
		URL          string `json:"url" doc:"Webhook URL to configure in your git provider"`
		BranchFilter string `json:"branch_filter,omitempty"`
		AutoRedeploy bool   `json:"auto_redeploy"`
	}
}

// --- Deploy (CI integration) ---

type GitDeployOutput struct {
	Body struct {
		Action string `json:"action" enum:"redeployed,synced_pending_manual,accepted,no_change" doc:"Deploy action taken"`
		JobID  string `json:"job_id,omitempty" doc:"Background job ID (async mode)"`
	}
}

// --- Webhook Management ---

type WebhookIDInput struct {
	ID string `path:"id" maxLength:"64" pattern:"^wh_[0-9a-f]+$" doc:"Webhook ID (format: wh_<hex>)"`
}

// WebhookOutput is returned on GET/PUT of a webhook. The secret is
// redacted (last 4 chars shown) because the full secret is only available
// at creation time.
type WebhookOutput struct {
	Body struct {
		ID           string `json:"id"`
		StackName    string `json:"stack_name"`
		Provider     string `json:"provider"`
		Secret       string `json:"secret" doc:"HMAC secret (redacted; full value shown only at creation)"`
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
