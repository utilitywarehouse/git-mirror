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
	lock       lock.RWMutex
	log        *slog.Logger
	repos      []*Repository
	commonENVs []string
}

// NewRepoPool will create mirror repositories based on given config.
// Remote repo will not be mirrored until either Mirror() or StartLoop() is called
func NewRepoPool(conf RepoPoolConfig, log *slog.Logger, commonENVs []string) (*RepoPool, error) {
	if err := conf.ValidateAndApplyDefaults(); err != nil {
		return nil, err
	}

	if log == nil {
		log = slog.Default()
	}

	rp := &RepoPool{log: log, commonENVs: commonENVs}

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
func (rp *RepoPool) StartLoop(ctx context.Context) {
	rp.lock.RLock()
	defer rp.lock.RUnlock()

	for _, repo := range rp.repos {
		if !repo.running {
			go repo.StartLoop(ctx)
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
