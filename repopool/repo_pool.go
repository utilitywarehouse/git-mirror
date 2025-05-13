package repopool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/utilitywarehouse/git-mirror/giturl"
	"github.com/utilitywarehouse/git-mirror/internal/lock"
	"github.com/utilitywarehouse/git-mirror/repository"
)

var (
	ErrExist    = errors.New("repo already exist")
	ErrNotExist = errors.New("repo does not exist")
)

// RepoPool represents the collection of mirrored repositories
// it provides simple wrapper around Repository methods.
// A RepoPool is safe for concurrent use by multiple goroutines.
type RepoPool struct {
	ctx        context.Context
	lock       lock.RWMutex
	log        *slog.Logger
	repos      []*repository.Repository
	cmd        string
	commonENVs []string
	Stopped    chan bool
}

// New will create repository pool based on given config.
// Remote repo will not be mirrored until either Mirror() or StartLoop() is called
func New(ctx context.Context, conf Config, log *slog.Logger, gitExec string, commonENVs []string) (*RepoPool, error) {
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
		cmd:        gitExec,
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

		rp.lock.RLock()
		defer rp.lock.RUnlock()

		for {
			time.Sleep(time.Second)
			// check if any repo mirror is still running
			var running bool
			for _, repo := range rp.repos {
				if repo.IsRunning() {
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
func (rp *RepoPool) AddRepository(repoConf repository.Config) error {
	remoteURL := giturl.NormaliseURL(repoConf.Remote)
	if repo, _ := rp.Repository(remoteURL); repo != nil {
		return ErrExist
	}

	rp.lock.Lock()
	defer rp.lock.Unlock()

	repo, err := repository.New(repoConf, rp.cmd, rp.commonENVs, rp.log)
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

// QueueMirrorRun is wrapper around repositories QueueMirrorRun method
func (rp *RepoPool) QueueMirrorRun(remote string) error {
	repo, err := rp.Repository(remote)
	if err != nil {
		return err
	}

	repo.QueueMirrorRun()
	return nil
}

// StartLoop will start mirror loop on all repositories
// if its not already started
func (rp *RepoPool) StartLoop() {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	for _, repo := range rp.repos {
		if !repo.IsRunning() {
			go repo.StartLoop(rp.ctx)
			continue
		}
	}
}

// Repository will return Repository object based on given remote URL
func (rp *RepoPool) Repository(remote string) (*repository.Repository, error) {
	gitURL, err := giturl.Parse(remote)
	if err != nil {
		return nil, err
	}

	rp.lock.RLock()
	defer rp.lock.RUnlock()

	for _, repo := range rp.repos {
		// err can be ignored as remote string from repo object will always be valid
		repoURL, _ := giturl.Parse(repo.Remote())

		if repoURL.Equals(gitURL) {
			return repo, nil
		}
	}
	return nil, ErrNotExist
}

// RepositoriesRemote returns remote URLs of all the repositories
func (rp *RepoPool) RepositoriesRemote() []string {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	var urls []string
	for _, repo := range rp.repos {
		urls = append(urls, repo.Remote())
	}
	return urls
}

// RepositoriesDirPath returns local paths of all the mirrored repositories
func (rp *RepoPool) RepositoriesDirPath() []string {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	var paths []string
	for _, repo := range rp.repos {
		paths = append(paths, repo.Directory())
	}
	return paths
}

// AddWorktreeLink is wrapper around repositories AddWorktreeLink method
func (rp *RepoPool) AddWorktreeLink(remote string, wt repository.WorktreeConfig) error {
	repo, err := rp.Repository(remote)
	if err != nil {
		return err
	}
	if err := rp.validateLinkPath(repo, wt.Link); err != nil {
		return err
	}

	rp.lock.Lock()
	defer rp.lock.Unlock()

	return repo.AddWorktreeLink(wt)
}

func (rp *RepoPool) validateLinkPath(repo *repository.Repository, link string) error {
	newAbsLink := repo.AbsoluteLink(link)

	rp.lock.RLock()
	defer rp.lock.RUnlock()

	for _, r := range rp.repos {
		for _, wl := range r.WorktreeLinks() {
			if wl.AbsoluteLink() == newAbsLink {
				return fmt.Errorf("repo with overlapping abs link path found repo:%s path:%s",
					r.Remote(), wl.AbsoluteLink())
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
		if repo.Remote() == remote {
			rp.log.Info("removing repository mirror", "remote", repo.Remote)

			rp.repos = slices.Delete(rp.repos, i, i+1)

			repo.StopLoop()

			// remove all published links
			for _, wt := range repo.WorktreeLinks() {
				if err := os.Remove(wt.AbsoluteLink()); err != nil {
					rp.log.Error("unable to remove published link", "err", err)
				}
			}

			return os.RemoveAll(repo.Directory())
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
func (rp *RepoPool) Clone(ctx context.Context, remote, dst, branch string, pathspecs []string, rmGitDir bool) (string, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return "", err
	}
	return repo.Clone(ctx, dst, branch, pathspecs, rmGitDir)
}

// MergeCommits is wrapper around repositories MergeCommits method
func (rp *RepoPool) MergeCommits(ctx context.Context, remote, mergeCommitHash string) ([]repository.CommitInfo, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return nil, err
	}
	return repo.MergeCommits(ctx, mergeCommitHash)
}

// BranchCommits is wrapper around repositories BranchCommits method
func (rp *RepoPool) BranchCommits(ctx context.Context, remote, branch string) ([]repository.CommitInfo, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return nil, err
	}
	return repo.BranchCommits(ctx, branch)
}

// ListCommitsWithChangedFiles is wrapper around repositories ListCommitsWithChangedFiles method
func (rp *RepoPool) ListCommitsWithChangedFiles(ctx context.Context, remote, ref1, ref2 string) ([]repository.CommitInfo, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return nil, err
	}
	return repo.ListCommitsWithChangedFiles(ctx, ref1, ref2)
}
