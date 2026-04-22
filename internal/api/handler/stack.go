package handler

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/docker"
	sopsInfra "github.com/erfianugrah/composer/internal/infra/sops"
)

// StackHandler registers stack management API endpoints.
type StackHandler struct {
	stacks *app.StackService
	jobs   *app.JobManager
}

func NewStackHandler(stacks *app.StackService, jobs *app.JobManager) *StackHandler {
	return &StackHandler{stacks: stacks, jobs: jobs}
}

// composeOp is the signature shared by Deploy, BuildAndDeploy, Stop, Restart, Pull.
type composeOp func(ctx context.Context, name string) (*docker.ComposeResult, error)

// runAsync creates a background job, spawns a goroutine with context.Background()
// (so request cancellation can't kill the subprocess), and returns immediately.
func (h *StackHandler) runAsync(jobType, stackName string, op composeOp) *dto.ComposeOpOutput {
	job := h.jobs.Create(jobType, stackName)
	h.jobs.Start(job.ID)
	go func() {
		result, err := op(context.Background(), stackName)
		if err != nil {
			errMsg := err.Error()
			if result != nil && result.Stderr != "" {
				errMsg = result.Stderr
			}
			h.jobs.Fail(job.ID, errMsg)
			return
		}
		stdout, stderr := "", ""
		if result != nil {
			stdout = result.Stdout
			stderr = result.Stderr
		}
		h.jobs.Complete(job.ID, stdout, stderr)
	}()
	out := &dto.ComposeOpOutput{}
	out.Body.JobID = job.ID
	return out
}

