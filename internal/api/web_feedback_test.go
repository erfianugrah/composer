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
		out = append(out, callSite{start: i, args: text[j+1 : end], snippet: text[i:min(end+1, len(text))]})
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

// enclosedByReporter walks outward from pos through unclosed '(' characters
// and reports whether any enclosing call is one of the reporter wrappers.
// This is true lexical enclosure: a nearby but non-enclosing call never
// satisfies it.
func enclosedByReporter(text string, pos int, reporters map[string]bool) bool {
	depth := 0
	for i := pos - 1; i >= 0; i-- {
		switch text[i] {
		case ')':
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
			// Left the enclosing expression without finding a reporter. Do not
			// cross a block boundary - an outer function's run() must not
			// launder an unwrapped call in a nested statement body.
			if depth == 0 {
				return false
			}
		}
	}
	return false
}

// identBefore reads the identifier immediately preceding index i, skipping a
// property access so `act.run(` yields "run".
func identBefore(text string, i int) string {
	end := i
	for end > 0 && (text[end-1] == ' ' || text[end-1] == '\n' || text[end-1] == '\t') {
		end--
	}
	start := end
	for start > 0 && isIdentChar(text[start-1]) {
		start--
	}
	return text[start:end]
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
