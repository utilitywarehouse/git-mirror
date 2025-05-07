package mirror

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/giturl"
)

var matchSpecialCharReg = regexp.MustCompile(`[\\:\/*?"<>|\s]`)
var matchDupUnderscoreReg = regexp.MustCompile(`_+`)

// RepoPoolConfig is the configuration to create repoPool
type RepoPoolConfig struct {
	// default config for all the repositories if not set
	Defaults DefaultConfig `yaml:"defaults"`
	// List of mirrored repositories.
	Repositories []RepositoryConfig `yaml:"repositories"`
}

// DefaultConfig is the default config for repositories if not set at repo level
type DefaultConfig struct {
	// Root is the absolute path to the root dir where all repositories directories
	// will be created all repos worktree links will be created here if not
	// specified in repo config
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
}

// RepositoryConfig represents the config for the mirrored repository
// of the given remote.
type RepositoryConfig struct {
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

// validateDefaults will verify default config
func (rpc *RepoPoolConfig) validateDefaults() error {
	dc := rpc.Defaults

	var errs []error

	if dc.Root != "" {
		if !filepath.IsAbs(dc.Root) {
			errs = append(errs, fmt.Errorf("repository root '%s' must be absolute", dc.Root))
		}
	}

	if dc.LinkRoot != "" {
		if !filepath.IsAbs(dc.LinkRoot) {
			errs = append(errs, fmt.Errorf("repository link_root '%s' must be absolute", dc.Root))
		}
	}

	if dc.Interval != 0 {
		if dc.Interval < minAllowedInterval {
			errs = append(errs, fmt.Errorf("provided interval between mirroring is too sort (%s), must be > %s", dc.Interval, minAllowedInterval))
		}
	}

	if dc.MirrorTimeout != 0 {
		if dc.MirrorTimeout < minAllowedInterval {
			errs = append(errs, fmt.Errorf("provided mirroring timeout is too sort (%s), must be > %s", dc.Interval, minAllowedInterval))
		}
	}

	// if any of the github app config is set all should be set
	if dc.Auth.GithubAppID != "" ||
		dc.Auth.GithubAppInstallationID != "" ||
		dc.Auth.GithubAppPrivateKeyPath != "" {
		if dc.Auth.GithubAppID == "" ||
			dc.Auth.GithubAppInstallationID == "" ||
			dc.Auth.GithubAppPrivateKeyPath == "" {
			errs = append(errs, fmt.Errorf("all of the Github app attribute is required"))
		}
	}

	switch dc.GitGC {
	case "":
	case gcAuto, gcAlways, gcAggressive, gcOff:
	default:
		errs = append(errs, fmt.Errorf("wrong gc value provided, must be one of %s, %s, %s, %s",
			gcAuto, gcAlways, gcAggressive, gcOff))
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", errs)
	}

	return nil
}

// DefaultRepoDir returns path of dir where all repositories mirrors are cloned
func DefaultRepoDir(root string) string {
	return filepath.Join(root, "repo-mirrors")
}

// applyDefaults will add  given default config to repository config if where needed
func (rpc *RepoPoolConfig) applyDefaults() {
	if rpc.Defaults.LinkRoot == "" {
		rpc.Defaults.LinkRoot = rpc.Defaults.Root
	}

	for i := range rpc.Repositories {
		repo := &rpc.Repositories[i]
		if repo.Root == "" {
			repo.Root = rpc.Defaults.Root
		}

		if repo.LinkRoot == "" {
			repo.LinkRoot = rpc.Defaults.LinkRoot
		}

		if repo.Interval == 0 {
			repo.Interval = rpc.Defaults.Interval
		}

		if repo.MirrorTimeout == 0 {
			repo.MirrorTimeout = rpc.Defaults.MirrorTimeout
		}

		if repo.GitGC == "" {
			repo.GitGC = rpc.Defaults.GitGC
		}

		if (repo.Auth == Auth{}) {
			repo.Auth = rpc.Defaults.Auth
		}
	}
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
func (repo *RepositoryConfig) PopulateEmptyLinkPaths() error {
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

// It is possible that same root is used for multiple repositories
// since Links are placed at the root, we need to make sure that all link's
// name (path) are diff.
// validateLinkPaths makes sures all link's absolute paths are different.
func (rpc *RepoPoolConfig) validateLinkPaths() error {
	var errs []error

	absLinks := make(map[string]bool)

	// add defaults before checking abs link paths
	for _, repo := range rpc.Repositories {
		for _, l := range repo.Worktrees {
			absL := absLink(repo.LinkRoot, l.Link)
			if ok := absLinks[absL]; ok {
				errs = append(errs, fmt.Errorf("links with overlapping abs path found name:%s path:%s",
					l.Link, absL))
				continue
			}
			absLinks[absL] = true
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", errs)
	}

	return nil

}

// ValidateAndApplyDefaults will validate link paths and default and apply defaults
func (conf *RepoPoolConfig) ValidateAndApplyDefaults() error {
	if err := conf.validateDefaults(); err != nil {
		return err
	}

	conf.applyDefaults()

	for _, repo := range conf.Repositories {
		if err := repo.PopulateEmptyLinkPaths(); err != nil {
			return err
		}
	}

	if err := conf.validateLinkPaths(); err != nil {
		return err
	}

	return nil
}
