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

func Test_Init_Scenarios(t *testing.T) {
	t.Run("root doesnt exist", func(t *testing.T) {
		env := setupEnv(t)
		defer env.cleanup()
		env.initUpstream("file", env.name)
		env.createAndMirror("link", testMainBranch)
		env.assertFileLinked("link", "file", env.name)
	})

	t.Run("existing root", func(t *testing.T) {
		env := setupEnv(t)
		defer env.cleanup()
		env.initUpstream("file", env.name)
		env.createAndMirror("link", testMainBranch)
		// re-create mirror repo with same root and worktree with absolute path
		env.createAndMirror(filepath.Join(env.root, "link"), testMainBranch)
		env.assertFileLinked("link", "file", env.name)
	})

	t.Run("existing root with diff repo", func(t *testing.T) {
		env := setupEnv(t)
		defer env.cleanup()
		env.initUpstream("file", env.name)
		mustInitRepo(t, filepath.Join(env.root, testUpstreamRepo), "file", env.name)

		env.createAndMirror(filepath.Join("sub", "dir", "link"), testMainBranch)
		env.assertFileLinked(filepath.Join("sub", "dir", "link"), "file", env.name)
		// change root so that its under existing repo and create new mirror repo
		env.root = filepath.Join(env.root, testUpstreamRepo)
		os.MkdirAll(filepath.Join(env.root, testUpstreamRepo, testUpstreamRepo), defaultDirMode)
		env.createAndMirror("link2", testMainBranch)
		env.assertFileLinked("link2", "file", env.name)
	})

	t.Run("fails sanity tests - recover gracefully", func(t *testing.T) {
		env := setupEnv(t)
		defer env.cleanup()
		env.initUpstream("file", env.name)
		env.createAndMirror("link", testMainBranch)

		// Modify remote origin URL
		env.execInRepo("git", "remote", "set-url", "origin", "blah/blah")
		env.createAndMirror("link", testMainBranch)
		env.assertFileLinked("link", "file", env.name)

		// Modify remote origin fetch path refs
		env.execInRepo("git", "config", "--add", "remote.origin.fetch", "+refs/heads/master:refs/remotes/origin/master")
		env.createAndMirror("link", testMainBranch)
		env.assertFileLinked("link", "file", env.name)
	})
}

// Test_Mirror_Lifecycle_Combined tests the full lifecycle (forward, backward,
// force-pushes, and deletions) across main, HEAD, branches, and tags simultaneously.
func Test_Mirror_Lifecycle_Combined(t *testing.T) {
	env := setupEnv(t)
	defer env.cleanup()

	lMain, lHead, lOther, lTag := "link-main", "link-head", "link-other", "link-tag"
	rMain, rHead, rOther, rTag := testMainBranch, "HEAD", "other-branch", "e2e-tag"

	var initialMainSHA, initialOtherSHA string

	t.Run("init upstream and add references", func(t *testing.T) {
		env.t = t
		initialMainSHA = env.initUpstream("file", env.name+"-main-1")
		env.checkout(rOther, true)
		initialOtherSHA = env.commit("file", env.name+"-other-1")
		env.checkout(rMain, false)
		env.exec("git", "tag", "-af", rTag, "-m", env.name+"-main-1")

		env.createAndMirror(lMain, rMain)

		env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: lHead, Ref: rHead})
		env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: lOther, Ref: rOther})
		env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: lTag, Ref: rTag})

		env.mirror()

		env.assertFilesLinked(lMain, map[string]string{"file": env.name + "-main-1"}, nil)
		env.assertFilesLinked(lHead, map[string]string{"file": env.name + "-main-1"}, nil)
		env.assertFilesLinked(lOther, map[string]string{"file": env.name + "-other-1"}, nil)
		env.assertFilesLinked(lTag, map[string]string{"file": env.name + "-main-1"}, nil)
	})

	t.Run("forward all references and add files", func(t *testing.T) {
		env.t = t
		env.commit("file", env.name+"-main-2")
		env.commit("delete_me.txt", "to be deleted")
		env.exec("git", "tag", "-af", rTag, "-m", env.name+"-main-2")
		env.checkout(rOther, false)
		env.commit("file", env.name+"-other-2")
		env.checkout(rMain, false)

		env.mirror()

		env.assertFilesLinked(lMain, map[string]string{"file": env.name + "-main-2", "delete_me.txt": "to be deleted"}, nil)
		env.assertFilesLinked(lHead, map[string]string{"file": env.name + "-main-2", "delete_me.txt": "to be deleted"}, nil)
		env.assertFilesLinked(lOther, map[string]string{"file": env.name + "-other-2"}, []string{"delete_me.txt"})
		env.assertFilesLinked(lTag, map[string]string{"file": env.name + "-main-2"}, nil)
	})

	t.Run("upstream file deletion", func(t *testing.T) {
		env.t = t
		env.exec("git", "rm", "delete_me.txt")
		env.exec("git", "commit", "-m", "delete file")
		env.mirror()

		// Verify delete_me.txt is removed from the active worktrees
		env.assertFilesLinked(lMain, map[string]string{"file": env.name + "-main-2"}, []string{"delete_me.txt"})
		env.assertFilesLinked(lHead, map[string]string{"file": env.name + "-main-2"}, []string{"delete_me.txt"})
	})

	t.Run("move all references backward", func(t *testing.T) {
		env.t = t
		env.exec("git", "reset", "-q", "--hard", initialMainSHA)
		env.exec("git", "tag", "-af", rTag, "-m", "moving back")
		env.checkout(rOther, false)
		env.exec("git", "reset", "-q", "--hard", initialOtherSHA)
		env.checkout(rMain, false)

		env.mirror()

		env.assertFilesLinked(lMain, map[string]string{"file": env.name + "-main-1"}, []string{"delete_me.txt"})
		env.assertFilesLinked(lHead, map[string]string{"file": env.name + "-main-1"}, []string{"delete_me.txt"})
		env.assertFilesLinked(lOther, map[string]string{"file": env.name + "-other-1"}, nil)
		env.assertFilesLinked(lTag, map[string]string{"file": env.name + "-main-1"}, nil)
	})

	t.Run("divergent history (force push)", func(t *testing.T) {
		env.t = t
		// Because we moved backward in the previous step, these new commits create a divergent history.
		env.commit("file", env.name+"-divergent-main")
		env.exec("git", "tag", "-af", rTag, "-m", "divergent tag")
		env.checkout(rOther, false)
		env.commit("file", env.name+"-divergent-other")
		env.checkout(rMain, false)

		env.mirror() // Mirror should fetch and forcefully update without conflicts

		env.assertFilesLinked(lMain, map[string]string{"file": env.name + "-divergent-main"}, nil)
		env.assertFilesLinked(lHead, map[string]string{"file": env.name + "-divergent-main"}, nil)
		env.assertFilesLinked(lOther, map[string]string{"file": env.name + "-divergent-other"}, nil)
		env.assertFilesLinked(lTag, map[string]string{"file": env.name + "-divergent-main"}, nil)
	})

	t.Run("remove worktrees", func(t *testing.T) {
		env.t = t
		env.repo.RemoveWorktreeLink(lMain)
		env.repo.RemoveWorktreeLink(lHead)
		env.repo.RemoveWorktreeLink(lOther)
		env.repo.RemoveWorktreeLink(lTag)
		env.mirror()

		env.assertMissingLink(lMain)
		env.assertMissingLink(lHead)
		env.assertMissingLink(lOther)
		env.assertMissingLink(lTag)
	})
}

