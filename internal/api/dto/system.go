package dto

// DiffOutput returns a compose diff result.
type DiffOutput struct {
	Body struct {
		HasChanges bool       `json:"has_changes"`
		Summary    string     `json:"summary"`
		Lines      []DiffLine `json:"lines"`
	}
}

type DiffLine struct {
	Type    string `json:"type"` // "context", "added", "removed"
	Content string `json:"content"`
	OldLine int    `json:"old_line,omitempty"`
	NewLine int    `json:"new_line,omitempty"`
}
