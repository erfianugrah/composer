package docker

import (
	"reflect"
	"testing"
)

func TestShellSplit(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		err  bool
	}{
		{"", nil, false},
		{"   ", nil, false},
		{"ps", []string{"ps"}, false},
		{"ps -a", []string{"ps", "-a"}, false},
		{"logs --tail 50 web", []string{"logs", "--tail", "50", "web"}, false},
		{`exec web sh -c "env"`, []string{"exec", "web", "sh", "-c", "env"}, false},
		{`exec web sh -c 'echo $A'`, []string{"exec", "web", "sh", "-c", "echo $A"}, false},
		{`exec web sh -c "hello world"`, []string{"exec", "web", "sh", "-c", "hello world"}, false},
		{`exec web sh -c 'with"quote'`, []string{"exec", "web", "sh", "-c", `with"quote`}, false},
		{`a\ b c`, []string{"a b", "c"}, false},
		{"multiple   spaces  between", []string{"multiple", "spaces", "between"}, false},
		{"tab\tseparated", []string{"tab", "separated"}, false},

		// Error cases
		{`"unterminated`, nil, true},
		{`'unterminated`, nil, true},
		{`ends_with_\`, nil, true},
	}
	for _, c := range cases {
		got, err := ShellSplit(c.in)
		if (err != nil) != c.err {
			t.Errorf("ShellSplit(%q) err=%v, want err=%v", c.in, err, c.err)
			continue
		}
		if !c.err && !reflect.DeepEqual(got, c.want) {
			t.Errorf("ShellSplit(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestComposeAllowed(t *testing.T) {
	cases := []struct {
		cmd     string
		isAdmin bool
		want    bool
	}{
		{"ps", false, true},
		{"logs", false, true},
		{"exec", false, false},
		{"exec", true, true},
		{"cp", false, false},
		{"cp", true, true},
		{"rm", false, false},
		{"rm", true, false},
	}
	for _, c := range cases {
		got, _ := ComposeAllowed(c.cmd, c.isAdmin)
		if got != c.want {
			t.Errorf("ComposeAllowed(%q, admin=%v) = %v, want %v", c.cmd, c.isAdmin, got, c.want)
		}
	}
}

func TestDockerAllowed(t *testing.T) {
	allowed := []string{"ps", "images", "network", "volume", "system", "info", "version", "inspect", "logs", "stats", "top", "port", "diff", "history", "search", "tag"}
	for _, cmd := range allowed {
		if ok, _ := DockerAllowed(cmd); !ok {
			t.Errorf("DockerAllowed(%q) should be true", cmd)
		}
	}
	denied := []string{"run", "rm", "rmi", "build", "push", "pull"}
	for _, cmd := range denied {
		if ok, _ := DockerAllowed(cmd); ok {
			t.Errorf("DockerAllowed(%q) should be false", cmd)
		}
	}
}