// Test_Clone_Operations builds a highly complex repository history once,
// mirrors it once, and then asserts all Clone permutations
func Test_Clone_Operations(t *testing.T) {
	env := setupEnv(t)
	defer env.cleanup()

	tempClone := mustTmpDir(t)
	defer os.RemoveAll(tempClone)

	otherBranch := "other-branch"
	tag := "e2e-tag"

	var fileSHA1, dir2SHA3, fileOtherSHA3, fileOtherSHA4, mergeCommit3 string

	t.Run("build complex history and mirror", func(t *testing.T) {
		env.t = t
		// 1. Initial State
		fileSHA1 = env.initUpstream("file", env.name+"-main-1")
		env.commit(filepath.Join("dir1", "file"), env.name+"-dir1-main-2")
		env.commit("file", env.name+"-main-2")

		// Add files of different types explicitly to test glob / wildcard cloning later
		env.commit(filepath.Join("docs", "api.yaml"), "yaml-docs")
		env.commit("root.txt", "text")

		// 2. Other Branch updates and tagging
		env.commit(filepath.Join("dir1", "file"), env.name+"-dir1-main-3")
		env.checkout(otherBranch, true)
		dir2SHA3 = env.commit(filepath.Join("dir2", "file"), env.name+"-dir2-other-3")
		fileOtherSHA3 = env.commit("file", env.name+"-other-3")
		env.exec("git", "tag", "-af", tag, "-m", "tagging")

		// 3. Back to main, Merge 1 (--no-ff)
		env.checkout(testMainBranch, false)
		env.commit("file2", env.name+"-main-3")
		env.exec("git", "merge", "--no-ff", otherBranch, "-m", "Merge 1")

		// 4. Update other branch further
		env.commit(filepath.Join("dir1", "file"), env.name+"-dir1-main-4")
		env.checkout(otherBranch, false)
		env.commit(filepath.Join("dir2", "file"), env.name+"-dir2-other-4")
		fileOtherSHA4 = env.commit("file", env.name+"-other-4")

		// 5. Back to main, Merge 2 (--no-ff)
		env.checkout(testMainBranch, false)
		env.commit("file2", env.name+"-main-4")
		env.exec("git", "merge", "--no-ff", otherBranch, "-m", "Merge 2")

		// 6. Squash merge
		otherBranch2 := otherBranch + "v2"
		env.commit(filepath.Join("dir1", "file"), env.name+"-dir1-main-5")
		env.checkout(otherBranch2, true)
		env.commit(filepath.Join("dir2", "file"), env.name+"-dir2-other-5")
		env.commit("file", env.name+"-other-5")
		env.checkout(testMainBranch, false)
		env.commit("file2", env.name+"-main-5")
		env.exec("git", "merge", "--squash", otherBranch2)
		env.exec("git", "commit", "-m", "Squash merge")
		mergeCommit3 = env.exec("git", "rev-list", "-n1", "HEAD")

		// Mirror the entire tree once
		env.createAndMirror("", "")
	})

	t.Run("clone checks", func(t *testing.T) {
		env.t = t
		cases := []struct {
			name        string
			ref         string
			pathspecs   []string
			rmGitDir    bool
			wantSHA     string
			wantFiles   map[string]string
			wantMissing []string
		}{
			{"branch", testMainBranch, nil, false, mergeCommit3, map[string]string{"file2": env.name + "-main-5", filepath.Join("dir1", "file"): env.name + "-dir1-main-5"}, nil},
			{"HEAD", "HEAD", nil, false, mergeCommit3, map[string]string{"file2": env.name + "-main-5"}, nil},
			{"other branch", otherBranch, nil, false, fileOtherSHA4, map[string]string{"file": env.name + "-other-4", filepath.Join("dir2", "file"): env.name + "-dir2-other-4"}, nil},
			{"other branch pathspec", otherBranch, []string{"dir2"}, false, fileOtherSHA4, map[string]string{filepath.Join("dir2", "file"): env.name + "-dir2-other-4"}, []string{"file", "dir1"}},
			{"HEAD pathspec", "HEAD", []string{"dir1"}, false, mergeCommit3, map[string]string{filepath.Join("dir1", "file"): env.name + "-dir1-main-5"}, []string{"file", "dir2"}},
			{"tag", tag, nil, false, fileOtherSHA3, map[string]string{"file": env.name + "-other-3", filepath.Join("dir2", "file"): env.name + "-dir2-other-3"}, nil},
			{"tag pathspec", tag, []string{"dir2"}, false, fileOtherSHA3, map[string]string{filepath.Join("dir2", "file"): env.name + "-dir2-other-3"}, []string{"file", "dir1"}},
			{"sha pathspec", dir2SHA3, []string{"dir2"}, false, dir2SHA3, map[string]string{filepath.Join("dir2", "file"): env.name + "-dir2-other-3"}, []string{"file", "dir1"}},
			{"rmGitDir", testMainBranch, nil, true, mergeCommit3, map[string]string{"file2": env.name + "-main-5"}, []string{".git"}},
			{"glob yaml", testMainBranch, []string{"**/*.yaml"}, false, mergeCommit3, map[string]string{filepath.Join("docs", "api.yaml"): "yaml-docs"}, []string{"file2", "root.txt", filepath.Join("dir1", "file")}},
			{"glob txt", testMainBranch, []string{"*.txt"}, false, mergeCommit3, map[string]string{"root.txt": "text"}, []string{"file2", filepath.Join("docs", "api.yaml")}},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				os.RemoveAll(tempClone)
				os.MkdirAll(tempClone, defaultDirMode)
				cloneSHA, err := env.repo.Clone(txtCtx, tempClone, tc.ref, tc.pathspecs, tc.rmGitDir)
				if err != nil {
					t.Fatalf("clone failed for ref %s: %v", tc.ref, err)
				}
				if cloneSHA != tc.wantSHA {
					t.Errorf("clone SHA mismatch for ref %s: want %s got %s", tc.ref, tc.wantSHA, cloneSHA)
				}
				assertDirFiles(t, tempClone, tc.wantFiles, tc.wantMissing)
			})
		}
	})

	t.Run("rollback and clone deleted branch", func(t *testing.T) {
		env.t = t
		env.exec("git", "branch", "-D", otherBranch)
		env.exec("git", "reset", "-q", "--hard", fileSHA1)
		env.mirror()

		if _, err := env.repo.Clone(txtCtx, tempClone, otherBranch, nil, true); err == nil {
			t.Errorf("expected clone to fail on deleted branch")
		}
	})
}

