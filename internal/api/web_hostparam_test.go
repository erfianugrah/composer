package api_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestWebCallsHostAwareEndpointsWithHost guards the multi-host regression that
// keeps recurring: the backend accepts ?host= on every container/resource
// endpoint, but a frontend callsite forgets to send it, so an action against a
// stack on a REMOTE docker host silently resolves on the LOCAL daemon and
// fails with "container not found" - which the UI then swallows.
//
// The rule is derived from the committed OpenAPI spec (single source of
// truth). Every `/api/v1/...` URL literal in web/src is reduced to a path
// shape (`${expr}` -> `{}`) and matched against the spec's paths. If any
// matching spec path declares a `host` query parameter, the literal must carry
// a host, in one of these shapes:
//
//	`?host=${...}`                     - written inline
//	`${hostSuffix()}` / `${hostQuery(x)}` - appended by a helper
//	`if (!selectedHost) return "/api/v1/x"` - host selects the URL
//	params.set("host", h) above the call   - URLSearchParams
//	url += `&host=${...}` just below       - post-hoc append
//
// Anything else must opt out with a `host-exempt: <reason>` comment on the
// line or the line above, so a deliberate local-only call is documented rather
// than indistinguishable from an oversight.
func TestWebCallsHostAwareEndpointsWithHost(t *testing.T) {
	const (
		specPath = "../../web/src/lib/api/openapi.json"
		webRoot  = "../../web/src"
	)

	hostAware, allShapes := loadHostAwarePathShapes(t, specPath)

	for _, src := range webSourceFiles(t, webRoot) {
		content, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		lines := strings.Split(string(content), "\n")

		for i, line := range lines {
			for _, lit := range apiLiterals(line) {
				shape := pathShape(lit.text)
				if shape == "" {
					continue
				}
				needsHost, known := shapeNeedsHost(shape, hostAware, allShapes)
				if !known {
					t.Errorf("%s:%d references %q which matches no path in the OpenAPI spec - "+
						"stale URL, or the spec needs regenerating (make generate)", src, i+1, shape)
					continue
				}
				if !needsHost {
					continue
				}
				if hostSatisfied(lines, i, lit) {
					continue
				}
				t.Errorf("%s:%d calls host-aware endpoint %s without a host param:\n\t%s\n"+
					"\tAppend ?host=<docker host> (see the hostSuffix/hostQuery helpers), or add a "+
					"`host-exempt: <reason>` comment if local-only is intentional.",
					src, i+1, shape, strings.TrimSpace(line))
			}
		}
	}
}

// loadHostAwarePathShapes returns the set of spec path shapes that accept a
// `host` query param, plus the set of all spec path shapes (used to detect
// URLs that match nothing at all).
func loadHostAwarePathShapes(t *testing.T, specPath string) (hostAware, all map[string]bool) {
	t.Helper()

	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		t.Fatalf("parse openapi spec: %v", err)
	}

	hostAware, all = map[string]bool{}, map[string]bool{}
	for p, methods := range spec.Paths {
		shape := specParam.ReplaceAllString(p, "{}")
		all[shape] = true
		for _, op := range methods {
			for _, param := range op.Parameters {
				if param.Name == "host" && param.In == "query" {
					hostAware[shape] = true
				}
			}
		}
	}
	if len(hostAware) == 0 {
		t.Fatal("no host-aware paths found in the OpenAPI spec - the matcher is broken")
	}
	return hostAware, all
}

func webSourceFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".ts", ".tsx", ".astro":
		default:
			return nil
		}
		// The generated API artifacts enumerate every path by definition.
		if strings.Contains(filepath.ToSlash(path), "/lib/api/") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk web sources: %v", err)
	}
	return out
}

var (
	specParam = regexp.MustCompile(`\{[^}]*\}`)
	// setHostParam matches an explicit host entry on a URLSearchParams built
	// just above the fetch call, e.g. `params.set("host", selectedHost)`.
	setHostParam = regexp.MustCompile(`\.set\(\s*["'` + "`" + `]host["'` + "`" + `]`)
	// urlAppend matches a post-hoc `url += ...host...` on a following line.
	urlAppend = regexp.MustCompile(`\+=.*[Hh]ost`)
)

type literal struct {
	text  string // literal contents, delimiters stripped
	start int    // index of the opening delimiter within the line
}

