package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

// ErrInvalidDependency wraps a depends_on validation failure (unknown stack,
// different host, self reference, cycle) so the API can answer 422 instead of
// 500. The wrapped message carries the specific reason.
var ErrInvalidDependency = errors.New("invalid stack dependency")

// batchStackTimeout bounds one stack's deploy inside a batch. Matches the
// 10-minute ceiling the synchronous single-stack handlers use.
const batchStackTimeout = 10 * time.Minute

// batchWaveConcurrency caps how many stacks in ONE wave deploy at a time.
//
// Every deploy forks a real `docker compose` process (infra/docker/compose.go)
// which then pulls images, so an uncapped wave is a fork storm proportional to
// however many stacks share a dependency depth. The path this endpoint
// replaces was the browser's bulk action, implicitly limited to ~6 connections
// per host; the server has no such limit and `maxItems:200` on the request is
// not a resource bound. A full bring-up of the servarr host is ~18 stacks in
// one wave, on a box that has already had resource exhaustion take down
// cAdvisor and systemd units, so this is a real ceiling rather than a
// theoretical one. Waves still execute in order; only within-wave parallelism
// is bounded.
const batchWaveConcurrency = 6

// BatchStackStatus is the per-stack outcome of a batch deploy.
type BatchStackStatus string

const (
	BatchOK      BatchStackStatus = "ok"
	BatchFailed  BatchStackStatus = "failed"
	BatchSkipped BatchStackStatus = "skipped"
)

// BatchStackResult is one stack's outcome in a batch deploy.
type BatchStackResult struct {
	Name   string
	Status BatchStackStatus
	// Error is the deploy failure message, or for a skipped stack the reason
	// (which in-batch dependency failed or was itself skipped).
	Error string
	// Wave is the zero-based wave the stack was scheduled in. Stacks that
	// were not found are reported with Wave == -1.
	Wave int
}

// BatchDeployResult is the outcome of DeployBatch. Results are sorted by
// wave then name so the order is stable across runs; Waves is the schedule
// that was computed (stacks in the same wave ran concurrently).
type BatchDeployResult struct {
	Results []BatchStackResult
	Waves   [][]string
}

// Counts tallies the results by status.
func (r *BatchDeployResult) Counts() (ok, failed, skipped int) {
	for _, res := range r.Results {
		switch res.Status {
		case BatchOK:
			ok++
		case BatchFailed:
			failed++
		case BatchSkipped:
			skipped++
		}
	}
	return ok, failed, skipped
}

// Summary renders "N ok, N failed, N skipped" for logs, job output and toasts.
func (r *BatchDeployResult) Summary() string {
	ok, failed, skipped := r.Counts()
	return fmt.Sprintf("%d ok, %d failed, %d skipped", ok, failed, skipped)
}

// BatchProgress is invoked once per stack as its outcome is known, from the
// goroutine that produced it. Used by the async job path to append live
// progress lines; may be nil.
type BatchProgress func(BatchStackResult)

// deployFunc is the per-stack operation a batch runs. StackService.Deploy in
// production; tests inject a fake to assert ordering without docker.
type deployFunc func(ctx context.Context, name string) (*docker.ComposeResult, error)

// UpdateDependsOn replaces a stack's deploy-ordering dependencies after
// domain validation (existence, same host, no self reference, no cycle).
// Validation failures are returned wrapped in ErrInvalidDependency.
func (s *StackService) UpdateDependsOn(ctx context.Context, name string, deps []string) (*stack.Stack, error) {
	s.locks.Lock(name)
	defer s.locks.Unlock(name)

	st, err := s.stacks.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrNotFound
	}
	all, err := s.stacks.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing stacks: %w", err)
	}
	if err := st.SetDependsOn(deps, all); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDependency, err)
	}
	if err := s.stacks.Update(ctx, st); err != nil {
		return nil, fmt.Errorf("persisting stack: %w", err)
	}
	return st, nil
}

// DeployBatch deploys the named stacks in dependency order.
//
// The requested stacks are grouped into waves by their DependsOn edges (only
// edges whose target is also in the request count; anything else is ignored,
// not auto-added). Each wave runs concurrently; the per-stack lock inside
// Deploy still serialises against any other compose operation on the same
// stack. A stack whose in-batch dependency failed or was skipped is never
// started - it is reported as skipped with the reason. Unknown names are
// reported as failed with "stack not found". A cycle in the stored edges
// aborts the whole batch with ErrInvalidDependency before anything deploys.
func (s *StackService) DeployBatch(ctx context.Context, names []string, progress BatchProgress) (*BatchDeployResult, error) {
	return s.deployBatch(ctx, names, s.Deploy, progress)
}