// Test_Commit_Log_Diff_Operations interleaves logs and diff assertions
// while progressively building a complex Git history to ensure `HEAD..branch`
// evaluations occur before branches are merged.
func Test_Commit_Log_Diff_Operations(t *testing.T) {
	env := setupEnv(t)
	defer env.cleanup()

	otherBranch := "other-branch"
	tag := "e2e-tag"

	var fileSHA1, fileSHA2, dir1SHA2, dir1SHA3, dir2SHA3, fileOtherSHA3, fileSHA3 string
	var mergeCommit1, dir1SHA4, dir2SHA4, fileOtherSHA4, fileSHA4 string
	var mergeCommit2, dir1SHA5, mergeCommit3 string

	t.Run("phase 1: branch divergence", func(t *testing.T) {
		env.t = t
		fileSHA1 = env.initUpstream("file", env.name+"-main-1")
		dir1SHA2 = env.commit(filepath.Join("dir1", "file"), env.name+"-dir1-main-2")
		fileSHA2 = env.commit("file", env.name+"-main-2")

		dir1SHA3 = env.commit(filepath.Join("dir1", "file"), env.name+"-dir1-main-3")
		env.checkout(otherBranch, true)
		dir2SHA3 = env.commit(filepath.Join("dir2", "file"), env.name+"-dir2-other-3")
		fileOtherSHA3 = env.commit("file", env.name+"-other-3")
		env.exec("git", "tag", "-af", tag, "-m", "tagging")

		env.checkout(testMainBranch, false)
		fileSHA3 = env.commit("file2", env.name+"-main-3")

		env.createAndMirror("", "")

		env.assertLogs([]commitLog{
			{"HEAD", "", fileSHA3, env.name + "-main-3", []string{"file2"}},
			{testMainBranch, "dir1", dir1SHA3, env.name + "-dir1-main-3", []string{filepath.Join("dir1", "file")}},
			{otherBranch, "", fileOtherSHA3, env.name + "-other-3", []string{"file"}},
			{otherBranch, "dir2", dir2SHA3, env.name + "-dir2-other-3", []string{filepath.Join("dir2", "file")}},
		})

		env.assertBranchDiff(otherBranch, []repository.CommitInfo{
			{Hash: fileOtherSHA3, ChangedFiles: []string{"file"}},
			{Hash: dir2SHA3, ChangedFiles: []string{filepath.Join("dir2", "file")}},
		})
	})

	t.Run("phase 2: merge 1", func(t *testing.T) {
		env.t = t
		env.exec("git", "merge", "--no-ff", otherBranch, "-m", "Merge 1")
		mergeCommit1 = env.exec("git", "rev-list", "-n1", "HEAD")
		env.mirror()

		env.assertLogs([]commitLog{
			{"HEAD", "", mergeCommit1, "Merge 1", []string{}},
			{testMainBranch, "dir2", dir2SHA3, env.name + "-dir2-other-3", []string{filepath.Join("dir2", "file")}},
		})

		env.assertMergeDiff(mergeCommit1, []repository.CommitInfo{
			{Hash: mergeCommit1},
			{Hash: fileOtherSHA3, ChangedFiles: []string{"file"}},
			{Hash: dir2SHA3, ChangedFiles: []string{filepath.Join("dir2", "file")}},
		})
	})

	t.Run("phase 3: branch divergence 2", func(t *testing.T) {
		env.t = t
		dir1SHA4 = env.commit(filepath.Join("dir1", "file"), env.name+"-dir1-main-4")

		env.checkout(otherBranch, false)
		dir2SHA4 = env.commit(filepath.Join("dir2", "file"), env.name+"-dir2-other-4")
		fileOtherSHA4 = env.commit("file", env.name+"-other-4")

		env.checkout(testMainBranch, false)
		fileSHA4 = env.commit("file2", env.name+"-main-4") // Modifying file2 prevents merge conflict!

		env.mirror()

		env.assertLogs([]commitLog{
			{"HEAD", "", fileSHA4, env.name + "-main-4", []string{"file2"}},
			{testMainBranch, "dir1", dir1SHA4, env.name + "-dir1-main-4", []string{filepath.Join("dir1", "file")}},
			{otherBranch, "", fileOtherSHA4, env.name + "-other-4", []string{"file"}},
		})

		// Diff evaluates new commits since the previous merge
		env.assertBranchDiff(otherBranch, []repository.CommitInfo{
			{Hash: fileOtherSHA4, ChangedFiles: []string{"file"}},
			{Hash: dir2SHA4, ChangedFiles: []string{filepath.Join("dir2", "file")}},
		})
	})

	t.Run("phase 4: merge 2", func(t *testing.T) {
		env.t = t
		env.exec("git", "merge", "--no-ff", otherBranch, "-m", "Merge 2")
		mergeCommit2 = env.exec("git", "rev-list", "-n1", "HEAD")
		env.mirror()

		env.assertLogs([]commitLog{
			{"HEAD", "", mergeCommit2, "Merge 2", []string{}},
			{testMainBranch, "dir2", dir2SHA4, env.name + "-dir2-other-4", []string{filepath.Join("dir2", "file")}},
		})

		env.assertMergeDiff(mergeCommit2, []repository.CommitInfo{
			{Hash: mergeCommit2},
			{Hash: fileOtherSHA4, ChangedFiles: []string{"file"}},
			{Hash: dir2SHA4, ChangedFiles: []string{filepath.Join("dir2", "file")}},
		})
	})

	t.Run("phase 5: squash merge", func(t *testing.T) {
		env.t = t
		otherBranch2 := otherBranch + "v2"
		dir1SHA5 = env.commit(filepath.Join("dir1", "file"), env.name+"-dir1-main-5")

		env.checkout(otherBranch2, true)
		env.commit(filepath.Join("dir2", "file"), env.name+"-dir2-other-5")
		env.commit("file", env.name+"-other-5")

		env.checkout(testMainBranch, false)
		env.commit("file2", env.name+"-main-5")

		env.exec("git", "merge", "--squash", otherBranch2)
		env.exec("git", "commit", "-m", "Squash merge v1")
		mergeCommit3 = env.exec("git", "rev-list", "-n1", "HEAD")
		env.mirror()

		env.assertLogs([]commitLog{
			{"HEAD", "", mergeCommit3, "Squash merge v1", []string{}},
			{"HEAD", "dir1", dir1SHA5, env.name + "-dir1-main-5", []string{filepath.Join("dir1", "file")}},
		})

		env.assertMergeDiff(mergeCommit3, []repository.CommitInfo{
			{Hash: mergeCommit3, ChangedFiles: []string{filepath.Join("dir2", "file"), "file"}},
		})
	})

	t.Run("historical log checks", func(t *testing.T) {
		env.t = t
		// Ensure we can still query historical hashes properly long after they are buried
		env.assertLogs([]commitLog{
			{fileSHA2, "dir1", dir1SHA2, env.name + "-dir1-main-2", []string{filepath.Join("dir1", "file")}},
			{mergeCommit1, "dir1", dir1SHA3, env.name + "-dir1-main-3", []string{filepath.Join("dir1", "file")}},
			{mergeCommit2, "dir1", dir1SHA4, env.name + "-dir1-main-4", []string{filepath.Join("dir1", "file")}},
		})
	})

	t.Run("phase 6: rollback and test deleted branch", func(t *testing.T) {
		env.t = t
		env.exec("git", "branch", "-D", otherBranch)
		env.exec("git", "reset", "-q", "--hard", fileSHA1)
		env.mirror()

		env.assertLogs([]commitLog{
			{"HEAD", "", fileSHA1, env.name + "-main-1", []string{"file"}},
			{"HEAD", "dir1", "", "", nil},
		})

		if _, err := env.repo.Hash(txtCtx, otherBranch, ""); err == nil {
			t.Errorf("expected hash to fail on deleted branch")
		}
	})
}

