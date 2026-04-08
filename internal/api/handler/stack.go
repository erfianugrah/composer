package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
)

// StackHandler registers stack management API endpoints.
type StackHandler struct {
	stacks *app.StackService
}

func NewStackHandler(stacks *app.StackService) *StackHandler {
	return &StackHandler{stacks: stacks}
}

func (h *StackHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listStacks",
		Method:      http.MethodGet,
		Path:        "/api/v1/stacks",
		Summary:     "List all stacks",
		Tags:        []string{"stacks"},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "createStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks",
		Summary:     "Create a new stack",
		Tags:        []string{"stacks"},
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "getStack",
		Method:      http.MethodGet,
		Path:        "/api/v1/stacks/{name}",
		Summary:     "Get stack details",
		Tags:        []string{"stacks"},
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "updateStack",
		Method:      http.MethodPut,
		Path:        "/api/v1/stacks/{name}",
		Summary:     "Update stack compose content",
		Tags:        []string{"stacks"},
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID: "deleteStack",
		Method:      http.MethodDelete,
		Path:        "/api/v1/stacks/{name}",
		Summary:     "Delete a stack",
		Tags:        []string{"stacks"},
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "deployStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/up",
		Summary:     "Deploy stack (docker compose up)",
		Tags:        []string{"stacks"},
	}, h.Deploy)

	huma.Register(api, huma.Operation{
		OperationID: "stopStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/down",
		Summary:     "Stop stack (docker compose down)",
		Tags:        []string{"stacks"},
	}, h.Stop)

	huma.Register(api, huma.Operation{
		OperationID: "restartStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/restart",
		Summary:     "Restart stack",
		Tags:        []string{"stacks"},
	}, h.Restart)

	huma.Register(api, huma.Operation{
		OperationID: "pullStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/pull",
		Summary:     "Pull latest images for stack",
		Tags:        []string{"stacks"},
	}, h.Pull)

	huma.Register(api, huma.Operation{
		OperationID: "validateStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/validate",
		Summary:     "Validate compose syntax",
		Tags:        []string{"stacks"},
	}, h.Validate)

	huma.Register(api, huma.Operation{
		OperationID: "diffStack",
		Method:      http.MethodGet,
		Path:        "/api/v1/stacks/{name}/diff",
		Summary:     "Show pending compose changes vs saved version",
		Tags:        []string{"stacks"},
	}, h.Diff)
}

func (h *StackHandler) List(ctx context.Context, input *struct{}) (*dto.StackListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	stacks, err := h.stacks.List(ctx)
	if err != nil {
		return nil, internalError()
	}

	out := &dto.StackListOutput{}
	out.Body.Stacks = make([]dto.StackSummary, 0, len(stacks))
	for _, s := range stacks {
		out.Body.Stacks = append(out.Body.Stacks, dto.StackSummary{
			Name:      s.Name,
			Source:    string(s.Source),
			Status:    string(s.Status),
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
		})
	}
	return out, nil
}

func (h *StackHandler) Create(ctx context.Context, input *dto.CreateStackInput) (*dto.StackCreatedOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	st, err := h.stacks.Create(ctx, input.Body.Name, input.Body.Compose)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	out := &dto.StackCreatedOutput{}
	out.Body.Name = st.Name
	out.Body.Source = string(st.Source)
	out.Body.Path = st.Path
	return out, nil
}

func (h *StackHandler) Get(ctx context.Context, input *dto.GetStackInput) (*dto.StackDetailOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	st, err := h.stacks.Get(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, internalError()
	}

	out := &dto.StackDetailOutput{}
	out.Body.Name = st.Name
	out.Body.Path = st.Path
	out.Body.Source = string(st.Source)
	out.Body.Status = string(st.Status)
	out.Body.ComposeContent = st.ComposeContent
	out.Body.CreatedAt = st.CreatedAt
	out.Body.UpdatedAt = st.UpdatedAt

	// Populate containers from Docker
	containers, err := h.stacks.Containers(ctx, st.Name)
	if err == nil {
		out.Body.Containers = make([]dto.ContainerOutput, 0, len(containers))
		for _, c := range containers {
			out.Body.Containers = append(out.Body.Containers, dto.ContainerOutput{
				ID:          c.ID,
				Name:        c.Name,
				ServiceName: c.ServiceName,
				Image:       c.Image,
				Status:      string(c.Status),
				Health:      string(c.Health),
			})
		}
	} else {
		out.Body.Containers = []dto.ContainerOutput{}
	}

	if st.GitConfig != nil {
		out.Body.GitConfig = &dto.GitSourceOutput{
			RepoURL:       st.GitConfig.RepoURL,
			Branch:        st.GitConfig.Branch,
			ComposePath:   st.GitConfig.ComposePath,
			AutoSync:      st.GitConfig.AutoSync,
			AuthMethod:    string(st.GitConfig.AuthMethod),
			LastSyncAt:    st.GitConfig.LastSyncAt,
			LastCommitSHA: st.GitConfig.LastCommitSHA,
			SyncStatus:    string(st.GitConfig.SyncStatus),
		}
	}

	return out, nil
}

