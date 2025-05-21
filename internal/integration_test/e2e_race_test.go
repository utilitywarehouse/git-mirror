//go:build deadlock_test

package e2e_test

import (
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/utilitywarehouse/git-mirror/repopool"
	"github.com/utilitywarehouse/git-mirror/repository"
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
	if err := repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: ref2, Pathspecs: []string{}}); err != nil {
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
	defer repo.StopLoop()

	t.Log("TEST-2: forward HEAD")
	fileSHA2 := mustCommit(t, upstream, "file", testName+"-2")

	time.Sleep(2 * time.Second)

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
				}
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				tempClone := mustTmpDir(t)
				defer os.RemoveAll(tempClone)

				if cloneSHA, err := repo.Clone(ctx, tempClone, testMainBranch, nil, i%2 == 0); err != nil {
					t.Errorf("unexpected error %s", err)
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

func Test_mirror_detect_race_slow_fetch(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	upstream := filepath.Join(testTmpDir, testUpstreamRepo)
	root := filepath.Join(testTmpDir, testRoot)

	testName := t.Name()

	t.Log("TEST-1: init upstream")
	fileSHA1 := mustInitRepo(t, upstream, "file", testName+"-1")

	rc := repository.Config{
		Remote:        "file://" + upstream,
		Root:          root,
		Interval:      testInterval,
		MirrorTimeout: 2 * time.Minute, // testing slow fetch
		GitGC:         "always",
	}

	// replace global git path with slower git wrapper script
	cwd, _ := os.Getwd()
	repo, err := repository.New(rc, exec.Command(path.Join(cwd, "git_slow_fetch.sh")).String(), testENVs, testLog)
	if err != nil {
		t.Fatalf("unable to create new repo error: %v", err)
	}

	if err := repo.Mirror(txtCtx); err != nil {
		t.Fatalf("unable to mirror error: %v", err)
	}

	// verify checkout files
	assertCommitLog(t, repo, "HEAD", "", fileSHA1, testName+"-1", []string{"file"})

	t.Run("slow-fetch-without-timeout", func(t *testing.T) {

		// all following assertions will always be true
		// this test is about testing deadlocks and detecting race conditions
		// due to background ctx following  assertions should always succeed
		for range 3 {
			wg := &sync.WaitGroup{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := repo.Mirror(ctx); err != nil {
					t.Error("unable to mirror error", "err", err)
				}
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()

				ctx := context.Background()

				time.Sleep(2 * time.Second) // wait for repo.Mirror to grab lock

				gotHash, err := repo.Hash(ctx, "HEAD", "")
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				} else if gotHash != fileSHA1 {
					t.Errorf("ref '%s' on path '%s' SHA mismatch got:%s want:%s", "HEAD", "", gotHash, fileSHA1)
				}

				if got, err := repo.Subject(ctx, fileSHA1); err != nil {
					t.Errorf("unexpected error: %v", err)
				} else if got != testName+"-1" {
					t.Errorf("subject mismatch sha:%s got:%s want:%s", gotHash, got, testName+"-1")
				}

				if got, err := repo.ChangedFiles(ctx, fileSHA1); err != nil {
					t.Errorf("unexpected error: %v", err)
				} else if slices.Compare(got, []string{"file"}) != 0 {
					t.Errorf("changed file mismatch sha:%s got:%s want:%s", gotHash, got, []string{"file"})
				}
			}()
			wg.Wait()
		}
	})

	t.Run("slow-fetch-with-client-timeout", func(t *testing.T) {
		// all following assertions will always be true
		// this test is about testing deadlocks and detecting race conditions
		// due to shorter ctx timeout Hash, Subject and ChangedFiles should always fail
		for range 3 {
			wg := &sync.WaitGroup{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := repo.Mirror(ctx); err != nil {
					t.Error("unable to mirror error", "err", err)
				}
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(2 * time.Second) // wait for repo.Mirror to grab lock

				ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
				if _, err := repo.Hash(ctx1, "HEAD", ""); err == nil {
					t.Errorf("error was expected due to sorter timeout")
				}

				ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
				if _, err := repo.Subject(ctx2, fileSHA1); err == nil {
					t.Errorf("error was expected due to sorter timeout")
				}

				ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
				if _, err := repo.ChangedFiles(ctx3, fileSHA1); err == nil {
					t.Errorf("error was expected due to sorter timeout")
				}

				cancel1()
				cancel2()
				cancel3()
			}()
			wg.Wait()
		}
	})

	t.Run("slow-fetch-with-mirror-timeout", func(t *testing.T) {
		for range 3 {
			ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)

			start := time.Now()
			repo.Mirror(ctx1)
			// 5s timeout + 5s waitDelay + 2s buffer
			if time.Since(start) > (5+5+2)*time.Second {
				t.Errorf("error: mirror function should have existed after ctx timeout but it took %s", time.Since(start))
			}
			cancel1()
		}
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

	rpc := repopool.Config{
		Defaults: repopool.DefaultConfig{
			Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
		},
	}

	rp, err := repopool.New(t.Context(), rpc, testLog, "", testENVs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("add-remove-repo-test", func(t *testing.T) {
		wg := &sync.WaitGroup{}

		// add/remove 2 repositories
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				t.Log("adding remote1", "count", i)
				readStopped := make(chan bool)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				newConfig := repopool.Config{
					Defaults: repopool.DefaultConfig{
						Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
					},
					Repositories: []repository.Config{{
						Remote:    remote1,
						Worktrees: []repository.WorktreeConfig{{Link: "link1"}}},
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
						time.Sleep(2 * time.Second)
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

				if err := rp.AddWorktreeLink(remote1, repository.WorktreeConfig{Link: "link2", Ref: "", Pathspecs: []string{}}); err != nil {
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

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				t.Log("adding remote2", "count", i)
				readStopped := make(chan bool)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				newConfig := repopool.Config{
					Defaults: repopool.DefaultConfig{
						Root: root, Interval: testInterval, MirrorTimeout: testTimeout, GitGC: "always",
					},
					Repositories: []repository.Config{{Remote: remote2,
						Worktrees: []repository.WorktreeConfig{{Link: "link3"}}},
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
						time.Sleep(2 * time.Second)
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

				if err := rp.AddWorktreeLink(remote2, repository.WorktreeConfig{Link: "link4", Ref: "", Pathspecs: []string{}}); err != nil {
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
