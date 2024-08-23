//go:build deadlock_test

package mirror

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func Test_mirror_detect_race(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)
	link1 := "link1" // on testBranchMain branch
	link2 := "link2" // on remote HEAD
	ref1 := testMainBranch
	ref2 := "HEAD"
	testName := t.Name()

	t.Log("TEST-1: init upstream")
	fileSHA1 := mustInitRepo(t, upstream, "file", testName+"-1")

	repo := mustCreateRepoAndMirror(t, upstream, root, link1, ref1)
	// add worktree for HEAD
	if err := repo.AddWorktreeLink(link2, ref2, ""); err != nil {
		t.Fatalf("unable to add worktree error: %v", err)
	}
	// mirror again for 2nd worktree
	if err := repo.Mirror(ctx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// verify checkout files
	assertCommitLog(t, repo, "HEAD", "", fileSHA1, testName+"-1", []string{"file"})
	assertLinkedFile(t, root, link1, "file", testName+"-1")
	assertLinkedFile(t, root, link2, "file", testName+"-1")

	// start mirror loop
	go repo.StartLoop(ctx)
	close(repo.stop)

	t.Log("TEST-2: forward HEAD")
	fileSHA2 := mustCommit(t, upstream, "file", testName+"-2")

	t.Run("test-1", func(t *testing.T) {
		wg := &sync.WaitGroup{}
		// all following assertions will always be true
		// this test is about testing deadlocks and detecting race conditions
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := repo.Mirror(ctx); err != nil {
					log.Fatalf("unable to mirror error: %v", err)
				}

				assertLinkedFile(t, root, link1, "file", testName+"-2")
				assertLinkedFile(t, root, link2, "file", testName+"-2")
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()

				assertCommitLog(t, repo, "HEAD", "", fileSHA2, testName+"-2", []string{"file"})
			}()
		}

		// clone tests
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := repo.Mirror(ctx); err != nil {
					log.Fatalf("unable to mirror error: %v", err)
				}
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				tempClone := mustTmpDir(t)
				defer os.RemoveAll(tempClone)

				if cloneSHA, err := repo.Clone(ctx, tempClone, testMainBranch, "", i%2 == 0); err != nil {
					t.Fatalf("unexpected error %s", err)
				} else {
					if cloneSHA != fileSHA2 {
						t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileSHA2)
					}
					assertFile(t, filepath.Join(tempClone, "file"), testName+"-2")
				}
			}()
		}
		wg.Wait()
	})

}
