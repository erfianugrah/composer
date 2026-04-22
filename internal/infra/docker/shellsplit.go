package docker

import (
	"errors"
	"sort"
	"strings"
)

// ShellSplit tokenizes a command string with minimal quote awareness.
// Handles single quotes, double quotes, and backslash escapes outside quotes.
// Designed for the docker/compose exec endpoints so arguments with embedded
// spaces survive the round-trip.
//
// Examples:
//
//	"ps -a"                 -> ["ps", "-a"]
//	"logs --tail 50 web"    -> ["logs", "--tail", "50", "web"]
//	`exec web sh -c "env"`  -> ["exec", "web", "sh", "-c", "env"]
//	`exec web sh -c 'echo $A'` -> ["exec", "web", "sh", "-c", "echo $A"]
//
// Returns an error on unterminated quotes or trailing backslashes.
func ShellSplit(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		inDq    bool // inside "..."
		inSq    bool // inside '...'
		escaped bool // previous char was backslash
		hasTok  bool // current builder holds a started token
	)
	flush := func() {
		if hasTok {
			args = append(args, cur.String())
			cur.Reset()
			hasTok = false
		}
	}
	for _, r := range s {
		if escaped {
			cur.WriteRune(r)
			hasTok = true
			escaped = false
			continue
		}
		switch {
		case r == '\\' && !inSq:
			// Backslash escapes the next char outside single quotes.
			escaped = true
		case r == '"' && !inSq:
			inDq = !inDq
			hasTok = true // "" is a valid empty token
		case r == '\'' && !inDq:
			inSq = !inSq
			hasTok = true
		case (r == ' ' || r == '\t') && !inDq && !inSq:
			flush()
		default:
			cur.WriteRune(r)
			hasTok = true
		}
	}
	if inDq || inSq {
		return nil, errors.New("unterminated quote in command string")
	}
	if escaped {
		return nil, errors.New("trailing backslash in command string")
	}
	flush()
	return args, nil
}

// sortedKeys is a small helper used when building error messages so the
// permitted list is deterministic. Defined here so allowlist.go stays
// dependency-free.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