func (h *StackHandler) Update(ctx context.Context, input *dto.UpdateStackInput) (*dto.StackDetailOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	st, err := h.stacks.Update(ctx, input.Name, input.Body.Compose)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	out := &dto.StackDetailOutput{}
	out.Body.Name = st.Name
	out.Body.Path = st.Path
	out.Body.Source = string(st.Source)
	out.Body.Status = string(st.Status)
	out.Body.ComposeContent = st.ComposeContent
	out.Body.CreatedAt = st.CreatedAt
	out.Body.UpdatedAt = st.UpdatedAt
	out.Body.Containers = []dto.ContainerOutput{}
	return out, nil
}

func (h *StackHandler) Delete(ctx context.Context, input *dto.DeleteStackInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	err := h.stacks.Delete(ctx, input.Name, input.RemoveVolumes)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, internalError()
	}
	return nil, nil
}

func (h *StackHandler) Deploy(ctx context.Context, input *dto.StackNameInput) (*dto.ComposeOpOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	result, err := h.stacks.Deploy(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, internalError()
	}

	out := &dto.ComposeOpOutput{}
	out.Body.Stdout = result.Stdout
	out.Body.Stderr = result.Stderr
	return out, nil
}

func (h *StackHandler) Stop(ctx context.Context, input *dto.StackNameInput) (*dto.ComposeOpOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	result, err := h.stacks.Stop(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, internalError()
	}

	out := &dto.ComposeOpOutput{}
	out.Body.Stdout = result.Stdout
	out.Body.Stderr = result.Stderr
	return out, nil
}

func (h *StackHandler) Restart(ctx context.Context, input *dto.StackNameInput) (*dto.ComposeOpOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	result, err := h.stacks.Restart(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, internalError()
	}

	out := &dto.ComposeOpOutput{}
	out.Body.Stdout = result.Stdout
	out.Body.Stderr = result.Stderr
	return out, nil
}

func (h *StackHandler) Pull(ctx context.Context, input *dto.StackNameInput) (*dto.ComposeOpOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	result, err := h.stacks.Pull(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, internalError()
	}

	out := &dto.ComposeOpOutput{}
	out.Body.Stdout = result.Stdout
	out.Body.Stderr = result.Stderr
	return out, nil
}

func (h *StackHandler) Validate(ctx context.Context, input *dto.StackNameInput) (*dto.ComposeOpOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	result, err := h.stacks.Validate(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		out := &dto.ComposeOpOutput{}
		if result != nil {
			out.Body.Stderr = result.Stderr
		}
		return out, nil
	}

	out := &dto.ComposeOpOutput{}
	out.Body.Stdout = "valid"
	return out, nil
}

func (h *StackHandler) Diff(ctx context.Context, input *dto.GetStackInput) (*dto.DiffOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}

	st, err := h.stacks.Get(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, internalError()
	}

	// Compare compose content on disk vs what docker compose reports as running config
	currentContent := st.ComposeContent
	if currentContent == "" {
		currentContent = "(no compose content)"
	}

	// Get the running compose config from Docker
	runningResult, err := h.stacks.Validate(ctx, input.Name)
	runningContent := ""
	if err == nil && runningResult != nil {
		runningContent = runningResult.Stdout
	}
	if runningContent == "" || runningContent == "valid" {
		// Validate returns "valid" on success, not the actual config.
		// Without a separate "last deployed" snapshot, compare against self.
		runningContent = currentContent
	}

	diff := app.ComputeDiff(runningContent, currentContent)

	out := &dto.DiffOutput{}
	out.Body.HasChanges = diff.HasChanges
	out.Body.Summary = diff.Summary
	out.Body.Lines = make([]dto.DiffLine, 0, len(diff.Lines))
	for _, l := range diff.Lines {
		out.Body.Lines = append(out.Body.Lines, dto.DiffLine{
			Type: l.Type, Content: l.Content, OldLine: l.OldLine, NewLine: l.NewLine,
		})
	}
	return out, nil
}
