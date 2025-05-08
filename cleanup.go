package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/utilitywarehouse/git-mirror/internal/utils"
	"github.com/utilitywarehouse/git-mirror/repopool"
	"github.com/utilitywarehouse/git-mirror/repository"
)

// cleanupOrphanedRepos deletes directory of the repos from the default root
// which are no longer referenced in config and it was removed while app was down.
// Any removal while app is running is already handled by ensureConfig() hence
// this function should be called once
// this is best effort clean up as orphaned published link will not be clean up
// as its not known where it was published.
func cleanupOrphanedRepos(config *repopool.Config, repoPool *repopool.RepoPool) {
	// if default root is not set repos might not be located in same dir
	if config.Defaults.Root == "" {
		return
	}

	repoDirs := repoPool.RepositoriesDirPath()
	defaultRepoDirRoot := repository.DefaultRepoDir(config.Defaults.Root)

	entries, err := os.ReadDir(defaultRepoDirRoot)
	if err != nil {
		logger.Error("unable to read root dir for clean up", "err", err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(defaultRepoDirRoot, entry.Name())

		if slices.Contains(repoDirs, fullPath) {
			continue
		}

		// since git-mirror creates bare repository for mirror
		// non-repo dir or non-bare repo dir must be skipped
		ok, err := isBareRepo(fullPath)
		if err != nil {
			logger.Error("unable to check if bare repo", "path", fullPath, "err", err)
			continue
		}

		if !ok {
			continue
		}

		logger.Info("removing orphaned repo dir...", "path", fullPath)
		if err := os.RemoveAll(fullPath); err != nil {
			logger.Error("unable orphaned repo dir", "path", fullPath, "err", err)
			continue
		}
	}
}

func isInsideGitDir(cwd string) bool {
	// err is expected here
	output, _ := runGitCommand(cwd, "rev-parse", "--is-inside-git-dir")
	return output == "true"
}

func isBareRepo(cwd string) (bool, error) {
	// bare repository doesn't have worktrees
	if !isInsideGitDir(cwd) {
		return false, nil
	}

	output, err := runGitCommand(cwd, "rev-parse", "--is-bare-repository")
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(output)
}

// runGitCommand runs git command with given arguments on given CWD
func runGitCommand(cwd string, args ...string) (string, error) {
	output, err := utils.RunCommand(context.TODO(), logger, nil, cwd, gitExecutablePath, args...)
	return strings.TrimSpace(output), err
}
