package main

import (
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
)

func Test_diffRepositories(t *testing.T) {

	tests := []struct {
		name             string
		initialConfig    *mirror.RepoPoolConfig
		newConfig        *mirror.RepoPoolConfig
		wantNewRepos     []mirror.RepositoryConfig
		wantRemovedRepos []string
	}{
		{
			name:          "empty",
			initialConfig: &mirror.RepoPoolConfig{},
			newConfig: &mirror.RepoPoolConfig{
				Defaults: mirror.DefaultConfig{Root: "/root"},
				Repositories: []mirror.RepositoryConfig{
					{Remote: "user@host.xz:path/to/repo1.git"},
					{Remote: "user@host.xz:path/to/repo2.git"},
				},
			},
			wantNewRepos: []mirror.RepositoryConfig{
				{Remote: "user@host.xz:path/to/repo1.git"},
				{Remote: "user@host.xz:path/to/repo2.git"},
			},
			wantRemovedRepos: nil,
		},
		{
			name: "replace_repo2_repo3",
			initialConfig: &mirror.RepoPoolConfig{
				Defaults: mirror.DefaultConfig{Root: "/root", Interval: 10 * time.Second},
				Repositories: []mirror.RepositoryConfig{
					{Remote: "user@host.xz:path/to/repo1.git"},
					{Remote: "user@host.xz:path/to/repo2.git"},
				},
			},
			newConfig: &mirror.RepoPoolConfig{
				Defaults: mirror.DefaultConfig{Root: "/root"},
				Repositories: []mirror.RepositoryConfig{
					{Remote: "user@host.xz:path/to/repo1.git"},
					{
						Remote:        "user@host.xz:path/to/repo3.git",
						Root:          "/another-root",
						Interval:      2 * time.Second,
						MirrorTimeout: 4 * time.Second,
						GitGC:         "off",
						Auth:          mirror.Auth{SSHKeyPath: "/another/path/to/key"},
					},
				},
			},
			wantNewRepos: []mirror.RepositoryConfig{
				{
					Remote:        "user@host.xz:path/to/repo3.git",
					Root:          "/another-root",
					Interval:      2 * time.Second,
					MirrorTimeout: 4 * time.Second,
					GitGC:         "off",
					Auth:          mirror.Auth{SSHKeyPath: "/another/path/to/key"},
				},
			},
			wantRemovedRepos: []string{"user@host.xz:path/to/repo2.git"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyGitDefaults(tt.initialConfig)
			repoPool, err := mirror.NewRepoPool(t.Context(), *tt.initialConfig, nil, nil)
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
		initialRepoConf *mirror.RepositoryConfig
		newRepoConf     *mirror.RepositoryConfig
		wantNewWTCs     []mirror.WorktreeConfig
		wantRemovedWTs  []string
	}{
		{
			name: "no_worktree",
			initialRepoConf: &mirror.RepositoryConfig{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
			},
			newRepoConf: &mirror.RepositoryConfig{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []mirror.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspec: ""},
					{Link: "link2", Ref: "other-branch", Pathspec: "path"},
				},
			},
			wantNewWTCs: []mirror.WorktreeConfig{
				{Link: "link", Ref: "master"},
				{Link: "link2", Ref: "other-branch", Pathspec: "path"},
			},
			wantRemovedWTs: nil,
		},
		{
			name: "replace_link_ref_path",
			initialRepoConf: &mirror.RepositoryConfig{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []mirror.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspec: ""},
					{Link: "link2", Ref: "other-branch", Pathspec: "path"},
				},
			},
			newRepoConf: &mirror.RepositoryConfig{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []mirror.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspec: "new-path"},
					{Link: "link2", Ref: "new-branch", Pathspec: "path"},
				},
			},
			wantNewWTCs: []mirror.WorktreeConfig{
				{Link: "link", Ref: "master", Pathspec: "new-path"},
				{Link: "link2", Ref: "new-branch", Pathspec: "path"},
			},
			wantRemovedWTs: []string{"link", "link2"},
		},
		{
			name: "add_new_link",
			initialRepoConf: &mirror.RepositoryConfig{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []mirror.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspec: ""},
					{Link: "link2", Ref: "other-branch", Pathspec: "path"},
				},
			},
			newRepoConf: &mirror.RepositoryConfig{
				Remote: "user@host.xz:path/to/repo1.git",
				Root:   "/root", Interval: 10 * time.Second, GitGC: "always",
				Worktrees: []mirror.WorktreeConfig{
					{Link: "link", Ref: "master", Pathspec: ""},
					{Link: "link3", Ref: "other-branch", Pathspec: "path"},
				},
			},
			wantNewWTCs: []mirror.WorktreeConfig{
				{Link: "link3", Ref: "other-branch", Pathspec: "path"},
			},
			wantRemovedWTs: []string{"link2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			repo, err := mirror.NewRepository(*tt.initialRepoConf, nil, slog.Default())
			if err != nil {
				t.Fatalf("failed to create repo error = %v", err)
			}

			gotNewWTCs, gotRemovedWTs := diffWorktrees(repo, tt.newRepoConf)

			// since these slices are based on map of worktrees order of elements
			// differs between runs
			slices.SortFunc(gotNewWTCs, func(a, b mirror.WorktreeConfig) int {
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
