package mirror

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestParseGitURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		want    *GitURL
		wantErr bool
	}{
		{"1",
			"user@host.xz:path/to/repo.git",
			&GitURL{Scheme: "scp", User: "user", Host: "host.xz", Path: "path/to", Repo: "repo.git"},
			false,
		},
		{"2",
			"git@github.com:org/repo",
			&GitURL{Scheme: "scp", User: "git", Host: "github.com", Path: "org", Repo: "repo"},
			false},
		{"3",
			"ssh://user@host.xz:123/path/to/repo.git",
			&GitURL{Scheme: "ssh", User: "user", Host: "host.xz:123", Path: "path/to", Repo: "repo.git"},
			false},
		{"4",
			"ssh://git@github.com/org/repo",
			&GitURL{Scheme: "ssh", User: "git", Host: "github.com", Path: "org", Repo: "repo"},
			false},
		{"5",
			"https://host.xz:345/path/to/repo.git",
			&GitURL{Scheme: "https", Host: "host.xz:345", Path: "path/to", Repo: "repo.git"},
			false},
		{"6",
			"https://github.com/org/repo",
			&GitURL{Scheme: "https", Host: "github.com", Path: "org", Repo: "repo"},
			false},
		{"7",
			"https://host.xz:123/path/to/repo.git",
			&GitURL{Scheme: "https", Host: "host.xz:123", Path: "path/to", Repo: "repo.git"},
			false},
		{
			"valid-special-char-scp",
			"user.name-with_@host-with_x.xz:123:path-with_.x/to/prr.test_test-repo0.git",
			&GitURL{Scheme: "scp", User: "user.name-with_", Host: "host-with_x.xz:123", Path: "path-with_.x/to", Repo: "prr.test_test-repo0.git"},
			false,
		},
		{
			"valid-special-char-ssh",
			"ssh://user.name-with_@host-with_x.xz:123/path-with_.x/to/prr.test_test-repo1.git",
			&GitURL{Scheme: "ssh", User: "user.name-with_", Host: "host-with_x.xz:123", Path: "path-with_.x/to", Repo: "prr.test_test-repo1.git"},
			false,
		},
		{
			"valid-special-char-https",
			"https://host-with_x.xz:123/path-with_.x/to/prr.test_test-repo2.git",
			&GitURL{Scheme: "https", Host: "host-with_x.xz:123", Path: "path-with_.x/to", Repo: "prr.test_test-repo2.git"},
			false,
		},
		{
			"valid-special-char-local",
			"file:///path-with_.x/to/prr.test_test-repo3.git",
			&GitURL{Scheme: "local", Path: "path-with_.x/to", Repo: "prr.test_test-repo3.git"},
			false,
		},

		{"invalid_ssh_hostname", "ssh://git@github.com:org/repo.git", nil, true},
		{"invalid_scp_url", "git@github.com/org/repo.git", nil, true},
		{"http", "http://host.xz:123/path/to/repo.git", nil, true},
		{"invalid_port1", "https://host.xz:yk/path/to/repo.git", nil, true},
		{"invalid_port2", "git@github.com:yk:org/repo.git", nil, true},
		{"invalid_port3", "ssh://git@github.com:yk/org/repo.git", nil, true},

		{"invalid_path_1", "git@host.xz:/r.git", nil, true},
		{"invalid_path_2", "git@host.xz:.git", nil, true},
		{"invalid_path_3", "git@host.xz:/.git", nil, true},
		{"invalid_path_4", "git@host.xz:/dd.git", nil, true},
		{"invalid_path_5", "git@host.xz:dd/.git", nil, true},
		{"invalid_path_6", "ssh://git@host.xz//r.git", nil, true},
		{"invalid_path_7", "ssh://git@host.xz/.git", nil, true},
		{"invalid_path_8", "ssh://git@host.xz//.git", nil, true},
		{"invalid_path_9", "ssh://git@host.xz//dd.git", nil, true},
		{"invalid_path_10", "ssh://git@host.xz/dd/.git", nil, true},
		{"invalid_path_11", "https://host.xz//r.git", nil, true},
		{"invalid_path_12", "https://host.xz/.git", nil, true},
		{"invalid_path_13", "https://host.xz//.git", nil, true},
		{"invalid_path_14", "https://host.xz//dd.git", nil, true},
		{"invalid_path_15", "https://host.xz/dd/.git", nil, true},

		{"invalid_hosts", "git@.:d/r.git", nil, true},
		{"invalid_hosts", "git@.d:d/r.git", nil, true},
		{"invalid_hosts", "git@d.:d/r.git", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGitURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGitURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateComparable(GitURL{})); diff != "" {
				t.Errorf("ParseGitURL() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSameRawURL(t *testing.T) {
	type args struct {
		lRepo string
		rRepo string
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{

		{"1", args{"user@host.xz:path/to/repo.git", "USER@HOST.XZ:PATH/TO/REPO.GIT"}, true, false},
		{"2", args{"git@github.com:org/repo.git", "git@github.com:org/repo.git"}, true, false},
		{"3", args{"git@github.com:org/repo.git", "ssh://git@github.com/org/repo.git"}, true, false},
		{"4", args{"git@github.com:org/repo.git", "https://github.com/org/repo.git"}, true, false},
		{"5", args{"ssh://user@host.xz:123/path/to/repo.git", "ssh://user@host.xz:123/path/to/REPO.GIT"}, true, false},
		{"6", args{"ssh://git@github.com/org/repo.git", "git@github.com:org/repo.git"}, true, false},
		{"7", args{"ssh://git@github.com/org/repo.git", "ssh://git@github.com/org/repo.git"}, true, false},
		{"8", args{"ssh://git@github.com/org/repo.git", "https://github.com/org/repo.git"}, true, false},
		{"9", args{"https://host.xz:345/path/to/repo.git", "HTTPS://HOST.XZ:345/path/to/repo.git"}, true, false},
		{"10", args{"https://github.com/org/repo.git", "git@github.com:org/repo.git"}, true, false},
		{"11", args{"https://github.com/org/repo.git", "ssh://git@github.com/org/repo.git"}, true, false},
		{"12", args{"https://github.com/org/repo.git", "https://github.com/org/repo.git"}, true, false},
		{"13", args{"user@host.xz:123:path/to/repo.git", "ssh://user@host.xz:123/path/to/repo.git"}, true, false},
		{"14", args{"user@host.xz:123:path/to/repo.git", "https://host.xz:123/path/to/repo.git"}, true, false},
		{"15", args{"ssh://user@host.xz:123/path/to/repo.git", "user@host.xz:123:path/to/repo.git"}, true, false},
		{"16", args{"ssh://user@host.xz:123/path/to/repo.git", "https://host.xz:123/path/to/repo.git"}, true, false},
		{"17", args{"https://host.xz:123/path/to/repo.git", "user@host.xz:123:path/to/repo.git"}, true, false},
		{"18", args{"https://host.xz:123/path/to/repo.git", "ssh://user@host.xz:123/path/to/repo.git"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SameRawURL(tt.args.lRepo, tt.args.rRepo)
			if (err != nil) != tt.wantErr {
				t.Errorf("SameURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SameURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
