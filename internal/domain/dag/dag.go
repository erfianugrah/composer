// Package dag is the dependency-graph orderer shared by pipeline steps and
// stack batch deploys: Kahn's algorithm grouped into waves, plus cycle
// detection. Standard library only, so it stays a domain package.
package dag

import (
	"fmt"
	"sort"
	"strings"
)

// CycleError reports the nodes that could not be ordered because they sit on,
// or downstream of, a dependency cycle. Nodes is sorted.
type CycleError struct {
	Nodes []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("dependency cycle involving: %s", strings.Join(e.Nodes, ", "))
}

// Waves orders ids into execution waves: every node in wave i has all of its
// in-set dependencies in an earlier wave, so the members of one wave can run
// concurrently. deps returns the dependency ids of a node. Dependencies that
// are not themselves in ids are ignored (the caller decides whether that is
// an error), as are duplicate ids. Nodes within a wave are sorted so the
// result is deterministic.
//
// On a cycle the waves computed before it are returned together with a
// *CycleError naming the nodes that could not be placed.
func Waves(ids []string, deps func(id string) []string) ([][]string, error) {
	inSet := make(map[string]bool, len(ids))
	order := make([]string, 0, len(ids))
	for _, id := range ids {
		if inSet[id] {
			continue
		}
		inSet[id] = true
		order = append(order, id)
	}

	inDegree := make(map[string]int, len(order))
	dependents := make(map[string][]string, len(order))
	for _, id := range order {
		inDegree[id] = 0
		for _, d := range deps(id) {
			if !inSet[d] {
				continue
			}
			inDegree[id]++
			dependents[d] = append(dependents[d], id)
		}
	}

	var ready []string
	for _, id := range order {
		if inDegree[id] == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	var waves [][]string
	placed := 0
	for len(ready) > 0 {
		waves = append(waves, ready)
		placed += len(ready)
		var next []string
		for _, n := range ready {
			for _, m := range dependents[n] {
				inDegree[m]--
				if inDegree[m] == 0 {
					next = append(next, m)
				}
			}
		}
		sort.Strings(next)
		ready = next
	}

	if placed < len(order) {
		var stuck []string
		for _, id := range order {
			if inDegree[id] > 0 {
				stuck = append(stuck, id)
			}
		}
		sort.Strings(stuck)
		return waves, &CycleError{Nodes: stuck}
	}
	return waves, nil
}
