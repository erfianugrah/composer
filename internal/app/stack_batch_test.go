package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/docker"
)

func newBatchService(t *testing.T, stacks ...*stack.Stack) (*StackService, *mockStackRepo) {
	t.Helper()
	repo := newMockStackRepo()
	for _, s := range stacks {
		repo.stacks[s.Name] = s
	}
	svc := NewStackService(repo, newMockGitConfigRepo(), nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil)
	return svc, repo
}

func batchStack(t *testing.T, name string, hostID *int64, deps ...string) *stack.Stack {
	t.Helper()
	s, err := stack.NewStackWithHost(name, "/opt/stacks/"+name, stack.SourceLocal, hostID)
	require.NoError(t, err)
	s.DependsOn = deps
	return s
}

// fakeDeployer records the order stacks were deployed in and how many ran at
// once, and fails the names it is told to.
type fakeDeployer struct {
	mu       sync.Mutex
	order    []string
	inFlight int
	maxSeen  int
	fail     map[string]string // name -> stderr
	hold     time.Duration
}

func (f *fakeDeployer) deploy(_ context.Context, name string) (*docker.ComposeResult, error) {
	f.mu.Lock()
	f.order = append(f.order, name)
	f.inFlight++
	if f.inFlight > f.maxSeen {
		f.maxSeen = f.inFlight
	}
	f.mu.Unlock()

	time.Sleep(f.hold)

	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()
	if stderr, ok := f.fail[name]; ok {
		return &docker.ComposeResult{Stderr: stderr}, errors.New("exit status 1")
	}
	return &docker.ComposeResult{Stdout: "ok"}, nil
}

func (f *fakeDeployer) indexOf(name string) int {
	for i, n := range f.order {
		if n == name {
			return i
		}
	}
	return -1
}

func resultsByName(res *BatchDeployResult) map[string]BatchStackResult {
	m := make(map[string]BatchStackResult, len(res.Results))
	for _, r := range res.Results {
		m[r.Name] = r
	}
	return m
}

func TestDeployBatch_OrdersWavesAndRunsWaveConcurrently(t *testing.T) {
	svc, _ := newBatchService(t,
		batchStack(t, "servarr", nil),
		batchStack(t, "sonarr", nil, "servarr"),
		batchStack(t, "radarr", nil, "servarr"),
		batchStack(t, "bazarr", nil, "sonarr", "radarr"),
	)
	fd := &fakeDeployer{hold: 30 * time.Millisecond}

	var progressed []string
	var pmu sync.Mutex
	res, err := svc.deployBatch(context.Background(), []string{"bazarr", "radarr", "sonarr", "servarr"}, fd.deploy, func(r BatchStackResult) {
		pmu.Lock()
		progressed = append(progressed, r.Name)
		pmu.Unlock()
	})
	require.NoError(t, err)

	assert.Equal(t, [][]string{{"servarr"}, {"radarr", "sonarr"}, {"bazarr"}}, res.Waves)
	assert.Less(t, fd.indexOf("servarr"), fd.indexOf("sonarr"))
	assert.Less(t, fd.indexOf("servarr"), fd.indexOf("radarr"))
	assert.Less(t, fd.indexOf("sonarr"), fd.indexOf("bazarr"))
	assert.Less(t, fd.indexOf("radarr"), fd.indexOf("bazarr"))
	assert.Equal(t, 2, fd.maxSeen, "radarr and sonarr share a wave and run concurrently; nothing else overlaps")

	by := resultsByName(res)
	for _, n := range []string{"servarr", "sonarr", "radarr", "bazarr"} {
		assert.Equal(t, BatchOK, by[n].Status, n)
	}
	assert.Equal(t, 0, by["servarr"].Wave)
	assert.Equal(t, 1, by["sonarr"].Wave)
	assert.Equal(t, 2, by["bazarr"].Wave)
	// Results are wave-then-name sorted regardless of completion order.
	assert.Equal(t, []string{"servarr", "radarr", "sonarr", "bazarr"}, namesOf(res))
	assert.Len(t, progressed, 4)
	ok, failed, skipped := res.Counts()
	assert.Equal(t, [3]int{4, 0, 0}, [3]int{ok, failed, skipped})
	assert.Equal(t, "4 ok, 0 failed, 0 skipped", res.Summary())
}

