package e2e_test

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/git-mirror/internal/utils"
	"github.com/utilitywarehouse/git-mirror/repopool"
	"github.com/utilitywarehouse/git-mirror/repository"
)

const (
	testUpstreamRepo = "upstream1"
	testRoot         = "root"
	testInterval     = 1 * time.Second
	testTimeout      = 10 * time.Second

	testMainBranch = "e2e-main"
	testGitUser    = "git-mirror-e2e"

	defaultDirMode fs.FileMode = os.FileMode(0755) // 'rwxr-xr-x'
)

var (
	testLog  = slog.Default()
	txtCtx   = context.TODO()
	testENVs []string
)

func TestMain(m *testing.M) {
	t := &testing.T{}

	testTmpDir := mustTmpDir(t)

	testENVs = []string{
		fmt.Sprintf(
			"GIT_CONFIG_GLOBAL=%s/gitconfig", testTmpDir,
		),
		`GIT_CONFIG_SYSTEM=/dev/null`,
	}

	mustExec(t, "", "git", "config", "--global", "user.name", testGitUser)
	mustExec(t, "", "git", "config", "--global", "user.email", testGitUser+"@example.com")

	code := m.Run()

	// clean up
	os.RemoveAll(testTmpDir)

	os.Exit(code)
}

// ##############################################
// Repository Tests
// ##############################################

func Test_init_root_doesnt_exist(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link := "link"

	t.Log("TEST1: init upstream and test mirror")
	mustInitRepo(t, upstream, "file", t.Name())

	mustCreateRepoAndMirror(t, upstream, root, link, testMainBranch)

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, link, "file", t.Name())
}

func Test_init_existing_root(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link := "link"
	ref := testMainBranch
	linkAbs := filepath.Join(root, link)

	t.Log("TEST1: init upstream and run mirror to create mirrors at root")

	mustInitRepo(t, upstream, "file", t.Name())

	// create mirror repo and add link for main branch
	mustCreateRepoAndMirror(t, upstream, root, "link", ref)

	t.Log("re-create mirror repo with same root and worktree with absolute path")

	mustCreateRepoAndMirror(t, upstream, root, linkAbs, ref)

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, link, "file", t.Name())
}

func Test_init_existing_root_with_diff_repo(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link := filepath.Join("sub", "dir", "link")
	ref := testMainBranch

	t.Log("TEST1: init both upstream and mirror repo")
	mustInitRepo(t, upstream, "file", t.Name())
	mustInitRepo(t, filepath.Join(root, testUpstreamRepo), "file", t.Name())

	// create NEW repo using same paths...(testing existing root)
	mustCreateRepoAndMirror(t, upstream, root, link, ref)

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, link, "file", t.Name())

	t.Log("TEST2: change root so that its under existing repo and create new mirror repo")
	// root = root/upstream.git
	root = filepath.Join(root, testUpstreamRepo)
	// create another dir 'root/upstream.git/upstream.git' so that `upstream.git` dir is inside
	// existing repo created above test
	if err := os.MkdirAll(filepath.Join(root, testUpstreamRepo, testUpstreamRepo), defaultDirMode); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// create NEW repo using same paths...(testing existing root)
	mustCreateRepoAndMirror(t, upstream, root, link, ref)

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, link, "file", t.Name())
}

func Test_init_existing_root_fails_sanity(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link := "link"
	ref := testMainBranch

	mustInitRepo(t, upstream, "file", t.Name())

	// create mirror repo1 and add link for main branch
	repo1 := mustCreateRepoAndMirror(t, upstream, root, link, ref)

	t.Log("TEST-1: modify remote 'origin' URL")
	mustExec(t, repo1.Directory(), "git", "remote", "set-url", "origin", "blah/blah")

	// create repo using same paths...
	mustCreateRepoAndMirror(t, upstream, root, link, ref)

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, link, "file", t.Name())

	t.Log("TEST-2: modify remote 'origin' fetch path refs")
	mustExec(t, repo1.Directory(), "git", "config", "--add", "remote.origin.fetch", "+refs/heads/master:refs/remotes/origin/master")

	// create repo using same paths...
	mustCreateRepoAndMirror(t, upstream, root, link, ref)

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, link, "file", t.Name())
}

func Test_mirror_head_and_main(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link1 := "link1" // on testBranchMain branch
	link2 := "link2" // on remote HEAD
	ref1 := testMainBranch
	ref2 := "HEAD"

	t.Log("TEST-1: init upstream")
	mustInitRepo(t, upstream, "file", t.Name()+"-1")

	repo := mustCreateRepoAndMirror(t, upstream, root, link1, ref1)
	// add worktree for HEAD
	if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref2, Pathspecs: []string{}}); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	// mirror again for 2nd worktree
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// verify checkout files
	assertLinkedFile(t, root, link1, "file", t.Name()+"-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-1")

	t.Log("TEST-2: forward HEAD")

	mustCommit(t, upstream, "file", t.Name()+"-2")
	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-2")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-2")

	t.Log("TEST-3: move HEAD backward")

	mustExec(t, upstream, "git", "reset", "-q", "--hard", "HEAD^")
	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-1")

	// remove worktrees
	if err := repo.RemoveWorktreeLink(link2); err != nil {
		t.Errorf("unable to remove worktree error: %v", err)
	}
	// run mirror loop to remove links
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertMissingLink(t, root, link2)

	if err := repo.RemoveWorktreeLink(link1); err != nil {
		t.Errorf("unable to remove worktree error: %v", err)
	}
	// run mirror loop to remove links
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertMissingLink(t, root, link1)
}

func Test_mirror_bad_ref(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link := "link"
	ref := "non-existent"

	t.Log("TEST-1: init upstream without non-existent branch")
	mustInitRepo(t, upstream, "file", t.Name())

	rc := repository.Config{
		Remote:        "file://" + upstream,
		Root:          root,
		Interval:      testInterval,
		MirrorTimeout: testTimeout,
		GitGC:         "always",
		Worktrees:     []repository.WorktreeConfig{{Link: link, Ref: ref}},
	}
	repo, err := repository.New(rc, "", testENVs, testLog)
	if err != nil {
		t.Fatalf("unable to create new repo error: %v", err)
	}

	if err := repo.Mirror(txtCtx); err == nil {
		t.Errorf("unexpected success for non-existent link")
	}

	// verify checkout files
	assertMissingLink(t, root, link)
}

