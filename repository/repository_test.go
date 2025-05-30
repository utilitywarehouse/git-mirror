package repository

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/utilitywarehouse/git-mirror/giturl"
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
				cmd:           "git",
				gitURL:        &giturl.URL{Scheme: "scp", User: "user", Host: "host.xz", Path: "path/to", Repo: "repo.git"},
				remote:        "user@host.xz:path/to/repo.git",
				root:          "/tmp",
				linkRoot:      "/tmp",
				dir:           "/tmp/repo-mirrors/repo.git",
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
			rc := Config{
				Remote:   tt.args.remoteURL,
				Root:     tt.args.root,
				Interval: tt.args.interval,
				GitGC:    tt.args.gc,
				Auth:     tt.args.auth,
			}
			got, err := New(rc, "git", nil, slog.Default())
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRepository() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.IgnoreFields(Repository{}, "log", "lock", "stop", "stopped", "queueMirror"), cmp.AllowUnexported(Repository{}, giturl.URL{})); diff != "" {
				t.Errorf("NewRepository() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRepo_AddWorktreeLink(t *testing.T) {
	r := &Repository{
		gitURL:        &giturl.URL{Scheme: "scp", User: "user", Host: "host.xz", Path: "path/to", Repo: "repo.git"},
		root:          "/tmp/root",
		linkRoot:      "/tmp/link-root",
		interval:      10 * time.Second,
		auth:          nil,
		log:           slog.Default(),
		gitGC:         "always",
		workTreeLinks: make(map[string]*WorkTreeLink),
		stop:          make(chan bool),
		stopped:       make(chan bool),
	}

	type args struct {
		wtc WorktreeConfig
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"all-valid", args{wtc: WorktreeConfig{"link", "master", []string{}}}, false},
		{"all-valid-with-paths", args{wtc: WorktreeConfig{"link2", "other-branch", []string{"path1", "path2/**/*.yaml", "*.c"}}}, false},
		{"duplicate-link", args{wtc: WorktreeConfig{"link", "master", []string{}}}, true},
		{"no-link", args{wtc: WorktreeConfig{"", "master", []string{}}}, true},
		{"no-ref", args{wtc: WorktreeConfig{"link3", "", []string{}}}, false},
		{"absLink", args{wtc: WorktreeConfig{"/tmp/link", "tag", []string{}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := r.AddWorktreeLink(tt.args.wtc); (err != nil) != tt.wantErr {
				t.Errorf("Repo.AddWorktreeLink() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	// compare all worktree links
	want := map[string]*WorkTreeLink{
		"link":      {link: "link", linkAbs: "/tmp/link-root/link", ref: "master", pathspecs: []string{}},
		"link2":     {link: "link2", linkAbs: "/tmp/link-root/link2", ref: "other-branch", pathspecs: []string{"*.c", "path1", "path2/**/*.yaml"}},
		"link3":     {link: "link3", linkAbs: "/tmp/link-root/link3", ref: "HEAD", pathspecs: []string{}},
		"/tmp/link": {link: "/tmp/link", linkAbs: "/tmp/link", ref: "tag", pathspecs: []string{}},
	}
	if diff := cmp.Diff(want, r.workTreeLinks, cmpopts.IgnoreFields(WorkTreeLink{}, "log"), cmp.AllowUnexported(WorkTreeLink{})); diff != "" {
		t.Errorf("Repo.AddWorktreeLink() worktreelinks mismatch (-want +got):\n%s", diff)
	}
}

func TestParseCommitWithChangedFilesList(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []CommitInfo
	}{
		{
			"empty",
			`

			`,
			[]CommitInfo{},
		},
		{
			"only_commit",
			`267fc66a734de9e4de57d9d20c83566a69cd703c
			`,
			[]CommitInfo{{Hash: "267fc66a734de9e4de57d9d20c83566a69cd703c"}},
		},
		{
			"no_changed_files",
			`
267fc66a734de9e4de57d9d20c83566a69cd703c


			`,
			[]CommitInfo{{Hash: "267fc66a734de9e4de57d9d20c83566a69cd703c"}},
		},
		{
			"multiple_commits",
			`267fc66a734de9e4de57d9d20c83566a69cd703c
1f68b80bc259e067fdb3dc4bb82cdbd43645e392
one/hello.tf

72ea9c9de6963e97ac472d9ea996e384c6923cca
readme

80e11d114dd3aa135c18573402a8e688599c69e0
one/readme
one/hello.tf
two/readme

			`,
			[]CommitInfo{
				{Hash: "267fc66a734de9e4de57d9d20c83566a69cd703c"},
				{Hash: "1f68b80bc259e067fdb3dc4bb82cdbd43645e392", ChangedFiles: []string{"one/hello.tf"}},
				{Hash: "72ea9c9de6963e97ac472d9ea996e384c6923cca", ChangedFiles: []string{"readme"}},
				{Hash: "80e11d114dd3aa135c18573402a8e688599c69e0", ChangedFiles: []string{"one/readme", "one/hello.tf", "two/readme"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCommitWithChangedFilesList(tt.output)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseCommitWithChangedFilesList() output mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