func (h *StackHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listStacks",
		Method:      http.MethodGet,
		Path:        "/api/v1/stacks",
		Summary:     "List all stacks",
		Description: "Returns every stack known to Composer with container counts and status (running / stopped / partial / unknown). Viewer+.",
		Tags:        []string{"stacks"},
		Errors:      errsViewer,
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "createStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks",
		Summary:     "Create a new stack",
		Description: "Creates a local-source stack from the supplied compose YAML. The compose content is written to `<stacks_dir>/<name>/compose.yaml` but not deployed. Use `deployStack` to bring services up.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "getStack",
		Method:      http.MethodGet,
		Path:        "/api/v1/stacks/{name}",
		Summary:     "Get stack details",
		Description: "Returns compose content, `.env` content (optionally SOPS-decrypted), detected Dockerfiles, containers, and git config (if git-backed).",
		Tags:        []string{"stacks"},
		Errors:      errsViewerNotFound,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "updateStack",
		Method:      http.MethodPut,
		Path:        "/api/v1/stacks/{name}",
		Summary:     "Update stack compose content",
		Description: "Overwrites the compose.yaml on disk. For git-backed stacks this marks sync_status=dirty until a new commit is pushed or rollback is performed.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID: "deleteStack",
		Method:      http.MethodDelete,
		Path:        "/api/v1/stacks/{name}",
		Summary:     "Delete a stack",
		Description: "Stops the stack, removes containers, and deletes the stack directory. Pass `?remove_volumes=true` to also drop named volumes (destructive).",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "deployStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/up",
		Summary:     "Deploy stack (docker compose up)",
		Description: "Runs `docker compose up -d` in the stack directory. Use `?async=true` to return a job ID immediately and poll `/api/v1/jobs/{id}` for completion. Synchronous calls time out at 10 minutes.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.Deploy)

	huma.Register(api, huma.Operation{
		OperationID: "stopStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/down",
		Summary:     "Stop stack (docker compose down)",
		Description: "Runs `docker compose down` to stop and remove containers. Volumes and networks are preserved. Supports `?async=true`.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.Stop)

	huma.Register(api, huma.Operation{
		OperationID: "buildAndDeployStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/build",
		Summary:     "Build images and deploy",
		Description: "Runs `docker compose up -d --build` to rebuild images from Dockerfiles then deploy. Supports `?async=true`.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.BuildAndDeploy)

	huma.Register(api, huma.Operation{
		OperationID: "restartStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/restart",
		Summary:     "Restart stack",
		Description: "Restarts all services in the stack via `docker compose restart`. Supports `?async=true`.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.Restart)

	huma.Register(api, huma.Operation{
		OperationID: "pullStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/pull",
		Summary:     "Pull latest images for stack",
		Description: "Pulls the newest version of every image referenced by compose.yaml. Does not redeploy — call `deployStack` afterwards to apply. Supports `?async=true`.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.Pull)

	huma.Register(api, huma.Operation{
		OperationID: "validateStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/validate",
		Summary:     "Validate compose syntax",
		Description: "Runs `docker compose config` to validate and normalize the compose file. Returns the validation output; non-zero exit surfaces compose syntax errors.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.Validate)

	huma.Register(api, huma.Operation{
		OperationID: "diffStack",
		Method:      http.MethodGet,
		Path:        "/api/v1/stacks/{name}/diff",
		Summary:     "Show pending compose changes vs saved version",
		Description: "Computes a line diff between the on-disk compose.yaml and Docker's resolved config. Useful to preview what a deploy would actually apply.",
		Tags:        []string{"stacks"},
		Errors:      errsViewerNotFound,
	}, h.Diff)

	huma.Register(api, huma.Operation{
		OperationID: "execComposeCommand",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/exec",
		Summary:     "Run a docker compose command against this stack",
		Description: "Runs an allowlisted `docker compose <args>` in the stack's directory. Read-only subcommands (ps, logs, top, config, images, port, version, ls, events, build) are operator+. Shell-equivalent subcommands (exec, cp) require admin.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.ExecCompose)

	huma.Register(api, huma.Operation{
		OperationID: "updateStackEnv",
		Method:      http.MethodPut,
		Path:        "/api/v1/stacks/{name}/env",
		Summary:     "Update .env file for a stack",
		Description: "Writes the provided content to `<stack>/.env`. Sending empty content deletes the file. Does not validate content or trigger a deploy.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.UpdateEnv)

	huma.Register(api, huma.Operation{
		OperationID: "getStackCredentials",
		Method:      http.MethodGet,
		Path:        "/api/v1/stacks/{name}/credentials",
		Summary:     "Get resolved credential chain for a stack",
		Description: "Returns the effective git/SSH/SOPS credentials used for this stack plus where each came from (per-stack, global, env). Redacts secret material — only sources are reported.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.GetCredentials)

	huma.Register(api, huma.Operation{
		OperationID: "updateStackCredentials",
		Method:      http.MethodPut,
		Path:        "/api/v1/stacks/{name}/credentials",
		Summary:     "Update per-stack credential overrides",
		Description: "Replaces the stack's per-stack credentials (git token, SSH key, SOPS age key, basic-auth). Sending an empty value for a field clears that override so the global credential takes effect.",
		Tags:        []string{"stacks"},
		Errors:      errsOperatorMutation,
	}, h.UpdateCredentials)

	huma.Register(api, huma.Operation{
		OperationID: "importStacks",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/import",
		Summary:     "Import stacks from an external directory",
		Description: "Scans a host directory (e.g. a Dockge data dir) and imports any discovered compose projects as local stacks. Existing stacks are skipped. Admin only.",
		Tags:        []string{"stacks"},
		Errors:      errsAdminMutation,
	}, h.Import)

	huma.Register(api, huma.Operation{
		OperationID: "convertToGit",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/convert/git",
		Summary:     "Convert a local stack to git-backed",
		Description: "Turns an existing local stack into a git-tracked stack by cloning the provided repo over the stack directory. Credentials can be supplied per-stack or defer to global settings.",
		Tags:        []string{"stacks", "git"},
		Errors:      errsOperatorMutation,
	}, h.ConvertToGit)

	huma.Register(api, huma.Operation{
		OperationID: "convertToLocal",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/convert/local",
		Summary:     "Detach a git-backed stack and convert to local",
		Description: "Removes the stack's git metadata so it's no longer tracked against a remote. The current on-disk compose content is preserved; future edits happen locally only.",
		Tags:        []string{"stacks", "git"},
		Errors:      errsOperatorMutation,
	}, h.ConvertToLocal)
}

func (h *StackHandler) List(ctx context.Context, input *struct{}) (*dto.StackListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	stacks, err := h.stacks.List(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	// Fetch every container once and bucket by stack name (compose project label).
	// Previously this loop made N individual Docker API calls — now it's one.
	type counts struct{ total, running int }
	byStack := map[string]counts{}
	if all, err := h.stacks.Containers(ctx, ""); err == nil {
		for _, c := range all {
			if c.StackName == "" {
				continue
			}
			b := byStack[c.StackName]
			b.total++
			if c.IsRunning() {
				b.running++
			}
			byStack[c.StackName] = b
		}
	}

	out := &dto.StackListOutput{}
	out.Body.Stacks = make([]dto.StackSummary, 0, len(stacks))
	for _, s := range stacks {
		b := byStack[s.Name]
		out.Body.Stacks = append(out.Body.Stacks, dto.StackSummary{
			Name:           s.Name,
			Source:         string(s.Source),
			Status:         string(s.Status),
			ContainerCount: b.total,
			RunningCount:   b.running,
			CreatedAt:      s.CreatedAt,
			UpdatedAt:      s.UpdatedAt,
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
		return nil, serverError(ctx, err)
	}

	out := &dto.StackDetailOutput{}
	out.Body.Name = st.Name
	out.Body.Path = st.Path
	out.Body.Source = string(st.Source)
	out.Body.Status = string(st.Status)
	out.Body.ComposeContent = st.ComposeContent
	out.Body.CreatedAt = st.CreatedAt
	out.Body.UpdatedAt = st.UpdatedAt

	// Read .env file if it exists
	envPath := st.Path + "/.env"
	if envData, err := os.ReadFile(envPath); err == nil {
		out.Body.EnvContent = string(envData)
		out.Body.EnvSopsEncrypted = sopsInfra.IsSopsEncrypted(envData)
		// Decrypt on demand (only when explicitly requested)
		if input.DecryptEnv && out.Body.EnvSopsEncrypted {
			out.Body.EnvContent = h.stacks.DecryptEnvContent(ctx, st.Name, envPath)
		}
	}

	// Scan for Dockerfiles in the stack directory
	out.Body.Dockerfiles = scanDockerfiles(st.Path)

	// Populate containers from Docker
	containers, err := h.stacks.Containers(ctx, st.Name)
	if err == nil {
		out.Body.Containers = make([]dto.ContainerOutput, 0, len(containers))
		for _, c := range containers {
			out.Body.Containers = append(out.Body.Containers, dto.ContainerOutput{
				ID:              c.ID,
				Name:            c.Name,
				ServiceName:     c.ServiceName,
				Image:           c.Image,
				Status:          string(c.Status),
				Health:          string(c.Health),
				ExitCode:        c.ExitCode,
				RestartPolicy:   c.RestartPolicy,
				CompletedOneOff: c.IsCompletedOneOff(),
			})
		}
	} else {
		out.Body.Containers = []dto.ContainerOutput{}
	}

	if st.GitConfig != nil {
		out.Body.GitConfig = &dto.GitSourceOutput{
			RepoURL:          st.GitConfig.RepoURL,
			Branch:           st.GitConfig.Branch,
			ComposePath:      st.GitConfig.ComposePath,
			AutoSync:         st.GitConfig.AutoSync,
			AuthMethod:       string(st.GitConfig.AuthMethod),
			LastSyncAt:       st.GitConfig.LastSyncAt,
			LastCommitSHA:    st.GitConfig.LastCommitSHA,
			SyncStatus:       string(st.GitConfig.SyncStatus),
			WorkingTreeDirty: st.GitConfig.SyncStatus == stack.GitDirty,
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
		return nil, serverError(ctx, err)
	}
	return nil, nil
}

func (h *StackHandler) Deploy(ctx context.Context, input *dto.StackNameInput) (*dto.ComposeOpOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if input.Async && h.jobs != nil {
		return h.runAsync("deploy", input.Name, h.stacks.Deploy), nil
	}
	// Use background context so client disconnect can't kill the subprocess mid-operation
	opCtx, opCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer opCancel()
	result, err := h.stacks.Deploy(opCtx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(ctx, err)
	}

	out := &dto.ComposeOpOutput{}
	out.Body.Stdout = result.Stdout
	out.Body.Stderr = result.Stderr
	return out, nil
}

func (h *StackHandler) BuildAndDeploy(ctx context.Context, input *dto.StackNameInput) (*dto.ComposeOpOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if input.Async && h.jobs != nil {
		return h.runAsync("build_deploy", input.Name, h.stacks.BuildAndDeploy), nil
	}
	opCtx, opCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer opCancel()
	result, err := h.stacks.BuildAndDeploy(opCtx, input.Name)
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
	out.Body.Stdout = result.Stdout
	out.Body.Stderr = result.Stderr
	return out, nil
}

func (h *StackHandler) Stop(ctx context.Context, input *dto.StackNameInput) (*dto.ComposeOpOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}
	if input.Async && h.jobs != nil {
		return h.runAsync("stop", input.Name, h.stacks.Stop), nil
	}
	opCtx, opCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer opCancel()
	result, err := h.stacks.Stop(opCtx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(ctx, err)
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
	if input.Async && h.jobs != nil {
		return h.runAsync("restart", input.Name, h.stacks.Restart), nil
	}
	opCtx, opCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer opCancel()
	result, err := h.stacks.Restart(opCtx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(ctx, err)
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
	if input.Async && h.jobs != nil {
		return h.runAsync("pull", input.Name, h.stacks.Pull), nil
	}
	opCtx, opCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer opCancel()
	result, err := h.stacks.Pull(opCtx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(ctx, err)
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

// Import scans an external directory and imports stacks into Composer.
func (h *StackHandler) Import(ctx context.Context, input *dto.ImportStacksInput) (*dto.ImportStacksOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	result, err := h.stacks.ImportFromDir(ctx, input.Body.SourceDir)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	out := &dto.ImportStacksOutput{}
	out.Body.Imported = result.Imported
	out.Body.Skipped = result.Skipped
	out.Body.Errors = result.Errors
	if out.Body.Imported == nil {
		out.Body.Imported = []string{}
	}
	if out.Body.Skipped == nil {
		out.Body.Skipped = []string{}
	}
	if out.Body.Errors == nil {
		out.Body.Errors = []string{}
	}
	return out, nil
}

// ConvertToGit converts a local stack to git-backed.
func (h *StackHandler) ConvertToGit(ctx context.Context, input *dto.ConvertToGitInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	branch := input.Body.Branch
	if branch == "" {
		branch = "main"
	}

	var creds *stack.GitCredentials
	if input.Body.Token != "" || input.Body.SSHKey != "" || input.Body.SSHKeyFile != "" || input.Body.Username != "" || input.Body.AgeKey != "" {
		creds = &stack.GitCredentials{
			Token:      input.Body.Token,
			SSHKey:     input.Body.SSHKey,
			SSHKeyFile: input.Body.SSHKeyFile,
			Username:   input.Body.Username,
			Password:   input.Body.Password,
			AgeKey:     input.Body.AgeKey,
		}
	}

	if err := h.stacks.ConvertToGit(ctx, input.Name, input.Body.RepoURL, branch, creds); err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

// ConvertToLocal detaches a git-backed stack from its repo.
func (h *StackHandler) ConvertToLocal(ctx context.Context, input *dto.ConvertToLocalInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	if err := h.stacks.ConvertToLocal(ctx, input.Name); err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

// ExecCompose runs an arbitrary docker compose subcommand against a stack.
// Requires operator+ role for read-only subcommands; admin for shell-equivalent
// ones (exec, cp). The command string is tokenized with quote-awareness so
// arguments containing spaces survive.
//
// Example: {"command": "logs --tail 50 web"} runs `docker compose logs --tail 50 web`
// Example: {"command": `exec web sh -c "env"`} runs `docker compose exec web sh -c "env"`
func (h *StackHandler) ExecCompose(ctx context.Context, input *dto.ExecComposeInput) (*dto.ExecComposeOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	args, err := docker.ShellSplit(input.Body.Command)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	if len(args) == 0 {
		return nil, huma.Error422UnprocessableEntity("command is empty")
	}

	// Role check: admin-only subcommands require privilege elevation even
	// though the endpoint itself is operator+.
	isAdmin := authmw.CheckRole(ctx, auth.RoleAdmin) == nil
	if docker.IsComposeAdminOnly(args[0]) && !isAdmin {
		return nil, huma.Error403Forbidden("compose " + args[0] + " requires admin role")
	}
	ok, permitted := docker.ComposeAllowed(args[0], isAdmin)
	if !ok {
		return nil, huma.Error422UnprocessableEntity(
			"command '" + args[0] + "' is not allowed; permitted: " + strings.Join(permitted, ", "),
		)
	}

	result, err := h.stacks.ExecCompose(ctx, input.Name, args)

	out := &dto.ExecComposeOutput{}
	if result != nil {
		out.Body.Stdout = result.Stdout
		out.Body.Stderr = result.Stderr
		out.Body.ExitCode = result.ExitCode
	}
	if err != nil {
		// Still return output even on non-zero exit (the output is the useful part)
		if result != nil {
			return out, nil
		}
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(ctx, err)
	}
	return out, nil
}

// scanDockerfiles finds Dockerfiles in a stack directory and returns their content.
// Looks for Dockerfile, Dockerfile.*, and *.dockerfile patterns, including one level
// of subdirectories (e.g., services/web/Dockerfile). Caps content at 64KB per file.
func scanDockerfiles(stackDir string) []dto.StackFile {
	const maxFileSize = 64 * 1024 // 64KB limit per file
	var files []dto.StackFile

	// Check root directory
	entries, err := os.ReadDir(stackDir)
	if err != nil {
		return nil
	}

	checkFile := func(dir, name string) {
		lower := strings.ToLower(name)
		isDockerfile := lower == "dockerfile" ||
			strings.HasPrefix(lower, "dockerfile.") ||
			strings.HasSuffix(lower, ".dockerfile")
		if !isDockerfile {
			return
		}
		fullPath := filepath.Join(dir, name)
		info, err := os.Stat(fullPath)
		if err != nil || info.Size() > maxFileSize {
			return
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return
		}
		rel, _ := filepath.Rel(stackDir, fullPath)
		files = append(files, dto.StackFile{Name: rel, Content: string(data)})
	}

	for _, e := range entries {
		if e.IsDir() {
			// Check one level of subdirectories
			subEntries, err := os.ReadDir(filepath.Join(stackDir, e.Name()))
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if !se.IsDir() {
					checkFile(filepath.Join(stackDir, e.Name()), se.Name())
				}
			}
		} else {
			checkFile(stackDir, e.Name())
		}
	}

	return files
}

// UpdateEnv saves the .env file content for a stack.
func (h *StackHandler) UpdateEnv(ctx context.Context, input *dto.UpdateEnvInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	st, err := h.stacks.Get(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(ctx, err)
	}

	envPath := st.Path + "/.env"
	if input.Body.Env == "" {
		// Empty env = delete the .env file
		os.Remove(envPath)
	} else {
		if err := os.WriteFile(envPath, []byte(input.Body.Env), 0600); err != nil {
			return nil, serverError(ctx, err)
		}
	}
	return nil, nil
}

func (h *StackHandler) GetCredentials(ctx context.Context, input *dto.GetStackInput) (*dto.StackCredentialsOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	creds, authMethod, resolved, err := h.stacks.ResolveCredentials(ctx, input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(ctx, err)
	}

	out := &dto.StackCredentialsOutput{}
	out.Body.AuthMethod = authMethod
	out.Body.Resolved.SSHSource = resolved.SSHSource
	out.Body.Resolved.TokenSource = resolved.TokenSource
	out.Body.Resolved.AgeSource = resolved.AgeSource

	if creds != nil {
		out.Body.PerStack.TokenSet = creds.Token != ""
		if creds.Token != "" && len(creds.Token) > 8 {
			out.Body.PerStack.TokenPreview = creds.Token[:8] + "..."
		}
		out.Body.PerStack.SSHKeySet = creds.SSHKey != ""
		out.Body.PerStack.SSHKeyFile = creds.SSHKeyFile
		out.Body.PerStack.AgeKeySet = creds.AgeKey != ""
		out.Body.PerStack.UsernameSet = creds.Username != ""
	}

	return out, nil
}

func (h *StackHandler) UpdateCredentials(ctx context.Context, input *dto.UpdateStackCredentialsInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	if err := h.stacks.UpdateCredentials(ctx, input.Name, &stack.GitCredentials{
		Token:      input.Body.Token,
		SSHKey:     input.Body.SSHKey,
		SSHKeyFile: input.Body.SSHKeyFile,
		AgeKey:     input.Body.AgeKey,
		Username:   input.Body.Username,
		Password:   input.Body.Password,
	}); err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(ctx, err)
	}
	return nil, nil
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
		return nil, serverError(ctx, err)
	}

	// Compare current compose.yaml on disk vs the normalized config Docker would use.
	// `docker compose config` outputs the fully-resolved, normalized YAML that Docker
	// actually sees. Differences mean the file has changes not yet applied.
	currentContent := st.ComposeContent
	if currentContent == "" {
		currentContent = "(no compose content)"
	}

	// Get the normalized config from Docker
	configResult, err := h.stacks.Config(ctx, input.Name)
	normalizedContent := ""
	if err == nil && configResult != nil && configResult.Stdout != "" {
		normalizedContent = configResult.Stdout
	}

	// If we can't get the normalized config, compare disk content against itself (no changes)
	if normalizedContent == "" {
		normalizedContent = currentContent
	}

	diff := app.ComputeDiff(normalizedContent, currentContent)

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