func Test_mirror_other_branch(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link1 := "link1" // on testBranchMain branch
	link2 := "link2" // on remote other-branch
	ref1 := testMainBranch
	ref2 := "other-branch"

	t.Log("TEST-1: init upstream and add other-branch")

	mustInitRepo(t, upstream, "file", t.Name()+"-main-1")
	// add other-branch and commit and switch back
	mustExec(t, upstream, "git", "checkout", "-q", "-b", ref2)
	mustCommit(t, upstream, "file", t.Name()+"-other-1")
	mustExec(t, upstream, "git", "checkout", "-q", ref1)

	repo := mustCreateRepoAndMirror(t, upstream, root, link1, ref1)
	// add 2nd worktree
	if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref2, Pathspecs: []string{}}); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	// mirror again for 2nd worktree
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// verify checkout files
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-other-1")

	t.Log("TEST-2: forward both branch")

	mustCommit(t, upstream, "file", t.Name()+"-main-2")
	mustExec(t, upstream, "git", "checkout", "-q", ref2)
	mustCommit(t, upstream, "file", t.Name()+"-other-2")
	mustExec(t, upstream, "git", "checkout", "-q", ref1)

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-2")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-other-2")

	t.Log("TEST-3: move both branch backward")

	mustExec(t, upstream, "git", "reset", "-q", "--hard", "HEAD^")
	mustExec(t, upstream, "git", "checkout", "-q", ref2)
	mustExec(t, upstream, "git", "reset", "-q", "--hard", "HEAD^")
	mustExec(t, upstream, "git", "checkout", "-q", ref1)

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-other-1")

	// remove worktrees
	if err := repo.RemoveWorktreeLink(link2); err != nil {
		t.Errorf("unable to remove worktree error: %v", err)
	}
	// run mirror loop to remove links
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertMissingLink(t, root, link2)

	if err := repo.RemoveWorktreeLink(link1); err != nil {
		t.Errorf("unable to remove worktree error: %v", err)
	}
	// run mirror loop to remove links
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertMissingLink(t, root, link1)
}

func Test_mirror_with_pathspec(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link1 := "link1" // on testBranchMain branch
	link2 := "link2" // on remote HEAD -- dir2
	link3 := "link3" // on remote HEAD -- dir3
	link4 := "link4" // on remote HEAD -- dir2 dir3
	ref1 := testMainBranch
	ref2 := "HEAD"
	ref3 := "HEAD"
	pathSpec2 := "dir2"
	pathSpec3 := "dir3"

	t.Log("TEST-1: init upstream without other dirs")

	firstSHA := mustInitRepo(t, upstream, "file", t.Name()+"-main-1")

	repo := mustCreateRepoAndMirror(t, upstream, root, link1, ref1)

	// mirror again for 2nd worktree
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// verify checkout files
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-1")
	assertMissingLinkFile(t, root, link2, "file")
	assertMissingLinkFile(t, root, link3, "file")

	t.Log("TEST-2: forward HEAD and create dir2 to test link2")

	mustCommit(t, upstream, "file", t.Name()+"-main-2")
	mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-main-2")

	// add worktree for HEAD on dir2
	if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref2, Pathspecs: []string{pathSpec2}}); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link4, Ref: ref2, Pathspecs: []string{pathSpec2}}); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-2")
	assertLinkedFile(t, root, link1, filepath.Join("dir2", "file"), t.Name()+"-main-2")

	assertMissingLinkFile(t, root, link2, "file")
	assertLinkedFile(t, root, link2, filepath.Join("dir2", "file"), t.Name()+"-main-2")

	assertMissingLinkFile(t, root, link3, "file")
	assertMissingLinkFile(t, root, link3, filepath.Join("dir2", "file"))

	assertMissingLinkFile(t, root, link4, "file")
	assertLinkedFile(t, root, link4, filepath.Join("dir2", "file"), t.Name()+"-main-2")

	t.Log("TEST-3: forward HEAD and create dir3 to test link3")

	mustCommit(t, upstream, "file", t.Name()+"-main-3")
	mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-main-3")
	mustCommit(t, upstream, filepath.Join("dir3", "file"), t.Name()+"-main-3")

	// add worktree for HEAD on dir3
	if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link3, Ref: ref3, Pathspecs: []string{pathSpec3}}); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	// update worktree link4
	if err := repo.RemoveWorktreeLink(link4); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link4, Ref: ref2, Pathspecs: []string{pathSpec3, pathSpec2}}); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-3")
	assertLinkedFile(t, root, link1, filepath.Join("dir2", "file"), t.Name()+"-main-3")
	assertLinkedFile(t, root, link1, filepath.Join("dir3", "file"), t.Name()+"-main-3")

	assertMissingLinkFile(t, root, link2, "file")
	assertLinkedFile(t, root, link2, filepath.Join("dir2", "file"), t.Name()+"-main-3")
	assertMissingLinkFile(t, root, link2, filepath.Join("dir3", "file"))

	assertMissingLinkFile(t, root, link3, "file")
	assertMissingLinkFile(t, root, link3, filepath.Join("dir2", "file"))
	assertLinkedFile(t, root, link3, filepath.Join("dir3", "file"), t.Name()+"-main-3")

	assertMissingLinkFile(t, root, link4, "file")
	assertLinkedFile(t, root, link4, filepath.Join("dir2", "file"), t.Name()+"-main-3")
	assertLinkedFile(t, root, link4, filepath.Join("dir3", "file"), t.Name()+"-main-3")

	t.Log("TEST-3: move HEAD backward by 3 commit to original state")

	mustExec(t, upstream, "git", "reset", "-q", "--hard", firstSHA)

	// remove worktrees with pathspec which doesn't exit
	if err := repo.RemoveWorktreeLink(link2); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	if err := repo.RemoveWorktreeLink(link3); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	if err := repo.RemoveWorktreeLink(link4); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-1")
	assertMissingLinkFile(t, root, link1, filepath.Join("dir2", "file"))
	assertMissingLinkFile(t, root, link1, filepath.Join("dir3", "file"))

	assertMissingLinkFile(t, root, link2, "file")
	assertMissingLinkFile(t, root, link2, filepath.Join("dir2", "file"))
	assertMissingLinkFile(t, root, link2, filepath.Join("dir3", "file"))

	assertMissingLinkFile(t, root, link3, "file")
	assertMissingLinkFile(t, root, link3, filepath.Join("dir2", "file"))
	assertMissingLinkFile(t, root, link3, filepath.Join("dir3", "file"))
}