func Test_mirror_bad_ref(t *testing.T) {
	env := setupEnv(t)
	defer env.cleanup()

	t.Run("init upstream without non-existent branch", func(t *testing.T) {
		env.initUpstream("file", env.name)

		rc := repository.Config{
			Remote:        "file://" + env.upstream,
			Root:          env.root,
			Interval:      testInterval,
			MirrorTimeout: testTimeout,
			GitGC:         "always",
			Worktrees:     []repository.WorktreeConfig{{Link: "link", Ref: "non-existent"}},
		}
		repo, err := repository.New(rc, "", testENVs, testLog)
		if err != nil {
			t.Fatalf("unable to create new repo error: %v", err)
		}

		if err := repo.Mirror(txtCtx); err == nil {
			t.Errorf("unexpected success for non-existent link")
		}
		assertMissingLink(t, env.root, "link")
	})
}

func Test_mirror_with_pathspec(t *testing.T) {
	env := setupEnv(t)
	defer env.cleanup()

	link1, link2, link3, link4 := "link1", "link2", "link3", "link4"
	ref1, ref2, ref3 := testMainBranch, "HEAD", "HEAD"
	pathSpec2, pathSpec3 := "dir2", "dir3"
	var firstSHA string

	t.Run("init upstream without other dirs", func(t *testing.T) {
		firstSHA = env.initUpstream("file", env.name+"-main-1")
		env.createAndMirror(link1, ref1)
		env.mirror()

		env.assertFileLinked(link1, "file", env.name+"-main-1")
		env.assertMissingLinkFile(link2, "file")
		env.assertMissingLinkFile(link3, "file")
	})

	t.Run("forward HEAD and create dir2 to test link2", func(t *testing.T) {
		env.commit("file", env.name+"-main-2")
		env.commit(filepath.Join("dir2", "file"), env.name+"-main-2")

		env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref2, Pathspecs: []string{pathSpec2}})
		env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: link4, Ref: ref2, Pathspecs: []string{pathSpec2}})
		env.mirror()

		env.assertFileLinked(link1, "file", env.name+"-main-2")
		env.assertFileLinked(link1, filepath.Join("dir2", "file"), env.name+"-main-2")

		env.assertMissingLinkFile(link2, "file")
		env.assertFileLinked(link2, filepath.Join("dir2", "file"), env.name+"-main-2")

		env.assertMissingLinkFile(link3, "file")
		env.assertMissingLinkFile(link3, filepath.Join("dir2", "file"))

		env.assertMissingLinkFile(link4, "file")
		env.assertFileLinked(link4, filepath.Join("dir2", "file"), env.name+"-main-2")
	})

	t.Run("forward HEAD and create dir3 to test link3", func(t *testing.T) {
		env.commit("file", env.name+"-main-3")
		env.commit(filepath.Join("dir2", "file"), env.name+"-main-3")
		env.commit(filepath.Join("dir3", "file"), env.name+"-main-3")

		env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: link3, Ref: ref3, Pathspecs: []string{pathSpec3}})
		env.repo.RemoveWorktreeLink(link4)
		env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: link4, Ref: ref2, Pathspecs: []string{pathSpec3, pathSpec2}})
		env.mirror()

		env.assertFileLinked(link1, "file", env.name+"-main-3")
		env.assertFileLinked(link1, filepath.Join("dir2", "file"), env.name+"-main-3")
		env.assertFileLinked(link1, filepath.Join("dir3", "file"), env.name+"-main-3")

		env.assertMissingLinkFile(link2, "file")
		env.assertFileLinked(link2, filepath.Join("dir2", "file"), env.name+"-main-3")
		env.assertMissingLinkFile(link2, filepath.Join("dir3", "file"))

		env.assertMissingLinkFile(link3, "file")
		env.assertMissingLinkFile(link3, filepath.Join("dir2", "file"))
		env.assertFileLinked(link3, filepath.Join("dir3", "file"), env.name+"-main-3")

		env.assertMissingLinkFile(link4, "file")
		env.assertFileLinked(link4, filepath.Join("dir2", "file"), env.name+"-main-3")
		env.assertFileLinked(link4, filepath.Join("dir3", "file"), env.name+"-main-3")
	})

	t.Run("move HEAD backward by 3 commit to original state", func(t *testing.T) {
		env.exec("git", "reset", "-q", "--hard", firstSHA)

		env.repo.RemoveWorktreeLink(link2)
		env.repo.RemoveWorktreeLink(link3)
		env.repo.RemoveWorktreeLink(link4)
		env.mirror()

		env.assertFileLinked(link1, "file", env.name+"-main-1")
		env.assertMissingLinkFile(link1, filepath.Join("dir2", "file"))
		env.assertMissingLinkFile(link1, filepath.Join("dir3", "file"))

		env.assertMissingLinkFile(link2, "file")
		env.assertMissingLinkFile(link2, filepath.Join("dir2", "file"))
		env.assertMissingLinkFile(link2, filepath.Join("dir3", "file"))

		env.assertMissingLinkFile(link3, "file")
		env.assertMissingLinkFile(link3, filepath.Join("dir2", "file"))
		env.assertMissingLinkFile(link3, filepath.Join("dir3", "file"))
	})
}

