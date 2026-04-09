package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/stack"
)

// GitHandler registers git operation endpoints for stacks.
type GitHandler struct {
	git *app.GitService
}

func NewGitHandler(git *app.GitService) *GitHandler {
	return &GitHandler{git: git}
}

func (h *GitHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "createGitStack", Method: http.MethodPost,
		Path: "/api/v1/stacks/git", Summary: "Clone a git repo and create a git-backed stack", Tags: []string{"git", "stacks"},
	}, h.CreateGitStack)

	huma.Register(api, huma.Operation{
		OperationID: "syncStack", Method: http.MethodPost,
		Path: "/api/v1/stacks/{name}/sync", Summary: "Git pull + detect changes", Tags: []string{"git"},
	}, h.Sync)

	huma.Register(api, huma.Operation{
		OperationID: "gitLog", Method: http.MethodGet,
		Path: "/api/v1/stacks/{name}/git/log", Summary: "Commit history for compose file", Tags: []string{"git"},
	}, h.Log)

	huma.Register(api, huma.Operation{
		OperationID: "gitStatus", Method: http.MethodGet,
		Path: "/api/v1/stacks/{name}/git/status", Summary: "Git sync status", Tags: []string{"git"},
	}, h.Status)

	huma.Register(api, huma.Operation{
		OperationID: "rollbackStack", Method: http.MethodPost,
		Path: "/api/v1/stacks/{name}/rollback", Summary: "Checkout a specific commit", Tags: []string{"git"},
	}, h.Rollback)

	huma.Register(api, huma.Operation{
		OperationID: "gitDiff", Method: http.MethodGet,
		Path: "/api/v1/stacks/{name}/git/diff", Summary: "Diff current vs last synced", Tags: []string{"git"},
	}, h.GitDiff)
}

func (h *GitHandler) Sync(ctx context.Context, input *dto.GitSyncInput) (*dto.GitSyncOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	changed, newSHA, err := h.git.Sync(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(err)
	}

	out := &dto.GitSyncOutput{}
	out.Body.Changed = changed
	out.Body.NewSHA = newSHA
	return out, nil
}

func (h *GitHandler) Log(ctx context.Context, input *dto.GitLogInput) (*dto.GitLogOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}

	commits, err := h.git.GitLog(ctx, input.Name, input.Limit)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(err)
	}

	out := &dto.GitLogOutput{}
	out.Body.Commits = make([]dto.GitCommitOutput, 0, len(commits))
	for _, c := range commits {
		out.Body.Commits = append(out.Body.Commits, dto.GitCommitOutput{
			SHA: c.SHA, ShortSHA: c.ShortSHA, Message: c.Message,
			Author: c.Author, Date: c.Date,
		})
	}
	return out, nil
}

func (h *GitHandler) Status(ctx context.Context, input *dto.GitStatusInput) (*dto.GitStatusOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}

	cfg, err := h.git.GitStatus(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found or not git-backed")
		}
		return nil, serverError(err)
	}

	out := &dto.GitStatusOutput{}
	out.Body.RepoURL = cfg.RepoURL
	out.Body.Branch = cfg.Branch
	out.Body.ComposePath = cfg.ComposePath
	out.Body.AutoSync = cfg.AutoSync
	out.Body.LastSyncAt = cfg.LastSyncAt
	out.Body.LastCommitSHA = cfg.LastCommitSHA
	out.Body.SyncStatus = string(cfg.SyncStatus)
	return out, nil
}

func (h *GitHandler) Rollback(ctx context.Context, input *dto.GitRollbackInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	if err := h.git.Rollback(ctx, input.Name, input.Body.CommitSHA); err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found or not git-backed")
		}
		return nil, serverError(err)
	}

	return nil, nil
}

func (h *GitHandler) GitDiff(ctx context.Context, input *dto.GitDiffInput) (*dto.DiffOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}

	// Get the current compose content from disk
	st, err := h.git.GitStatus(ctx, input.Name)
	if err != nil {
		return nil, huma.Error404NotFound("stack not found or not git-backed")
	}
	_ = st

	// Compare working tree (disk) vs committed (HEAD) version
	diffLines, hasChanges, err := h.git.WorkingDiff(ctx, input.Name)
	if err != nil {
		// If diff fails (e.g., no commits yet), return empty diff
		out := &dto.DiffOutput{}
		out.Body.HasChanges = false
		out.Body.Summary = "Could not compute diff"
		out.Body.Lines = []dto.DiffLine{}
		return out, nil
	}

	out := &dto.DiffOutput{}
	out.Body.HasChanges = hasChanges
	if hasChanges {
		out.Body.Summary = fmt.Sprintf("%d lines changed", len(diffLines))
		out.Body.Lines = make([]dto.DiffLine, 0, len(diffLines))
		for _, l := range diffLines {
			out.Body.Lines = append(out.Body.Lines, dto.DiffLine{
				Type: l.Type, Content: l.Content, OldLine: l.OldLine, NewLine: l.NewLine,
			})
		}
	} else {
		out.Body.Summary = "No uncommitted changes"
		out.Body.Lines = []dto.DiffLine{}
	}
	return out, nil
}

// CreateGitStack clones a git repository and creates a git-backed stack.
func (h *GitHandler) CreateGitStack(ctx context.Context, input *dto.CreateGitStackInput) (*dto.StackCreatedOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	branch := input.Body.Branch
	if branch == "" {
		branch = "main"
	}
	composePath := input.Body.ComposePath
	if composePath == "" {
		composePath = "compose.yaml"
	}
	authMethod := stack.GitAuthNone
	if input.Body.AuthMethod != "" {
		authMethod = stack.GitAuthMethod(input.Body.AuthMethod)
	}

	gitCfg := &stack.GitSource{
		RepoURL:     input.Body.RepoURL,
		Branch:      branch,
		ComposePath: composePath,
		AutoSync:    true,
		AuthMethod:  authMethod,
	}

	// Build credentials from input
	if input.Body.Token != "" || input.Body.SSHKey != "" || input.Body.Username != "" {
		gitCfg.Credentials = &stack.GitCredentials{
			Token:    input.Body.Token,
			SSHKey:   input.Body.SSHKey,
			Username: input.Body.Username,
			Password: input.Body.Password,
		}
	}

	st, err := h.git.CreateGitStack(ctx, input.Body.Name, gitCfg)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	out := &dto.StackCreatedOutput{}
	out.Body.Name = st.Name
	out.Body.Source = string(st.Source)
	out.Body.Path = st.Path
	return out, nil
}
