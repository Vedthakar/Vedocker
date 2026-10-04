package main

import "testing"

func TestNormalizeGitHubURL(t *testing.T) {
	valid := map[string]string{
		"https://github.com/owner/repo":                    "https://github.com/owner/repo",
		"https://github.com/owner/repo/":                   "https://github.com/owner/repo",
		"https://github.com/owner/repo.git":                "https://github.com/owner/repo",
		"http://github.com/owner/repo":                     "https://github.com/owner/repo",
		"github.com/owner/repo":                            "https://github.com/owner/repo",
		"www.github.com/owner/repo":                        "https://github.com/owner/repo",
		"https://GitHub.com/Owner/My.Repo_1":               "https://github.com/Owner/My.Repo_1",
		"  https://github.com/owner/repo  ":                "https://github.com/owner/repo",
		"https://github.com/owner/repo/tree/main":          "https://github.com/owner/repo",
		"https://github.com/owner/repo/blob/main/x.go":     "https://github.com/owner/repo",
		"https://github.com/owner/repo?tab=readme-ov-file": "https://github.com/owner/repo",
		"https://github.com/owner/repo#readme":             "https://github.com/owner/repo",
		"git@github.com:owner/repo.git":                    "https://github.com/owner/repo",
		"https://github.com/a-b-c/repo-name":               "https://github.com/a-b-c/repo-name",
	}
	for in, want := range valid {
		got, err := normalizeGitHubURL(in)
		if err != nil {
			t.Errorf("normalizeGitHubURL(%q) returned error %v, want %q", in, err, want)
			continue
		}
		if got != want {
			t.Errorf("normalizeGitHubURL(%q) = %q, want %q", in, got, want)
		}
	}

	invalid := []string{
		"",
		"   ",
		"owner/repo",
		"https://gitlab.com/owner/repo",
		"https://github.com.evil.com/owner/repo",
		"https://evilgithub.com/owner/repo",
		"https://api.github.com/repos/owner/repo",
		"https://gist.github.com/owner/abc123",
		"https://user:pass@github.com/owner/repo",
		"https://github.com:8443/owner/repo",
		"ftp://github.com/owner/repo",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"https://github.com/owner",
		"https://github.com/",
		"https://github.com/owner/..",
		"https://github.com/owner/.",
		"https://github.com/../repo",
		"https://github.com/-owner/repo",
		"https://github.com/owner-/repo",
		"https://github.com/ow--ner/repo",
		"https://github.com/owner/re po",
		"https://github.com/owner/repo;rm -rf /",
		"https://github.com/owner/repo%2F..%2F..",
		"https://github.com/owner/repo/settings",
		"https://github.com/owner\\repo",
		"https://github.com/owner/$(whoami)",
	}
	for _, in := range invalid {
		if got, err := normalizeGitHubURL(in); err == nil {
			t.Errorf("normalizeGitHubURL(%q) = %q, want an error", in, got)
		}
	}
}

func TestNewDaemonClientRequiresLoopback(t *testing.T) {
	for _, ok := range []string{
		"http://127.0.0.1:18080",
		"http://localhost:18080",
		"http://[::1]:18080",
		"http://127.0.0.1:18080/",
	} {
		if _, err := newDaemonClient(ok); err != nil {
			t.Errorf("newDaemonClient(%q) returned error %v", ok, err)
		}
	}
	for _, bad := range []string{
		"http://0.0.0.0:18080",
		"http://192.168.1.10:18080",
		"http://example.com:18080",
		"http://127.0.0.1.nip.io:18080",
		"unix:///var/run/vedocker.sock",
		"http://127.0.0.1:18080/api",
		"http://user@127.0.0.1:18080",
	} {
		if _, err := newDaemonClient(bad); err == nil {
			t.Errorf("newDaemonClient(%q) succeeded, want an error", bad)
		}
	}
}

func TestValidContainerID(t *testing.T) {
	for _, ok := range []string{"web", "repo-1712345678901234567", "my_app.v2"} {
		if err := validContainerID(ok); err != nil {
			t.Errorf("validContainerID(%q) returned error %v", ok, err)
		}
	}
	for _, bad := range []string{"", "../etc", "a/b", "a..b", "-rf", ".hidden", "a b", "a?b"} {
		if err := validContainerID(bad); err == nil {
			t.Errorf("validContainerID(%q) succeeded, want an error", bad)
		}
	}
}

func TestTailLines(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"", 10, ""},
		{"a\nb\nc\n", 2, "b\nc\n"},
		{"a\nb\nc", 5, "a\nb\nc\n"},
		{"a\nb\nc\n", 1, "c\n"},
	}
	for _, c := range cases {
		if got := tailLines(c.in, c.n); got != c.want {
			t.Errorf("tailLines(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