func TestDeployBatch_FailedDependencySkipsDependentsTransitively(t *testing.T) {
	svc, _ := newBatchService(t,
		batchStack(t, "servarr", nil),
		batchStack(t, "sonarr", nil, "servarr"),
		batchStack(t, "bazarr", nil, "sonarr"),
		batchStack(t, "unrelated", nil),
	)
	fd := &fakeDeployer{fail: map[string]string{"servarr": "network servarr_lan declared as external, but could not be found"}}

	res, err := svc.deployBatch(context.Background(), []string{"servarr", "sonarr", "bazarr", "unrelated"}, fd.deploy, nil)
	require.NoError(t, err)

	by := resultsByName(res)
	assert.Equal(t, BatchFailed, by["servarr"].Status)
	assert.Contains(t, by["servarr"].Error, "servarr_lan", "compose stderr is surfaced over the bare exit status")
	assert.Equal(t, BatchSkipped, by["sonarr"].Status)
	assert.Equal(t, `dependency "servarr" failed`, by["sonarr"].Error)
	assert.Equal(t, BatchSkipped, by["bazarr"].Status)
	assert.Equal(t, `dependency "sonarr" was skipped`, by["bazarr"].Error)
	assert.Equal(t, BatchOK, by["unrelated"].Status)

	// The skipped stacks were never started.
	assert.Equal(t, -1, fd.indexOf("sonarr"))
	assert.Equal(t, -1, fd.indexOf("bazarr"))
	ok, failed, skipped := res.Counts()
	assert.Equal(t, [3]int{1, 1, 2}, [3]int{ok, failed, skipped})
}

func TestDeployBatch_DependencyOutsideBatchIsIgnored(t *testing.T) {
	svc, _ := newBatchService(t,
		batchStack(t, "servarr", nil),
		batchStack(t, "sonarr", nil, "servarr"),
	)
	fd := &fakeDeployer{}

	// servarr is not requested, so sonarr has no in-batch dependency and
	// deploys in wave 0 even though servarr was never touched.
	res, err := svc.deployBatch(context.Background(), []string{"sonarr"}, fd.deploy, nil)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"sonarr"}}, res.Waves)
	assert.Equal(t, BatchOK, resultsByName(res)["sonarr"].Status)
	assert.Equal(t, []string{"sonarr"}, fd.order)
}

func TestDeployBatch_UnknownAndDuplicateNames(t *testing.T) {
	svc, _ := newBatchService(t, batchStack(t, "a", nil))
	fd := &fakeDeployer{}

	res, err := svc.deployBatch(context.Background(), []string{"a", " a ", "ghost", ""}, fd.deploy, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, fd.order, "duplicates deploy once, blanks are dropped")
	by := resultsByName(res)
	assert.Equal(t, BatchOK, by["a"].Status)
	assert.Equal(t, BatchFailed, by["ghost"].Status)
	assert.Equal(t, "stack not found", by["ghost"].Error)
	assert.Equal(t, -1, by["ghost"].Wave)
	assert.Equal(t, []string{"a", "ghost"}, namesOf(res), "not-found entries sort after scheduled ones")
}

func TestDeployBatch_StoredCycleAbortsBeforeDeploying(t *testing.T) {
	svc, _ := newBatchService(t,
		batchStack(t, "a", nil, "b"),
		batchStack(t, "b", nil, "a"),
	)
	fd := &fakeDeployer{}

	_, err := svc.deployBatch(context.Background(), []string{"a", "b"}, fd.deploy, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidDependency))
	assert.Empty(t, fd.order, "nothing deploys when the schedule cannot be computed")
}

