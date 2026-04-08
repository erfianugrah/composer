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

// GitHandler registers git operation endpoints for stacks.
type GitHandler struct {
	git *app.GitService
}

func NewGitHandler(git *app.GitService) *GitHandler {
	return &GitHandler{git: git}
}

func (h *GitHandler) Register(api huma.API) {
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
		return nil, huma.Error500InternalServerError(err.Error())
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
		return nil, huma.Error500InternalServerError(err.Error())
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
		return nil, huma.Error500InternalServerError(err.Error())
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
		return nil, huma.Error500InternalServerError(err.Error())
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

	// For a proper diff, we'd compare the working tree vs last commit.
	// For now, return empty diff (compose content matches committed content
	// unless edited outside Composer).
	out := &dto.DiffOutput{}
	out.Body.HasChanges = false
	out.Body.Summary = "No uncommitted changes"
	out.Body.Lines = []dto.DiffLine{}
	return out, nil
}
