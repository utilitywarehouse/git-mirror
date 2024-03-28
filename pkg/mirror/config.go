package mirror

import (
	"fmt"
	"path/filepath"
	"time"
)

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
	// if path is not absolute it will be created under repository root
	Link string `yaml:"link"`

	// Ref represents the git reference of the worktree branch, tags or hash
	// are supported. default is HEAD
	Ref string `yaml:"ref"`

	// Pathspec of the dirs to checkout if required
	Pathspec string `yaml:"pathspec"`
}

// Auth represents authentication config of the repository
type Auth struct {
	// path to the ssh key used to fetch remote
	SSHKeyPath string `yaml:"ssh_key_path"`

	// path to the known hosts of the remote host
	SSHKnownHostsPath string `yaml:"ssh_known_hosts_path"`
}

// ValidateDefaults will verify default config
func (rpc *RepoPoolConfig) ValidateDefaults() error {
	dc := rpc.Defaults

	var errs []error

	if dc.Root != "" {
		if !filepath.IsAbs(dc.Root) {
			errs = append(errs, fmt.Errorf("repository root '%s' must be absolute", dc.Root))
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

// ApplyDefaults will add  given default config to repository config if where needed
func (rpc *RepoPoolConfig) ApplyDefaults() {
	for i := range rpc.Repositories {
		repo := &rpc.Repositories[i]
		if repo.Root == "" {
			repo.Root = rpc.Defaults.Root
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

// It is possible that same root is used for multiple repositories
// since Links are placed at the root, we need to make sure that all link's
// name (path) are diff.
// ValidateLinkPaths makes sures all link's absolute paths are different.
func (rpc *RepoPoolConfig) ValidateLinkPaths() error {
	var errs []error

	absLinks := make(map[string]bool)

	rpc.ApplyDefaults()

	// add defaults before checking abs link paths
	for _, repo := range rpc.Repositories {
		for _, l := range repo.Worktrees {
			absL := absLink(repo.Root, l.Link)
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

// gitSSHCommand returns the environment variable to be used for configuring
// git over ssh.
func (a Auth) gitSSHCommand() string {
	sshKeyPath := a.SSHKeyPath
	if sshKeyPath == "" {
		sshKeyPath = "/dev/null"
	}
	knownHostsOptions := "-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no"
	if a.SSHKeyPath != "" && a.SSHKnownHostsPath != "" {
		knownHostsOptions = fmt.Sprintf("-o UserKnownHostsFile=%s", a.SSHKnownHostsPath)
	}
	return fmt.Sprintf(`GIT_SSH_COMMAND=ssh -q -F none -o IdentitiesOnly=yes -o IdentityFile=%s %s`, sshKeyPath, knownHostsOptions)
}
