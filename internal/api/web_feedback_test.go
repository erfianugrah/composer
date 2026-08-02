package api_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestWebMutatingCallsReportOutcome enforces that every mutating API call in
// the UI tells the operator what happened.
//
// The app already had the machinery - a global `toast` API with <Toaster/>
// mounted in the root layout - but for a long time it was called from exactly
// one place (`runBulk`). Bulk actions announced their result; single-item
// actions did not, so clicking Restart on a container produced no button
// state, no row change and no message. The only way to find out whether the
// request had even been sent was the browser network tab.
//
// The rule: a mutating `apiFetch` must be lexically enclosed by a `run(...)`
// or `runBulk(...)` callback. Those wrappers own pending state and emit
// exactly one success/error toast, so routing every mutation through them
// makes feedback structural instead of something each callsite must remember.
//
// Enclosure is checked by walking outward through unclosed parens from the
// call, not by proximity - a `toast` call that merely sits nearby does not
// satisfy it.
//
// Opt out with a `feedback-exempt: <reason>` comment on the call line or the
// line above (e.g. login, which navigates away, or polling that would spam).
func TestWebMutatingCallsReportOutcome(t *testing.T) {
	const webRoot = "../../web/src"

	// Wrappers that guarantee pending state + exactly one outcome toast.
	reporters := map[string]bool{"run": true, "runBulk": true}

	for _, src := range webSourceFiles(t, webRoot) {
		// The wrappers themselves call apiFetch-shaped functions internally.
		if strings.HasSuffix(src, "use-busy.ts") || strings.HasSuffix(src, "use-action.ts") {
			continue
		}
		content, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		text := string(content)

		for _, call := range findCalls(text, "apiFetch") {
			if !mutatingCall.MatchString(call.args) {
				continue
			}
			if hasExemption(text, call.start) {
				continue
			}
			if enclosedByReporter(text, call.start, reporters) {
				continue
			}
			t.Errorf("%s:%d mutating apiFetch does not report its outcome:\n\t%s\n"+
				"\tWrap it in useAction's run(key, () => apiFetch(...), labels) so the button "+
				"shows pending state and the user gets a success/error toast, or add a "+
				"`feedback-exempt: <reason>` comment if silence is genuinely correct.",
				src, lineOf(text, call.start), firstLine(call.snippet))
		}
	}
}

var mutatingCall = regexp.MustCompile(`method:\s*["'` + "`" + `](POST|PUT|DELETE|PATCH)`)

type callSite struct {
	start   int    // index of the callee identifier
	end     int    // index just past the call's closing paren
	args    string // text between the call's outer parens
	snippet string // the call as written
}

// findCalls locates every `<name>(...)` invocation and captures its argument
// text by forward paren matching, so a multi-line call is handled whole.
func findCalls(text, name string) []callSite {
	var out []callSite
	for i := 0; i+len(name) < len(text); i++ {
		if !strings.HasPrefix(text[i:], name) {
			continue
		}
		// Reject identifier suffixes (apiFetchRaw) and prefixes (myApiFetch).
		if i > 0 && isIdentChar(text[i-1]) {
			continue
		}
		j := i + len(name)
		// Skip a generic parameter list: apiFetch<StackData>(...)
		if j < len(text) && text[j] == '<' {
			depth := 0
			for ; j < len(text); j++ {
				if text[j] == '<' {
					depth++
				} else if text[j] == '>' {
					depth--
					if depth == 0 {
						j++
						break
					}
				}
			}
		}
		if j >= len(text) || text[j] != '(' {
			continue
		}
		end := matchParen(text, j)
		if end < 0 {
			continue
		}
		out = append(out, callSite{start: i, end: end, args: text[j+1 : end], snippet: text[i:min(end+1, len(text))]})
		i = j
	}
	return out
}

