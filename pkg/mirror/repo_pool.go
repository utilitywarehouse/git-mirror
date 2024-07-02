package mirror

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/giturl"
)

var (
	ErrExist    = fmt.Errorf("repo already exist")
	ErrNotExist = fmt.Errorf("repo does not exist")
)

// RepoPool represents the collection of mirrored repositories
// it provides simple wrapper around Repository methods.
// A RepoPool is safe for concurrent use by multiple goroutines.
type RepoPool struct {
	log   *slog.Logger
	repos []*Repository
}

// NewRepoPool will create mirror repositories based on given config.
// Remote repo will not be mirrored until either Mirror() or StartLoop() is called
func NewRepoPool(conf RepoPoolConfig, log *slog.Logger, commonENVs []string) (*RepoPool, error) {
	if err := conf.ValidateDefaults(); err != nil {
		return nil, err
	}

	if err := conf.ValidateLinkPaths(); err != nil {
		return nil, err
	}

	conf.ApplyDefaults()

	if log == nil {
		log = slog.Default()
	}

	rp := &RepoPool{log: log}

	for _, repoConf := range conf.Repositories {

		repo, err := NewRepository(repoConf, commonENVs, log)
		if err != nil {
			return nil, err
		}

		if err := rp.AddRepository(repo); err != nil {
			return nil, err
		}
	}

	return rp, nil
}

// AddRepository will add given repository to repoPool.
// Remote repo will not be mirrored until either Mirror() or StartLoop() is called
func (rp *RepoPool) AddRepository(repo *Repository) error {
	if repo, _ := rp.Repository(repo.remote); repo != nil {
		return ErrExist
	}

	rp.repos = append(rp.repos, repo)

	return nil
}

// Mirror will trigger mirror on every repo in foreground with given timeout.
// It will error out if any of the repository mirror errors.
// Ideally Mirror should be used for the first mirror cycle to ensure repositories are
// successfully mirrored
func (rp *RepoPool) Mirror(ctx context.Context, timeout time.Duration) error {
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

// StartLoop will start mirror loop on all repositories
// if its not already started
func (rp *RepoPool) StartLoop() {
	for _, repo := range rp.repos {
		if !repo.running {
			go repo.StartLoop(context.TODO())
			continue
		}
		rp.log.Info("start loop is already running", "repo", repo.gitURL.Repo)
	}
}

// Repository will return Repository object based on given remote URL
func (rp *RepoPool) Repository(remote string) (*Repository, error) {
	gitURL, err := giturl.Parse(remote)
	if err != nil {
		return nil, err
	}

	for _, repo := range rp.repos {
		if giturl.SameURL(repo.gitURL, gitURL) {
			return repo, nil
		}
	}
	return nil, ErrNotExist
}

// AddWorktreeLink is wrapper around repositories AddWorktreeLink method
func (rp *RepoPool) AddWorktreeLink(remote string, link, ref, pathspec string) error {
	repo, err := rp.Repository(remote)
	if err != nil {
		return err
	}
	if err := rp.validateLinkPath(repo, link); err != nil {
		return err
	}
	return repo.AddWorktreeLink(link, ref, pathspec)
}

func (rp *RepoPool) validateLinkPath(repo *Repository, link string) error {
	newAbsLink := absLink(repo.root, link)

	for _, r := range rp.repos {
		for _, wl := range r.workTreeLinks {
			if wl.link == newAbsLink {
				return fmt.Errorf("repo with overlapping abs link path found repo:%s path:%s",
					r.gitURL.Repo, wl.link)
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

// LogMsg is wrapper around repositories LogMsg method
func (rp *RepoPool) LogMsg(ctx context.Context, remote, ref, path string) (string, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return "", err
	}
	return repo.LogMsg(ctx, ref, path)
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

// ChangedFilesBetweenRefs is wrapper around repositories ChangedFilesBetweenRefs method
// git diff --name-only HEAD...refs/pull/13179/head
// git diff --name-only --merge-base HEAD refs/pull/13179/head
func (rp *RepoPool) ChangedFilesBetweenRefs(ctx context.Context, remote, ref1, ref2 string) ([]string, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return nil, err
	}
	return repo.ChangedFilesBetweenRefs(ctx, ref1, ref2)
}

// CommitsBetweenRefs is wrapper around repositories CommitsBetweenRefs method
// git rev-list ^HEAD refs/pull/13179/head
func (rp *RepoPool) CommitsBetweenRefs(ctx context.Context, remote, ref1, ref2 string) ([]string, error) {
	repo, err := rp.Repository(remote)
	if err != nil {
		return nil, err
	}
	return repo.CommitsBetweenRefs(ctx, ref1, ref2)
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
