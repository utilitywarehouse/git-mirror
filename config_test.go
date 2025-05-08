package main

import (
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/git-mirror/repopool"
	"github.com/utilitywarehouse/git-mirror/repository"
)

func Test_diffRepositories(t *testing.T) {
	tests := []struct {
		name             string
		initialConfig    *repopool.Config
		newConfig        *repopool.Config
		wantNewRepos     []repository.Config
		wantRemovedRepos []string
	}{
		{
			name:          "empty",
			initialConfig: &repopool.Config{},
			newConfig: &repopool.Config{
				Defaults: repopool.DefaultConfig{Root: "/root"},
				Repositories: []repository.Config{
					{Remote: "user@host.xz:path/to/repo1.git"},
					{Remote: "user@host.xz:path/to/repo2.git"},
				},
			},
			wantNewRepos: []repository.Config{
				{Remote: "user@host.xz:path/to/repo1.git"},
				{Remote: "user@host.xz:path/to/repo2.git"},
			},
			wantRemovedRepos: nil,
		},
		{
			name: "replace_repo2_repo3",
			initialConfig: &repopool.Config{
				Defaults: repopool.DefaultConfig{Root: "/root", Interval: 10 * time.Second},
				Repositories: []repository.Config{
					{Remote: "user@host.xz:path/to/repo1.git"},
					{Remote: "user@host.xz:path/to/repo2.git"},
				},
			},
			newConfig: &repopool.Config{
				Defaults: repopool.DefaultConfig{Root: "/root"},
				Repositories: []repository.Config{
					{Remote: "user@host.xz:path/to/repo1.git"},
					{
						Remote:        "user@host.xz:path/to/repo3.git",
						Root:          "/another-root",
						Interval:      2 * time.Second,
						MirrorTimeout: 4 * time.Second,
						GitGC:         "off",
						Auth:          repository.Auth{SSHKeyPath: "/another/path/to/key"},
					},
				},
			},
			wantNewRepos: []repository.Config{
				{
					Remote:        "user@host.xz:path/to/repo3.git",
					Root:          "/another-root",
					Interval:      2 * time.Second,
					MirrorTimeout: 4 * time.Second,
					GitGC:         "off",
					Auth:          repository.Auth{SSHKeyPath: "/another/path/to/key"},
				},
			},
			wantRemovedRepos: []string{"user@host.xz:path/to/repo2.git"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyGitDefaults(tt.initialConfig)
			repoPool, err := repopool.New(t.Context(), *tt.initialConfig, nil, "", nil)
			if err != nil {
				t.Fatalf("could not create git mirror pool err:%v", err)
			}

			gotNewRepos, gotRemovedRepos := diffRepositories(repoPool, tt.newConfig)
			if diff := cmp.Diff(gotNewRepos, tt.wantNewRepos); diff != "" {
				t.Errorf("diffRepositories() NewRepos mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(gotRemovedRepos, tt.wantRemovedRepos); diff != "" {
				t.Errorf("diffRepositories() RemovedRepos mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_diffWorktrees(t *testing.T) {
	tests := []struct {
		name            string
		initialRepoConf *repository.Config
		newRepoConf     *repository.Config
		wantNewWTCs     []repository.WorktreeConfig
		wantRemovedWTs  []string
	}{
		{
			name: "no_worktree",
			initialRepoConf: &repository.Config{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
			},
			newRepoConf: &repository.Config{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []repository.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspecs: nil},
					{Link: "link2", Ref: "other-branch", Pathspecs: []string{"path1", "path2/**/*.yaml", "*.c"}},
					{Link: "", Ref: "master", Pathspecs: nil},
				},
			},
			wantNewWTCs: []repository.WorktreeConfig{
				{Link: "", Ref: "master", Pathspecs: nil},
				{Link: "link", Ref: "master"},
				{Link: "link2", Ref: "other-branch", Pathspecs: []string{"path1", "path2/**/*.yaml", "*.c"}},
			},
			wantRemovedWTs: nil,
		},
		{
			name: "replace_link_ref_path",
			initialRepoConf: &repository.Config{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []repository.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspecs: nil},
					{Link: "link2", Ref: "other-branch", Pathspecs: []string{"path1", "path2/**/*.yaml", "*.c"}},
					{Link: "link3", Ref: "other-branch", Pathspecs: []string{"path"}},
					{Link: "", Ref: "master", Pathspecs: nil},
				},
			},
			newRepoConf: &repository.Config{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []repository.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspecs: []string{"new-path"}},
					{Link: "link2", Ref: "new-branch", Pathspecs: []string{"path1", "path2/**/*.yaml", "*.c"}},
					{Link: "link3", Ref: "other-branch", Pathspecs: []string{"path", "new-path"}},
					{Link: "", Ref: "new-branch", Pathspecs: nil},
				},
			},
			wantNewWTCs: []repository.WorktreeConfig{
				{Link: "", Ref: "new-branch", Pathspecs: nil},
				{Link: "link", Ref: "master", Pathspecs: []string{"new-path"}},
				{Link: "link2", Ref: "new-branch", Pathspecs: []string{"path1", "path2/**/*.yaml", "*.c"}},
				{Link: "link3", Ref: "other-branch", Pathspecs: []string{"path", "new-path"}},
			},
			wantRemovedWTs: []string{"link", "link2", "link3", "repo1/master"},
		},
		{
			name: "rearrange-path",
			initialRepoConf: &repository.Config{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []repository.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspecs: []string{"a", "b/**/c"}},
					{Link: "link2", Ref: "other-branch", Pathspecs: []string{"path1", "path2/**/*.yaml", "*.c"}},
				},
			},
			newRepoConf: &repository.Config{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []repository.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspecs: []string{"b/**/c", "a"}},
					{Link: "link2", Ref: "other-branch", Pathspecs: []string{"path1", "*.c", "path2/**/*.yaml"}},
				},
			},
			wantNewWTCs:    nil,
			wantRemovedWTs: nil,
		},
		{
			name: "add_new_link",
			initialRepoConf: &repository.Config{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []repository.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspecs: nil},
					{Link: "link2", Ref: "other-branch", Pathspecs: []string{"path1", "path2/**/*.yaml", "*.c"}},
				},
			},
			newRepoConf: &repository.Config{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []repository.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspecs: nil},
					{Link: "link3", Ref: "other-branch", Pathspecs: []string{"path1", "path2/**/*.yaml", "*.c"}},
					{Link: "", Ref: "master", Pathspecs: nil},
				},
			},
			wantNewWTCs: []repository.WorktreeConfig{
				{Ref: "master"},
				{Link: "link3", Ref: "other-branch", Pathspecs: []string{"path1", "path2/**/*.yaml", "*.c"}},
			},
			wantRemovedWTs: []string{"link2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.initialRepoConf.PopulateEmptyLinkPaths(); err != nil {
				t.Fatalf("failed to create repo error = %v", err)
			}

			repo, err := repository.New(*tt.initialRepoConf, "", nil, slog.Default())
			if err != nil {
				t.Fatalf("failed to create repo error = %v", err)
			}

			gotNewWTCs, gotRemovedWTs := diffWorktrees(repo, tt.newRepoConf)

			// since these slices are based on map of worktrees order of elements
			// differs between runs
			slices.SortFunc(gotNewWTCs, func(a, b repository.WorktreeConfig) int {
				switch {
				case a.Link > b.Link:
					return 1
				case a.Link == b.Link:
					return 0
				default:
					return -1
				}
			})
			slices.Sort(gotRemovedWTs)

			if diff := cmp.Diff(gotNewWTCs, tt.wantNewWTCs); diff != "" {
				t.Errorf("diffWorktrees() NewWTCs mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(gotRemovedWTs, tt.wantRemovedWTs); diff != "" {
				t.Errorf("diffWorktrees() RemovedWTs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_validateConfigYaml(t *testing.T) {
	tests := []struct {
		name      string
		yamlData  []byte
		wantError bool
	}{
		{
			name: "valid - full config",
			yamlData: []byte(`
defaults:
  root: /tmp/git-mirror
  link_root: /tmp/links
  interval: 30s
  mirror_timeout: 2m
  git_gc: always
  auth:
    ssh_key_path: /etc/git-secret/ssh
    ssh_known_hosts_path: /etc/git-secret/known_hosts

repositories:
  - remote: https://github.com/utilitywarehouse/git-mirror
    worktrees:
      - link: aaa
        ref: main
      - link: bbb
        ref: main
  - remote: https://github.com/utilitywarehouse/another-repo
    root: /some/other/location
    link_root: /some/path
    interval: 1m
    mirror_timeout: 5m
    git_gc: always
    auth:
      ssh_key_path: /some/other/location
      ssh_known_hosts_path: /some/other/location
    worktrees:
      - link: alerts
        ref: main
        pathspecs:
          - path
          - path2/*.yaml
`),
			wantError: false,
		},
		{
			name: "valid - empty config",
			yamlData: []byte(`
`),
			wantError: false,
		},
		{
			name: "valid - defaults config only",
			yamlData: []byte(`
defaults:
`),
			wantError: false,
		},
		{
			name: "valid - repositories config only",
			yamlData: []byte(`
repositories:
`),
			wantError: false,
		},
		{
			name: "invalid - unexpected key",
			yamlData: []byte(`
not-valid:
  test: test

defaults:
  root: /tmp/git-mirror

repositories:
  - remote: https://github.com/utilitywarehouse/git-mirror
`),
			wantError: true,
		},
		{
			name: "invalid - unexpected key in defaults",
			yamlData: []byte(`
defaults:
  root: /tmp/git-mirror
  not_valid: test

repositories:
  - remote: https://github.com/utilitywarehouse/git-mirror
`),
			wantError: true,
		},
		{
			name: "invalid - unexpected key in auth",
			yamlData: []byte(`
defaults:
  root: /tmp/git-mirror
  auth:
    not_valid: test

repositories:
  - remote: https://github.com/utilitywarehouse/git-mirror
`),
			wantError: true,
		},
		{
			name: "invalid - unexpected key in repositories",
			yamlData: []byte(`
defaults:
  root: /tmp/git-mirror

repositories:
  - remote: https://github.com/utilitywarehouse/git-mirror
    not_valid: test
`),
			wantError: true,
		},
		{
			name: "invalid - unexpected key in repository worktrees",
			yamlData: []byte(`
defaults:
  root: /tmp/git-mirror

repositories:
  - remote: https://github.com/utilitywarehouse/git-mirror
    worktrees:
      - link: aaa
        not_valid: test
`),
			wantError: true,
		},
		{
			name: "invalid - repositories is not an array",
			yamlData: []byte(`
defaults:
  root: /tmp/git-mirror

repositories: https://github.com/utilitywarehouse/git-mirror
`),
			wantError: true,
		},
		{
			name: "invalid - worktrees is not an array",
			yamlData: []byte(`
defaults:
  root: /tmp/git-mirror

repositories:
  - remote: https://github.com/utilitywarehouse/git-mirror
    worktrees: test
`),
			wantError: true,
		},
		{
			name: "invalid - pathspecs is not an array",
			yamlData: []byte(`
defaults:
  root: /tmp/git-mirror

repositories:
  - remote: https://github.com/utilitywarehouse/git-mirror
    worktrees:
      - link: aaa
        not_valid: test
        pathspecs: readme.md
`),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfigYaml(tt.yamlData)
			if (err != nil) != tt.wantError {
				t.Errorf("validateConfigYaml() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
