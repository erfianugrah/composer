package dag_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/domain/dag"
)

func depsFrom(m map[string][]string) func(string) []string {
	return func(id string) []string { return m[id] }
}

func TestWaves_Empty(t *testing.T) {
	waves, err := dag.Waves(nil, depsFrom(nil))
	require.NoError(t, err)
	assert.Empty(t, waves)
}

func TestWaves_Linear(t *testing.T) {
	waves, err := dag.Waves([]string{"c", "b", "a"}, depsFrom(map[string][]string{
		"b": {"a"}, "c": {"b"},
	}))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"a"}, {"b"}, {"c"}}, waves)
}

func TestWaves_Diamond_SortedWithinWave(t *testing.T) {
	waves, err := dag.Waves([]string{"d", "c", "b", "a"}, depsFrom(map[string][]string{
		"b": {"a"}, "c": {"a"}, "d": {"b", "c"},
	}))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"a"}, {"b", "c"}, {"d"}}, waves)
}

func TestWaves_IgnoresDependenciesOutsideSet(t *testing.T) {
	// "web" depends on "db" but "db" was not requested: the edge is dropped,
	// not auto-added. This is the batch-deploy contract.
	waves, err := dag.Waves([]string{"web", "cache"}, depsFrom(map[string][]string{
		"web": {"db"}, "cache": {"db"},
	}))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"cache", "web"}}, waves)
}

func TestWaves_DuplicateIDsAndDuplicateEdges(t *testing.T) {
	waves, err := dag.Waves([]string{"a", "b", "a", "b"}, depsFrom(map[string][]string{
		"b": {"a", "a"},
	}))
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"a"}, {"b"}}, waves)
}

func TestWaves_SelfDependencyIsCycle(t *testing.T) {
	_, err := dag.Waves([]string{"a"}, depsFrom(map[string][]string{"a": {"a"}}))
	var cycle *dag.CycleError
	require.True(t, errors.As(err, &cycle))
	assert.Equal(t, []string{"a"}, cycle.Nodes)
}

func TestWaves_CycleReturnsPartialWavesAndStuckNodes(t *testing.T) {
	// x is independent and lands in wave 0; a<->b cycle; c hangs off the
	// cycle and is therefore also unplaceable.
	waves, err := dag.Waves([]string{"a", "b", "c", "x"}, depsFrom(map[string][]string{
		"a": {"b"}, "b": {"a"}, "c": {"a"},
	}))
	var cycle *dag.CycleError
	require.True(t, errors.As(err, &cycle))
	assert.Equal(t, []string{"a", "b", "c"}, cycle.Nodes)
	assert.Equal(t, [][]string{{"x"}}, waves)
	assert.Contains(t, err.Error(), "a, b, c")
}