func Test_mirror_switch_branch_after_restart(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link1 := "link1" // on testBranchMain branch
	link2 := "link2" // on remote other-branch
	ref1 := testMainBranch
	ref2 := "other-branch"

	mustInitRepo(t, upstream, "file", t.Name()+"-main-1")
	// add other-branch and commit and switch back
	mustExec(t, upstream, "git", "checkout", "-q", "-b", ref2)
	mustCommit(t, upstream, "file", t.Name()+"-other-1")
	mustExec(t, upstream, "git", "checkout", "-q", ref1)

	repo1 := mustCreateRepoAndMirror(t, upstream, root, link1, ref1)
	// add 2nd worktree
	if err := repo1.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref2, Pathspecs: []string{}}); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	// mirror again for 2nd worktree
	if err := repo1.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// verify checkout files
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-other-1")

	t.Log("TEST-1: trigger restart by creating new repo with switched links")

	repo2 := mustCreateRepoAndMirror(t, upstream, root, link1, ref2)
	// add 2nd worktree
	if err := repo2.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref1, Pathspecs: []string{}}); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	// mirror again for 2nd worktree
	if err := repo2.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// verify checkout files switch
	assertLinkedFile(t, root, link1, "file", t.Name()+"-other-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-main-1")

	t.Log("TEST-2: forward both branch")

	mustCommit(t, upstream, "file", t.Name()+"-main-2")
	mustExec(t, upstream, "git", "checkout", "-q", ref2)
	mustCommit(t, upstream, "file", t.Name()+"-other-2")
	mustExec(t, upstream, "git", "checkout", "-q", ref1)

	// mirror new commits
	if err := repo2.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-other-2")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-main-2")

	t.Log("TEST-3: move both branch backward")

	mustExec(t, upstream, "git", "reset", "-q", "--hard", "HEAD^")
	mustExec(t, upstream, "git", "checkout", "-q", ref2)
	mustExec(t, upstream, "git", "reset", "-q", "--hard", "HEAD^")
	mustExec(t, upstream, "git", "checkout", "-q", ref1)

	// mirror new commits
	if err := repo2.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-other-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-main-1")
}

func Test_mirror_tag_sha(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link1 := "link1" // on e2e-tag tag
	link2 := "link2" // on remote other-branch
	ref1 := "e2e-tag"
	ref2 := "" // will be calculated later

	t.Log("TEST-1: init upstream and add tag and get current SHA")

	ref2 = mustInitRepo(t, upstream, "file", t.Name()+"-main-1")
	// add tag at current commit
	mustExec(t, upstream, "git", "tag", "-af", ref1, "-m", t.Name()+"-main-1")

	repo := mustCreateRepoAndMirror(t, upstream, root, link1, ref1)
	// add 2nd worktree
	if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref2, Pathspecs: []string{}}); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	// mirror again for 2nd worktree
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// verify checkout files
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-main-1")

	t.Log("TEST-2: commit and move tag forward")

	mustCommit(t, upstream, "file", t.Name()+"-main-2")
	mustExec(t, upstream, "git", "tag", "-af", ref1, "-m", t.Name()+"-main-2")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-2")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-main-1")

	t.Log("TEST-3: move tag backward")

	mustExec(t, upstream, "git", "reset", "-q", "--hard", "HEAD^")
	mustExec(t, upstream, "git", "tag", "-af", ref1, "-m", t.Name()+"-main-3")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"-main-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-main-1")
}

func Test_mirror_with_crash(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link1 := "link1" // on testBranchMain branch
	ref1 := testMainBranch

	t.Log("TEST-1: init upstream")
	mustInitRepo(t, upstream, "file", t.Name()+"- 1")

	repo := mustCreateRepoAndMirror(t, upstream, root, link1, ref1)

	// verify checkout files
	assertLinkedFile(t, root, link1, "file", t.Name()+"- 1")

	t.Log("TEST-2: forward HEAD and delete link 1 symlink")

	if err := os.Remove(filepath.Join(root, link1)); err != nil {
		t.Fatalf("unexpected error:%s", err)
	}
	mustCommit(t, upstream, "file", t.Name()+"- 2")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"- 2")

	t.Log("TEST-3: forward HEAD and delete actual worktree")

	wtPath, err := utils.ReadAbsLink(repo.WorktreeLinks()[link1].AbsoluteLink())
	if err != nil {
		t.Fatalf("unexpected error:%s", err)
	}
	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatalf("unexpected error:%s", err)
	}
	mustCommit(t, upstream, "file", t.Name()+"- 3")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"- 3")

	t.Log("TEST-3: move HEAD backward and delete root repository")

	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("unexpected error:%s", err)
	}
	mustExec(t, upstream, "git", "reset", "-q", "--hard", "HEAD^")
	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	assertLinkedFile(t, root, link1, "file", t.Name()+"- 2")
}

