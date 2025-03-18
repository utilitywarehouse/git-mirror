package main

import (
	"context"
	"errors"
	"os"
	"path"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/giturl"
	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	"gopkg.in/yaml.v3"
)

const (
	defaultGitGC             = "always"
	defaultInterval          = 30 * time.Second
	defaultMirrorTimeout     = 2 * time.Minute
	defaultSSHKeyPath        = "/etc/git-secret/ssh"
	defaultSSHKnownHostsPath = "/etc/git-secret/known_hosts"
)

var defaultRoot = path.Join(os.TempDir(), "git-mirror", "src")

// WatchConfig polls the config file every interval and reloads if modified
func WatchConfig(ctx context.Context, path string, interval time.Duration, onChange func(*mirror.RepoPoolConfig)) {
	var lastModTime time.Time

	for {
		fileInfo, err := os.Stat(path)
		if err != nil {
			logger.Error("Error checking config file", "err", err)
			time.Sleep(interval) // retry after given interval
			continue
		}

		modTime := fileInfo.ModTime()
		if modTime.After(lastModTime) {
			logger.Info("reloading config file...")
			lastModTime = modTime

			newConfig, err := parseConfigFile(path)
			if err != nil {
				logger.Error("failed to reload config", "err", err)
			} else {
				onChange(newConfig)
			}
		}

		t := time.NewTimer(interval)
		select {
		case <-t.C:
		case <-ctx.Done():
			return
		}
	}
}

func ensureConfig(repoPool *mirror.RepoPool, newConfig *mirror.RepoPoolConfig) {

	// add default values
	applyGitDefaults(newConfig)

	// validate and apply defaults to new config before compare
	if err := newConfig.ValidateAndApplyDefaults(); err != nil {
		logger.Error("failed to validate new config", "err", err)
		return
	}

	newRepos, removedRepos := diffRepositories(repoPool, newConfig)
	for _, repo := range removedRepos {
		if err := repoPool.RemoveRepository(repo); err != nil {
			logger.Error("failed to remove repository", "remote", repo, "err", err)
		}
	}
	for _, repo := range newRepos {
		if err := repoPool.AddRepository(repo); err != nil {
			logger.Error("failed to add new repository", "remote", repo.Remote, "err", err)
		}
	}

	// find matched repos and check for worktree diffs
	for _, newRepoConf := range newConfig.Repositories {
		repo, err := repoPool.Repository(newRepoConf.Remote)
		if err != nil {
			continue
		}

		newWTs, removedWTs := diffWorktrees(repo, &newRepoConf)

		// 1st remove then add new in case new one has same link with diff reference
		for _, wt := range removedWTs {
			if err := repoPool.RemoveWorktreeLink(newRepoConf.Remote, wt); err != nil {
				logger.Error("failed to remove worktree", "remote", newRepoConf.Remote, "link", wt, "err", err)
			}
		}
		for _, wt := range newWTs {
			if err := repoPool.AddWorktreeLink(newRepoConf.Remote, wt); err != nil {
				logger.Error("failed to add worktree", "remote", newRepoConf.Remote, "link", wt.Link, "err", err)
			}
		}
	}
}

func applyGitDefaults(mirrorConf *mirror.RepoPoolConfig) {
	if mirrorConf.Defaults.Root == "" {
		mirrorConf.Defaults.Root = defaultRoot
	}

	if mirrorConf.Defaults.GitGC == "" {
		mirrorConf.Defaults.GitGC = defaultGitGC
	}

	if mirrorConf.Defaults.Interval == 0 {
		mirrorConf.Defaults.Interval = defaultInterval
	}

	if mirrorConf.Defaults.MirrorTimeout == 0 {
		mirrorConf.Defaults.MirrorTimeout = defaultMirrorTimeout
	}

	if mirrorConf.Defaults.Auth.SSHKeyPath == "" {
		mirrorConf.Defaults.Auth.SSHKeyPath = defaultSSHKeyPath
	}

	if mirrorConf.Defaults.Auth.SSHKnownHostsPath == "" {
		mirrorConf.Defaults.Auth.SSHKnownHostsPath = defaultSSHKnownHostsPath
	}
}

func parseConfigFile(path string) (*mirror.RepoPoolConfig, error) {
	yamlFile, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	conf := &mirror.RepoPoolConfig{}
	err = yaml.Unmarshal(yamlFile, conf)
	if err != nil {
		return nil, err
	}
	return conf, nil
}

func diffRepositories(repoPool *mirror.RepoPool, newConfig *mirror.RepoPoolConfig) (
	newRepos []mirror.RepositoryConfig,
	removedRepos []string,
) {
	for _, newRepo := range newConfig.Repositories {
		if _, err := repoPool.Repository(newRepo.Remote); errors.Is(err, mirror.ErrNotExist) {
			newRepos = append(newRepos, newRepo)
		}
	}

	for _, currentRepoURL := range repoPool.RepositoriesRemote() {
		var found bool
		for _, newRepo := range newConfig.Repositories {
			if currentRepoURL == giturl.NormaliseURL(newRepo.Remote) {
				found = true
				break
			}
		}
		if !found {
			removedRepos = append(removedRepos, currentRepoURL)
		}
	}

	return
}

func diffWorktrees(repo *mirror.Repository, newRepoConf *mirror.RepositoryConfig) (
	newWTCs []mirror.WorktreeConfig,
	removedWTs []string,
) {
	currentWTLinks := repo.WorktreeLinks()

	for _, newWTC := range newRepoConf.Worktrees {
		if _, ok := currentWTLinks[newWTC.Link]; !ok {
			newWTCs = append(newWTCs, newWTC)
		}
	}

	// for existing worktree
	for cLink, wt := range currentWTLinks {
		var found bool
		for _, newWTC := range newRepoConf.Worktrees {
			if newWTC.Link == cLink {
				// wt link name is matching so make sure other
				// config match as well if not replace it
				if !wt.Equals(newWTC) {
					newWTCs = append(newWTCs, newWTC)
					break
				}
				found = true
				break
			}
		}
		if !found {
			removedWTs = append(removedWTs, cLink)
		}
	}

	return
}
