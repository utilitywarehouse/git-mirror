//go:build deadlock_test

package mirror

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func Test_mirror_detect_race_clone(t *testing.T) {
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
	if err := repo.AddWorktreeLink(WorktreeConfig{link2, ref2, []string{}}); err != nil {
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

	t.Run("clone-test", func(t *testing.T) {
		wg := &sync.WaitGroup{}
		// all following assertions will always be true
		// this test is about testing deadlocks and detecting race conditions
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := repo.Mirror(ctx); err != nil {
					t.Error("unable to mirror", "err", err)
					os.Exit(1)
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
					t.Error("unable to mirror error", "err", err)
					os.Exit(1)
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

func Test_mirror_detect_race_repo_pool(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	tempClone := mustTmpDir(t)
	defer os.RemoveAll(tempClone)

	upstream1 := filepath.Join(testTmpDir, testUpstreamRepo)
	remote1 := "file://" + upstream1
	upstream2 := filepath.Join(testTmpDir, "upstream2")
	remote2 := "file://" + upstream2
	root := filepath.Join(testTmpDir, testRoot)

	fileU1SHA1 := mustInitRepo(t, upstream1, "file", t.Name()+"-u1-main-1")
	fileU2SHA1 := mustInitRepo(t, upstream2, "file", t.Name()+"-u2-main-1")

	rpc := RepoPoolConfig{
		Defaults: DefaultConfig{
			Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
		},
	}

	rp, err := NewRepoPool(t.Context(), rpc, testLog, testENVs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("add-remove-repo-test", func(t *testing.T) {
		wg := &sync.WaitGroup{}
		wg.Add(1)

		// add/remove 2 repositories
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				t.Log("adding remote1", "count", i)
				readStopped := make(chan bool)
				ctx, cancel := context.WithCancel(t.Context())

				newConfig := RepoPoolConfig{
					Defaults: DefaultConfig{
						Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
					},
					Repositories: []RepositoryConfig{{
						Remote:    remote1,
						Worktrees: []WorktreeConfig{{Link: "link1"}}},
					},
				}
				if err := newConfig.ValidateAndApplyDefaults(); err != nil {
					t.Error("failed to validate new config", "err", err)
					return
				}

				if err := rp.AddRepository(newConfig.Repositories[0]); err != nil {
					t.Error("unexpected err", "err", err)
					return
				}

				rp.StartLoop()

				go func() {
					for {
						time.Sleep(1 * time.Second)
						select {
						case <-ctx.Done():
							close(readStopped)
							return
						default:
							if got, err := rp.Hash(txtCtx, remote1, "HEAD", ""); err != nil {
								t.Error("unexpected err", "count", i, "err", err)
							} else if got != fileU1SHA1 {
								t.Errorf("remote1 hash mismatch got:%s want:%s", got, fileU1SHA1)
							}
						}

					}
				}()

				if err := rp.AddWorktreeLink(remote1, WorktreeConfig{"link2", "", []string{}}); err != nil {
					t.Error("unexpected err", "err", err)
					return
				}

				time.Sleep(2 * time.Second)

				if err := rp.RemoveWorktreeLink(remote1, "link1"); err != nil {
					t.Error("unexpected err", "err", err)
					return
				}

				cancel()
				<-readStopped

				if err := rp.RemoveRepository(remote1); err != nil {
					t.Error("unexpected err", "err", err)
					return
				}
			}

		}()

		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				t.Log("adding remote2", "count", i)
				readStopped := make(chan bool)
				ctx, cancel := context.WithCancel(t.Context())

				newConfig := RepoPoolConfig{
					Defaults: DefaultConfig{
						Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
					},
					Repositories: []RepositoryConfig{{Remote: remote2,
						Worktrees: []WorktreeConfig{{Link: "link3"}}},
					},
				}
				if err := newConfig.ValidateAndApplyDefaults(); err != nil {
					t.Error("failed to validate new config", "err", err)
					return
				}

				if err := rp.AddRepository(newConfig.Repositories[0]); err != nil {
					t.Error("unexpected err", "err", err)
					return
				}

				rp.StartLoop()

				// start loop to trigger read on repo pool
				go func() {
					for {
						time.Sleep(1 * time.Second)
						select {
						case <-ctx.Done():
							close(readStopped)
							return
						default:
							if got, err := rp.Hash(txtCtx, remote2, "HEAD", ""); err != nil {
								t.Error("unexpected err", "count", i, "err", err)
							} else if got != fileU2SHA1 {
								t.Errorf("remote2 hash mismatch got:%s want:%s", got, fileU2SHA1)
							}
						}
					}
				}()

				if err := rp.AddWorktreeLink(remote2, WorktreeConfig{"link4", "", []string{}}); err != nil {
					t.Error("unexpected err", "err", err)
					return
				}

				time.Sleep(2 * time.Second)

				if err := rp.RemoveWorktreeLink(remote2, "link3"); err != nil {
					t.Error("unexpected err", "err", err)
					return
				}

				cancel()

				<-readStopped

				if err := rp.RemoveRepository(remote2); err != nil {
					t.Error("unexpected err", "err", err)
					return
				}
			}
		}()

		wg.Wait()
	})
}
