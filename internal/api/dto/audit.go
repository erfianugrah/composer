package dto

// AuditListInput is the query parameters for listAuditLog.
type AuditListInput struct {
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Max entries to return"`
	Action string `query:"action" doc:"Filter by action (e.g. stack.deploy, stack.up)"`
	Stack  string `query:"stack" doc:"Filter by stack name (matches in resource path)"`
}

// AuditEntryDTO is a single audit log entry.
type AuditEntryDTO struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource"`
	Detail    map[string]any `json:"detail,omitempty"`
	IPAddress string         `json:"ip_address"`
	CreatedAt string         `json:"created_at" format:"date-time"`
}

// AuditListOutput is the response body for listAuditLog.
type AuditListOutput struct {
	Body struct {
		Entries []AuditEntryDTO `json:"entries"`
	}
}
