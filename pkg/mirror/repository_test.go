package mirror

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestNewRepo(t *testing.T) {
	type args struct {
		remoteURL string
		root      string
		interval  time.Duration
		auth      Auth
		gc        string
	}
	tests := []struct {
		name    string
		args    args
		want    *Repository
		wantErr bool
	}{
		{
			"1",
			args{
				remoteURL: "user@host.xz:path/to/repo.git",
				root:      "/tmp",
				interval:  10 * time.Second,
				auth:      Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "path/to/host"},
				gc:        "always",
			},
			&Repository{
				gitURL:        &GitURL{scheme: "scp", user: "user", host: "host.xz", path: "path/to", repo: "repo.git"},
				remote:        "user@host.xz:path/to/repo.git",
				root:          "/tmp",
				dir:           "/tmp/repo.git",
				gitGC:         "always",
				interval:      10 * time.Second,
				auth:          &Auth{SSHKeyPath: "/path/to/key", SSHKnownHostsPath: "path/to/host"},
				workTreeLinks: map[string]*WorkTreeLink{},
			},
			false,
		},
		{
			"no-abs-root",
			args{
				remoteURL: "user@host.xz:path/to/repo.git",
				root:      "tmp",
				interval:  10 * time.Second,
				gc:        "always",
			},
			nil,
			true,
		}, {
			"test-interval",
			args{
				remoteURL: "user@host.xz:path/to/repo.git",
				root:      "/tmp",
				interval:  10 * time.Millisecond,
				gc:        "always",
			},
			nil,
			true,
		}, {
			"test-wrong-url",
			args{
				remoteURL: "host.xz:path/to/repo.git",
				root:      "/tmp",
				interval:  10 * time.Second,
				gc:        "always",
			},
			nil,
			true,
		}, {
			"test-no-gc",
			args{
				remoteURL: "user@host.xz:path/to/repo.git",
				root:      "/tmp",
				interval:  10 * time.Second,
				gc:        "",
			},
			nil,
			true,
		}, {
			"test-wrong-gc",
			args{
				remoteURL: "user@host.xz:path/to/repo.git",
				root:      "/tmp",
				interval:  10 * time.Second,
				gc:        "blah",
			},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := RepositoryConfig{
				Remote:   tt.args.remoteURL,
				Root:     tt.args.root,
				Interval: tt.args.interval,
				GitGC:    tt.args.gc,
				Auth:     tt.args.auth,
			}
			got, err := NewRepository(rc, nil, slog.Default())
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRepository() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.IgnoreFields(Repository{}, "log", "lock", "stop", "stopped"), cmp.AllowUnexported(Repository{}, GitURL{})); diff != "" {
				t.Errorf("NewRepository() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRepo_AddWorktreeLink(t *testing.T) {
	r := &Repository{
		gitURL:        &GitURL{scheme: "scp", user: "user", host: "host.xz", path: "path/to", repo: "repo.git"},
		root:          "/tmp/root",
		interval:      10 * time.Second,
		auth:          nil,
		log:           slog.Default(),
		gitGC:         "always",
		workTreeLinks: make(map[string]*WorkTreeLink),
		stop:          make(chan bool),
		stopped:       make(chan bool),
	}

	type args struct {
		link     string
		ref      string
		pathspec string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"all-valid", args{"link", "master", ""}, false},
		{"all-valid-with-path", args{"link2", "other-branch", "path"}, false},
		{"duplicate-link", args{"link", "master", ""}, true},
		{"no-link", args{"", "master", ""}, true},
		{"no-ref", args{"link3", "", ""}, false},
		{"absLink", args{"/tmp/link", "tag", ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := r.AddWorktreeLink(tt.args.link, tt.args.ref, tt.args.pathspec); (err != nil) != tt.wantErr {
				t.Errorf("Repo.AddWorktreeLink() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	// compare all worktree links
	want := map[string]*WorkTreeLink{
		"link":      {name: "link", link: "/tmp/root/link", ref: "master"},
		"link2":     {name: "link2", link: "/tmp/root/link2", ref: "other-branch", pathspec: "path"},
		"link3":     {name: "link3", link: "/tmp/root/link3", ref: "HEAD"},
		"/tmp/link": {name: "link", link: "/tmp/link", ref: "tag"},
	}
	if diff := cmp.Diff(want, r.workTreeLinks, cmpopts.IgnoreFields(WorkTreeLink{}, "log"), cmp.AllowUnexported(WorkTreeLink{})); diff != "" {
		t.Errorf("Repo.AddWorktreeLink() worktreelinks mismatch (-want +got):\n%s", diff)
	}
}
