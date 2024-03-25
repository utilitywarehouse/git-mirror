package mirror

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testUpstreamRepo = "upstream1"
	testRoot         = "root"
	testInterval     = 1 * time.Second

	testMainBranch = "e2e-mai"
	testGitUser    = "git-mirror-e2e"
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
	mustExec(t, repo1.dir, "git", "remote", "set-url", "origin", "blah/blah")

	// create repo using same paths...
	mustCreateRepoAndMirror(t, upstream, root, link, ref)

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, link, "file", t.Name())

	t.Log("TEST-2: modify remote 'origin' fetch path refs")
	mustExec(t, repo1.dir, "git", "config", "--add", "remote.origin.fetch", "+refs/heads/master:refs/remotes/origin/master")

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
	if err := repo.AddWorktreeLink(link2, ref2, ""); err != nil {
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

	rc := RepositoryConfig{
		Remote:    "file://" + upstream,
		Root:      root,
		Interval:  testInterval,
		GitGC:     "always",
		Worktrees: []WorktreeConfig{{Link: link, Ref: ref}},
	}
	repo, err := NewRepository(rc, testENVs, testLog)
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
	if err := repo.AddWorktreeLink(link2, ref2, ""); err != nil {
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
}

func Test_mirror_with_pathspec(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link1 := "link1" // on testBranchMain branch
	link2 := "link2" // on remote HEAD -- dir2
	link3 := "link3" // on remote HEAD -- dir3
	ref1 := testMainBranch
	ref2 := "HEAD"
	ref3 := "HEAD"
	pathSpec2 := "dir2"
	pathSpec3 := "dir3"

	t.Log("TEST-1: init upstream without other dirs")

	firstSHA := mustInitRepo(t, upstream, "file", t.Name()+"-main-1")

	repo := mustCreateRepoAndMirror(t, upstream, root, link1, ref1)
	// add worktree for HEAD
	if err := repo.AddWorktreeLink(link2, ref2, pathSpec2); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	if err := repo.AddWorktreeLink(link3, ref3, pathSpec3); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
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

	t.Log("TEST-3: forward HEAD and create dir3 to test link3")

	mustCommit(t, upstream, "file", t.Name()+"-main-3")
	mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-main-3")
	mustCommit(t, upstream, filepath.Join("dir3", "file"), t.Name()+"-main-3")
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

	t.Log("TEST-3: move HEAD backward by 3 commit to original state")

	mustExec(t, upstream, "git", "reset", "-q", "--hard", firstSHA)
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
	if err := repo1.AddWorktreeLink(link2, ref2, ""); err != nil {
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
	if err := repo2.AddWorktreeLink(link2, ref1, ""); err != nil {
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
	if err := repo.AddWorktreeLink(link2, ref2, ""); err != nil {
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

	wtPath, err := repo.workTreeLinks[link1].currentWorktree()
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

	assertCommitLog(t, repo, "HEAD", "", fileSHA1, t.Name()+"-main-1")
	assertCommitLog(t, repo, testMainBranch, "", fileSHA1, t.Name()+"-main-1")

	t.Log("TEST-2: forward HEAD and create dir1 on HEAD")

	dir1SHA2 := mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-2")
	fileSHA2 := mustCommit(t, upstream, "file", t.Name()+"-main-2")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}
	// log @ root
	assertCommitLog(t, repo, "HEAD", "", fileSHA2, t.Name()+"-main-2")
	assertCommitLog(t, repo, testMainBranch, "", fileSHA2, t.Name()+"-main-2")
	// log @ dir1
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA2, t.Name()+"-dir1-main-2")
	assertCommitLog(t, repo, testMainBranch, "dir1", dir1SHA2, t.Name()+"-dir1-main-2")

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
	assertCommitLog(t, repo, "HEAD", "", fileSHA3, t.Name()+"-main-3")
	assertCommitLog(t, repo, testMainBranch, "", fileSHA3, t.Name()+"-main-3")
	// log @ HEAD on dir
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA3, t.Name()+"-dir1-main-3")
	assertCommitLog(t, repo, testMainBranch, "dir1", dir1SHA3, t.Name()+"-dir1-main-3")
	assertCommitLog(t, repo, testMainBranch, "dir2", "", "")
	// log @ other-branch
	assertCommitLog(t, repo, otherBranch, "", fileOtherSHA3, t.Name()+"-other-3")
	assertCommitLog(t, repo, otherBranch, "dir1", dir1SHA3, t.Name()+"-dir1-main-3")
	assertCommitLog(t, repo, otherBranch, "dir2", dir2SHA3, t.Name()+"-dir2-other-3")

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
	assertCommitLog(t, repo, "HEAD", "", fileSHA4, t.Name()+"-main-4")
	assertCommitLog(t, repo, testMainBranch, "", fileSHA4, t.Name()+"-main-4")
	// log @ HEAD on dir
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA4, t.Name()+"-dir1-main-4")
	assertCommitLog(t, repo, testMainBranch, "dir1", dir1SHA4, t.Name()+"-dir1-main-4")
	assertCommitLog(t, repo, testMainBranch, "dir2", "", "")
	// log @ other-branch
	assertCommitLog(t, repo, otherBranch, "", fileOtherSHA4, t.Name()+"-other-4")
	// dir1 on other-branch will be always behind
	assertCommitLog(t, repo, otherBranch, "dir1", dir1SHA3, t.Name()+"-dir1-main-3")
	assertCommitLog(t, repo, otherBranch, "dir2", dir2SHA4, t.Name()+"-dir2-other-4")

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
	assertCommitLog(t, repo, "HEAD", "", fileSHA3, t.Name()+"-main-3")
	assertCommitLog(t, repo, testMainBranch, "", fileSHA3, t.Name()+"-main-3")
	// log @ HEAD on dir
	assertCommitLog(t, repo, "HEAD", "dir1", dir1SHA3, t.Name()+"-dir1-main-3")
	assertCommitLog(t, repo, testMainBranch, "dir1", dir1SHA3, t.Name()+"-dir1-main-3")
	assertCommitLog(t, repo, testMainBranch, "dir2", "", "")
	// log @ other-branch
	assertCommitLog(t, repo, otherBranch, "", fileOtherSHA3, t.Name()+"-other-3")
	assertCommitLog(t, repo, otherBranch, "dir1", dir1SHA3, t.Name()+"-dir1-main-3")
	assertCommitLog(t, repo, otherBranch, "dir2", dir2SHA3, t.Name()+"-dir2-other-3")

	t.Log("TEST-5: move HEAD backward to test1 and delete other-branch")

	mustExec(t, upstream, "git", "branch", "-D", otherBranch)
	mustExec(t, upstream, "git", "reset", "-q", "--hard", fileSHA1)

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// log @ HEAD on root
	assertCommitLog(t, repo, "HEAD", "", fileSHA1, t.Name()+"-main-1")
	assertCommitLog(t, repo, testMainBranch, "", fileSHA1, t.Name()+"-main-1")
	// log @ HEAD on dir
	assertCommitLog(t, repo, "HEAD", "dir1", "", "")
	assertCommitLog(t, repo, testMainBranch, "dir1", "", "")
	assertCommitLog(t, repo, testMainBranch, "dir2", "", "")

	// all checks on other-branch should fail
	if _, err := repo.Hash(txtCtx, otherBranch, ""); err == nil {
		t.Errorf("unexpected success for non-existent branch:%s", otherBranch)
	}
	if _, err := repo.Hash(txtCtx, otherBranch, "dir1"); err == nil {
		t.Errorf("unexpected success for non-existent branch:%s", otherBranch)
	}
	if _, err := repo.LogMsg(txtCtx, otherBranch, ""); err == nil {
		t.Errorf("unexpected success for non-existent branch:%s", otherBranch)
	}
	if _, err := repo.LogMsg(txtCtx, otherBranch, "dir1"); err == nil {
		t.Errorf("unexpected success for non-existent branch:%s", otherBranch)
	}
}

