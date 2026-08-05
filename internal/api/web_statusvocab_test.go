package api_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestWebStatusVocabularyIsShared keeps every surface on one status vocabulary.
//
// Four components each carried a private statusColor map. They agreed on the
// states they happened to share and diverged elsewhere: the container list
// fell back to the "created" style for an unrecognised status while the stack
// page fell back to "unknown". Divergence like that is invisible in review -
// nothing is broken in any single file - and only shows up as two pages
// disagreeing about the same container.
func TestWebStatusVocabularyIsShared(t *testing.T) {
	const webRoot = "../../web/src"

	localMap := regexp.MustCompile(`(?m)^\s*(?:const|let|var)\s+statusColor\b`)
	// `statusColor[x] || statusColor.y` - the inline fallback that let the two
	// container tables disagree. statusClass() exists so there is one answer.
	inlineFallback := regexp.MustCompile(`statusColor\[[^\]]+\]\s*\|\|`)
	statuses := daemonStatuses(t)

	for _, src := range webSourceFiles(t, webRoot) {
		content, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		text := string(content)

		// The shared module is where the one map and the one fallback live.
		if strings.HasSuffix(filepathSlash(src), "lib/status-colors.ts") {
			continue
		}
		if loc := localMap.FindStringIndex(text); loc != nil {
			t.Errorf("%s:%d defines its own statusColor map.\n"+
				"\tImport it from @/lib/status-colors instead - private copies drift.",
				src, lineOf(text, loc[0]))
		}
		if loc := inlineFallback.FindStringIndex(text); loc != nil {
			t.Errorf("%s:%d picks its own fallback for an unknown status.\n"+
				"\tUse statusClass(x) so every surface renders an unrecognised status the same way.",
				src, lineOf(text, loc[0]))
		}
		if line, name := statusStyleMap(text, statuses); line > 0 {
			t.Errorf("%s:%d maps container statuses to styles in a private %q map.\n"+
				"\tstatusColor was only ever the name this happened to have; the\n"+
				"\tviolation is the second vocabulary. Use statusClass from\n"+
				"\t@/lib/status-colors.",
				src, line, name)
		}
	}
}

// statusStyleMap finds an object literal mapping daemon status names to style
// strings, whatever the variable is called, and returns the first offending
// line plus that variable's name.
//
// Matching the identifier `statusColor` only ever caught the copies that kept
// the name. Renaming one to badgeTone evaded the gate completely while being
// the identical divergence, so the check now keys on content: the keys must
// come from the vocabulary the Go domain actually emits, and the values must
// look like theme classes, which keeps a status->label map from being
// mistaken for a status->colour one. Three keys is the threshold - two
// statuses side by side is a conditional, not a vocabulary.
func statusStyleMap(text string, statuses map[string]bool) (int, string) {
	var (
		declLine = regexp.MustCompile(`(?:const|let|var)\s+(\w+)`)
		keyLine  = regexp.MustCompile(`^\s*(\w+)\s*:\s*(.+)$`)
		styleish = regexp.MustCompile(`\b(?:text|bg|border|ring)-|cp-`)
	)
	hits, start, name, owner := 0, 0, "", ""
	for i, line := range strings.Split(text, "\n") {
		if m := declLine.FindStringSubmatch(line); m != nil && strings.Contains(line, "{") {
			owner, hits, start = m[1], 0, 0
		}
		m := keyLine.FindStringSubmatch(line)
		if m == nil {
			// Leaving the literal ends the run; an unrelated pair of status
			// keys in two different objects must not add up to a map.
			if strings.Contains(line, "}") {
				hits, start = 0, 0
			}
			continue
		}
		if !statuses[m[1]] || !styleish.MatchString(m[2]) {
			continue
		}
		hits++
		if start == 0 {
			start, name = i+1, owner
		}
		if hits >= 3 {
			return start, name
		}
	}
	return 0, ""
}

// daemonStatuses is the container status vocabulary the Go domain can emit,
// read from the entity rather than duplicated here so a new status cannot be
// added on one side only.
func daemonStatuses(t *testing.T) map[string]bool {
	t.Helper()
	entity, err := os.ReadFile("../../internal/domain/container/entity.go")
	if err != nil {
		t.Fatalf("read entity: %v", err)
	}
	decl := regexp.MustCompile(`Status\w+\s+ContainerStatus\s*=\s*"(\w+)"`)
	out := map[string]bool{}
	for _, m := range decl.FindAllStringSubmatch(string(entity), -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		t.Fatal("found no ContainerStatus constants - the matcher is broken")
	}
	return out
}

// TestWebStatusVocabularyCoversDaemonStates checks the shared map against the
// container states the Go domain can actually emit. Three of them
// (restarting, removing, dead) were in none of the old per-component maps, so
// a dead container rendered in the same neutral grey as one stopped on
// purpose - red is reserved for states a human must act on, and this was one.
func TestWebStatusVocabularyCoversDaemonStates(t *testing.T) {
	const colorsPath = "../../web/src/lib/status-colors.ts"

	colors, err := os.ReadFile(colorsPath)
	if err != nil {
		t.Fatalf("read status colors: %v", err)
	}

	key := regexp.MustCompile(`(?m)^\s*(\w+):`)
	defined := map[string]bool{}
	for _, m := range key.FindAllStringSubmatch(string(colors), -1) {
		defined[m[1]] = true
	}

	for status := range daemonStatuses(t) {
		if !defined[status] {
			t.Errorf("container status %q has no entry in web/src/lib/status-colors.ts - "+
				"it will render with the unknown fallback", status)
		}
	}
}

func filepathSlash(p string) string { return strings.ReplaceAll(p, `\`, "/") }
