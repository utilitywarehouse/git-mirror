package repopool

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/git-mirror/repository"
)

func TestRepoPoolConfig_validateDefaults(t *testing.T) {
	type args struct {
		dc DefaultConfig
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"empty", args{dc: DefaultConfig{}}, false},
		{"valid", args{dc: DefaultConfig{"/root", "", time.Second, 2 * time.Second, "always", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, false},
		{"valid_with_link_root", args{dc: DefaultConfig{"/root", "/link_root", time.Second, 2 * time.Second, "always", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, false},
		{"invalid_root", args{dc: DefaultConfig{"root", "", time.Second, 2 * time.Second, "always", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, true},
		{"invalid_link_root", args{dc: DefaultConfig{"/root", "link_root", time.Second, 2 * time.Second, "always", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, true},
		{"invalid_interval", args{dc: DefaultConfig{"/root", "/link_root", time.Millisecond, 2 * time.Second, "always", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, true},
		{"invalid_timeout", args{dc: DefaultConfig{"/root", "/link_root", time.Second, time.Millisecond, "always", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, true},
		{"valid_gc", args{dc: DefaultConfig{"/root", "/link_root", time.Second, 2 * time.Second, "", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, false},
		{"invalid_gc", args{dc: DefaultConfig{"/root", "/link_root", time.Second, 2 * time.Second, "blah", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, true},
		{"valid_gh_app", args{dc: DefaultConfig{"/root", "", time.Second, 2 * time.Second, "always", repository.Auth{GithubAppID: "12", GithubAppInstallationID: "34", GithubAppPrivateKeyPath: "/path/to/key"}}}, false},
		{"invalid_gh_app", args{dc: DefaultConfig{"/root", "", time.Second, 2 * time.Second, "always", repository.Auth{GithubAppID: "12", GithubAppPrivateKeyPath: "/path/to/key"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{Defaults: tt.args.dc}
			if err := config.validateDefaults(); (err != nil) != tt.wantErr {
				t.Errorf("ValidateDefaults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRepoPoolConfig_applyDefaults(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   Config
	}{
		{
			"1",
			Config{},
			Config{},
		},
		{"all_def",
			Config{
				Defaults: DefaultConfig{
					"/root", "/link_root", time.Second, 2 * time.Second, "always", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
				},
				Repositories: []repository.Config{
					{Remote: "user@host.xz:path/to/repo1.git"},
					{Remote: "user@host.xz:path/to/repo2.git"},
					{
						Remote:        "user@host.xz:path/to/repo3.git",
						Root:          "/another-root",
						LinkRoot:      "/another-link-root",
						Interval:      2 * time.Second,
						MirrorTimeout: 4 * time.Second,
						GitGC:         "off",
						Auth:          repository.Auth{SSHKeyPath: "/path/to/key"},
					},
				},
			},
			Config{
				Defaults: DefaultConfig{
					"/root", "/link_root", time.Second, 2 * time.Second, "always", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
				},
				Repositories: []repository.Config{
					{
						Remote:        "user@host.xz:path/to/repo1.git",
						Root:          "/root",
						LinkRoot:      "/link_root",
						Interval:      time.Second,
						MirrorTimeout: 2 * time.Second,
						GitGC:         "always",
						Auth:          repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
					},
					{
						Remote:        "user@host.xz:path/to/repo2.git",
						Root:          "/root",
						LinkRoot:      "/link_root",
						Interval:      time.Second,
						MirrorTimeout: 2 * time.Second,
						GitGC:         "always",
						Auth:          repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
					},
					{
						Remote:        "user@host.xz:path/to/repo3.git",
						Root:          "/another-root",
						LinkRoot:      "/another-link-root",
						Interval:      2 * time.Second,
						MirrorTimeout: 4 * time.Second,
						GitGC:         "off",
						Auth:          repository.Auth{SSHKeyPath: "/path/to/key"},
					},
				}},
		},
		{"no_link_root_def",
			Config{
				Defaults: DefaultConfig{
					"/root", "", time.Second, 2 * time.Second, "always", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
				},
				Repositories: []repository.Config{
					{Remote: "user@host.xz:path/to/repo1.git"},
				},
			},
			Config{
				Defaults: DefaultConfig{
					"/root", "/root", time.Second, 2 * time.Second, "always", repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
				},
				Repositories: []repository.Config{
					{
						Remote:        "user@host.xz:path/to/repo1.git",
						Root:          "/root",
						LinkRoot:      "/root",
						Interval:      time.Second,
						MirrorTimeout: 2 * time.Second,
						GitGC:         "always",
						Auth:          repository.Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
					},
				}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			tt.config.applyDefaults()

			if diff := cmp.Diff(tt.config, tt.want); diff != "" {
				t.Errorf("ApplyDefaults() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRepoPoolConfig_validateLinkPaths(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			"valid",
			Config{
				Defaults: DefaultConfig{Root: "/root"},
				Repositories: []repository.Config{
					{
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}, {Link: "link2"}},
					},
					{
						Worktrees: []repository.WorktreeConfig{{Link: "/diff-abs/link1"}, {Link: "link3"}},
					},
					{
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			false,
		}, {
			"valid_with_link_root",
			Config{
				Defaults: DefaultConfig{LinkRoot: "/root"},
				Repositories: []repository.Config{
					{
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}, {Link: "link2"}},
					},
					{
						Worktrees: []repository.WorktreeConfig{{Link: "/diff-abs/link1"}, {Link: "link3"}},
					},
					{
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			false,
		}, {
			"same-link-name-diff-repo",
			Config{
				Defaults: DefaultConfig{LinkRoot: "/root"},
				Repositories: []repository.Config{
					{
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}, {Link: "link2"}},
					},
					{
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			true,
		}, {
			"same-link-name-diff-repo",
			Config{
				Repositories: []repository.Config{
					{
						Root:      "/root",
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}, {Link: "link2"}},
					},
					{
						Root:      "/root",
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			true,
		}, {
			"same-link-name-same-repo",
			Config{
				Defaults: DefaultConfig{LinkRoot: "/root"},
				Repositories: []repository.Config{
					{
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}, {Link: "link1"}},
					},
				},
			},
			true,
		}, {
			"same-link-with-abs",
			Config{
				Defaults: DefaultConfig{LinkRoot: "/root"},
				Repositories: []repository.Config{
					{
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
					},
					{
						Worktrees: []repository.WorktreeConfig{{Link: "/root/link1"}},
					},
				},
			},
			true,
		}, {
			"same-link-with-abs-2",
			Config{
				Repositories: []repository.Config{
					{
						Worktrees: []repository.WorktreeConfig{{Link: "/root/link1"}},
					},
					{
						Worktrees: []repository.WorktreeConfig{{Link: "/root/link1"}},
					},
				},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			tt.config.applyDefaults()

			if err := tt.config.validateLinkPaths(); (err != nil) != tt.wantErr {
				t.Errorf("validateLinkPaths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRepoPoolConfig_PopulateLinkPaths(t *testing.T) {
	tests := []struct {
		name       string
		config     Config
		wantConfig Config
		wantValid  bool
	}{
		{
			"valid",
			Config{
				Repositories: []repository.Config{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []repository.WorktreeConfig{{Link: "link1", Ref: "main"}, {Link: "", Ref: "main"}, {Link: "", Ref: ""}},
					},
					{
						Remote:    "https://github.com/org/repo2.git",
						Worktrees: []repository.WorktreeConfig{{Link: "/diff-abs/link1"}, {Link: "link3"}},
					},
					{
						Remote:    "https://github.com/org/repo3.git",
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			Config{
				Repositories: []repository.Config{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []repository.WorktreeConfig{{Link: "link1", Ref: "main"}, {Link: "repo1/main", Ref: "main"}, {Link: "repo1/HEAD", Ref: "HEAD"}},
					},
					{
						Remote:    "https://github.com/org/repo2.git",
						Worktrees: []repository.WorktreeConfig{{Link: "/diff-abs/link1"}, {Link: "link3"}},
					},
					{
						Remote:    "https://github.com/org/repo3.git",
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			true,
		}, {
			"multiple-repo-empty-link",
			Config{
				Repositories: []repository.Config{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []repository.WorktreeConfig{{Link: "link1", Ref: "main"}, {Link: "", Ref: "main"}, {Link: "", Ref: ""}},
					},
					{
						Remote:    "https://github.com/org/repo2.git",
						Worktrees: []repository.WorktreeConfig{{Link: "diff/link1", Ref: "main"}, {Link: "", Ref: "main"}, {Link: "", Ref: ""}},
					},
					{
						Remote:    "https://github.com/org/repo3.git",
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			Config{
				Repositories: []repository.Config{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []repository.WorktreeConfig{{Link: "link1", Ref: "main"}, {Link: "repo1/main", Ref: "main"}, {Link: "repo1/HEAD", Ref: "HEAD"}},
					},
					{
						Remote:    "https://github.com/org/repo2.git",
						Worktrees: []repository.WorktreeConfig{{Link: "diff/link1", Ref: "main"}, {Link: "repo2/main", Ref: "main"}, {Link: "repo2/HEAD", Ref: "HEAD"}},
					},
					{
						Remote:    "https://github.com/org/repo3.git",
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			true,
		}, {
			"one-repo-2-empty-link-same-ref",
			Config{
				Repositories: []repository.Config{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []repository.WorktreeConfig{{Link: "", Ref: "main"}, {Link: "", Ref: "main"}, {Link: "", Ref: ""}},
					},
				},
			},
			Config{
				Repositories: []repository.Config{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []repository.WorktreeConfig{{Link: "repo1/main", Ref: "main"}, {Link: "repo1/main", Ref: "main"}, {Link: "repo1/HEAD", Ref: "HEAD"}},
					},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.applyDefaults()

			for _, repo := range tt.config.Repositories {
				if err := repo.PopulateEmptyLinkPaths(); err != nil {
					t.Errorf("populateEmptyLinkPaths() error = %v", err)
				}
			}

			if diff := cmp.Diff(tt.config, tt.wantConfig); diff != "" {
				t.Errorf("PopulateEmptyLinkPaths() config mismatch (-want +got):\n%s", diff)
			}

			if err := tt.config.validateLinkPaths(); (err == nil) != tt.wantValid {
				t.Errorf("validateLinkPaths() error = %v, wantValid %v", err, tt.wantValid)
			}
		})
	}
}
