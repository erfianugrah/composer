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
	}
}

// TestWebStatusVocabularyCoversDaemonStates checks the shared map against the
// container states the Go domain can actually emit. Three of them
// (restarting, removing, dead) were in none of the old per-component maps, so
// a dead container rendered in the same neutral grey as one stopped on
// purpose - red is reserved for states a human must act on, and this was one.
func TestWebStatusVocabularyCoversDaemonStates(t *testing.T) {
	const (
		entityPath = "../../internal/domain/container/entity.go"
		colorsPath = "../../web/src/lib/status-colors.ts"
	)

	entity, err := os.ReadFile(entityPath)
	if err != nil {
		t.Fatalf("read entity: %v", err)
	}
	colors, err := os.ReadFile(colorsPath)
	if err != nil {
		t.Fatalf("read status colors: %v", err)
	}

	// Status<Name> ContainerStatus = "value"
	decl := regexp.MustCompile(`Status\w+\s+ContainerStatus\s*=\s*"(\w+)"`)
	matches := decl.FindAllStringSubmatch(string(entity), -1)
	if len(matches) == 0 {
		t.Fatal("found no ContainerStatus constants - the matcher is broken")
	}

	key := regexp.MustCompile(`(?m)^\s*(\w+):`)
	defined := map[string]bool{}
	for _, m := range key.FindAllStringSubmatch(string(colors), -1) {
		defined[m[1]] = true
	}

	for _, m := range matches {
		if !defined[m[1]] {
			t.Errorf("container status %q has no entry in web/src/lib/status-colors.ts - "+
				"it will render with the unknown fallback", m[1])
		}
	}
}

func filepathSlash(p string) string { return strings.ReplaceAll(p, `\`, "/") }
