//go:build deadlock_test

package e2e_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/utilitywarehouse/git-mirror/repopool"
	"github.com/utilitywarehouse/git-mirror/repository"
)

func Test_mirror_detect_race_clone(t *testing.T) {
	env := setupEnv(t)
	defer env.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	link1, link2 := "link1", "link2"

	t.Log("TEST-1: init upstream")
	env.initUpstream("file", env.name+"-1")

	env.createAndMirror(link1, testMainBranch)
	env.repo.AddWorktreeLink(repository.WorktreeConfig{Link: link2, Ref: "HEAD"})
	env.mirror()

	// start background mirror loop
	go env.repo.StartLoop(ctx)
	defer env.repo.StopLoop()

	t.Log("TEST-2: forward HEAD")
	fileSHA2 := env.commit("file", env.name+"-2")

	time.Sleep(3 * time.Second) // wait for repo.Mirror to grab lock

	t.Run("clone-test", func(t *testing.T) {
		var wg sync.WaitGroup

		// Concurrent Mirror and Read loops
		for i := 0; i < 100; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				env.repo.Mirror(ctx)
				env.assertFileLinked(link1, "file", env.name+"-2")
				env.assertFileLinked(link2, "file", env.name+"-2")
			}()

			go func() {
				defer wg.Done()
				env.repo.Hash(ctx, "HEAD", "")
			}()
		}

		// Concurrent Clone loops
		for i := 0; i < 10; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				env.repo.Mirror(ctx)
			}()

			go func(idx int) {
				defer wg.Done()
				tempClone := mustTmpDir(t)
				defer os.RemoveAll(tempClone)

				if cloneSHA, err := env.repo.Clone(ctx, tempClone, testMainBranch, nil, idx%2 == 0); err != nil {
					t.Errorf("unexpected error %s", err)
				} else if cloneSHA != fileSHA2 {
					t.Errorf("clone sha mismatch got:%s want:%s", cloneSHA, fileSHA2)
				}
				assertFile(t, filepath.Join(tempClone, "file"), env.name+"-2")
			}(i)
		}
		wg.Wait()
	})
}

func Test_mirror_detect_race_slow_fetch(t *testing.T) {
	env := setupEnv(t)
	defer env.cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fileSHA1 := env.initUpstream("file", env.name+"-1")

	rc := repository.Config{
		Remote:        "file://" + env.upstream,
		Root:          env.root,
		Interval:      testInterval,
		MirrorTimeout: 2 * time.Minute, // testing slow fetch
		GitGC:         "always",
	}

	// replace global git path with slower git wrapper script
	cwd, _ := os.Getwd()
	repo, err := repository.New(rc, exec.Command(filepath.Join(cwd, "git_slow_fetch.sh")).String(), testENVs, testLog)
	if err != nil {
		t.Fatalf("unable to create new repo error: %v", err)
	}
	env.repo = repo
	env.mirror()

	t.Run("slow-fetch-without-timeout", func(t *testing.T) {
		for range 3 {
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				env.repo.Mirror(ctx)
			}()

			go func() {
				defer wg.Done()
				time.Sleep(time.Second) // wait for repo.Mirror to grab lock

				// Due to background ctx, these reads should block until mirror releases lock, then succeed
				if _, err := env.repo.Hash(context.Background(), "HEAD", ""); err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if _, err := env.repo.Subject(context.Background(), fileSHA1); err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if _, err := env.repo.ChangedFiles(context.Background(), fileSHA1); err != nil {
					t.Errorf("unexpected error: %v", err)
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
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				env.repo.Mirror(ctx)
			}()

			go func() {
				defer wg.Done()
				time.Sleep(2 * time.Second) // wait for repo.Mirror to grab lock

				// Due to short timeout, these should fail while waiting for the lock
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
			ctxShort, cancelShort := context.WithTimeout(context.Background(), 5*time.Second)
			start := time.Now()

			env.repo.Mirror(ctxShort)

			// 5s timeout + 5s waitDelay + 2s buffer
			if time.Since(start) > (5+5+2)*time.Second {
				t.Errorf("mirror function should have exited after ctx timeout but took %s", time.Since(start))
			}
			cancelShort()
		}
	})
}

func Test_mirror_detect_race_repo_pool(t *testing.T) {
	testTmpDir := mustTmpDir(t)
	defer os.RemoveAll(testTmpDir)

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

	rp, err := repopool.New(context.Background(), rpc, testLog, "", testENVs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// stressPoolFn encapsulates the identical pool stress logic for a remote
	stressPoolFn := func(remote string, expectedSHA string) {
		for i := 0; i < 10; i++ {
			readStopped := make(chan bool)
			ctx, cancel := context.WithCancel(context.Background())

			newConfig := repopool.Config{
				Defaults: rpc.Defaults,
				Repositories: []repository.Config{{
					Remote:    remote,
					Worktrees: []repository.WorktreeConfig{{Link: "linkA"}},
				}},
			}

			if err := newConfig.ValidateAndApplyDefaults(); err != nil {
				t.Errorf("failed to validate new config err: %v", err)
				cancel()
				return
			}
			if err := rp.AddRepository(newConfig.Repositories[0]); err != nil {
				t.Errorf("unexpected err: %v", err)
				cancel()
				return
			}

			rp.StartLoop()

			// Continuous background reading
			go func() {
				for {
					time.Sleep(time.Second)
					select {
					case <-ctx.Done():
						close(readStopped)
						return
					default:
						if got, err := rp.Hash(context.Background(), remote, "HEAD", ""); err != nil {
							t.Errorf("unexpected err: %v", err)
						} else if got != expectedSHA {
							t.Errorf("%s hash mismatch got:%s want:%s", remote, got, expectedSHA)
						}
					}
				}
			}()

			// Concurrently mutate worktrees
			if err := rp.AddWorktreeLink(remote, repository.WorktreeConfig{Link: "linkB"}); err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			time.Sleep(2 * time.Second)
			if err := rp.RemoveWorktreeLink(remote, "linkA"); err != nil {
				t.Errorf("unexpected err: %v", err)
			}

			// Clean up and wait for reader to stop
			cancel()
			<-readStopped

			if err := rp.RemoveRepository(remote); err != nil {
				t.Errorf("unexpected err: %v", err)
			}
		}
	}

	t.Run("add-remove-repo-test", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(2)

		// Run stress test concurrently on both repositories
		go func() {
			defer wg.Done()
			stressPoolFn(remote1, fileU1SHA1)
		}()
		go func() {
			defer wg.Done()
			stressPoolFn(remote2, fileU2SHA1)
		}()

		wg.Wait()
	})
}