func Test_clone(t *testing.T) {
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

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, testMainBranch, "", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
	}

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, "HEAD", "", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
	}

	t.Log("TEST-2: forward HEAD and create other-branch")

	remoteDir1SHA := mustCommit(t, upstream, filepath.Join("dir1", "file"), t.Name()+"-dir1-main-2")
	mustExec(t, upstream, "git", "checkout", "-q", "-b", otherBranch)
	remoteDir2SHA := mustCommit(t, upstream, filepath.Join("dir2", "file"), t.Name()+"-dir2-other-2")
	remoteOtherSHA := mustCommit(t, upstream, "file", t.Name()+"-other-2")
	mustExec(t, upstream, "git", "checkout", "-q", testMainBranch)
	remoteSHA2 := mustCommit(t, upstream, "file", t.Name()+"-main-2")

	// mirror new commits
	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// Clone other branch
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, otherBranch, "", false); err != nil {
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
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, otherBranch, "dir2", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteDir2SHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteDir2SHA)
		}
		assertMissingFile(t, tempClone, "file")
		assertMissingFile(t, filepath.Join(tempClone, "dir1"), "/file")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir2", "file")), t.Name()+"-dir2-other-2")
	}

	// Clone main
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, testMainBranch, "", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA2)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-2")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
	}

	// Clone main
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, testMainBranch, "dir1", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteDir1SHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteDir1SHA)
		}
		assertMissingFile(t, tempClone, "file")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
		assertMissingFile(t, filepath.Join(tempClone, "dir2"), "/file")
	}

	// Clone HEAD
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, "HEAD", "", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA2)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-2")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-2")
	}

	// Clone HEAD
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, "HEAD", "dir1", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteDir1SHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteDir1SHA)
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

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, testMainBranch, "", true); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteSHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteSHA)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-main-1")
		assertFile(t, filepath.Join(tempClone, filepath.Join("dir1", "file")), t.Name()+"-dir1-main-1")
		assertMissingFile(t, tempClone, ".git")
	}

	if cloneSHA, err := repo.Clone(txtCtx, tempClone, "HEAD", "", true); err != nil {
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
	if cloneSHA, err := repo.Clone(txtCtx, tempClone, otherBranch, "dir1", true); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != remoteDir1SHA {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, remoteDir1SHA)
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

	if _, err := repo.Clone(txtCtx, tempClone, otherBranch, "", true); err == nil {
		t.Errorf("unexpected success for non-existent branch:%s", otherBranch)
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
	if err := repo.AddWorktreeLink(link2, ref2, ""); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}

	go repo.StartLoop(txtCtx)

	time.Sleep(testInterval)
	if repo.running != true {
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
	repo.stop <- true

	time.Sleep(testInterval)

	if repo.running != false {
		t.Errorf("repo still running after sending stop signal")
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

	rpc := RepoPoolConfig{
		Defaults: DefaultConfig{
			Root: root, Interval: testInterval, GitGC: "always",
		},
		Repositories: []RepositoryConfig{
			{
				Remote:    remote1,
				Worktrees: []WorktreeConfig{{Link: "link1"}},
			},
			{
				Remote:    remote2,
				Worktrees: []WorktreeConfig{{Link: "link2"}},
			},
		},
	}

	rp, err := NewRepoPool(rpc, testLog, testENVs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// add worktree
	// we will verify this worktree in next mirror loop
	if err := rp.AddWorktreeLink(remote1, "link3", "", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	}

	time.Sleep(time.Second)

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

	if cloneSHA, err := rp.Clone(txtCtx, remote1, tempClone, testMainBranch, "", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU1SHA1 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU1SHA1)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u1-main-1")
	}

	if cloneSHA, err := rp.Clone(txtCtx, remote2, tempClone, testMainBranch, "", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU2SHA1 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU2SHA1)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u2-main-1")
	}

	t.Log("TEST-2: forward both upstream and test mirrors")

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

	if got, err := rp.LogMsg(txtCtx, remote1, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if !strings.Contains(got, t.Name()+"-u1-main-2") {
		t.Errorf("remote1 commit msg mismatch got:%s want:%s", got, t.Name()+"-u1-main-2")
	}
	if got, err := rp.LogMsg(txtCtx, remote2, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if !strings.Contains(got, t.Name()+"-u2-main-2") {
		t.Errorf("remote2 commit msg mismatch got:%s want:%s", got, t.Name()+"-u2-main-2")
	}

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, "link1", "file", t.Name()+"-u1-main-2")
	assertLinkedFile(t, root, "link3", "file", t.Name()+"-u1-main-2")
	assertLinkedFile(t, root, "link2", "file", t.Name()+"-u2-main-2")

	if cloneSHA, err := rp.Clone(txtCtx, remote1, tempClone, testMainBranch, "", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU1SHA2 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU1SHA2)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u1-main-2")
	}

	if cloneSHA, err := rp.Clone(txtCtx, remote2, tempClone, testMainBranch, "", false); err != nil {
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

	if got, err := rp.LogMsg(txtCtx, remote1, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if !strings.Contains(got, t.Name()+"-u1-main-1") {
		t.Errorf("remote1 commit msg mismatch got:%s want:%s", got, t.Name()+"-u1-main-1")
	}
	if got, err := rp.LogMsg(txtCtx, remote2, "HEAD", ""); err != nil {
		t.Fatalf("unexpected err:%s", err)
	} else if !strings.Contains(got, t.Name()+"-u2-main-1") {
		t.Errorf("remote2 commit msg mismatch got:%s want:%s", got, t.Name()+"-u2-main-1")
	}

	// after mirror we should expect a symlink at root and a file with test function name
	assertLinkedFile(t, root, "link1", "file", t.Name()+"-u1-main-1")
	assertLinkedFile(t, root, "link3", "file", t.Name()+"-u1-main-1")
	assertLinkedFile(t, root, "link2", "file", t.Name()+"-u2-main-1")

	if cloneSHA, err := rp.Clone(txtCtx, remote1, tempClone, testMainBranch, "", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU1SHA1 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU1SHA1)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u1-main-1")
	}

	if cloneSHA, err := rp.Clone(txtCtx, remote2, tempClone, testMainBranch, "", false); err != nil {
		t.Fatalf("unexpected error %s", err)
	} else {
		if cloneSHA != fileU2SHA1 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU2SHA1)
		}
		assertFile(t, filepath.Join(tempClone, "file"), t.Name()+"-u2-main-1")
	}
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

	rpc := RepoPoolConfig{
		Defaults: DefaultConfig{
			Root: root, Interval: testInterval, GitGC: "always",
		},
		Repositories: []RepositoryConfig{
			{
				Remote:    remote1,
				Worktrees: []WorktreeConfig{{Link: "link1"}},
			},
			{
				Remote:    remote2,
				Worktrees: []WorktreeConfig{{Link: "link2"}},
			},
		},
	}

	rp, err := NewRepoPool(rpc, testLog, testENVs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(time.Second)

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

	repo1, err := rp.Repo(remote1)
	if err != nil {
		t.Errorf("unexpected err:%s", err)
	}

	if err := rp.AddRepository(repo1); err == nil {
		t.Errorf("unexpected success for non existing repo")
	} else if err != ErrExist {
		t.Errorf("error mismatch got:%s want:%s", err, ErrNotExist)
	}

	// try getting repo with wrong URL
	if _, err := rp.Repo("ssh://host.xh/repo.git"); err == nil {
		t.Errorf("unexpected success for non existing repo")
	}

	t.Log("TEST-3: try non existing repo")
	// check non existing repo
	if _, err := rp.Repo(nonExistingRemote); err == nil {
		t.Errorf("unexpected success for non existing repo")
	} else if err != ErrNotExist {
		t.Errorf("error mismatch got:%s want:%s", err, ErrNotExist)
	}
	if _, err := rp.Hash(context.Background(), nonExistingRemote, "HEAD", ""); err == nil {
		t.Errorf("unexpected success for non existing repo")
	} else if err != ErrNotExist {
		t.Errorf("error mismatch got:%s want:%s", err, ErrNotExist)
	}
	if _, err := rp.LogMsg(context.Background(), nonExistingRemote, "HEAD", ""); err == nil {
		t.Errorf("unexpected success for non existing repo")
	} else if err != ErrNotExist {
		t.Errorf("error mismatch got:%s want:%s", err, ErrNotExist)
	}
	if _, err := rp.Clone(context.Background(), nonExistingRemote, testTmpDir, "HEAD", "", false); err == nil {
		t.Errorf("unexpected success for non existing repo")
	} else if err != ErrNotExist {
		t.Errorf("error mismatch got:%s want:%s", err, ErrNotExist)
	}
}

func TestRepoPool_validateLinkPath(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream1 := filepath.Join(testTmpDir, testUpstreamRepo)
	remote1 := "file://" + upstream1
	upstream2 := filepath.Join(testTmpDir, "upstream2")
	remote2 := "file://" + upstream2
	root := filepath.Join(testTmpDir, testRoot)

	t.Log("TEST-1: init both upstream and test mirrors")

	mustInitRepo(t, upstream1, "file", t.Name()+"-u1-main-1")
	mustInitRepo(t, upstream2, "file", t.Name()+"-u2-main-1")

	rpc := RepoPoolConfig{
		Defaults: DefaultConfig{
			Root: root, Interval: testInterval, GitGC: "always",
		},
		Repositories: []RepositoryConfig{
			{
				Remote:    remote1,
				Worktrees: []WorktreeConfig{{Link: "link1"}},
			},
			{
				Remote:    remote2,
				Worktrees: []WorktreeConfig{{Link: "link2"}},
			},
		},
	}

	rp, err := NewRepoPool(rpc, nil, testENVs)
	if err != nil {
		t.Fatalf("unexpected err:%s", err)
	}

	tests := []struct {
		name    string
		repo    *Repository
		link    string
		wantErr bool
	}{
		{"add-repo2-link-to-repo1", rp.repos[0], "link2", true},
		{"add-repo2-abs-link-to-repo1", rp.repos[0], filepath.Join(root, "link2"), true},
		{"add-repo1-link-to-repo2", rp.repos[1], "link1", true},
		{"add-repo1-abs-link-to-repo2", rp.repos[1], filepath.Join(root, "link1"), true},
		{"add-new-link", rp.repos[0], "link3", false},
		{"add-new-link", rp.repos[1], "link3", false},
		{"add-new-abs-link", rp.repos[0], filepath.Join(testTmpDir, "temp", "link1"), false},
		{"add-new-abs-link", rp.repos[1], filepath.Join(testTmpDir, "temp", "link2"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if err := rp.validateLinkPath(tt.repo, tt.link); (err != nil) != tt.wantErr {
				t.Errorf("RepoPool.validateLinkPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ##############################################
// HELPER FUNCS
// ##############################################

func mustCreateRepoAndMirror(t *testing.T, upstream, root, link, ref string) *Repository {
	t.Helper()

	// create mirror repo and add link for main branch
	rc := RepositoryConfig{
		Remote:   "file://" + upstream,
		Root:     root,
		Interval: testInterval,
		GitGC:    "always",
	}
	repo, err := NewRepository(rc, testENVs, testLog)
	if err != nil {
		t.Fatalf("unable to create new repo error: %v", err)
	}
	if link != "" {
		if err := repo.AddWorktreeLink(link, ref, ""); err != nil {
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
	if err := reCreate(repo); err != nil {
		t.Fatalf("unable to re-create err: %v", err)
	}

	mustExec(t, repo, "git", "init", "-q", "-b", testMainBranch)

	return mustCommit(t, repo, file, content)
}

func mustCommit(t *testing.T, repo, file, content string) string {
	t.Helper()

	dirs, _ := SplitAbs(file)
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

	if _, err := readAbsLink(linkAbs); err != nil {
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

	if target, err := readAbsLink(linkAbs); err != nil {
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

func assertCommitLog(t *testing.T, repo *Repository, ref, path, wantSHA, wantMsg string) {
	t.Helper()

	// add user info
	if wantMsg != "" {
		wantMsg = fmt.Sprintf("%s (%s)", wantMsg, testGitUser)
	}
	if got, err := repo.Hash(txtCtx, ref, path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if got != wantSHA {
		t.Errorf("ref '%s' on path '%s' SHA mismatch got:%s want:%s", ref, path, got, wantSHA)
	}
	if got, err := repo.LogMsg(txtCtx, ref, path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if got != wantMsg {
		t.Errorf("ref '%s' on path '%s' Msg mismatch got:%s want:%s", ref, path, got, wantMsg)
	}
}

func mustExec(t *testing.T, cwd string, name string, arg ...string) string {
	t.Helper()

	cmd := exec.Command(name, arg...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	cmd.Env = testENVs

	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("err:%v run(%s): { stdoutStderr %q }", cmd.String(), err, stdoutStderr)
	}
	return strings.TrimSpace(string(stdoutStderr))
}
