package mirror

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/giturl"
	"github.com/utilitywarehouse/git-mirror/pkg/lock"
)

var (
	ErrExist    = fmt.Errorf("repo already exist")
	ErrNotExist = fmt.Errorf("repo does not exist")
)

// RepoPool represents the collection of mirrored repositories
// it provides simple wrapper around Repository methods.
// A RepoPool is safe for concurrent use by multiple goroutines.
type RepoPool struct {
	ctx        context.Context
	lock       lock.RWMutex
	log        *slog.Logger
	repos      []*Repository
	commonENVs []string
	Stopped    chan bool
}

// NewRepoPool will create mirror repositories based on given config.
// Remote repo will not be mirrored until either Mirror() or StartLoop() is called
func NewRepoPool(ctx context.Context, conf RepoPoolConfig, log *slog.Logger, commonENVs []string) (*RepoPool, error) {
	if err := conf.ValidateAndApplyDefaults(); err != nil {
		return nil, err
	}

	if log == nil {
		log = slog.Default()
	}
	repoCtx, repoCancel := context.WithCancel(ctx)

	rp := &RepoPool{
		ctx:        repoCtx,
		log:        log,
		commonENVs: commonENVs,
		Stopped:    make(chan bool),
	}

	// start shutdown watcher
	go func() {
		defer func() {
			close(rp.Stopped)
		}()

		// wait for shutdown signal
		<-ctx.Done()

		// signal repository
		repoCancel()

		for {
			time.Sleep(time.Second)
			// check if any repo mirror is still running
			var running bool
			for _, repo := range rp.repos {
				if repo.running {
					running = true
					break
				}
			}

			if !running {
				return
			}
		}
	}()

	for _, repoConf := range conf.Repositories {
		if err := rp.AddRepository(repoConf); err != nil {
			return nil, err
		}
	}

	return rp, nil
}

// AddRepository will add given repository to repoPool.
// Remote repo will not be mirrored until either Mirror() or StartLoop() is called
func (rp *RepoPool) AddRepository(repoConf RepositoryConfig) error {
	remoteURL := giturl.NormaliseURL(repoConf.Remote)
	if repo, _ := rp.Repository(remoteURL); repo != nil {
		return ErrExist
	}

	rp.lock.Lock()
	defer rp.lock.Unlock()

	repo, err := NewRepository(repoConf, rp.commonENVs, rp.log)
	if err != nil {
		return err
	}
	rp.repos = append(rp.repos, repo)

	return nil
}

// MirrorAll will trigger mirror on every repo in foreground with given timeout.
// It will error out if any of the repository mirror errors.
// Ideally MirrorAll should be used for the first mirror cycle to ensure repositories are
// successfully mirrored
func (rp *RepoPool) MirrorAll(ctx context.Context, timeout time.Duration) error {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	for _, repo := range rp.repos {
		mCtx, cancel := context.WithTimeout(ctx, timeout)
		err := repo.Mirror(mCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("repository mirror failed err:%w", err)
		}
	}

	return nil
}

// Mirror is wrapper around repositories Mirror method
func (rp *RepoPool) Mirror(ctx context.Context, remote string) error {
	repo, err := rp.Repository(remote)
	if err != nil {
		return err
	}

	return repo.Mirror(ctx)
}

// StartLoop will start mirror loop on all repositories
// if its not already started
func (rp *RepoPool) StartLoop() {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	for _, repo := range rp.repos {
		if !repo.running {
			go repo.StartLoop(rp.ctx)
			continue
		}
	}
}

// Repository will return Repository object based on given remote URL
func (rp *RepoPool) Repository(remote string) (*Repository, error) {
	gitURL, err := giturl.Parse(remote)
	if err != nil {
		return nil, err
	}

	rp.lock.RLock()
	defer rp.lock.RUnlock()

	for _, repo := range rp.repos {
		if repo.gitURL.Equals(gitURL) {
			return repo, nil
		}
	}
	return nil, ErrNotExist
}

func (rp *RepoPool) RepositoriesRemote() []string {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	var urls []string
	for _, repo := range rp.repos {
		urls = append(urls, repo.remote)
	}
	return urls
}

