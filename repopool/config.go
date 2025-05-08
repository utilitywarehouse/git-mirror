package repopool

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/utilitywarehouse/git-mirror/internal/utils"
	"github.com/utilitywarehouse/git-mirror/repository"
)

// Config is the configuration to create repoPool
type Config struct {
	// default config for all the repositories if not set
	Defaults DefaultConfig `yaml:"defaults"`
	// List of mirrored repositories.
	Repositories []repository.Config `yaml:"repositories"`
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
	Auth repository.Auth `yaml:"auth"`
}

// validateDefaults will verify default config
func (rpc *Config) validateDefaults() error {
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
		if dc.Interval < repository.MinAllowedInterval {
			errs = append(errs, fmt.Errorf("provided interval between mirroring is too sort (%s), must be > %s", dc.Interval, repository.MinAllowedInterval))
		}
	}

	if dc.MirrorTimeout != 0 {
		if dc.MirrorTimeout < repository.MinAllowedInterval {
			errs = append(errs, fmt.Errorf("provided mirroring timeout is too sort (%s), must be > %s", dc.Interval, repository.MinAllowedInterval))
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
	case repository.GCAuto, repository.GCAlways, repository.GCAggressive, repository.GCOff:
	default:
		errs = append(errs, fmt.Errorf("wrong gc value provided, must be one of %s, %s, %s, %s",
			repository.GCAuto, repository.GCAlways, repository.GCAggressive, repository.GCOff))
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", errs)
	}

	return nil
}

// applyDefaults will add  given default config to repository config if where needed
func (rpc *Config) applyDefaults() {
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

		if (repo.Auth == repository.Auth{}) {
			repo.Auth = rpc.Defaults.Auth
		}
	}
}

// It is possible that same root is used for multiple repositories
// since Links are placed at the root, we need to make sure that all link's
// name (path) are diff.
// validateLinkPaths makes sures all link's absolute paths are different.
func (rpc *Config) validateLinkPaths() error {
	var errs []error

	absLinks := make(map[string]bool)

	// add defaults before checking abs link paths
	for _, repo := range rpc.Repositories {
		for _, l := range repo.Worktrees {
			absL := utils.AbsLink(repo.LinkRoot, l.Link)
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
func (conf *Config) ValidateAndApplyDefaults() error {
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