func (s *StackService) deployBatch(ctx context.Context, names []string, deploy deployFunc, progress BatchProgress) (*BatchDeployResult, error) {
	res := &BatchDeployResult{}
	var mu sync.Mutex
	outcome := make(map[string]BatchStackStatus)
	record := func(r BatchStackResult) {
		mu.Lock()
		res.Results = append(res.Results, r)
		outcome[r.Name] = r.Status
		mu.Unlock()
		if progress != nil {
			progress(r)
		}
	}

	// Resolve the request: de-duplicate, then look each name up.
	seen := make(map[string]bool, len(names))
	byName := make(map[string]*stack.Stack, len(names))
	var found []*stack.Stack
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		st, err := s.stacks.GetByName(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("looking up stack %q: %w", name, err)
		}
		if st == nil {
			record(BatchStackResult{Name: name, Status: BatchFailed, Error: "stack not found", Wave: -1})
			continue
		}
		byName[name] = st
		found = append(found, st)
	}

	waves, err := stack.DeployOrder(found)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidDependency, err)
	}
	res.Waves = waves
	s.log.Info("batch deploy scheduled", zap.Any("waves", waves), zap.Int("stacks", len(found)))

	// blocked reads `outcome` under the same lock record() writes it with. The
	// read happens while the CURRENT wave's goroutines are already running
	// (they are spawned inside the same loop), so an unguarded read is a
	// concurrent map read/write: a fatal, unrecoverable runtime error.
	blocked := func(st *stack.Stack) string {
		mu.Lock()
		defer mu.Unlock()
		return blockedBy(st, byName, outcome)
	}

	// Bounded within-wave parallelism. Buffered channel as a semaphore: a
	// goroutine takes a slot before deploying and releases it after, so at
	// most batchWaveConcurrency `docker compose` processes run at once.
	sem := make(chan struct{}, batchWaveConcurrency)

	for wi, wave := range waves {
		var wg sync.WaitGroup
		for _, name := range wave {
			st := byName[name]
			if reason := blocked(st); reason != "" {
				record(BatchStackResult{Name: name, Status: BatchSkipped, Error: reason, Wave: wi})
				continue
			}
			wg.Add(1)
			go func(name string, wave int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				stackCtx, cancel := context.WithTimeout(ctx, batchStackTimeout)
				defer cancel()
				result, err := deploy(stackCtx, name)
				if err != nil {
					msg := err.Error()
					if result != nil && strings.TrimSpace(result.Stderr) != "" {
						msg = strings.TrimSpace(result.Stderr)
					}
					record(BatchStackResult{Name: name, Status: BatchFailed, Error: msg, Wave: wave})
					return
				}
				record(BatchStackResult{Name: name, Status: BatchOK, Wave: wave})
			}(name, wi)
		}
		wg.Wait()
	}

	// Stable order: by wave, then name. Not-found entries (Wave -1) sort last.
	sort.SliceStable(res.Results, func(i, j int) bool {
		wi, wj := res.Results[i].Wave, res.Results[j].Wave
		if wi < 0 {
			wi = len(waves)
		}
		if wj < 0 {
			wj = len(waves)
		}
		if wi != wj {
			return wi < wj
		}
		return res.Results[i].Name < res.Results[j].Name
	})
	s.log.Info("batch deploy finished", zap.String("summary", res.Summary()))
	return res, nil
}

// blockedBy returns the reason st must be skipped: the first in-batch
// dependency that did not deploy successfully. Only dependencies that are
// part of this batch are consulted (byName holds exactly those). outcome is
// written by wave goroutines, so callers MUST hold the batch mutex while
// calling this (see the `blocked` closure in deployBatch).
func blockedBy(st *stack.Stack, byName map[string]*stack.Stack, outcome map[string]BatchStackStatus) string {
	var reasons []string
	for _, dep := range st.DependsOn {
		if _, inBatch := byName[dep]; !inBatch {
			continue
		}
		switch outcome[dep] {
		case BatchOK:
		case BatchSkipped:
			reasons = append(reasons, fmt.Sprintf("dependency %q was skipped", dep))
		default:
			reasons = append(reasons, fmt.Sprintf("dependency %q failed", dep))
		}
	}
	return strings.Join(reasons, "; ")
}
