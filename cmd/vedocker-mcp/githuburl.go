package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	// GitHub usernames and org names: alphanumerics and single hyphens, no
	// leading or trailing hyphen, at most 39 characters.
	githubOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9]|-[A-Za-z0-9]){0,38}$`)
	githubRepoPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// normalizeGitHubURL accepts the ways people paste a GitHub repo link and
// returns https://github.com/<owner>/<repo>, the only form the daemon accepts.
// Anything that is not a github.com repo is rejected.
func normalizeGitHubURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("github_url is required")
	}
	if strings.ContainsAny(s, " \t\r\n\\") {
		return "", fmt.Errorf("github_url must not contain whitespace or backslashes")
	}

	// git@github.com:owner/repo.git
	if rest, ok := strings.CutPrefix(s, "git@github.com:"); ok {
		s = "https://github.com/" + rest
	}

	if !strings.Contains(s, "://") {
		s = "https://" + s
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("github_url is not a valid URL: %v", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("github_url must be an https://github.com URL, got scheme %q", u.Scheme)
	}
	if u.User != nil {
		return "", fmt.Errorf("github_url must not contain credentials")
	}
	if u.Port() != "" {
		return "", fmt.Errorf("github_url must not contain a port")
	}

	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return "", fmt.Errorf("only github.com repositories are supported, got host %q", u.Hostname())
	}

	segments := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return "", fmt.Errorf("github_url must look like https://github.com/<owner>/<repo>")
	}

	owner := segments[0]
	repo := strings.TrimSuffix(segments[1], ".git")

	// Extra segments are only allowed when they point inside the same repo,
	// such as /tree/main or /blob/main/README.md.
	if len(segments) > 2 {
		switch segments[2] {
		case "tree", "blob", "commit", "commits", "issues", "pulls", "pull", "releases", "actions", "wiki":
		default:
			return "", fmt.Errorf("github_url must point at a repository, not %q", u.Path)
		}
	}

	if !githubOwnerPattern.MatchString(owner) {
		return "", fmt.Errorf("invalid GitHub owner %q", owner)
	}
	if !githubRepoPattern.MatchString(repo) || repo == "." || repo == ".." {
		return "", fmt.Errorf("invalid GitHub repository name %q", repo)
	}

	return fmt.Sprintf("https://github.com/%s/%s", owner, repo), nil
}
