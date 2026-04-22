package dto

// TemplateIDInput is a path parameter for a template lookup.
type TemplateIDInput struct {
	ID string `path:"id" doc:"Template ID"`
}

// TemplateSummary is a list-view entry for the template catalog.
type TemplateSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Icon        string `json:"icon"`
}

// TemplateListOutput is the response body for listTemplates.
type TemplateListOutput struct {
	Body struct {
		Templates []TemplateSummary `json:"templates"`
	}
}

// TemplateDetailOutput is the response body for getTemplate.
// Includes the full compose.yaml content.
type TemplateDetailOutput struct {
	Body struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Icon        string `json:"icon"`
		Compose     string `json:"compose"`
	}
}