func Test_mirror_switch_branch_after_restart(t *testing.T) {
	env := setupEnv(t)
	defer env.cleanup()
	link1, link2 := "link1", "link2"
	ref1, ref2 := testMainBranch, "other-branch"

	env.initUpstream("file", env.name+"-main-1")
	env.exec("git", "checkout", "-q", "-b", ref2)
	env.commit("file", env.name+"-other-1")
	env.exec("git", "checkout", "-q", ref1)

	env.createAndMirror(link1, ref1)
	env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref2, Pathspecs: []string{}})
	env.mirror()

	env.assertFileLinked(link1, "file", env.name+"-main-1")
	env.assertFileLinked(link2, "file", env.name+"-other-1")

	t.Run("trigger restart by creating new repo with switched links", func(t *testing.T) {
		env.createAndMirror(link1, ref2)
		env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref1, Pathspecs: []string{}})
		env.mirror()

		env.assertFileLinked(link1, "file", env.name+"-other-1")
		env.assertFileLinked(link2, "file", env.name+"-main-1")
	})

	t.Run("forward both branches", func(t *testing.T) {
		env.commit("file", env.name+"-main-2")
		env.exec("git", "checkout", "-q", ref2)
		env.commit("file", env.name+"-other-2")
		env.exec("git", "checkout", "-q", ref1)

		env.mirror()
		env.assertFileLinked(link1, "file", env.name+"-other-2")
		env.assertFileLinked(link2, "file", env.name+"-main-2")
	})

	t.Run("move both branches backward", func(t *testing.T) {
		env.exec("git", "reset", "-q", "--hard", "HEAD^")
		env.exec("git", "checkout", "-q", ref2)
		env.exec("git", "reset", "-q", "--hard", "HEAD^")
		env.exec("git", "checkout", "-q", ref1)

		env.mirror()
		env.assertFileLinked(link1, "file", env.name+"-other-1")
		env.assertFileLinked(link2, "file", env.name+"-main-1")
	})
}

