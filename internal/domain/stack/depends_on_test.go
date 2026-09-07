package stack_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/domain/dag"
	"github.com/erfianugrah/composer/internal/domain/stack"
)

func mk(t *testing.T, name string, hostID *int64, deps ...string) *stack.Stack {
	t.Helper()
	s, err := stack.NewStackWithHost(name, "/opt/stacks/"+name, stack.SourceLocal, hostID)
	require.NoError(t, err)
	s.DependsOn = deps
	return s
}

func i64(v int64) *int64 { return &v }

func TestSetDependsOn_Valid(t *testing.T) {
	servarr := mk(t, "servarr", nil)
	sonarr := mk(t, "sonarr", nil)
	radarr := mk(t, "radarr", nil)
	all := []*stack.Stack{servarr, sonarr, radarr}

	before := sonarr.UpdatedAt
	require.NoError(t, sonarr.SetDependsOn([]string{" servarr ", "servarr", "radarr"}, all))
	assert.Equal(t, []string{"servarr", "radarr"}, sonarr.DependsOn, "trimmed and de-duplicated, order kept")
	assert.False(t, sonarr.UpdatedAt.Before(before))

	// Clearing is always allowed.
	require.NoError(t, sonarr.SetDependsOn(nil, all))
	assert.Empty(t, sonarr.DependsOn)
}

func TestSetDependsOn_Rejections(t *testing.T) {
	local := mk(t, "servarr", nil)
	remote := mk(t, "remote-db", i64(7))
	self := mk(t, "sonarr", nil)
	all := []*stack.Stack{local, remote, self}

	tests := []struct {
		name string
		deps []string
		want string
	}{
		{"self reference", []string{"sonarr"}, "cannot depend on itself"},
		{"unknown stack", []string{"ghost"}, "not a known stack"},
		{"different host", []string{"remote-db"}, "different docker host"},
		{"empty name", []string{""}, "is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := self.SetDependsOn(tt.deps, all)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Empty(t, self.DependsOn, "a rejected update must not touch the stack")
		})
	}
}

func TestSetDependsOn_SameRemoteHostAllowed(t *testing.T) {
	a := mk(t, "a", i64(3))
	b := mk(t, "b", i64(3))
	require.NoError(t, b.SetDependsOn([]string{"a"}, []*stack.Stack{a, b}))
	assert.Equal(t, []string{"a"}, b.DependsOn)
}

func TestSetDependsOn_RejectsCycle(t *testing.T) {
	a := mk(t, "a", nil)
	b := mk(t, "b", nil, "a")
	c := mk(t, "c", nil, "b")
	all := []*stack.Stack{a, b, c}

	// a -> c would close a <- b <- c <- a.
	err := a.SetDependsOn([]string{"c"}, all)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
	var cycle *dag.CycleError
	assert.True(t, errors.As(err, &cycle), "the dag error is wrapped, not flattened")
	assert.Empty(t, a.DependsOn)

	// Two-node cycle.
	err = a.SetDependsOn([]string{"b"}, all)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")

	// A new edge that does not close a loop is fine.
	d := mk(t, "d", nil)
	require.NoError(t, a.SetDependsOn([]string{"d"}, append(all, d)))
}

func TestSameHost(t *testing.T) {
	assert.True(t, stack.SameHost(mk(t, "a", nil), mk(t, "b", nil)))
	assert.True(t, stack.SameHost(mk(t, "a", i64(1)), mk(t, "b", i64(1))))
	assert.False(t, stack.SameHost(mk(t, "a", nil), mk(t, "b", i64(1))))
	assert.False(t, stack.SameHost(mk(t, "a", i64(2)), mk(t, "b", i64(1))))
}

func TestDeployOrder_WavesAndExternalDepsIgnored(t *testing.T) {
	servarr := mk(t, "servarr", nil)
	sonarr := mk(t, "sonarr", nil, "servarr")
	radarr := mk(t, "radarr", nil, "servarr")
	bazarr := mk(t, "bazarr", nil, "sonarr", "radarr")
	// caddy depends on a stack that is NOT part of this batch.
	caddy := mk(t, "caddy", nil, "waf")

	waves, err := stack.DeployOrder([]*stack.Stack{bazarr, caddy, radarr, sonarr, servarr})
	require.NoError(t, err)
	assert.Equal(t, [][]string{
		{"caddy", "servarr"},
		{"radarr", "sonarr"},
		{"bazarr"},
	}, waves)
}

func TestDeployOrder_CycleFromStoredEdges(t *testing.T) {
	// Validation stops this being written, but a stale row must still fail
	// loudly rather than deploy in an arbitrary order.
	a := mk(t, "a", nil, "b")
	b := mk(t, "b", nil, "a")
	_, err := stack.DeployOrder([]*stack.Stack{a, b})
	var cycle *dag.CycleError
	require.True(t, errors.As(err, &cycle))
	assert.Equal(t, []string{"a", "b"}, cycle.Nodes)
}
