package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	"github.com/erfianugrah/composer/internal/app"
)

type TemplateHandler struct{}

func NewTemplateHandler() *TemplateHandler {
	return &TemplateHandler{}
}

func (h *TemplateHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listTemplates", Method: http.MethodGet,
		Path: "/api/v1/templates", Summary: "List available stack templates",
		Description: "Returns the built-in stack template catalog (nginx, postgres, etc.). Public endpoint to help onboarding before login.",
		Tags:        []string{"templates"},
		Security:    []map[string][]string{}, // public
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplate", Method: http.MethodGet,
		Path: "/api/v1/templates/{id}", Summary: "Get template compose content",
		Description: "Returns the full compose.yaml body for a template. Public endpoint.",
		Tags:        []string{"templates"},
		Security:    []map[string][]string{}, // public
	}, h.Get)
}

func (h *TemplateHandler) List(ctx context.Context, input *struct{}) (*dto.TemplateListOutput, error) {
	// Templates are public -- no auth required (helps with onboarding)
	templates := app.BuiltinTemplates()
	out := &dto.TemplateListOutput{}
	out.Body.Templates = make([]dto.TemplateSummary, 0, len(templates))
	for _, t := range templates {
		out.Body.Templates = append(out.Body.Templates, dto.TemplateSummary{
			ID: t.ID, Name: t.Name, Description: t.Description,
			Category: t.Category, Icon: t.Icon,
		})
	}
	return out, nil
}

func (h *TemplateHandler) Get(ctx context.Context, input *dto.TemplateIDInput) (*dto.TemplateDetailOutput, error) {
	t := app.GetTemplate(input.ID)
	if t == nil {
		return nil, huma.Error404NotFound("template not found")
	}

	out := &dto.TemplateDetailOutput{}
	out.Body.ID = t.ID
	out.Body.Name = t.Name
	out.Body.Description = t.Description
	out.Body.Category = t.Category
	out.Body.Icon = t.Icon
	out.Body.Compose = t.Compose
	return out, nil
}
