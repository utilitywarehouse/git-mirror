package repository

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/utilitywarehouse/git-mirror/giturl"
)

var matchSpecialCharReg = regexp.MustCompile(`[\\:\/*?"<>|\s]`)
var matchDupUnderscoreReg = regexp.MustCompile(`_+`)

// Config represents the config for the mirrored repository
// of the given remote.
type Config struct {
	// git URL of the remote repo to mirror
	Remote string `yaml:"remote"`

	// Root is the absolute path to the root dir where repo dir
	// will be created. Worktree links will be created here if
	// absolute path is not provided
	Root string `yaml:"root"`

	// LinkRoot is the absolute path to the dir which is the root for the worktree links
	// if link is a relative path it will be relative to this dir
	// if link is not specified it will be constructed from repo name and worktree ref
	// and it will be placed in this dir
	// if not specified it will be same as root
	LinkRoot string `yaml:"link_root"`

	// Interval is time duration for how long to wait between mirrors
	Interval time.Duration `yaml:"interval"`

	// MirrorTimeout represents the total time allowed for the complete mirror loop
	MirrorTimeout time.Duration `yaml:"mirror_timeout"`

	// GitGC garbage collection string. valid values are
	// 'auto', 'always', 'aggressive' or 'off'
	GitGC string `yaml:"git_gc"`

	// Auth config to fetch remote repos
	Auth Auth `yaml:"auth"`

	// Worktrees contains list of worktrees links which will be maintained.
	// worktrees are optional repo can be mirrored without worktree
	Worktrees []WorktreeConfig `yaml:"worktrees"`
}

// Worktree represents maintained worktree on given link.
type WorktreeConfig struct {
	// Link is the path at which to create a symlink to the worktree dir
	// if path is not absolute it will be created under repository link_root
	// if link is not specified it will be constructed from repo name and worktree ref
	// and it will be placed in link_root dir
	Link string `yaml:"link"`

	// Ref represents the git reference of the worktree branch, tags or hash
	// are supported. default is HEAD
	Ref string `yaml:"ref"`

	// Pathspecs of the dirs to checkout if required
	Pathspecs []string `yaml:"pathspecs"`
}

// Auth represents authentication config of the repository
type Auth struct {
	// username to use for basic or token based authentication
	Username string `yaml:"username"`

	// password or personal access token to use for authentication
	Password string `yaml:"password"`

	// SSH Details
	// path to the ssh key used to fetch remote
	SSHKeyPath string `yaml:"ssh_key_path"`

	// path to the known hosts of the remote host
	SSHKnownHostsPath string `yaml:"ssh_known_hosts_path"`

	// Github APP Details
	// The application id or the client ID of the Github app
	GithubAppID string `yaml:"github_app_id"`
	// The installation id of the app (in the organization).
	GithubAppInstallationID string `yaml:"github_app_installation_id"`
	// path to the github app private key
	GithubAppPrivateKeyPath string `yaml:"github_app_private_key_path"`
}

// DefaultRepoDir returns path of dir where all repositories mirrors are cloned
func DefaultRepoDir(root string) string {
	return filepath.Join(root, "repo-mirrors")
}

func normaliseReference(ref string) string {
	ref = strings.TrimSpace(ref)
	// remove special char not allowed in file name
	ref = matchSpecialCharReg.ReplaceAllString(ref, "_")
	ref = matchDupUnderscoreReg.ReplaceAllString(ref, "_")
	return ref
}

func generateLink(remote, ref string) (string, error) {
	gitURL, err := giturl.Parse(remote)
	if err != nil {
		return "", err
	}
	normalisedRef := normaliseReference(ref)

	// reject ref with all special char and . and .. has special meaning
	if normalisedRef == "_" ||
		normalisedRef == "." || normalisedRef == ".." {
		return "", fmt.Errorf("reference cant be normalised")
	}

	// if reference is an hash then shorter version can be used as link path
	if IsFullCommitHash(normalisedRef) {
		normalisedRef = normalisedRef[:7]
	}

	return filepath.Join(strings.TrimRight(gitURL.Repo, ".git"), normalisedRef), nil
}

// PopulateEmptyLinkPaths will try and generate missing link paths
func (repo *Config) PopulateEmptyLinkPaths() error {
	for i := range repo.Worktrees {
		if repo.Worktrees[i].Link != "" {
			continue
		}
		if repo.Worktrees[i].Ref == "" {
			repo.Worktrees[i].Ref = "HEAD"
		}
		link, err := generateLink(repo.Remote, repo.Worktrees[i].Ref)
		if err != nil {
			return err
		}
		repo.Worktrees[i].Link = link
	}
	return nil
}