func Test_commit_hash_msg(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	otherBranch := "other-branch"

	t.Log("TEST-1: init upstream and verify 1st commit after mirror")

	fileSHA1 := mustInitRepo(t, upstream, "file", t.Name()+"-main-1")

	repo := mustCreateRepoAndMirror(t, upstream, root, "", "")

	assertCommitLog(t, repo, "HEAD", "", fileSHA1, t.Name()+"-main-1", []string{"file"})
	assertCommitLog(t, repo, testMainBranch, "", fileSHA1, t.Name()+"-main-1", []string{"file"})

	t.Log("TEST-2: forward HEAD and create dir1 on HEAD")

	dir1SHA2 := mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-2")
	fileSHA2 := mustCommit(t, upstream, "file", t.Name()+"-main-2")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	// log @ root
	assertCommitLog(t, repo, "HEAD", "", fileSHA2, t.Name()+"-main-2", []string{"file"})
	assertCommitLog(t, repo, testMainBranch, "", fileSHA2, t.Name()+"-main-2", []string{"file"})
	// log @ dir1
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA2, t.Name()+"-dir1-main-2", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, testMainBranch, "dir1", dir1SHA2, t.Name()+"-dir1-main-2", []string{filepath.Join("dir1", "file")})

	t.Log("TEST-3: forward HEAD and create other-branch")

	dir1SHA3 := mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-3")
	mustExec(t, upstream, "git", "checkout", "-q", "-b", otherBranch)
	dir2SHA3 := mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-dir2-other-3")
	fileOtherSHA3 := mustCommit(t, upstream, "file", t.Name()+"-other-3")
	mustExec(t, upstream, "git", "checkout", "-q", testMainBranch)
	fileSHA3 := mustCommit(t, upstream, "file", t.Name()+"-main-3")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// log @ HEAD on root
	assertCommitLog(t, repo, "HEAD", "", fileSHA3, t.Name()+"-main-3", []string{"file"})
	assertCommitLog(t, repo, testMainBranch, "", fileSHA3, t.Name()+"-main-3", []string{"file"})
	// log @ HEAD on dir
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA3, t.Name()+"-dir1-main-3", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, testMainBranch, "dir1", dir1SHA3, t.Name()+"-dir1-main-3", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, testMainBranch, "dir2", "", "", nil)
	// log @ other-branch
	assertCommitLog(t, repo, otherBranch, "", fileOtherSHA3, t.Name()+"-other-3", []string{"file"})
	assertCommitLog(t, repo, otherBranch, "dir1", dir1SHA3, t.Name()+"-dir1-main-3", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, otherBranch, "dir2", dir2SHA3, t.Name()+"-dir2-other-3", []string{filepath.Join("dir2", "file")})

	wantDiffList := []repository.CommitInfo{
		{Hash: fileOtherSHA3, ChangedFiles: []string{"file"}},
		{Hash: dir2SHA3, ChangedFiles: []string{filepath.Join("dir2", "file")}},
	}
	if got, err := repo.BranchCommits(txtCtx, otherBranch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if diff := cmp.Diff(wantDiffList, got); diff != "" {
		t.Errorf("BranchCommits() mismatch (-want +got):\n%s", diff)
	}

	t.Log("TEST-4: forward HEAD and other-branch")

	dir1SHA4 := mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-4")
	mustExec(t, upstream, "git", "checkout", "-q", otherBranch)
	dir2SHA4 := mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-dir2-other-4")
	fileOtherSHA4 := mustCommit(t, upstream, "file", t.Name()+"-other-4")
	mustExec(t, upstream, "git", "checkout", "-q", testMainBranch)
	fileSHA4 := mustCommit(t, upstream, "file", t.Name()+"-main-4")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// log @ HEAD on root
	assertCommitLog(t, repo, "HEAD", "", fileSHA4, t.Name()+"-main-4", []string{"file"})
	assertCommitLog(t, repo, testMainBranch, "", fileSHA4, t.Name()+"-main-4", []string{"file"})
	// log @ HEAD on dir
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA4, t.Name()+"-dir1-main-4", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, testMainBranch, "dir1", dir1SHA4, t.Name()+"-dir1-main-4", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, testMainBranch, "dir2", "", "", nil)
	// log @ other-branch
	assertCommitLog(t, repo, otherBranch, "", fileOtherSHA4, t.Name()+"-other-4", []string{"file"})
	// dir1 on other-branch will be always behind
	assertCommitLog(t, repo, otherBranch, "dir1", dir1SHA3, t.Name()+"-dir1-main-3", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, otherBranch, "dir2", dir2SHA4, t.Name()+"-dir2-other-4", []string{filepath.Join("dir2", "file")})

	wantDiffList = []repository.CommitInfo{
		// new commits
		{Hash: fileOtherSHA4, ChangedFiles: []string{"file"}},
		{Hash: dir2SHA4, ChangedFiles: []string{filepath.Join("dir2", "file")}},
		// old commits
		{Hash: fileOtherSHA3, ChangedFiles: []string{"file"}},
		{Hash: dir2SHA3, ChangedFiles: []string{filepath.Join("dir2", "file")}},
	}
	if got, err := repo.BranchCommits(txtCtx, otherBranch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if diff := cmp.Diff(wantDiffList, got); diff != "" {
		t.Errorf("BranchCommits() mismatch (-want +got):\n%s", diff)
	}

	t.Log("TEST-4: move HEAD and other-branch backward to test3")

	mustExec(t, upstream, "git", "checkout", "-q", otherBranch)
	mustExec(t, upstream, "git", "reset", "-q", "--hard", fileOtherSHA3)
	mustExec(t, upstream, "git", "checkout", "-q", testMainBranch)
	mustExec(t, upstream, "git", "reset", "-q", "--hard", fileSHA3)

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// log @ HEAD on root
	assertCommitLog(t, repo, "HEAD", "", fileSHA3, t.Name()+"-main-3", []string{"file"})
	assertCommitLog(t, repo, testMainBranch, "", fileSHA3, t.Name()+"-main-3", []string{"file"})
	// log @ HEAD on dir
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA3, t.Name()+"-dir1-main-3", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, testMainBranch, "dir1", dir1SHA3, t.Name()+"-dir1-main-3", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, testMainBranch, "dir2", "", "", nil)
	// log @ other-branch
	assertCommitLog(t, repo, otherBranch, "", fileOtherSHA3, t.Name()+"-other-3", []string{"file"})
	assertCommitLog(t, repo, otherBranch, "dir1", dir1SHA3, t.Name()+"-dir1-main-3", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, otherBranch, "dir2", dir2SHA3, t.Name()+"-dir2-other-3", []string{filepath.Join("dir2", "file")})

	wantDiffList = []repository.CommitInfo{
		// old commits
		{Hash: fileOtherSHA3, ChangedFiles: []string{"file"}},
		{Hash: dir2SHA3, ChangedFiles: []string{filepath.Join("dir2", "file")}},
	}
	if got, err := repo.BranchCommits(txtCtx, otherBranch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if diff := cmp.Diff(wantDiffList, got); diff != "" {
		t.Errorf("BranchCommits() mismatch (-want +got):\n%s", diff)
	}

	t.Log("TEST-5: move HEAD backward to test1 and delete other-branch")

	mustExec(t, upstream, "git", "branch", "-D", otherBranch)
	mustExec(t, upstream, "git", "reset", "-q", "--hard", fileSHA1)

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// log @ HEAD on root
	assertCommitLog(t, repo, "HEAD", "", fileSHA1, t.Name()+"-main-1", []string{"file"})
	assertCommitLog(t, repo, testMainBranch, "", fileSHA1, t.Name()+"-main-1", []string{"file"})
	// log @ HEAD on dir
	assertCommitLog(t, repo, "HEAD", "dir1", "", "", nil)
	assertCommitLog(t, repo, testMainBranch, "dir1", "", "", nil)
	assertCommitLog(t, repo, testMainBranch, "dir2", "", "", nil)

	// all checks on other-branch should fail
	if _, err := repo.Hash(txtCtx, otherBranch, ""); err == nil {
		t.Errorf("unexpected success for non-existent branch:%s", otherBranch)
	}
	if _, err := repo.Hash(txtCtx, otherBranch, "dir1"); err == nil {
		t.Errorf("unexpected success for non-existent branch:%s", otherBranch)
	}
}

