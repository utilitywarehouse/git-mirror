package repository

import (
	"testing"
)

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