func Test_mirror_with_crash(t *testing.T) {
	env := setupEnv(t)
	defer env.cleanup()
	link1, ref1 := "link1", testMainBranch

	t.Run("init upstream", func(t *testing.T) {
		env.initUpstream("file", env.name+"- 1")
		env.createAndMirror(link1, ref1)
		env.assertFileLinked(link1, "file", env.name+"- 1")
	})

	t.Run("forward HEAD and delete link 1 symlink", func(t *testing.T) {
		if err := os.Remove(filepath.Join(env.root, link1)); err != nil {
			t.Fatalf("unexpected error:%s", err)
		}
		env.commit("file", env.name+"- 2")
		env.mirror()
		env.assertFileLinked(link1, "file", env.name+"- 2")
	})

	t.Run("forward HEAD and delete actual worktree", func(t *testing.T) {
		wtPath, err := utils.ReadAbsLink(env.repo.WorktreeLinks()[link1].AbsoluteLink())
		if err != nil {
			t.Fatalf("unexpected error:%s", err)
		}
		if err := os.RemoveAll(wtPath); err != nil {
			t.Fatalf("unexpected error:%s", err)
		}

		env.commit("file", env.name+"- 3")
		env.mirror()
		env.assertFileLinked(link1, "file", env.name+"- 3")
	})

	t.Run("move HEAD backward and delete root repository", func(t *testing.T) {
		if err := os.RemoveAll(env.root); err != nil {
			t.Fatalf("unexpected error:%s", err)
		}
		env.exec("git", "reset", "-q", "--hard", "HEAD^")
		env.mirror()
		env.assertFileLinked(link1, "file", env.name+"- 2")
	})
}

func Test_mirror_loop(t *testing.T) {
	env := setupEnv(t)
	defer env.cleanup()
	link1, link2 := "link1", "link2"
	ref1, ref2 := testMainBranch, "HEAD"

	t.Run("init upstream and start mirror loop", func(t *testing.T) {
		env.initUpstream("file", env.name+"-1")
		env.createAndMirror(link1, ref1)
		env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref2, Pathspecs: []string{}})

		go env.repo.StartLoop(txtCtx)
		time.Sleep(testInterval)
		if !env.repo.IsRunning() {
			t.Errorf("repo running state is still false after starting mirror loop")
		}

		time.Sleep(testInterval)
		env.assertFileLinked(link1, "file", env.name+"-1")
		env.assertFileLinked(link2, "file", env.name+"-1")
	})

	t.Run("forward HEAD", func(t *testing.T) {
		env.commit("file", env.name+"-2")
		time.Sleep(testInterval * 2)
		env.assertFileLinked(link1, "file", env.name+"-2")
		env.assertFileLinked(link2, "file", env.name+"-2")
	})

	t.Run("move HEAD backward", func(t *testing.T) {
		env.exec("git", "reset", "-q", "--hard", "HEAD^")
		time.Sleep(testInterval * 2)
		env.assertFileLinked(link1, "file", env.name+"-1")
		env.assertFileLinked(link2, "file", env.name+"-1")

		env.repo.StopLoop()
		if env.repo.IsRunning() {
			t.Errorf("repo still running after calling StopLoop")
		}
	})
}

// ##############################################
// RepoPool Tests
// ##############################################

