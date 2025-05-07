package mirror

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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
		{"valid", args{dc: DefaultConfig{"/root", "", time.Second, 2 * time.Second, "always", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, false},
		{"valid_with_link_root", args{dc: DefaultConfig{"/root", "/link_root", time.Second, 2 * time.Second, "always", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, false},
		{"invalid_root", args{dc: DefaultConfig{"root", "", time.Second, 2 * time.Second, "always", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, true},
		{"invalid_link_root", args{dc: DefaultConfig{"/root", "link_root", time.Second, 2 * time.Second, "always", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, true},
		{"invalid_interval", args{dc: DefaultConfig{"/root", "/link_root", time.Millisecond, 2 * time.Second, "always", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, true},
		{"invalid_timeout", args{dc: DefaultConfig{"/root", "/link_root", time.Second, time.Millisecond, "always", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, true},
		{"valid_gc", args{dc: DefaultConfig{"/root", "/link_root", time.Second, 2 * time.Second, "", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, false},
		{"invalid_gc", args{dc: DefaultConfig{"/root", "/link_root", time.Second, 2 * time.Second, "blah", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"}}}, true},
		{"valid_gh_app", args{dc: DefaultConfig{"/root", "", time.Second, 2 * time.Second, "always", Auth{GithubAppID: "12", GithubAppInstallationID: "34", GithubAppPrivateKeyPath: "/path/to/key"}}}, false},
		{"invalid_gh_app", args{dc: DefaultConfig{"/root", "", time.Second, 2 * time.Second, "always", Auth{GithubAppID: "12", GithubAppPrivateKeyPath: "/path/to/key"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := RepoPoolConfig{Defaults: tt.args.dc}
			if err := config.validateDefaults(); (err != nil) != tt.wantErr {
				t.Errorf("ValidateDefaults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRepoPoolConfig_applyDefaults(t *testing.T) {
	tests := []struct {
		name   string
		config RepoPoolConfig
		want   RepoPoolConfig
	}{
		{
			"1",
			RepoPoolConfig{},
			RepoPoolConfig{},
		},
		{"all_def",
			RepoPoolConfig{
				Defaults: DefaultConfig{
					"/root", "/link_root", time.Second, 2 * time.Second, "always", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
				},
				Repositories: []RepositoryConfig{
					{Remote: "user@host.xz:path/to/repo1.git"},
					{Remote: "user@host.xz:path/to/repo2.git"},
					{
						Remote:        "user@host.xz:path/to/repo3.git",
						Root:          "/another-root",
						LinkRoot:      "/another-link-root",
						Interval:      2 * time.Second,
						MirrorTimeout: 4 * time.Second,
						GitGC:         "off",
						Auth:          Auth{SSHKeyPath: "/path/to/key"},
					},
				},
			},
			RepoPoolConfig{
				Defaults: DefaultConfig{
					"/root", "/link_root", time.Second, 2 * time.Second, "always", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
				},
				Repositories: []RepositoryConfig{
					{
						Remote:        "user@host.xz:path/to/repo1.git",
						Root:          "/root",
						LinkRoot:      "/link_root",
						Interval:      time.Second,
						MirrorTimeout: 2 * time.Second,
						GitGC:         "always",
						Auth:          Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
					},
					{
						Remote:        "user@host.xz:path/to/repo2.git",
						Root:          "/root",
						LinkRoot:      "/link_root",
						Interval:      time.Second,
						MirrorTimeout: 2 * time.Second,
						GitGC:         "always",
						Auth:          Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
					},
					{
						Remote:        "user@host.xz:path/to/repo3.git",
						Root:          "/another-root",
						LinkRoot:      "/another-link-root",
						Interval:      2 * time.Second,
						MirrorTimeout: 4 * time.Second,
						GitGC:         "off",
						Auth:          Auth{SSHKeyPath: "/path/to/key"},
					},
				}},
		},
		{"no_link_root_def",
			RepoPoolConfig{
				Defaults: DefaultConfig{
					"/root", "", time.Second, 2 * time.Second, "always", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
				},
				Repositories: []RepositoryConfig{
					{Remote: "user@host.xz:path/to/repo1.git"},
				},
			},
			RepoPoolConfig{
				Defaults: DefaultConfig{
					"/root", "/root", time.Second, 2 * time.Second, "always", Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
				},
				Repositories: []RepositoryConfig{
					{
						Remote:        "user@host.xz:path/to/repo1.git",
						Root:          "/root",
						LinkRoot:      "/root",
						Interval:      time.Second,
						MirrorTimeout: 2 * time.Second,
						GitGC:         "always",
						Auth:          Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
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
		config  RepoPoolConfig
		wantErr bool
	}{
		{
			"valid",
			RepoPoolConfig{
				Defaults: DefaultConfig{Root: "/root"},
				Repositories: []RepositoryConfig{
					{
						Worktrees: []WorktreeConfig{{Link: "link1"}, {Link: "link2"}},
					},
					{
						Worktrees: []WorktreeConfig{{Link: "/diff-abs/link1"}, {Link: "link3"}},
					},
					{
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			false,
		}, {
			"valid_with_link_root",
			RepoPoolConfig{
				Defaults: DefaultConfig{LinkRoot: "/root"},
				Repositories: []RepositoryConfig{
					{
						Worktrees: []WorktreeConfig{{Link: "link1"}, {Link: "link2"}},
					},
					{
						Worktrees: []WorktreeConfig{{Link: "/diff-abs/link1"}, {Link: "link3"}},
					},
					{
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			false,
		}, {
			"same-link-name-diff-repo",
			RepoPoolConfig{
				Defaults: DefaultConfig{LinkRoot: "/root"},
				Repositories: []RepositoryConfig{
					{
						Worktrees: []WorktreeConfig{{Link: "link1"}, {Link: "link2"}},
					},
					{
						Worktrees: []WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			true,
		}, {
			"same-link-name-diff-repo",
			RepoPoolConfig{
				Repositories: []RepositoryConfig{
					{
						Root:      "/root",
						Worktrees: []WorktreeConfig{{Link: "link1"}, {Link: "link2"}},
					},
					{
						Root:      "/root",
						Worktrees: []WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			true,
		}, {
			"same-link-name-same-repo",
			RepoPoolConfig{
				Defaults: DefaultConfig{LinkRoot: "/root"},
				Repositories: []RepositoryConfig{
					{
						Worktrees: []WorktreeConfig{{Link: "link1"}, {Link: "link1"}},
					},
				},
			},
			true,
		}, {
			"same-link-with-abs",
			RepoPoolConfig{
				Defaults: DefaultConfig{LinkRoot: "/root"},
				Repositories: []RepositoryConfig{
					{
						Worktrees: []WorktreeConfig{{Link: "link1"}},
					},
					{
						Worktrees: []WorktreeConfig{{Link: "/root/link1"}},
					},
				},
			},
			true,
		}, {
			"same-link-with-abs-2",
			RepoPoolConfig{
				Repositories: []RepositoryConfig{
					{
						Worktrees: []WorktreeConfig{{Link: "/root/link1"}},
					},
					{
						Worktrees: []WorktreeConfig{{Link: "/root/link1"}},
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
		config     RepoPoolConfig
		wantConfig RepoPoolConfig
		wantValid  bool
	}{
		{
			"valid",
			RepoPoolConfig{
				Repositories: []RepositoryConfig{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []WorktreeConfig{{Link: "link1", Ref: "main"}, {Link: "", Ref: "main"}, {Link: "", Ref: ""}},
					},
					{
						Remote:    "https://github.com/org/repo2.git",
						Worktrees: []WorktreeConfig{{Link: "/diff-abs/link1"}, {Link: "link3"}},
					},
					{
						Remote:    "https://github.com/org/repo3.git",
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			RepoPoolConfig{
				Repositories: []RepositoryConfig{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []WorktreeConfig{{Link: "link1", Ref: "main"}, {Link: "repo1/main", Ref: "main"}, {Link: "repo1/HEAD", Ref: "HEAD"}},
					},
					{
						Remote:    "https://github.com/org/repo2.git",
						Worktrees: []WorktreeConfig{{Link: "/diff-abs/link1"}, {Link: "link3"}},
					},
					{
						Remote:    "https://github.com/org/repo3.git",
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			true,
		}, {
			"multiple-repo-empty-link",
			RepoPoolConfig{
				Repositories: []RepositoryConfig{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []WorktreeConfig{{Link: "link1", Ref: "main"}, {Link: "", Ref: "main"}, {Link: "", Ref: ""}},
					},
					{
						Remote:    "https://github.com/org/repo2.git",
						Worktrees: []WorktreeConfig{{Link: "diff/link1", Ref: "main"}, {Link: "", Ref: "main"}, {Link: "", Ref: ""}},
					},
					{
						Remote:    "https://github.com/org/repo3.git",
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			RepoPoolConfig{
				Repositories: []RepositoryConfig{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []WorktreeConfig{{Link: "link1", Ref: "main"}, {Link: "repo1/main", Ref: "main"}, {Link: "repo1/HEAD", Ref: "HEAD"}},
					},
					{
						Remote:    "https://github.com/org/repo2.git",
						Worktrees: []WorktreeConfig{{Link: "diff/link1", Ref: "main"}, {Link: "repo2/main", Ref: "main"}, {Link: "repo2/HEAD", Ref: "HEAD"}},
					},
					{
						Remote:    "https://github.com/org/repo3.git",
						Root:      "/another-root",
						LinkRoot:  "/another-link-root",
						Worktrees: []WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			true,
		}, {
			"one-repo-2-empty-link-same-ref",
			RepoPoolConfig{
				Repositories: []RepositoryConfig{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []WorktreeConfig{{Link: "", Ref: "main"}, {Link: "", Ref: "main"}, {Link: "", Ref: ""}},
					},
				},
			},
			RepoPoolConfig{
				Repositories: []RepositoryConfig{
					{
						Remote:    "https://github.com/org/repo1.git",
						Worktrees: []WorktreeConfig{{Link: "repo1/main", Ref: "main"}, {Link: "repo1/main", Ref: "main"}, {Link: "repo1/HEAD", Ref: "HEAD"}},
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

func TestAuth_gitSSHCommand(t *testing.T) {
	type fields struct {
		SSHKeyPath        string
		SSHKnownHostsPath string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{"both-provided", fields{"path/to/ssh", "path/to/known_host"},
			"GIT_SSH_COMMAND=ssh -q -F none -o IdentitiesOnly=yes -o IdentityFile=path/to/ssh -o UserKnownHostsFile=path/to/known_host",
		},
		{"only-ssh-key", fields{"path/to/ssh", ""},
			"GIT_SSH_COMMAND=ssh -q -F none -o IdentitiesOnly=yes -o IdentityFile=path/to/ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no",
		},
		{"no-key", fields{"", ""},
			"GIT_SSH_COMMAND=ssh -q -F none -o IdentitiesOnly=yes -o IdentityFile=/dev/null -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Repository{
				auth: &Auth{
					SSHKeyPath:        tt.fields.SSHKeyPath,
					SSHKnownHostsPath: tt.fields.SSHKnownHostsPath,
				},
			}
			if got := r.gitSSHCommand(); got != tt.want {
				t.Errorf("Auth.gitSSHCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_normaliseReference(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{"1", "// TODO: Add test cases.", "_TODO_Add_test_cases."},
		{"2", "name/ref", "name_ref"},
		{"3", `with lots of < > : " / \ | ? * char`, "with_lots_of_char"},
		{"4", `remotes/origin/MO-1001`, "remotes_origin_MO-1001"},
		{"5", `remotes/origin/revert-130445-uw-releaser-very-very-long-reference-service-64bbae965ce8d4a0eaf929f9455f40a72d3b3208`,
			"remotes_origin_revert-130445-uw-releaser-very-very-long-reference-service-64bbae965ce8d4a0eaf929f9455f40a72d3b3208"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normaliseReference(tt.ref); got != tt.want {
				t.Errorf("normaliseReference() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_generateLink(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		ref     string
		want    string
		wantErr bool
	}{
		{"1", "git@github.com:org/repo.git", "master", "repo/master", false},
		{"2", "ssh://git@github.com/org/repo.git", "21f541a953776c5d7c5c5c9d00cdfb26e6c9ecdb", "repo/21f541a", false},
		{"3", "https://github.com/org/repo.git", "remotes/origin/MO-1001", "repo/remotes_origin_MO-1001", false},
		{"4", "git@github.com:org/repo.git", "v2.16.1-3", "repo/v2.16.1-3", false},
		{"5", "ssh://git@github.com/org/repo.git", `< > : " / \ | ? *`, "", true},
		{"6", "https://github.com/org/repo.git", ".", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateLink(tt.remote, tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateLink() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("generateLink() = %v, want %v", got, tt.want)
			}
		})
	}
}