// A wide wave whose members all have an in-batch dependency, with deploys
// that return instantly: the skip check runs on the loop goroutine while the
// same wave's goroutines are already recording outcomes. Guard the outcome
// map or this is a concurrent map read/write - a fatal runtime error that
// takes composerd down, not a recoverable panic. Run under -race.
func TestDeployBatch_WideWaveDoesNotRaceOnOutcomes(t *testing.T) {
	all := []*stack.Stack{batchStack(t, "base", nil)}
	names := []string{"base"}
	for i := 0; i < 200; i++ {
		n := fmt.Sprintf("dep%03d", i)
		all = append(all, batchStack(t, n, nil, "base"))
		names = append(names, n)
	}
	svc, _ := newBatchService(t, all...)
	fd := &fakeDeployer{} // hold == 0: each deploy returns immediately

	res, err := svc.deployBatch(context.Background(), names, fd.deploy, nil)
	require.NoError(t, err)
	ok, failed, skipped := res.Counts()
	assert.Equal(t, [3]int{201, 0, 0}, [3]int{ok, failed, skipped})
}

func TestUpdateDependsOn(t *testing.T) {
	remoteHost := int64(4)
	svc, repo := newBatchService(t,
		batchStack(t, "servarr", nil),
		batchStack(t, "sonarr", nil),
		batchStack(t, "remote", &remoteHost),
	)
	ctx := context.Background()

	st, err := svc.UpdateDependsOn(ctx, "sonarr", []string{"servarr"})
	require.NoError(t, err)
	assert.Equal(t, []string{"servarr"}, st.DependsOn)
	assert.Equal(t, []string{"servarr"}, repo.stacks["sonarr"].DependsOn, "persisted through the repository")

	// Cycle: servarr -> sonarr would close the loop.
	_, err = svc.UpdateDependsOn(ctx, "servarr", []string{"sonarr"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidDependency))
	assert.Contains(t, err.Error(), "cycle")
	assert.Empty(t, repo.stacks["servarr"].DependsOn)

	// Cross-host, self and unknown are all 422-class errors.
	for _, deps := range [][]string{{"remote"}, {"sonarr"}, {"nope"}} {
		_, err = svc.UpdateDependsOn(ctx, "sonarr", deps)
		assert.True(t, errors.Is(err, ErrInvalidDependency), "deps=%v err=%v", deps, err)
	}
	assert.Equal(t, []string{"servarr"}, repo.stacks["sonarr"].DependsOn, "rejected updates leave the stored list alone")

	// Unknown stack.
	_, err = svc.UpdateDependsOn(ctx, "ghost", nil)
	assert.True(t, errors.Is(err, ErrNotFound))

	// Clear.
	st, err = svc.UpdateDependsOn(ctx, "sonarr", nil)
	require.NoError(t, err)
	assert.Empty(t, st.DependsOn)
}

func namesOf(res *BatchDeployResult) []string {
	out := make([]string, 0, len(res.Results))
	for _, r := range res.Results {
		out = append(out, r.Name)
	}
	return out
}

// The cap exists so a wide wave cannot fork one `docker compose` process per
// stack. Asserted both ways: bounded, and NOT serialised - a cap that
// accidentally reduced the wave to one at a time would also pass a
// bound-only check while destroying the point of waves.
func TestDeployBatch_CapsWithinWaveConcurrency(t *testing.T) {
	all := []*stack.Stack{batchStack(t, "base", nil)}
	names := []string{"base"}
	for i := 0; i < 40; i++ {
		n := fmt.Sprintf("dep%02d", i)
		all = append(all, batchStack(t, n, nil, "base"))
		names = append(names, n)
	}
	svc, _ := newBatchService(t, all...)
	// hold long enough that an uncapped wave would show ~40 in flight.
	fd := &fakeDeployer{hold: 20 * time.Millisecond}

	res, err := svc.deployBatch(context.Background(), names, fd.deploy, nil)
	require.NoError(t, err)
	ok, failed, skipped := res.Counts()
	assert.Equal(t, [3]int{41, 0, 0}, [3]int{ok, failed, skipped})

	fd.mu.Lock()
	peak := fd.maxSeen
	fd.mu.Unlock()
	assert.LessOrEqual(t, peak, batchWaveConcurrency,
		"within-wave parallelism must be capped at batchWaveConcurrency")
	assert.Greater(t, peak, 1,
		"the cap must still allow a wave to run in parallel, not serialise it")
}
