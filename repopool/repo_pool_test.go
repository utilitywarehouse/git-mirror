package repopool

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/utilitywarehouse/git-mirror/repository"
)

const (
	testUpstreamRepo = "upstream1"
	testRoot         = "root"
	testInterval     = 1 * time.Second
	testTimeout      = 10 * time.Second
)

func TestRepoPool_validateLinkPath(t *testing.T) {
	root := "/tmp/root"

	rpc := Config{
		Defaults: DefaultConfig{
			Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
		},
		Repositories: []repository.Config{
			{
				Remote:    "git@github.com:org/repo1.git",
				Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
			},
			{
				Remote:    "git@github.com:org/repo2.git",
				Worktrees: []repository.WorktreeConfig{{Link: "link2"}},
			},
		},
	}

	rp, err := New(t.Context(), rpc, nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected err:%s", err)
	}

	tests := []struct {
		name    string
		repo    *repository.Repository
		link    string
		wantErr bool
	}{
		{"add-repo2-link-to-repo1", rp.repos[0], "link2", true},
		{"add-repo2-abs-link-to-repo1", rp.repos[0], filepath.Join(root, "link2"), true},
		{"add-repo1-link-to-repo2", rp.repos[1], "link1", true},
		{"add-repo1-abs-link-to-repo2", rp.repos[1], filepath.Join(root, "link1"), true},
		{"add-new-link", rp.repos[0], "link3", false},
		{"add-new-link", rp.repos[1], "link3", false},
		{"add-new-abs-link", rp.repos[0], filepath.Join(os.TempDir(), "temp", "link1"), false},
		{"add-new-abs-link", rp.repos[1], filepath.Join(os.TempDir(), "temp", "link2"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if err := rp.validateLinkPath(tt.repo, tt.link); (err != nil) != tt.wantErr {
				t.Errorf("RepoPool.validateLinkPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