func Test_commit_hash_files_merge(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	otherBranch := "other-branch"

	t.Log("TEST-1: init upstream and verify 1st commit after mirror")

	mustInitRepo(t, upstream, "file", t.Name()+"-main-1")

	repo := mustCreateRepoAndMirror(t, upstream, root, "", "")

	t.Log("TEST-2: forward HEAD and create dir1 on HEAD")

	dir1SHA2 := mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-2")
	fileSHA2 := mustCommit(t, upstream, "file", t.Name()+"-main-2")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	assertCommitLog(t, repo, "HEAD", "", fileSHA2, t.Name()+"-main-2", []string{"file"})
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA2, t.Name()+"-dir1-main-2", []string{filepath.Join("dir1", "file")})

	t.Log("TEST-3: forward HEAD and create other-branch")

	dir1SHA3 := mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-3")
	// create branch and push commits
	mustExec(t, upstream, "git", "checkout", "-q", "-b", otherBranch)
	dir2SHA3 := mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-dir2-other-3")
	fileOtherSHA3 := mustCommit(t, upstream, "file", t.Name()+"-other-3")
	// check out master and push more commit
	mustExec(t, upstream, "git", "checkout", "-q", testMainBranch)
	mustCommit(t, upstream, "file2", t.Name()+"-main-3")
	// merge other branch to master and get merge commit
	mustExec(t, upstream, "git", "merge", "--no-ff", otherBranch, "-m", "Merging otherBranch with no-ff v1")
	mergeCommit1 := mustExec(t, upstream, "git", "rev-list", "-n1", "HEAD")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	assertCommitLog(t, repo, "HEAD", "", mergeCommit1, "Merging otherBranch with no-ff v1", []string{})
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA3, t.Name()+"-dir1-main-3", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, testMainBranch, "dir2", dir2SHA3, t.Name()+"-dir2-other-3", []string{filepath.Join("dir2", "file")})

	wantDiffList := []repository.CommitInfo{
		{Hash: mergeCommit1},
		{Hash: fileOtherSHA3, ChangedFiles: []string{"file"}},
		{Hash: dir2SHA3, ChangedFiles: []string{filepath.Join("dir2", "file")}},
	}
	if got, err := repo.MergeCommits(txtCtx, mergeCommit1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if diff := cmp.Diff(wantDiffList, got); diff != "" {
		t.Errorf("CommitsOfMergeCommit() mismatch (-want +got):\n%s", diff)
	}

	t.Log("TEST-4: add more commits to same other-branch and merge")

	dir1SHA4 := mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-4")
	// switch to other branch and push commits
	mustExec(t, upstream, "git", "checkout", "-q", otherBranch)
	dir2SHA4 := mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-dir2-other-4")
	fileOtherSHA4 := mustCommit(t, upstream, "file", t.Name()+"-other-4")
	// check out master and push more commit
	mustExec(t, upstream, "git", "checkout", "-q", testMainBranch)
	mustCommit(t, upstream, "file2", t.Name()+"-main-4")
	// merge other branch to master and get merge commit
	mustExec(t, upstream, "git", "merge", "--no-ff", otherBranch, "-m", "Merging otherBranch with no-ff v2")
	mergeCommit2 := mustExec(t, upstream, "git", "rev-list", "-n1", "HEAD")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	assertCommitLog(t, repo, "HEAD", "", mergeCommit2, "Merging otherBranch with no-ff v2", []string{})
	assertCommitLog(t, repo, testMainBranch, "dir1", dir1SHA4, t.Name()+"-dir1-main-4", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, testMainBranch, "dir2", dir2SHA4, t.Name()+"-dir2-other-4", []string{filepath.Join("dir2", "file")})

	wantDiffList = []repository.CommitInfo{
		{Hash: mergeCommit2},
		// new commits on same branch
		{Hash: fileOtherSHA4, ChangedFiles: []string{"file"}},
		{Hash: dir2SHA4, ChangedFiles: []string{filepath.Join("dir2", "file")}},
	}
	if got, err := repo.MergeCommits(txtCtx, mergeCommit2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if diff := cmp.Diff(wantDiffList, got); diff != "" {
		t.Errorf("CommitsOfMergeCommit() mismatch (-want +got):\n%s", diff)
	}

	t.Log("TEST-5: create new branch and then do squash merge")
	otherBranch = otherBranch + "v2"

	dir1SHA5 := mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-5")
	// create branch and push commits
	mustExec(t, upstream, "git", "checkout", "-q", "-b", otherBranch)
	mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-dir2-other-5")
	mustCommit(t, upstream, "file", t.Name()+"-other-5")
	// check out master and push more commit
	mustExec(t, upstream, "git", "checkout", "-q", testMainBranch)
	mustCommit(t, upstream, "file2", t.Name()+"-main-5")
	// merge other branch to master and get merge commit
	mustExec(t, upstream, "git", "merge", "--squash", otherBranch)
	mustExec(t, upstream, "git", "commit", "-m", "Merging otherBranch with squash v1")
	mergeCommit3 := mustExec(t, upstream, "git", "rev-list", "-n1", "HEAD")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	assertCommitLog(t, repo, "HEAD", "", mergeCommit3, "Merging otherBranch with squash v1", []string{})
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA5, t.Name()+"-dir1-main-5", []string{filepath.Join("dir1", "file")})
	assertCommitLog(t, repo, "HEAD", "dir2", mergeCommit3, "Merging otherBranch with squash v1", []string{filepath.Join("dir2", "file"), "file"})

	wantDiffList = []repository.CommitInfo{
		{Hash: mergeCommit3, ChangedFiles: []string{filepath.Join("dir2", "file"), "file"}},
	}
	if got, err := repo.MergeCommits(txtCtx, mergeCommit3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if diff := cmp.Diff(wantDiffList, got); diff != "" {
		t.Errorf("CommitsOfMergeCommit() mismatch (-want +got):\n%s", diff)
	}
}

func Test_clone_branch(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)
	tempClone := mustTmpDir(t)
	defer os.RemoveAll(tempClone)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	otherBranch := "other-branch"

	t.Log("TEST-1: init upstream and verify 1st commit after mirror")

	mustInitRepo(t, upstream, "file", t.Name()+"-main-1")
	remoteSHA := mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-1")

	repo := mustCreateRepoAndMirror(t, upstream, root, "", "")

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, testMainBranch, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
	}

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, "HEAD", nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
	}

	t.Log("TEST-2: forward HEAD and create other-branch")

	mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-2")
	mustExec(t, upstream, "git", "checkout", "-q", "-b", otherBranch)
	mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-dir2-other-2")
	remoteOtherSHA := mustCommit(t, upstream, "file", t.Name()+"-other-2")
	mustExec(t, upstream, "git", "checkout", "-q", testMainBranch)
	remoteSHA2 := mustCommit(t, upstream, "file", t.Name()+"-main-2")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// Clone other branch
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, otherBranch, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteOtherSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteOtherSHA)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-other-2")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir2", "file")), t.Name()+"-dir2-other-2")
	}

	// Clone other branch with dir2 pathspec
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, otherBranch, []string{"dir2"}, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteOtherSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteOtherSHA)
		}
		assertMissingFile(t, tempClone, "file")
		assertMissingFile(t, filepath.Join(tempClone, "dir1"), "/file")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir2", "file")), t.Name()+"-dir2-other-2")
	}

	// Clone main
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, testMainBranch, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA2)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-2")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
	}

	// Clone main
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, testMainBranch, []string{"dir1"}, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA2)
		}
		assertMissingFile(t, tempClone, "file")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
		assertMissingFile(t, filepath.Join(tempClone, "dir2"), "/file")
	}

	// Clone HEAD
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, "HEAD", nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA2)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-2")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
	}

	// Clone HEAD
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, "HEAD", []string{"dir1"}, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA2)
		}
		assertMissingFile(t, tempClone, "file")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
		assertMissingFile(t, filepath.Join(tempClone, "dir2"), "/file")
	}

	t.Log("TEST-3: move HEAD backward to init")
	// do not delete other-branch
	mustExec(t, upstream, "git", "reset", "-q", "--hard", remoteSHA)

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, testMainBranch, nil, true); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
		assertMissingFile(t, tempClone, ".git")
	}

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, "HEAD", nil, true); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
		assertMissingFile(t, tempClone, ".git")
	}

	// we still have other branch
	// Clone other branch with dir2 pathspec
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, otherBranch, []string{"dir1"}, true); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteOtherSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteOtherSHA)
		}
		assertMissingFile(t, tempClone, "file")
		assertMissingFile(t, filepath.Join(tempClone, "dir2"), "/file")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
		assertMissingFile(t, tempClone, ".git")
	}

	t.Log("TEST-4: delete other branch")
	mustExec(t, upstream, "git", "branch", "-D", otherBranch)

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	if _, err := repo.Clone(txtCtx, tempClone, otherBranch, nil, true); err == nil {
		t.Errorf("unexpected success for non-existent branch:%s", otherBranch)
	}
}