func (rp *RepoPool) RepositoriesDirPath() []string {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	var paths []string
	for _, repo := range rp.repos {
		paths = append(paths, repo.dir)
	}
	return paths
}

// AddWorktreeLink is wrapper around repositories AddWorktreeLink method
func (rp *RepoPool) AddWorktreeLink(remote string, wt WorktreeConfig) error {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	repo, err := rp.Repository(remote)
	if err != nil {
		return err
	}
	if err := rp.validateLinkPath(repo, wt.Link); err != nil {
		return err
	}
	return repo.AddWorktreeLink(wt)
}

func (rp *RepoPool) validateLinkPath(repo *Repository, link string) error {
	newAbsLink := absLink(repo.root, link)

	for _, r := range rp.repos {
		for _, wl := range r.WorktreeLinks() {
			if wl.linkAbs == newAbsLink {
				return fmt.Errorf("repo with overlapping abs link path found repo:%s path:%s",
					r.gitURL.Repo, wl.linkAbs)
			}
		}
	}

	return nil
}

// RemoveWorktreeLink is wrapper around repositories RemoveWorktreeLink method
func (rp *RepoPool) RemoveWorktreeLink(remote string, wtLink string) error {
	repo, err := rp.Repository(remote)
	if err != nil {
		return err
	}
	return repo.RemoveWorktreeLink(wtLink)
}

// RemoveRepository will remove given repository from the repoPool.
func (rp *RepoPool) RemoveRepository(remote string) error {
	rp.lock.Lock()
	defer rp.lock.Unlock()

	for i, repo := range rp.repos {
		if repo.remote == remote {
			rp.log.Info("removing repository mirror", "remote", repo.remote)

			rp.repos = slices.Delete(rp.repos, i, i+1)

			repo.StopLoop()

			// remove all published links
			for _, wt := range repo.WorktreeLinks() {
				if err := os.Remove(wt.linkAbs); err != nil {
					rp.log.Error("unable to remove published link", "err", err)
				}
			}

			return os.RemoveAll(repo.dir)
		}
	}

	return ErrNotExist
}

// Hash is wrapper around repositories hash method
func (rp *RepoPool) Hash(ctx context.Context, remote, ref, path string) (string, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return "", err
	}
	return repo.Hash(ctx, ref, path)
}

// Subject is wrapper around repositories Subject method
func (rp *RepoPool) Subject(ctx context.Context, remote, hash string) (string, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return "", err
	}
	return repo.Subject(ctx, hash)
}

// ChangedFiles is wrapper around repositories ChangedFiles method
func (rp *RepoPool) ChangedFiles(ctx context.Context, remote, hash string) ([]string, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return nil, err
	}
	return repo.ChangedFiles(ctx, hash)
}

// ObjectExists is wrapper around repositories ObjectExists method
func (rp *RepoPool) ObjectExists(ctx context.Context, remote, obj string) error {
	repo, err := rp.Repository(remote)
	if err != nil {
		return err
	}
	return repo.ObjectExists(ctx, obj)
}

// Clone is wrapper around repositories Clone method
func (rp *RepoPool) Clone(ctx context.Context, remote, dst, branch, pathspec string, rmGitDir bool) (string, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return "", err
	}
	return repo.Clone(ctx, dst, branch, pathspec, rmGitDir)
}

// MergeCommits is wrapper around repositories MergeCommits method
func (rp *RepoPool) MergeCommits(ctx context.Context, remote, mergeCommitHash string) ([]CommitInfo, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return nil, err
	}
	return repo.MergeCommits(ctx, mergeCommitHash)
}

// BranchCommits is wrapper around repositories BranchCommits method
func (rp *RepoPool) BranchCommits(ctx context.Context, remote, branch string) ([]CommitInfo, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return nil, err
	}
	return repo.BranchCommits(ctx, branch)
}

// ListCommitsWithChangedFiles is wrapper around repositories ListCommitsWithChangedFiles method
func (rp *RepoPool) ListCommitsWithChangedFiles(ctx context.Context, remote, ref1, ref2 string) ([]CommitInfo, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return nil, err
	}
	return repo.ListCommitsWithChangedFiles(ctx, ref1, ref2)
}