// matchParen returns the index of the ')' closing the '(' at open.
func matchParen(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// enclosedByReporter walks outward from pos through unclosed brackets and
// reports whether any enclosing call is one of the reporter wrappers. This is
// true lexical enclosure: a nearby but non-enclosing call never satisfies it.
//
// Braces are balanced exactly like parens on the way out. Only an UNPAIRED '{'
// is a block boundary. An earlier version incremented depth on ')' but treated
// every '{' as a boundary, so any brace in an earlier argument - a template
// literal key (`${id}:stop`) or an options object - aborted the walk and the
// call was reported as unwrapped even though it was correctly wrapped. That is
// the worst failure mode for a gate that drives an agent loop: it rejects
// correct work, so the loop cannot converge. See TestEnclosedByReporter.
func enclosedByReporter(text string, pos int, reporters map[string]bool) bool {
	depth := 0
	for i := pos - 1; i >= 0; i-- {
		switch text[i] {
		case ')', '}':
			depth++
		case '(':
			if depth > 0 {
				depth--
				continue
			}
			// Unclosed paren: this call encloses pos.
			if reporters[identBefore(text, i)] {
				return true
			}
		case '{':
			if depth > 0 {
				depth--
				continue
			}
			// Unpaired brace: we left the enclosing expression into a
			// statement block. An outer function's run() must not launder an
			// unwrapped call sitting in a sibling statement.
			return false
		}
	}
	return false
}

// TestEnclosedByReporter pins the enclosure walk itself. The feedback gate is
// only as trustworthy as this function: a false positive rejects correct code,
// a false negative lets a silent action through.
func TestEnclosedByReporter(t *testing.T) {
	reporters := map[string]bool{"run": true, "runBulk": true}
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"plain string key", `act.run("save", () => apiFetch(u, { method: "POST" }))`, true},
		{"template-literal key", "act.run(`${id}:save`, () => apiFetch(u, { method: \"POST\" }))", true},
		{"helper-call key", `act.run(actionKey(id, a), () => apiFetch(u, { method: "POST" }))`, true},
		{"object arg before callback", `act.run(k, { x: 1 }, () => apiFetch(u, { method: "POST" }))`, true},
		{"runBulk", "runBulk(ids, (id) => apiFetch(`/x/${id}`, { method: \"DELETE\" }), l)", true},
		{"explicit type argument on run", `act.run<{ id: string }>(k, () => apiFetch(u, { method: "POST" }))`, true},
		{"multiline", "act.run(\n  actionKey(id, action),\n  () => apiFetch(`/api/v1/c/${id}`, { method: \"POST\" }),\n  { running: \"x\" },\n)", true},
		{"bare call", `const go = async () => { await apiFetch(u, { method: "POST" }); }`, false},
		{"sibling statement after a wrapped call", "const go = async () => {\n  act.run(k, () => apiFetch(a, { method: \"POST\" }));\n  await apiFetch(b, { method: \"POST\" });\n}", false},
		{"nested then-block", `act.run(k, () => x).then(() => { apiFetch(b, { method: "POST" }); })`, false},
		{"nearby toast decoy", `const go = async () => { toast.success("hi"); await apiFetch(u, { method: "POST" }); }`, false},
	}
	for _, tt := range tests {
		// Target the LAST apiFetch: in the negative cases the earlier one is
		// legitimately wrapped and the trailing one is the offender.
		pos := strings.LastIndex(tt.src, "apiFetch")
		if pos < 0 {
			t.Fatalf("%s: fixture has no apiFetch", tt.name)
		}
		if got := enclosedByReporter(tt.src, pos, reporters); got != tt.want {
			t.Errorf("%s: enclosedByReporter() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// identBefore reads the identifier immediately preceding index i, skipping a
// property access so `act.run(` yields "run", and skipping an explicit type
// argument list so `act.run<{ id: string }>(` also yields "run".
func identBefore(text string, i int) string {
	end := i
	end = skipSpaceBack(text, end)
	if end > 0 && text[end-1] == '>' {
		depth := 0
		for end > 0 {
			end--
			if text[end] == '>' {
				depth++
			} else if text[end] == '<' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		end = skipSpaceBack(text, end)
	}
	start := end
	for start > 0 && isIdentChar(text[start-1]) {
		start--
	}
	return text[start:end]
}

func skipSpaceBack(text string, i int) int {
	for i > 0 && (text[i-1] == ' ' || text[i-1] == '\n' || text[i-1] == '\t') {
		i--
	}
	return i
}

func isIdentChar(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// hasExemption reports whether the call's line, or the line above, carries an
// explicit opt-out comment.
func hasExemption(text string, pos int) bool {
	lineStart := strings.LastIndexByte(text[:pos], '\n') + 1
	lineEnd := lineStart + strings.IndexByte(text[lineStart:], '\n')
	if lineEnd < lineStart {
		lineEnd = len(text)
	}
	if strings.Contains(text[lineStart:lineEnd], "feedback-exempt:") {
		return true
	}
	prevStart := strings.LastIndexByte(text[:max(lineStart-1, 0)], '\n') + 1
	return strings.Contains(text[prevStart:lineStart], "feedback-exempt:")
}

func lineOf(text string, pos int) int {
	return strings.Count(text[:pos], "\n") + 1
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " ..."
	}
	return strings.TrimSpace(s)
}