func Test_clone_tag_sha(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)
	tempClone := mustTmpDir(t)
	defer os.RemoveAll(tempClone)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	otherBranch := "other-branch"
	tag := "e2e-tag"
	sha := "" // will be calculated later

	t.Log("TEST-1: init upstream and verify 1st commit after mirror")

	mustInitRepo(t, upstream, "file", t.Name()+"-main-1")
	sha = mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-1")
	// add tag at current commit
	mustExec(t, upstream, "git", "tag", "-af", tag, "-m", t.Name()+"-main-1")

	repo := mustCreateRepoAndMirror(t, upstream, root, "", "")

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, tag, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != sha {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, sha)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
	}

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, sha, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != sha {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, sha)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
	}

	t.Log("TEST-2: forward HEAD and create other-branch")

	mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-2")
	mustExec(t, upstream, "git", "checkout", "-q", "-b", otherBranch)
	remoteDir2SHA := mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-dir2-other-2")
	remoteOtherSHA := mustCommit(t, upstream, "file", t.Name()+"-other-2")
	mustExec(t, upstream, "git", "checkout", "-q", testMainBranch)
	remoteSHA2 := mustCommit(t, upstream, "file", t.Name()+"-main-2")
	mustExec(t, upstream, "git", "tag", "-af", tag, "-m", t.Name()+"-main-2")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// Clone sha without path
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, remoteOtherSHA, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteOtherSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteOtherSHA)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-other-2")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir2", "file")), t.Name()+"-dir2-other-2")
	}

	// Clone sha with path
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, remoteDir2SHA, []string{"dir2"}, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteDir2SHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteDir2SHA)
		}
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir2", "file")), t.Name()+"-dir2-other-2")
	}

	// Clone tag without path
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, tag, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA2)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-2")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
	}

	// Clone tag with path
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, tag, []string{"dir1"}, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA2)
		}
		assertMissingFile(t, tempClone, "file")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
		assertMissingFile(t, filepath.Join(tempClone, "dir2"), "/file")
	}

	t.Log("TEST-3: move HEAD backward to init")
	// do not delete other-branch
	mustExec(t, upstream, "git", "reset", "-q", "--hard", sha)
	mustExec(t, upstream, "git", "tag", "-af", tag, "-m", t.Name()+"-main-3")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, tag, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != sha {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, sha)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
	}

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, sha, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != sha {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, sha)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
	}
}

func Test_mirror_loop(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link1 := "link1" // on testBranchMain branch
	link2 := "link2" // on remote HEAD
	ref1 := testMainBranch
	ref2 := "HEAD"

	t.Log("TEST-1: init upstream and start mirror loop")
	mustInitRepo(t, upstream, "file", t.Name()+"-1")

	repo := mustCreateRepoAndMirror(t, upstream, root, link1, ref1)
	// add worktree for HEAD
	if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref2, Pathspecs: []string{}}); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}

	go repo.StartLoop(txtCtx)

	time.Sleep(testInterval)
	if repo.IsRunning() != true {
		t.Errorf("repo running state is still false after starting mirror loop")
	}

	// wait for the mirror
	time.Sleep(testInterval)

	// verify checkout files
	assertLinkedFile(t, root, link1, "file", t.Name()+"-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-1")

	t.Log("TEST-2: forward HEAD")

	mustCommit(t, upstream, "file", t.Name()+"-2")

	// wait for the mirror
	time.Sleep(testInterval)

	assertLinkedFile(t, root, link1, "file", t.Name()+"-2")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-2")

	t.Log("TEST-3: move HEAD backward")

	mustExec(t, upstream, "git", "reset", "-q", "--hard", "HEAD^")

	// wait for the mirror
	time.Sleep(testInterval)

	assertLinkedFile(t, root, link1, "file", t.Name()+"-1")
	assertLinkedFile(t, root, link2, "file", t.Name()+"-1")

	// STOP mirror loop
	repo.StopLoop()
	if repo.IsRunning() {
		t.Errorf("repo still running after calling StopLoop")
	}
}

// ##############################################
// RepoPool Tests
// ##############################################

