package mirror

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// The repository name can contain
	// ASCII letters, digits, and the characters ., -, and _.

	// user@host.xz:path/to/repo.git
	scpURLRgx = regexp.MustCompile(`^(?P<user>[\w\-\.]+)@(?P<host>([\w\-]+\.?[\w\-]+)+(\:\d+)?):(?P<path>([\w\-\.]+\/)*)(?P<repo>[\w\-\.]+(\.git)?)$`)

	// ssh://user@host.xz[:port]/path/to/repo.git
	sshURLRgx = regexp.MustCompile(`^ssh://(?P<user>[\w\-\.]+)@(?P<host>([\w\-]+\.?[\w\-]+)+(\:\d+)??)/(?P<path>([\w\-\.]+\/)*)(?P<repo>[\w\-\.]+(\.git)?)$`)

	// https://host.xz[:port]/path/to/repo.git
	httpsURLRgx = regexp.MustCompile(`^https://(?P<host>([\w\-]+\.?[\w\-]+)+(\:\d+)?)/(?P<path>([\w\-\.]+\/)*)(?P<repo>[\w\-\.]+(\.git)?)$`)

	// file:///path/to/repo.git
	localURLRgx = regexp.MustCompile(`^file:///(?P<path>([\w\-\.]+\/)*)(?P<repo>[\w\-\.]+(\.git)?)$`)
)

// GitURL represents parsed git url
type GitURL struct {
	scheme string // value will be either 'scp', 'ssh', 'https' or 'local'
	user   string // might be empty for http and local urls
	host   string // host or host:port
	path   string // path to the repo
	repo   string // repository name from the path includes .git
}

// NormaliseURL will return normalised url
func NormaliseURL(rawURL string) string {
	nURL := strings.ToLower(strings.TrimSpace(rawURL))
	nURL = strings.TrimRight(nURL, "/")

	return nURL
}

// ParseGitURL parses a raw url into a GitURL structure.
// valid git urls are...
//   - user@host.xz:path/to/repo.git
//   - ssh://user@host.xz[:port]/path/to/repo.git
//   - https://host.xz[:port]/path/to/repo.git
func ParseGitURL(rawURL string) (*GitURL, error) {
	gURL := &GitURL{}

	rawURL = NormaliseURL(rawURL)

	var sections []string

	switch {
	case isSCPURL(rawURL):
		sections = scpURLRgx.FindStringSubmatch(rawURL)
		gURL.scheme = "scp"
		gURL.user = sections[scpURLRgx.SubexpIndex("user")]
		gURL.host = sections[scpURLRgx.SubexpIndex("host")]
		gURL.path = sections[scpURLRgx.SubexpIndex("path")]
		gURL.repo = sections[scpURLRgx.SubexpIndex("repo")]
	case isSSHURL(rawURL):
		sections = sshURLRgx.FindStringSubmatch(rawURL)
		gURL.scheme = "ssh"
		gURL.user = sections[sshURLRgx.SubexpIndex("user")]
		gURL.host = sections[sshURLRgx.SubexpIndex("host")]
		gURL.path = sections[sshURLRgx.SubexpIndex("path")]
		gURL.repo = sections[sshURLRgx.SubexpIndex("repo")]
	case isHTTPSURL(rawURL):
		sections = httpsURLRgx.FindStringSubmatch(rawURL)
		gURL.scheme = "https"
		gURL.host = sections[httpsURLRgx.SubexpIndex("host")]
		gURL.path = sections[httpsURLRgx.SubexpIndex("path")]
		gURL.repo = sections[httpsURLRgx.SubexpIndex("repo")]
	case isLocalURL(rawURL):
		sections = localURLRgx.FindStringSubmatch(rawURL)
		gURL.scheme = "local"
		gURL.path = sections[localURLRgx.SubexpIndex("path")]
		gURL.repo = sections[localURLRgx.SubexpIndex("repo")]
	default:
		return nil, fmt.Errorf(
			"provided '%s' remote url is invalid, supported urls are 'user@host.xz:path/to/repo.git','ssh://user@host.xz/path/to/repo.git' or 'https://host.xz/path/to/repo.git'",
			rawURL)
	}

	// scp path doesn't have leading "/"
	// also removing training "/" for consistency
	gURL.path = strings.Trim(gURL.path, "/")

	if gURL.path == "" {
		return nil, fmt.Errorf("repo path (org) cannot be empty")
	}
	if gURL.repo == "" || gURL.repo == ".git" {
		return nil, fmt.Errorf("repo name is invalid")
	}

	return gURL, nil
}

// SameURL returns whether or not the two parsed git URLs are equivalent.
// git URLs can be represented in multiple schemes so if host, path and repo name
// of URLs are same then those URLs are for the same remote repository
func SameURL(lURL, rURL *GitURL) bool {
	return lURL.host == rURL.host &&
		lURL.path == rURL.path &&
		lURL.repo == rURL.repo
}

// SameRawURL returns whether or not the two remote URL strings are equivalent
func SameRawURL(lRepo, rRepo string) (bool, error) {
	lURL, err := ParseGitURL(lRepo)
	if err != nil {
		return false, err
	}
	rURL, err := ParseGitURL(rRepo)
	if err != nil {
		return false, err
	}

	return SameURL(lURL, rURL), nil
}

// isSCPURL returns true if supplied URL is scp-like syntax
func isSCPURL(rawURL string) bool {
	return scpURLRgx.MatchString(rawURL)
}

// isSSHURL returns true if supplied URL is SSH URL
func isSSHURL(rawURL string) bool {
	return sshURLRgx.MatchString(rawURL)
}

// isHTTPSURL returns true if supplied URL is HTTPS URL
func isHTTPSURL(rawURL string) bool {
	return httpsURLRgx.MatchString(rawURL)
}

// isLocalURL returns true if supplied URL is HTTPS URL
func isLocalURL(rawURL string) bool {
	return localURLRgx.MatchString(rawURL)
}
