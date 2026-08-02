package api_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestWebActionsReportErrorExactlyOnce enforces one failure, one message.
//
// Routing every mutating call through useAction gave each one an error toast.
// Callsites that already rendered an inline banner then reported the same
// failure twice, in two visual languages - "Failed to create network foo" in a
// toast and the identical API error in a banner underneath. Two channels for
// one event is the inconsistency the feedback sweep was supposed to remove.
//
// The rule is a choice, not a default: a callsite either lets the toast report
// the failure, or renders it inline and passes `inlineError: true` to suppress
// the toast. Both directions are checked, because the failure mode of a
// careless fix is reporting the error ZERO times:
//
//	error flows into a set*() call  =>  the run() must pass inlineError: true
//	run() passes inlineError: true  =>  the error must flow into a set*() call
func TestWebActionsReportErrorExactlyOnce(t *testing.T) {
	const webRoot = "../../web/src"

	for _, src := range webSourceFiles(t, webRoot) {
		content, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		text := string(content)
		lines := strings.Split(text, "\n")

		for _, call := range findCalls(text, "run") {
			// Only useAction's run(...), not arbitrary functions called "run".
			if !strings.Contains(call.args, "apiFetch") {
				continue
			}
			line := lineOf(text, call.start)
			declaresInline := inlineErrorOpt.MatchString(call.args)

			errVar := errorBindingBefore(text, call.start)
			rendered := errVar != "" && rendersInline(lines, line, errVar)

			switch {
			case rendered && !declaresInline:
				t.Errorf("%s:%d reports this failure twice - inline banner AND error toast.\n"+
					"\tPass `inlineError: true` in the run() options to let the banner own it, "+
					"or drop the set*(%s) and let the toast own it.", src, line, errVar)
			case declaresInline && !rendered:
				t.Errorf("%s:%d passes inlineError: true but never renders the error.\n"+
					"\tThe toast is suppressed and nothing replaces it, so this failure is "+
					"reported nowhere. Render it inline or remove inlineError.", src, line)
			}
		}
	}
}

var (
	inlineErrorOpt = regexp.MustCompile(`inlineError:\s*true`)
	// `const { error: err } = await act.run(` / `const { error } = await run(`
	errorBinding = regexp.MustCompile(`\{[^}]*\berror\b\s*(?::\s*(\w+))?[^}]*\}\s*=`)
)

// errorBindingBefore returns the identifier the call's error is bound to, or
// "" when the result is discarded.
func errorBindingBefore(text string, callStart int) string {
	lineStart := strings.LastIndexByte(text[:callStart], '\n') + 1
	// Everything on the line before the call: the destructuring pattern, if any.
	m := errorBinding.FindStringSubmatch(text[lineStart:callStart])
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1] // renamed: { error: err }
	}
	return "error"
}

// rendersInline reports whether the bound error is handed to a set*() state
// setter within the statements following the call.
func rendersInline(lines []string, callLine int, errVar string) bool {
	setter := regexp.MustCompile(`\bset[A-Z]\w*\(\s*` + regexp.QuoteMeta(errVar) + `\b`)
	start := callLine - 1
	end := min(start+14, len(lines))
	for _, l := range lines[start:end] {
		if setter.MatchString(l) {
			return true
		}
	}
	return false
}