func Test_RepoPool_Success(t *testing.T) {
	testName := t.Name()
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)
	tempClone := mustTmpDir(t)
	defer os.RemoveAll(tempClone)

	upstream1 := filepath.Join(testTmpDir, testUpstreamRepo)
	remote1 := "file://" + upstream1
	upstream2 := filepath.Join(testTmpDir, "upstream2")
	remote2 := "file://" + upstream2
	root := filepath.Join(testTmpDir, testRoot)

	var fileU1SHA1, fileU2SHA1, fileU1SHA2, fileU2SHA2 string
	var rp *repopool.RepoPool

	t.Run("init both upstream and test mirrors", func(t *testing.T) {
		fileU1SHA1 = mustInitRepo(t, upstream1, "file", testName+"-u1-main-1")
		fileU2SHA1 = mustInitRepo(t, upstream2, "file", testName+"-u2-main-1")

		rpc := repopool.Config{
			Defaults: repopool.DefaultConfig{
				Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
			},
			Repositories: []repository.Config{
				{Remote: remote1, Worktrees: []repository.WorktreeConfig{{Link: "link1"}}},
				{Remote: remote2, Worktrees: []repository.WorktreeConfig{{Link: "link2"}}},
			},
		}

		var err error
		rp, err = repopool.New(context.Background(), rpc, testLog, "", testENVs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := rp.AddWorktreeLink(remote1, repository.WorktreeConfig{Link: "link3", Ref: "", Pathspecs: []string{}}); err != nil {
			t.Fatalf("unexpected err:%s", err)
		}
		if err := rp.MirrorAll(context.TODO(), testTimeout); err != nil {
			t.Fatalf("unexpected err:%s", err)
		}

		if got, _ := rp.Hash(txtCtx, remote1, "HEAD", ""); got != fileU1SHA1 {
			t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU1SHA1)
		}
		if got, _ := rp.Hash(txtCtx, remote2, "HEAD", ""); got != fileU2SHA1 {
			t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU2SHA1)
		}

		assertLinkedFile(t, root, "link1", "file", testName+"-u1-main-1")
		assertLinkedFile(t, root, "link2", "file", testName+"-u2-main-1")

		if cloneSHA, _ := rp.Clone(txtCtx, remote1, tempClone, testMainBranch, nil, false); cloneSHA != fileU1SHA1 {
			t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileU1SHA1)
		}
		assertFile(t, filepath.Join(tempClone, "file"), testName+"-u1-main-1")
	})

	t.Run("forward both upstream and test mirrors", func(t *testing.T) {
		rp.StartLoop()
		time.Sleep(time.Second)

		fileU1SHA2 = mustCommit(t, upstream1, "file", testName+"-u1-main-2")
		fileU2SHA2 = mustCommit(t, upstream2, "file", testName+"-u2-main-2")

		time.Sleep(2 * time.Second)

		if got, _ := rp.Hash(txtCtx, remote1, "HEAD", ""); got != fileU1SHA2 {
			t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU1SHA2)
		}
		if got, _ := rp.Hash(txtCtx, remote2, "HEAD", ""); got != fileU2SHA2 {
			t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU2SHA2)
		}
		assertLinkedFile(t, root, "link1", "file", testName+"-u1-main-2")
		assertLinkedFile(t, root, "link2", "file", testName+"-u2-main-2")
		assertLinkedFile(t, root, "link3", "file", testName+"-u1-main-2")
	})

	t.Run("move HEAD backward on both upstream and test mirrors", func(t *testing.T) {
		mustExec(t, upstream1, "git", "reset", "-q", "--hard", fileU1SHA1)
		mustExec(t, upstream2, "git", "reset", "-q", "--hard", fileU2SHA1)

		time.Sleep(2 * time.Second)

		if got, _ := rp.Hash(txtCtx, remote1, "HEAD", ""); got != fileU1SHA1 {
			t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU1SHA1)
		}
		if got, _ := rp.Hash(txtCtx, remote2, "HEAD", ""); got != fileU2SHA1 {
			t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU2SHA1)
		}
		assertLinkedFile(t, root, "link1", "file", testName+"-u1-main-1")
		assertLinkedFile(t, root, "link2", "file", testName+"-u2-main-1")
		assertLinkedFile(t, root, "link3", "file", testName+"-u1-main-1")

		rp.RemoveWorktreeLink(remote2, "link2")
		time.Sleep(2 * time.Second)
		assertMissingLink(t, root, "link2")

		repo, _ := rp.Repository(remote1)
		rp.RemoveRepository(remote1)
		// once repo is removed public link should be removed
		assertMissingLink(t, root, "link1")
		// root dir should be empty
		assertMissingLink(t, repo.Directory(), "")
	})
}
func Test_RepoPool_Error(t *testing.T) {
	testName := t.Name()
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	upstream1 := filepath.Join(testTmpDir, testUpstreamRepo)
	remote1 := "file://" + upstream1
	upstream2 := filepath.Join(testTmpDir, "upstream2")
	remote2 := "file://" + upstream2
	root := filepath.Join(testTmpDir, testRoot)

	nonExistingRemote := "file://" + filepath.Join(testTmpDir, "upstream3.git")

	var rp *repopool.RepoPool

	t.Run("init both upstream and test mirrors", func(t *testing.T) {
		mustInitRepo(t, upstream1, "file", testName+"-u1-main-1")
		mustInitRepo(t, upstream2, "file", testName+"-u2-main-1")

		rpc := repopool.Config{
			Defaults: repopool.DefaultConfig{
				Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
			},
			Repositories: []repository.Config{
				{Remote: remote1, Worktrees: []repository.WorktreeConfig{{Link: "link1"}}},
				{Remote: remote2, Worktrees: []repository.WorktreeConfig{{Link: "link2"}}},
			},
		}

		var err error
		rp, err = repopool.New(context.Background(), rpc, testLog, "", testENVs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rp.StartLoop()
		time.Sleep(2 * time.Second)
		assertLinkedFile(t, root, "link1", "file", testName+"-u1-main-1")
	})

	t.Run("try adding existing repo again", func(t *testing.T) {
		repo1, _ := rp.Repository(remote1)
		if err := rp.AddRepository(repository.Config{Remote: repo1.Remote()}); err != repopool.ErrExist {
			t.Errorf("error mismatch got:%s want:%s", err, repopool.ErrExist)
		}
	})

	t.Run("try non existing repo", func(t *testing.T) {
		if _, err := rp.Repository(nonExistingRemote); err != repopool.ErrNotExist {
			t.Errorf("error mismatch got:%s want:%s", err, repopool.ErrNotExist)
		}
	})
}

// ##############################################
// HELPER FUNCS AND FIXTURE
// ##############################################

type testEnv struct {
	t        *testing.T
	name     string
	tmpDir   string
	upstream string
	root     string
	repo     *repository.Repository
}

func setupEnv(t *testing.T) *testEnv {
	t.Helper()
	tmpDir := mustTmpDir(t)
	return &testEnv{
		t:        t,
		name:     t.Name(),
		tmpDir:   tmpDir,
		upstream: filepath.Join(tmpDir, testUpstreamRepo),
		root:     filepath.Join(tmpDir, testRoot),
	}
}

func (e *testEnv) cleanup() {
	e.t.Helper()
	os.RemoveAll(e.tmpDir)
}

func (e *testEnv) initUpstream(file, content string) string {
	e.t.Helper()
	return mustInitRepo(e.t, e.upstream, file, content)
}

func (e *testEnv) commit(file, content string) string {
	e.t.Helper()
	return mustCommit(e.t, e.upstream, file, content)
}

func (e *testEnv) exec(command string, args ...string) string {
	e.t.Helper()
	return mustExec(e.t, e.upstream, command, args...)
}

func (e *testEnv) execInRepo(command string, args ...string) string {
	e.t.Helper()
	return mustExec(e.t, e.repo.Directory(), command, args...)
}

func (e *testEnv) createAndMirror(link, ref string) {
	e.t.Helper()

	// create mirror repo and add link for main branch
	rc := repository.Config{
		Remote:        "file://" + e.upstream,
		Root:          e.root,
		Interval:      testInterval,
		MirrorTimeout: testTimeout,
		GitGC:         "always",
	}
	repo, err := repository.New(rc, "", testENVs, testLog)
	if err != nil {
		e.t.Fatalf("unable to create new repo error: %v", err)
	}
	if link != "" {
		if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link, Ref: ref, Pathspecs: []string{}}); err != nil {
			e.t.Fatalf("unable to add worktree error: %v", err)
		}
	}
	// Trigger a mirror
	if err := repo.Mirror(txtCtx); err != nil {
		e.t.Fatalf("unable to mirror error: %v", err)
	}
	e.repo = repo
}

