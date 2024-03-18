package mirror

import (
	"fmt"
	"path/filepath"
	"time"
)

type RepoPoolConfig struct {
	Defaults     DefaultConfig      `yaml:"defaults"`
	Repositories []RepositoryConfig `yaml:"repositories"`
}

type DefaultConfig struct {
	Root     string        `yaml:"root"`
	Interval time.Duration `yaml:"interval"`
	GitGC    string        `yaml:"git_gc"`
	Auth     Auth          `yaml:"auth"`
}

type RepositoryConfig struct {
	Remote    string           `yaml:"remote"`
	Root      string           `yaml:"root"`
	Interval  time.Duration    `yaml:"interval"`
	GitGC     string           `yaml:"git_gc"`
	Auth      Auth             `yaml:"auth"`
	Worktrees []WorktreeConfig `yaml:"worktrees"`
}

type WorktreeConfig struct {
	Link     string `yaml:"link"`
	Ref      string `yaml:"ref"`
	Pathspec string `yaml:"pathspec"`
}

type Auth struct {
	SSHKeyPath        string `yaml:"ssh_key_path"`
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

		if repo.GitGC == "" {
			repo.GitGC = rpc.Defaults.GitGC
		}

		if (repo.Auth == Auth{}) {
			repo.Auth = rpc.Defaults.Auth
		}
	}
}

// is it possible that same root is used for multiple repositories
// since Links are placed at the root, we need to make sure that all link name/path are diff
// ie all links absolute path should be different
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
