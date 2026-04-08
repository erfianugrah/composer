package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	domcontainer "github.com/erfianugrah/composer/internal/domain/container"
	"github.com/erfianugrah/composer/internal/domain/event"
	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

// StackService orchestrates stack management operations.
type StackService struct {
	stacks    stack.StackRepository
	gitCfgs   stack.GitConfigRepository
	docker    *docker.Client
	compose   *docker.Compose
	bus       event.Bus
	stacksDir string
}

// NewStackService creates a new StackService.
func NewStackService(
	stacks stack.StackRepository,
	gitCfgs stack.GitConfigRepository,
	dockerClient *docker.Client,
	compose *docker.Compose,
	bus event.Bus,
	stacksDir string,
) *StackService {
	return &StackService{
		stacks:    stacks,
		gitCfgs:   gitCfgs,
		docker:    dockerClient,
		compose:   compose,
		bus:       bus,
		stacksDir: stacksDir,
	}
}

// Create creates a new local stack with the given compose content.
func (s *StackService) Create(ctx context.Context, name, composeContent string) (*stack.Stack, error) {
	stackPath := filepath.Join(s.stacksDir, name)

	st, err := stack.NewStack(name, stackPath, stack.SourceLocal)
	if err != nil {
		return nil, err
	}
	st.ComposeContent = composeContent

	if err := os.MkdirAll(stackPath, 0755); err != nil {
		return nil, fmt.Errorf("creating stack directory: %w", err)
	}
	composePath := filepath.Join(stackPath, "compose.yaml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		return nil, fmt.Errorf("writing compose file: %w", err)
	}

	if err := s.stacks.Create(ctx, st); err != nil {
		os.RemoveAll(stackPath)
		return nil, fmt.Errorf("persisting stack: %w", err)
	}

	s.publishEvent(event.StackCreated{Name: name, Timestamp: time.Now()})

	return st, nil
}

// Get retrieves a stack by name with containers and compose content.
func (s *StackService) Get(ctx context.Context, name string) (*stack.Stack, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	composePath := filepath.Join(st.Path, "compose.yaml")
	content, err := os.ReadFile(composePath)
	if err == nil {
		st.ComposeContent = string(content)
	}

	if st.Source == stack.SourceGit {
		cfg, err := s.gitCfgs.GetByStackName(ctx, name)
		if err == nil && cfg != nil {
			st.GitConfig = cfg
		}
	}

	containers, err := s.docker.ListContainers(ctx, name)
	if err == nil {
		st.Status = deriveStackStatus(containers)
	}

	return st, nil
}

// List returns all stacks with runtime status.
func (s *StackService) List(ctx context.Context) ([]*stack.Stack, error) {
	stacks, err := s.stacks.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, st := range stacks {
		containers, err := s.docker.ListContainers(ctx, st.Name)
		if err == nil {
			st.Status = deriveStackStatus(containers)
		}
	}

	return stacks, nil
}

// Update updates compose content. Writes to disk + DB.
func (s *StackService) Update(ctx context.Context, name, composeContent string) (*stack.Stack, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	st.UpdateCompose(composeContent)

	composePath := filepath.Join(st.Path, "compose.yaml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		return nil, fmt.Errorf("writing compose file: %w", err)
	}

	if err := s.stacks.Update(ctx, st); err != nil {
		return nil, err
	}

	s.publishEvent(event.StackUpdated{Name: name, Timestamp: time.Now()})

	return st, nil
}

// Delete removes a stack.
func (s *StackService) Delete(ctx context.Context, name string, removeVolumes bool) error {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return err
	}
	if st == nil {
		return ErrNotFound
	}

	// Stop containers first (best effort)
	s.compose.Down(ctx, st.Path, removeVolumes)

	if err := s.stacks.Delete(ctx, name); err != nil {
		return err
	}

	os.RemoveAll(st.Path)

	s.publishEvent(event.StackDeleted{Name: name, Timestamp: time.Now()})

	return nil
}

// Deploy runs docker compose up.
func (s *StackService) Deploy(ctx context.Context, name string) (*docker.ComposeResult, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	result, err := s.compose.Up(ctx, st.Path)
	if err != nil {
		s.publishEvent(event.StackError{Name: name, Error: err.Error(), Timestamp: time.Now()})
		return result, err
	}

	s.publishEvent(event.StackDeployed{Name: name, Timestamp: time.Now()})
	return result, nil
}

// Stop runs docker compose down.
func (s *StackService) Stop(ctx context.Context, name string) (*docker.ComposeResult, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	result, err := s.compose.Down(ctx, st.Path, false)
	if err != nil {
		return result, err
	}

	s.publishEvent(event.StackStopped{Name: name, Timestamp: time.Now()})
	return result, nil
}

// Restart runs docker compose restart.
func (s *StackService) Restart(ctx context.Context, name string) (*docker.ComposeResult, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	result, err := s.compose.Restart(ctx, st.Path)
	if err == nil {
		s.publishEvent(event.StackDeployed{Name: name, Timestamp: time.Now()})
	}
	return result, err
}

// Pull runs docker compose pull.
func (s *StackService) Pull(ctx context.Context, name string) (*docker.ComposeResult, error) {
	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}

	result, err := s.compose.Pull(ctx, st.Path)
	if err == nil {
		s.publishEvent(event.StackUpdated{Name: name, Timestamp: time.Now()})
	}
	return result, err
}

// publishEvent sends an event to the bus if one is configured.
func (s *StackService) publishEvent(evt event.Event) {
	if s.bus != nil {
		s.bus.Publish(evt)
	}
}

// Containers returns the containers for a stack.
func (s *StackService) Containers(ctx context.Context, stackName string) ([]domcontainer.Container, error) {
	return s.docker.ListContainers(ctx, stackName)
}

func deriveStackStatus(containers []domcontainer.Container) stack.Status {
	if len(containers) == 0 {
		return stack.StatusStopped
	}

	running := 0
	for _, c := range containers {
		if c.IsRunning() {
			running++
		}
	}

	switch {
	case running == len(containers):
		return stack.StatusRunning
	case running == 0:
		return stack.StatusStopped
	default:
		return stack.StatusPartial
	}
}
