package gitutil

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeGitHubRemote(t *testing.T) {
	const want = "https://github.com/MGT-Insurance/Agent-Teams"
	for _, raw := range []string{
		"git@github.com:MGT-Insurance/Agent-Teams.git",
		"git@github.com:MGT-Insurance/Agent-Teams",
		"https://github.com/MGT-Insurance/Agent-Teams.git",
		"https://github.com/MGT-Insurance/Agent-Teams",
		"https://github.com/MGT-Insurance/Agent-Teams/",
		"ssh://git@github.com/MGT-Insurance/Agent-Teams.git",
		"ssh://git@github.com/MGT-Insurance/Agent-Teams\n",
	} {
		got, err := NormalizeGitHubRemote(raw)
		if err != nil || got != want {
			t.Errorf("NormalizeGitHubRemote(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"git@gitlab.com:o/r.git",
		"https://example.com/o/r",
		"https://github.com/onlyowner",
		"/local/path",
		"",
	} {
		if got, err := NormalizeGitHubRemote(raw); err == nil {
			t.Errorf("NormalizeGitHubRemote(%q) = %q; want error", raw, got)
		}
	}
}

func execReturning(out string, err error, got *[]string) ExecFunc {
	return func(name string, args ...string) ([]byte, []byte, error) {
		*got = append([]string{name}, args...)
		return []byte(out), nil, err
	}
}

func TestRemoteURL(t *testing.T) {
	var call []string
	r := NewWithExec(execReturning("git@github.com:MGT-Insurance/x.git\n", nil, &call))
	got, err := r.RemoteURL("/repo")
	if err != nil || got != "https://github.com/MGT-Insurance/x" {
		t.Fatalf("RemoteURL = %q, %v", got, err)
	}
	if strings.Join(call, " ") != "git -C /repo remote get-url origin" {
		t.Errorf("call = %v", call)
	}
	if _, err := NewWithExec(execReturning("", errors.New("no origin"), &call)).RemoteURL("/repo"); err == nil {
		t.Error("exec failure not reported")
	}
}

func TestRemoteHead(t *testing.T) {
	sha := strings.Repeat("a1", 20)
	var call []string
	got, err := NewWithExec(execReturning(sha+"\trefs/heads/main\n", nil, &call)).RemoteHead("/repo", "main")
	if err != nil || got != sha {
		t.Fatalf("RemoteHead = %q, %v", got, err)
	}
	if strings.Join(call, " ") != "git -C /repo ls-remote origin refs/heads/main" {
		t.Errorf("call = %v", call)
	}
	for name, out := range map[string]string{
		"zero lines":   "",
		"two lines":    sha + "\trefs/heads/main\n" + sha + "\trefs/heads/main\n",
		"short sha":    "abc123\trefs/heads/main\n",
		"uppercase":    strings.ToUpper(sha) + "\trefs/heads/main\n",
		"wrong ref":    sha + "\trefs/heads/other\n",
		"missing tab":  sha + "\n",
		"non-hex 40ch": strings.Repeat("z", 40) + "\trefs/heads/main\n",
	} {
		if got, err := NewWithExec(execReturning(out, nil, &call)).RemoteHead("/repo", "main"); err == nil {
			t.Errorf("%s: RemoteHead = %q; want error", name, got)
		}
	}
	if _, err := NewWithExec(execReturning("", errors.New("boom"), &call)).RemoteHead("/repo", "main"); err == nil {
		t.Error("exec failure not reported")
	}
}