func Test_RepoPool_Success(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	tempClone := mustTmpDir(t)
	defer os.RemoveAll(tempClone)

	upstream1 := filepath.Join(testTmpDir, testUpstreamRepo)
	remote1 := "file://" + upstream1
	upstream2 := filepath.Join(testTmpDir, "upstream2")
	remote2 := "file://" + upstream2
	root := filepath.Join(testTmpDir, testRoot)

	t.Log("TEST-1: init both upstream and test mirrors")

	fileU1SHA1 := mustInitRepo(t, upstream1, "file", t.Name()+"-u1-main-1")
	fileU2SHA1 := mustInitRepo(t, upstream2, "file", t.Name()+"-u2-main-1")

	rpc := repopool.Config{
		Defaults: repopool.DefaultConfig{
			Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
		},
		Repositories: []repository.Config{
			{
				Remote:    remote1,
				Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
			},
			{
				Remote:    remote2,
				Worktrees: []repository.WorktreeConfig{{Link: "link2"}},
			},
		},
	}

	rp, err := repopool.New(t.Context(), rpc, testLog, "", testENVs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// add worktree
	// we will verify this worktree in next mirror loop
	if err := rp.AddWorktreeLink(remote1, repository.WorktreeConfig{Link: "link3", Ref: "", Pathspecs: []string{}}); err != nil {
		t.Fatalf("unexpected err:%s", err)
	}

	// run initial mirror
	if err := rp.MirrorAll(context.TODO(), testTimeout); err != nil {
		t.Fatalf("unexpected err:%s", err)
	}

	// verify Hash and checked out files
	if got, err := rp.Hash(txtCtx, remote1, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if got != fileU1SHA1 {
		t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU1SHA1)
	}
	if got, err := rp.Hash(txtCtx, remote2, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if got != fileU2SHA1 {
		t.Errorf("remote2 hash mismatch got:%s want:%s", got, fileU2SHA1)
	}

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, "link1", "file", t.Name()+"-u1-main-1")
	assertLinkedFile(t, root, "link2", "file", t.Name()+"-u2-main-1")

	if cloneSHA, err := rp.Clone(txtCtx, remote1, tempClone, testMainBranch, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU1SHA1 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU1SHA1)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u1-main-1")
	}

	if cloneSHA, err := rp.Clone(txtCtx, remote2, tempClone, testMainBranch, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU2SHA1 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU2SHA1)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u2-main-1")
	}

	t.Log("TEST-2: forward both upstream and test mirrors")

	// start mirror loop
	rp.StartLoop()

	time.Sleep(time.Second)
	// start mirror loop again this should be no op
	rp.StartLoop()

	fileU1SHA2 := mustCommit(t, upstream1, "file", t.Name()+"-u1-main-2")
	fileU2SHA2 := mustCommit(t, upstream2, "file", t.Name()+"-u2-main-2")

	// wait for the mirror
	time.Sleep(time.Second)

	// verify Hash, commit msg and checked out files
	if got, err := rp.Hash(txtCtx, remote1, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if got != fileU1SHA2 {
		t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU1SHA2)
	}
	if got, err := rp.Hash(txtCtx, remote2, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if got != fileU2SHA2 {
		t.Errorf("remote2 hash mismatch got:%s want:%s", got, fileU2SHA2)
	}

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, "link1", "file", t.Name()+"-u1-main-2")
	assertLinkedFile(t, root, "link3", "file", t.Name()+"-u1-main-2")
	assertLinkedFile(t, root, "link2", "file", t.Name()+"-u2-main-2")

	if cloneSHA, err := rp.Clone(txtCtx, remote1, tempClone, testMainBranch, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU1SHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU1SHA2)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u1-main-2")
	}

	if cloneSHA, err := rp.Clone(txtCtx, remote2, tempClone, testMainBranch, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU2SHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU2SHA2)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u2-main-2")
	}

	t.Log("TEST-3: move HEAD backward on both upstream and test mirrors")

	mustExec(t, upstream1, "git", "reset", "-q", "--hard", fileU1SHA1)
	mustExec(t, upstream2, "git", "reset", "-q", "--hard", fileU2SHA1)

	// wait for the mirror
	time.Sleep(2 * time.Second)

	// verify Hash and checked out files
	if got, err := rp.Hash(txtCtx, remote1, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if got != fileU1SHA1 {
		t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU1SHA1)
	}
	if got, err := rp.Hash(txtCtx, remote2, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if got != fileU2SHA1 {
		t.Errorf("remote2 hash mismatch got:%s want:%s", got, fileU2SHA1)
	}

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, "link1", "file", t.Name()+"-u1-main-1")
	assertLinkedFile(t, root, "link3", "file", t.Name()+"-u1-main-1")
	assertLinkedFile(t, root, "link2", "file", t.Name()+"-u2-main-1")

	// remove worktrees
	if err := rp.RemoveWorktreeLink(remote2, "link2"); err != nil {
		t.Errorf("unable to remove worktree error: %v", err)
	}
	// wait for the mirror
	time.Sleep(2 * time.Second)

	assertMissingLink(t, root, "link2")

	if cloneSHA, err := rp.Clone(txtCtx, remote1, tempClone, testMainBranch, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU1SHA1 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU1SHA1)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u1-main-1")
	}

	if cloneSHA, err := rp.Clone(txtCtx, remote2, tempClone, testMainBranch, nil, false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU2SHA1 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU2SHA1)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u2-main-1")
	}

	// remove repository
	repo, _ := rp.Repository(remote1)
	if err := rp.RemoveRepository(remote1); err != nil {
		t.Errorf("unable to remove repository error: %v", err)
	}
	if len(rp.RepositoriesRemote()) > 1 {
		t.Errorf("there should be only 1 repo in repoPool now")
	}
	// once repo is removed public link should be removed
	assertMissingLink(t, root, "link1")
	// root dir should be empty
	assertMissingLink(t, repo.Directory(), "")
}

