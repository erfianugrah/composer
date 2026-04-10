package handler

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
		OperationID: "buildAndDeployStack",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/build",
		Summary:     "Build images from Dockerfiles and deploy (docker compose up --build)",
		Tags:        []string{"stacks"},
	}, h.BuildAndDeploy)

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

	huma.Register(api, huma.Operation{
		OperationID: "execComposeCommand",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/exec",
		Summary:     "Run a docker compose command against this stack",
		Tags:        []string{"stacks"},
	}, h.ExecCompose)

	huma.Register(api, huma.Operation{
		OperationID: "updateStackEnv",
		Method:      http.MethodPut,
		Path:        "/api/v1/stacks/{name}/env",
		Summary:     "Update .env file for a stack",
		Tags:        []string{"stacks"},
	}, h.UpdateEnv)

	huma.Register(api, huma.Operation{
		OperationID: "getStackCredentials",
		Method:      http.MethodGet,
		Path:        "/api/v1/stacks/{name}/credentials",
		Summary:     "Get resolved credential chain for a stack",
		Tags:        []string{"stacks"},
	}, h.GetCredentials)

	huma.Register(api, huma.Operation{
		OperationID: "updateStackCredentials",
		Method:      http.MethodPut,
		Path:        "/api/v1/stacks/{name}/credentials",
		Summary:     "Update per-stack credential overrides",
		Tags:        []string{"stacks"},
	}, h.UpdateCredentials)

	huma.Register(api, huma.Operation{
		OperationID: "importStacks",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/import",
		Summary:     "Import stacks from an external directory (e.g. Dockge migration)",
		Tags:        []string{"stacks"},
	}, h.Import)

	huma.Register(api, huma.Operation{
		OperationID: "convertToGit",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/convert/git",
		Summary:     "Convert a local stack to git-backed",
		Tags:        []string{"stacks", "git"},
	}, h.ConvertToGit)

	huma.Register(api, huma.Operation{
		OperationID: "convertToLocal",
		Method:      http.MethodPost,
		Path:        "/api/v1/stacks/{name}/convert/local",
		Summary:     "Detach a git-backed stack and convert to local",
		Tags:        []string{"stacks", "git"},
	}, h.ConvertToLocal)
}

func (h *StackHandler) List(ctx context.Context, input *struct{}) (*dto.StackListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	stacks, err := h.stacks.List(ctx)
	if err != nil {
		return nil, serverError(err)
	}

	out := &dto.StackListOutput{}
	out.Body.Stacks = make([]dto.StackSummary, 0, len(stacks))
	for _, s := range stacks {
		// Get container counts for this stack
		var containerCount, runningCount int
		if h.stacks != nil {
			containers, err := h.stacks.Containers(ctx, s.Name)
			if err == nil {
				containerCount = len(containers)
				for _, c := range containers {
					if c.IsRunning() {
						runningCount++
					}
				}
			}
		}
		out.Body.Stacks = append(out.Body.Stacks, dto.StackSummary{
			Name:           s.Name,
			Source:         string(s.Source),
			Status:         string(s.Status),
			ContainerCount: containerCount,
			RunningCount:   runningCount,
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
		return nil, serverError(err)
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
		return nil, serverError(err)
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
	result, err := h.stacks.Deploy(context.Background(), input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(err)
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
	result, err := h.stacks.BuildAndDeploy(context.Background(), input.Name)
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
	result, err := h.stacks.Stop(context.Background(), input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(err)
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
	result, err := h.stacks.Restart(context.Background(), input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(err)
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
	result, err := h.stacks.Pull(context.Background(), input.Name)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			return nil, huma.Error404NotFound("stack not found")
		}
		return nil, serverError(err)
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
// Requires operator+ role. The command string is split by whitespace into args
// and passed to `docker compose <args>` in the stack's directory.
//
// Example: {"command": "logs --tail 50 web"} runs `docker compose logs --tail 50 web`
// Example: {"command": "ps"} runs `docker compose ps`
// Example: {"command": "exec web env"} runs `docker compose exec web env`
func (h *StackHandler) ExecCompose(ctx context.Context, input *dto.ExecComposeInput) (*dto.ExecComposeOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleOperator); err != nil {
		return nil, err
	}

	args := strings.Fields(input.Body.Command)
	if len(args) == 0 {
		return nil, huma.Error422UnprocessableEntity("command is empty")
	}

	// Allowlist of safe compose subcommands (S13)
	allowed := map[string]bool{
		"ps": true, "logs": true, "top": true, "config": true,
		"images": true, "port": true, "version": true, "ls": true,
		"events": true, "build": true,
	}
	// exec and cp require admin (shell-equivalent access)
	adminOnly := map[string]bool{"exec": true, "cp": true}
	if adminOnly[args[0]] {
		if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
			return nil, huma.Error403Forbidden("compose " + args[0] + " requires admin role")
		}
		allowed[args[0]] = true
	}
	if !allowed[args[0]] {
		return nil, huma.Error422UnprocessableEntity("command '" + args[0] + "' is not allowed; permitted: ps, logs, top, config, images, port, version, ls, events, build (+ exec, cp for admin)")
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
		return nil, serverError(err)
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
		return nil, serverError(err)
	}

	envPath := st.Path + "/.env"
	if input.Body.Env == "" {
		// Empty env = delete the .env file
		os.Remove(envPath)
	} else {
		if err := os.WriteFile(envPath, []byte(input.Body.Env), 0600); err != nil {
			return nil, serverError(err)
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
		return nil, serverError(err)
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
		return nil, serverError(err)
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
		return nil, serverError(err)
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
