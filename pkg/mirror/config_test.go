package mirror

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestRepoPoolConfig_ValidateDefaults(t *testing.T) {
	type args struct {
		dc DefaultConfig
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"empty", args{dc: DefaultConfig{}}, false},
		{"valid", args{dc: DefaultConfig{"/root", time.Second, 2 * time.Second, "always", Auth{"/path/to/key", "/host"}}}, false},
		{"invalid_root", args{dc: DefaultConfig{"root", time.Second, 2 * time.Second, "always", Auth{"/path/to/key", "/host"}}}, true},
		{"invalid_interval", args{dc: DefaultConfig{"/root", time.Millisecond, 2 * time.Second, "always", Auth{"/path/to/key", "/host"}}}, true},
		{"invalid_timeout", args{dc: DefaultConfig{"/root", time.Second, time.Millisecond, "always", Auth{"/path/to/key", "/host"}}}, true},
		{"valid_gc", args{dc: DefaultConfig{"/root", time.Second, 2 * time.Second, "", Auth{"/path/to/key", "/host"}}}, false},
		{"invalid_gc", args{dc: DefaultConfig{"/root", time.Second, 2 * time.Second, "blah", Auth{"/path/to/key", "/host"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := RepoPoolConfig{Defaults: tt.args.dc}
			if err := config.ValidateDefaults(); (err != nil) != tt.wantErr {
				t.Errorf("ValidateDefaults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRepoPoolConfig_ApplyDefaults(t *testing.T) {
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
					"/root", time.Second, 2 * time.Second, "always", Auth{"/path/to/key", "/host"},
				},
				Repositories: []RepositoryConfig{
					{Remote: "user@host.xz:path/to/repo1.git"},
					{Remote: "user@host.xz:path/to/repo2.git"},
					{
						Remote:        "user@host.xz:path/to/repo3.git",
						Root:          "/another-root",
						Interval:      2 * time.Second,
						MirrorTimeout: 4 * time.Second,
						GitGC:         "off",
						Auth:          Auth{SSHKeyPath: "/path/to/key"},
					},
				},
			},
			RepoPoolConfig{
				Defaults: DefaultConfig{
					"/root", time.Second, 2 * time.Second, "always", Auth{"/path/to/key", "/host"},
				},
				Repositories: []RepositoryConfig{
					{
						Remote:        "user@host.xz:path/to/repo1.git",
						Root:          "/root",
						Interval:      time.Second,
						MirrorTimeout: 2 * time.Second,
						GitGC:         "always",
						Auth:          Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
					},
					{
						Remote:        "user@host.xz:path/to/repo2.git",
						Root:          "/root",
						Interval:      time.Second,
						MirrorTimeout: 2 * time.Second,
						GitGC:         "always",
						Auth:          Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "/host"},
					},
					{
						Remote:        "user@host.xz:path/to/repo3.git",
						Root:          "/another-root",
						Interval:      2 * time.Second,
						MirrorTimeout: 4 * time.Second,
						GitGC:         "off",
						Auth:          Auth{SSHKeyPath: "/path/to/key"},
					},
				}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			tt.config.ApplyDefaults()

			if diff := cmp.Diff(tt.config, tt.want); diff != "" {
				t.Errorf("ApplyDefaults() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRepoPoolConfig_ValidateLinkPaths(t *testing.T) {
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
						Worktrees: []WorktreeConfig{{Link: "link1"}},
					},
				},
			},
			false,
		}, {
			"same-link-name-diff-repo",
			RepoPoolConfig{
				Defaults: DefaultConfig{Root: "/root"},
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
				Defaults: DefaultConfig{Root: "/root"},
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
				Defaults: DefaultConfig{Root: "/root"},
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
			"same-link-with-abs",
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
			if err := tt.config.ValidateLinkPaths(); (err != nil) != tt.wantErr {
				t.Errorf("validateLinkPaths() error = %v, wantErr %v", err, tt.wantErr)
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
			a := Auth{
				SSHKeyPath:        tt.fields.SSHKeyPath,
				SSHKnownHostsPath: tt.fields.SSHKnownHostsPath,
			}
			if got := a.gitSSHCommand(); got != tt.want {
				t.Errorf("Auth.gitSSHCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}