// apiLiterals extracts every quoted/backticked literal on the line that starts
// with /api/v1/. The scanner is `${...}`-aware so a nested template literal
// (e.g. `${host ? `&host=${h}` : ""}`) does not terminate the outer literal
// early - truncating there would hide the very token being checked.
func apiLiterals(line string) []literal {
	var out []literal
	for i := 0; i < len(line); i++ {
		q := line[i]
		if q != '"' && q != '\'' && q != '`' {
			continue
		}
		depth := 0
		j := i + 1
		for ; j < len(line); j++ {
			switch {
			case line[j] == '$' && j+1 < len(line) && line[j+1] == '{':
				depth++
				j++
			case line[j] == '}' && depth > 0:
				depth--
			case line[j] == q && depth == 0:
				body := line[i+1 : j]
				if strings.HasPrefix(body, "/api/v1/") {
					out = append(out, literal{text: body, start: i})
				}
				i = j
				j = len(line)
			}
		}
		if j >= len(line) && i < len(line) && line[i] == q {
			// Unterminated on this line (multi-line template) - take the rest.
			body := line[i+1:]
			if strings.HasPrefix(body, "/api/v1/") {
				out = append(out, literal{text: body, start: i})
			}
			break
		}
	}
	return out
}

// pathShape reduces a URL literal to its spec-comparable path shape:
// `/api/v1/containers/${c.id}/stop?tail=5` -> `/api/v1/containers/{}/stop`.
//
// An interpolation that starts a segment is a path parameter and becomes `{}`.
// One that continues a segment (`.../stop${hostSuffix()}`) is a query-string
// suffix, so parsing stops there. Returns "" when the literal is not an
// /api/v1 path.
func pathShape(lit string) string {
	if !strings.HasPrefix(lit, "/api/v1/") {
		return ""
	}
	var b strings.Builder
	depth := 0
	for i := 0; i < len(lit); i++ {
		switch {
		case lit[i] == '$' && i+1 < len(lit) && lit[i+1] == '{':
			if depth == 0 {
				if s := b.String(); !strings.HasSuffix(s, "/") {
					// Mid-segment interpolation: a query suffix, not a path param.
					return s
				}
				b.WriteString("{}")
			}
			depth++
			i++
		case lit[i] == '}' && depth > 0:
			depth--
		case depth == 0:
			if lit[i] == '?' || lit[i] == '#' {
				return b.String()
			}
			b.WriteByte(lit[i])
		}
	}
	return b.String()
}

// shapeNeedsHost reports whether a URL shape hits a host-aware endpoint, and
// whether it matched any spec path at all. A `{}` segment on either side is a
// wildcard, so `/api/v1/containers/{}/{}` (a dynamic verb) matches every
// container action path - all of which are host-aware.
func shapeNeedsHost(shape string, hostAware, all map[string]bool) (needsHost, known bool) {
	for spec := range all {
		if !shapesMatch(shape, spec) {
			continue
		}
		known = true
		if hostAware[spec] {
			return true, true
		}
	}
	return false, known
}

func shapesMatch(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] == "{}" || bs[i] == "{}" || as[i] == bs[i] {
			continue
		}
		return false
	}
	return true
}

// hostSatisfied reports whether the callsite supplies a docker host by any of
// the accepted shapes (see the test doc comment).
func hostSatisfied(lines []string, i int, lit literal) bool {
	line := lines[i]

	if strings.Contains(line, "host-exempt:") ||
		(i > 0 && strings.Contains(lines[i-1], "host-exempt:")) {
		return true
	}
	// Inline, or via a hostSuffix()/hostQuery() interpolation.
	if strings.Contains(strings.ToLower(lit.text), "host") {
		return true
	}
	// The host is what selects this URL: `if (!selectedHost) return "..."`.
	if strings.Contains(strings.ToLower(line[:lit.start]), "host") {
		return true
	}
	// URLSearchParams built above the call.
	if setHostParam.MatchString(strings.Join(lines[max(i-8, 0):i], "\n")) {
		return true
	}
	// Post-hoc `url += ...host...` append below the call (LogViewer builds
	// the URL in one branch and appends the host a few lines later).
	for _, l := range lines[min(i+1, len(lines)):min(i+12, len(lines))] {
		if urlAppend.MatchString(l) {
			return true
		}
	}
	return false
}
