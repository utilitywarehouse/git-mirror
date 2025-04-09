package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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

var (
	defaultRoot = path.Join(os.TempDir(), "git-mirror")

	configSuccess = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "git_mirror_config_last_reload_successful",
		Help: "Whether the last configuration reload attempt was successful.",
	})
	configSuccessTime = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "git_mirror_config_last_reload_success_timestamp_seconds",
		Help: "Timestamp of the last successful configuration reload.",
	})
	allowedRepoPoolConfig = getAllowedKeys(mirror.RepoPoolConfig{})
	allowedDefaults       = getAllowedKeys(mirror.DefaultConfig{})
	allowedAuthKeys       = getAllowedKeys(mirror.Auth{})
	allowedRepoKeys       = getAllowedKeys(mirror.RepositoryConfig{})
	allowedWorktreeKeys   = getAllowedKeys(mirror.WorktreeConfig{})
)

// WatchConfig polls the config file every interval and reloads if modified
func WatchConfig(ctx context.Context, path string, watchConfig bool, interval time.Duration, onChange func(*mirror.RepoPoolConfig) bool) {
	var lastModTime time.Time
	var success bool

	for {
		lastModTime, success = loadConfig(path, lastModTime, onChange)
		if success {
			configSuccess.Set(1)
			configSuccessTime.SetToCurrentTime()
		} else {
			configSuccess.Set(0)
		}

		if !watchConfig {
			return
		}

		t := time.NewTimer(interval)
		select {
		case <-t.C:
		case <-ctx.Done():
			return
		}
	}
}

func loadConfig(path string, lastModTime time.Time, onChange func(*mirror.RepoPoolConfig) bool) (time.Time, bool) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		logger.Error("Error checking config file", "err", err)
		return lastModTime, false
	}

	modTime := fileInfo.ModTime()
	if modTime.Equal(lastModTime) {
		return lastModTime, true
	}

	logger.Info("reloading config file...")

	newConfig, err := parseConfigFile(path)
	if err != nil {
		logger.Error("failed to reload config", "err", err)
		// update modTime to re-evaluate after an update
		return modTime, false
	}
	return modTime, onChange(newConfig)
}

// ensureConfig will do the diff between current repoPool state and new config
// and based on that diff it will add/remove repositories and worktrees
func ensureConfig(repoPool *mirror.RepoPool, newConfig *mirror.RepoPoolConfig) bool {
	success := true

	// add default values
	applyGitDefaults(newConfig)

	// validate and apply defaults to new config before compare
	if err := newConfig.ValidateAndApplyDefaults(); err != nil {
		logger.Error("failed to validate new config", "err", err)
		return false
	}

	newRepos, removedRepos := diffRepositories(repoPool, newConfig)
	for _, repo := range removedRepos {
		if err := repoPool.RemoveRepository(repo); err != nil {
			logger.Error("failed to remove repository", "remote", repo, "err", err)
			success = false
		}
	}
	for _, repo := range newRepos {
		if err := repoPool.AddRepository(repo); err != nil {
			logger.Error("failed to add new repository", "remote", repo.Remote, "err", err)
			success = false
		}
	}

	// find matched repos and check for worktree diffs
	for _, newRepoConf := range newConfig.Repositories {
		repo, err := repoPool.Repository(newRepoConf.Remote)
		if err != nil {
			logger.Error("unable to check worktree changes", "remote", newRepoConf.Remote, "err", err)
			success = false
			continue
		}

		newWTs, removedWTs := diffWorktrees(repo, &newRepoConf)

		// 1st remove then add new in case new one has same link with diff reference
		for _, wt := range removedWTs {
			if err := repoPool.RemoveWorktreeLink(newRepoConf.Remote, wt); err != nil {
				logger.Error("failed to remove worktree", "remote", newRepoConf.Remote, "link", wt, "err", err)
				success = false
			}
		}
		for _, wt := range newWTs {
			if err := repoPool.AddWorktreeLink(newRepoConf.Remote, wt); err != nil {
				logger.Error("failed to add worktree", "remote", newRepoConf.Remote, "link", wt.Link, "err", err)
				success = false
			}
		}
	}

	// start mirror Loop on newly added repos
	repoPool.StartLoop()

	return success
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
		return nil, fmt.Errorf("unable to read file err:%w", err)
	}

	err = validateConfigYaml(yamlFile)
	if err != nil {
		return nil, fmt.Errorf("invalid config err:%w", err)
	}

	conf := &mirror.RepoPoolConfig{}
	err = yaml.Unmarshal(yamlFile, conf)
	if err != nil {
		return nil, fmt.Errorf("unable to decode config err:%w", err)
	}

	return conf, nil
}

