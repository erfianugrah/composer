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
			// Scan from where the CALL ENDS, not a fixed offset from where it
			// starts: a run() with a long options object pushed its own
			// `if (err) setError(err)` outside a start-relative window, and the
			// gate went green with a real double-report still in the tree.
			rendered := errVar != "" && rendersInline(lines, lineOf(text, call.end), errVar)

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
// setter in the statements immediately following the call. `callEndLine` is
// the line the call's closing paren sits on, so the window is independent of
// how many lines the arguments span.
func rendersInline(lines []string, callEndLine int, errVar string) bool {
	setter := regexp.MustCompile(`\bset[A-Z]\w*\(\s*` + regexp.QuoteMeta(errVar) + `\b`)
	start := max(callEndLine-1, 0)
	end := min(start+6, len(lines))
	for _, l := range lines[start:end] {
		if setter.MatchString(l) {
			return true
		}
	}
	return false
}

// TestRendersInlineWindow pins the lookahead used by the error-channel gate.
//
// The first version measured from where the call STARTED, so a run() with a
// long options object pushed its own `if (err) setError(err)` past the window
// and the gate reported the file clean. A permissive gate is worse than none:
// it converts "not checked" into "checked and fine". The window is now
// measured from the call's closing paren.
func TestRendersInlineWindow(t *testing.T) {
	tests := []struct {
		name        string
		lines       []string
		callEndLine int
		want        bool
	}{
		{
			name: "banner immediately after the call",
			lines: []string{
				`);`,
				`if (err) setError(err);`,
			},
			callEndLine: 1,
			want:        true,
		},
		{
			name: "banner after a long options object still counts",
			lines: append(
				[]string{`const { error: err } = await act.run(`},
				append(make([]string, 12), `);`, `if (err) setError(err);`)...,
			),
			callEndLine: 14, // the `);` line
			want:        true,
		},
		{
			name: "an unrelated setter far below does not count",
			lines: append(
				[]string{`);`},
				append(make([]string, 20), `if (err) setError(err);`)...,
			),
			callEndLine: 1,
			want:        false,
		},
		{
			name: "a setter for a different variable does not count",
			lines: []string{
				`);`,
				`if (other) setError(other);`,
			},
			callEndLine: 1,
			want:        false,
		},
	}
	for _, tt := range tests {
		if got := rendersInline(tt.lines, tt.callEndLine, "err"); got != tt.want {
			t.Errorf("%s: rendersInline() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