func Test_RepoPool_Error(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream1 := filepath.Join(testTmpDir, testUpstreamRepo)
	remote1 := "file://" + upstream1
	upstream2 := filepath.Join(testTmpDir, "upstream2")
	remote2 := "file://" + upstream2
	root := filepath.Join(testTmpDir, testRoot)

	nonExistingRemote := "file://" + filepath.Join(testTmpDir, "upstream3.git")

	t.Log("TEST-1: init both upstream and test mirrors")

	fileU1SHA1 := mustInitRepo(t, upstream1, "file", t.Name()+"-u1-main-1")
	fileU2SHA1 := mustInitRepo(t, upstream2, "file", t.Name()+"-u2-main-1")

	rpc := repopool.Config{
		Defaults: repopool.DefaultConfig{
			Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
		},
		Repositories: []repository.Config{
			{
				Remote:    remote1,
				Worktrees: []repository.WorktreeConfig{{Link: "link1"}},
			},
			{
				Remote:    remote2,
				Worktrees: []repository.WorktreeConfig{{Link: "link2"}},
			},
		},
	}

	rp, err := repopool.New(t.Context(), rpc, testLog, "", testENVs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// start mirror loop
	rp.StartLoop()

	time.Sleep(2 * time.Second)

	// verify Hash and checked out files
	if got, err := rp.Hash(txtCtx, remote1, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if got != fileU1SHA1 {
		t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU1SHA1)
	}
	if got, err := rp.Hash(txtCtx, remote2, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if got != fileU2SHA1 {
		t.Errorf("remote2 hash mismatch got:%s want:%s", got, fileU2SHA1)
	}

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, "link1", "file", t.Name()+"-u1-main-1")
	assertLinkedFile(t, root, "link2", "file", t.Name()+"-u2-main-1")

	t.Log("TEST-2: try adding existing repo again")

	repo1, err := rp.Repository(remote1)
	if err != nil {
		t.Errorf("unexpected err:%s", err)
	}

	if err := rp.AddRepository(repository.Config{Remote: repo1.Remote()}); err == nil {
		t.Errorf("unexpected success for non existing repo")
	} else if err != repopool.ErrExist {
		t.Errorf("error mismatch got:%s want:%s", err, repopool.ErrNotExist)
	}

	// try getting repo with wrong URL
	if _, err := rp.Repository("ssh://host.xh/repo.git"); err == nil {
		t.Errorf("unexpected success for non existing repo")
	}

	t.Log("TEST-3: try non existing repo")
	// check non existing repo
	if _, err := rp.Repository(nonExistingRemote); err == nil {
		t.Errorf("unexpected success for non existing repo")
	} else if err != repopool.ErrNotExist {
		t.Errorf("error mismatch got:%s want:%s", err, repopool.ErrNotExist)
	}
	if _, err := rp.Hash(context.Background(), nonExistingRemote, "HEAD", ""); err == nil {
		t.Errorf("unexpected success for non existing repo")
	} else if err != repopool.ErrNotExist {
		t.Errorf("error mismatch got:%s want:%s", err, repopool.ErrNotExist)
	}
	if _, err := rp.Clone(context.Background(), nonExistingRemote, testTmpDir, "HEAD", nil, false); err == nil {
		t.Errorf("unexpected success for non existing repo")
	} else if err != repopool.ErrNotExist {
		t.Errorf("error mismatch got:%s want:%s", err, repopool.ErrNotExist)
	}
}

// ##############################################
// HELPER FUNCS
// ##############################################

func mustCreateRepoAndMirror(t *testing.T, upstream, root, link, ref string) *repository.Repository {
	t.Helper()

	// create mirror repo and add link for main branch
	rc := repository.Config{
		Remote:        "file://" + upstream,
		Root:          root,
		Interval:      testInterval,
		MirrorTimeout: testTimeout,
		GitGC:         "always",
	}
	repo, err := repository.New(rc, "", testENVs, testLog)
	if err != nil {
		t.Fatalf("unable to create new repo error: %v", err)
	}
	if link != "" {
		if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link, Ref: ref, Pathspecs: []string{}}); err != nil {
			t.Fatalf("unable to add worktree error: %v", err)
		}
	}
	// Trigger a mirror
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	return repo
}

func mustInitRepo(t *testing.T, repo, file, content string) string {
	t.Helper()

	// clear old data if any
	if err := utils.ReCreate(repo); err != nil {
		t.Fatalf("unable to re-create err: %v", err)
	}

	mustExec(t, repo, "git", "init", "-q", "-b", testMainBranch)

	return mustCommit(t, repo, file, content)
}

func mustCommit(t *testing.T, repo, file, content string) string {
	t.Helper()

	dirs, _ := utils.SplitAbs(file)
	if dirs != "" && dirs != "/" {
		if err := os.MkdirAll(filepath.Join(repo, dirs), defaultDirMode); err != nil {
			t.Fatalf("unable to create file path dirs err: %v", err)
		}
	}

	if err := os.WriteFile(filepath.Join(repo, file), []byte(content), defaultDirMode); err != nil {
		t.Fatalf("unable to write to file err: %v", err)
	}
	mustExec(t, repo, "git", "add", file)
	msg := content
	if len(content) > 50 {
		msg = content[:50]
	}
	mustExec(t, repo, "git", "commit", "-m", msg)
	return mustExec(t, repo, "git", "rev-list", "-n1", "HEAD")
}

func mustTmpDir(t *testing.T) string {
	t.Helper()

	testTmpDir, err := os.MkdirTemp("", "git-mirror-e2e-*")
	if err != nil {
		t.Fatalf("unable to make dir: %v", err)
	}
	return testTmpDir
}

func assertLinkedFile(t *testing.T, root, link, file, expected string) {
	t.Helper()
	linkAbs := filepath.Join(root, link)

	if _, err := utils.ReadAbsLink(linkAbs); err != nil {
		t.Fatalf("unable to read link error: %v", err)
	}
	assertFile(t, filepath.Join(linkAbs, file), expected)
}

func assertFile(t *testing.T, absFile string, expected string) {
	t.Helper()

	if got, err := os.ReadFile(absFile); err != nil {
		t.Fatalf("unable to read file error: %v", err)
	} else if string(got) != expected {
		t.Errorf("expected %q to contain %q but got %q", absFile, expected, got)
	}
}

func assertMissingLink(t *testing.T, root, link string) {
	t.Helper()

	linkAbs := filepath.Join(root, link)

	if target, err := utils.ReadAbsLink(linkAbs); err != nil {
		t.Fatalf("unable to read link error: %v", err)
	} else if target != "" {
		t.Errorf("link %s should not have any symlink but found %s", link, target)
	}
}

func assertMissingLinkFile(t *testing.T, root, link, file string) {
	t.Helper()
	assertMissingFile(t, filepath.Join(root, link), file)
}

func assertMissingFile(t *testing.T, path, file string) {
	t.Helper()

	filepath.Join(path, file)

	_, err := os.Stat(filepath.Join(path, file))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("unable to read existing file error: %v", err)
	} else if os.IsNotExist(err) {
		return
	} else {
		t.Errorf("file (%s) exits but it should not", filepath.Join(path, file))
	}
}

func assertCommitLog(t *testing.T, repo *repository.Repository,
	ref, path, wantSHA, wantSub string,
	wantChangedFiles []string) {
	t.Helper()
	// add user info
	gotHash, err := repo.Hash(txtCtx, ref, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if gotHash != wantSHA {
		t.Errorf("ref '%s' on path '%s' SHA mismatch got:%s want:%s", ref, path, gotHash, wantSHA)
	}

	if wantSHA == "" {
		return
	}

	if got, err := repo.Subject(txtCtx, gotHash); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if got != wantSub {
		t.Errorf("subject mismatch sha:%s got:%s want:%s", gotHash, got, wantSub)
	}

	if len(wantChangedFiles) > 0 {
		if got, err := repo.ChangedFiles(txtCtx, gotHash); err != nil {
			t.Fatalf("unexpected error: %v", err)
		} else if slices.Compare(got, wantChangedFiles) != 0 {
			t.Errorf("changed file mismatch sha:%s got:%s want:%s", gotHash, got, wantChangedFiles)
		}
	}
}

func mustExec(t *testing.T, cwd string, command string, arg ...string) string {
	t.Helper()

	out, err := utils.RunCommand(context.TODO(), slog.Default(), testENVs, cwd, command, arg...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}