func validateConfigYaml(yamlData []byte) error {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(yamlData, &raw); err != nil {
		return fmt.Errorf("unable to decode config err:%w", err)
	}

	// check all root config sections for unexpected keys
	if key := findUnexpectedKey(raw, allowedRepoPoolConfig); key != "" {
		return fmt.Errorf("unexpected key: .%v", key)
	}

	// check ".defaults" if it's not empty
	if raw["defaults"] != nil {
		defaultsMap, ok := raw["defaults"].(map[string]interface{})
		if !ok {
			return fmt.Errorf(".defaults config is not valid")
		}

		if key := findUnexpectedKey(defaultsMap, allowedDefaults); key != "" {
			return fmt.Errorf("unexpected key: .defaults.%v", key)
		}

		// check ".defaults.auth"
		if authMap, ok := defaultsMap["auth"].(map[string]interface{}); ok {
			if key := findUnexpectedKey(authMap, allowedAuthKeys); key != "" {
				return fmt.Errorf("unexpected key: .defaults.auth.%v", key)
			}
		}
	}

	// skip further config checks if ".repositories" is empty
	if raw["repositories"] == nil {
		return nil
	}

	// check ".repositories"
	reposInterface, ok := raw["repositories"].([]interface{})
	if !ok {
		return fmt.Errorf(".repositories config must be an array")
	}

	// check each repository in ".repositories"
	for _, repoInterface := range reposInterface {
		repoMap, ok := repoInterface.(map[string]interface{})
		if !ok {
			return fmt.Errorf(".repositories config is not valid")
		}

		if key := findUnexpectedKey(repoMap, allowedRepoKeys); key != "" {
			return fmt.Errorf("unexpected key: .repositories[%v].%v", repoMap["remote"], key)
		}

		// skip further repository checks if "worktrees" is empty
		if repoMap["worktrees"] == nil {
			continue
		}

		// check "worktrees" in each repository
		worktreesInterface, ok := repoMap["worktrees"].([]interface{})
		if !ok {
			return fmt.Errorf("worktrees config must be an array in .repositories[%v]", repoMap["remote"])
		}

		for i, worktreeInterface := range worktreesInterface {
			worktreeMap, ok := worktreeInterface.(map[string]interface{})
			if !ok {
				return fmt.Errorf("worktrees config is not valid in .repositories[%v]", repoMap["remote"])
			}

			if key := findUnexpectedKey(worktreeMap, allowedWorktreeKeys); key != "" {
				return fmt.Errorf("unexpected key: .repositories[%v].worktrees[%v].%v", repoMap["remote"], i, key)
			}

			// Check "pathspecs" in each worktree
			if pathspecsInterface, exists := worktreeMap["pathspecs"]; exists {
				if _, ok := pathspecsInterface.([]interface{}); !ok {
					return fmt.Errorf("pathspecs config must be an array in .repositories[%v].worktrees[%v]", repoMap["remote"], i)
				}
			}
		}
	}

	return nil
}

// getAllowedKeys retrieves a list of allowed keys from the specified struct
func getAllowedKeys(config interface{}) []string {
	var allowedKeys []string
	val := reflect.ValueOf(config)
	typ := reflect.TypeOf(config)

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		yamlTag := field.Tag.Get("yaml")
		if yamlTag != "" {
			allowedKeys = append(allowedKeys, yamlTag)
		}
	}
	return allowedKeys
}

func findUnexpectedKey(raw map[string]interface{}, allowedKeys []string) string {
	for key := range raw {
		if !slices.Contains(allowedKeys, key) {
			return key
		}
	}

	return ""
}

// diffRepositories will do the diff between current state and new config and
// return new repositories config and list of remote url which are not found in config
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

// diffWorktrees will do the diff between current repo's worktree state and new worktree config
// it will return new worktree configs and link names of the link not found in new config
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