func (e *testEnv) mirror() {
	e.t.Helper()
	if err := e.repo.Mirror(txtCtx); err != nil {
		e.t.Fatalf("unable to mirror error: %v", err)
	}
}

func (e *testEnv) checkout(branch string, create bool) {
	e.t.Helper()
	if create {
		e.exec("git", "checkout", "-q", "-b", branch)
	} else {
		e.exec("git", "checkout", "-q", branch)
	}
}

func (e *testEnv) assertFilesLinked(link string, expected map[string]string, missing []string) {
	e.t.Helper()
	for file, content := range expected {
		e.assertFileLinked(link, file, content)
	}
	for _, file := range missing {
		e.assertMissingLinkFile(link, file)
	}
}

func (e *testEnv) assertFileLinked(link, file, expected string) {
	e.t.Helper()
	assertLinkedFile(e.t, e.root, link, file, expected)
}

func (e *testEnv) assertMissingLink(link string) {
	e.t.Helper()
	assertMissingLink(e.t, e.root, link)
}

func (e *testEnv) assertMissingLinkFile(link, file string) {
	e.t.Helper()
	assertMissingFile(e.t, filepath.Join(e.root, link), file)
}

type commitLog struct {
	ref     string
	path    string
	sha     string
	sub     string
	changed []string
}

func (e *testEnv) assertLogs(logs []commitLog) {
	e.t.Helper()
	for _, l := range logs {
		assertCommitLog(e.t, e.repo, l.ref, l.path, l.sha, l.sub, l.changed)
	}
}

func (e *testEnv) assertBranchDiff(branch string, expected []repository.CommitInfo) {
	e.t.Helper()
	commits, err := e.repo.BranchCommits(txtCtx, branch)
	if err != nil {
		e.t.Fatalf("unexpected error fetching branch commits: %v", err)
	}
	if diff := cmp.Diff(expected, commits); diff != "" {
		e.t.Errorf("BranchCommits mismatch (-want +got):\n%s", diff)
	}
}

func (e *testEnv) assertMergeDiff(mergeHash string, expected []repository.CommitInfo) {
	e.t.Helper()
	commits, err := e.repo.MergeCommits(txtCtx, mergeHash)
	if err != nil {
		e.t.Fatalf("unexpected error fetching merge commits: %v", err)
	}
	if diff := cmp.Diff(expected, commits); diff != "" {
		e.t.Errorf("MergeCommits mismatch (-want +got):\n%s", diff)
	}
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

func assertDirFiles(t *testing.T, root string, expected map[string]string, missing []string) {
	t.Helper()
	for file, content := range expected {
		assertFile(t, filepath.Join(root, file), content)
	}
	for _, file := range missing {
		assertMissingFile(t, root, file)
	}
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
