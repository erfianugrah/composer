package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/app"
)

type TemplateHandler struct{}

func NewTemplateHandler() *TemplateHandler {
	return &TemplateHandler{}
}

func (h *TemplateHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listTemplates", Method: http.MethodGet,
		Path: "/api/v1/templates", Summary: "List available stack templates", Tags: []string{"templates"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplate", Method: http.MethodGet,
		Path: "/api/v1/templates/{id}", Summary: "Get template compose content", Tags: []string{"templates"},
	}, h.Get)
}

type TemplateListOutput struct {
	Body struct {
		Templates []TemplateSummary `json:"templates"`
	}
}

type TemplateSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Icon        string `json:"icon"`
}

type TemplateIDInput struct {
	ID string `path:"id" doc:"Template ID"`
}

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

func (h *TemplateHandler) List(ctx context.Context, input *struct{}) (*TemplateListOutput, error) {
	// Templates are public -- no auth required (helps with onboarding)
	templates := app.BuiltinTemplates()
	out := &TemplateListOutput{}
	out.Body.Templates = make([]TemplateSummary, 0, len(templates))
	for _, t := range templates {
		out.Body.Templates = append(out.Body.Templates, TemplateSummary{
			ID: t.ID, Name: t.Name, Description: t.Description,
			Category: t.Category, Icon: t.Icon,
		})
	}
	return out, nil
}

func (h *TemplateHandler) Get(ctx context.Context, input *TemplateIDInput) (*TemplateDetailOutput, error) {
	t := app.GetTemplate(input.ID)
	if t == nil {
		return nil, huma.Error404NotFound("template not found")
	}

	out := &TemplateDetailOutput{}
	out.Body.ID = t.ID
	out.Body.Name = t.Name
	out.Body.Description = t.Description
	out.Body.Category = t.Category
	out.Body.Icon = t.Icon
	out.Body.Compose = t.Compose
	return out, nil
}
