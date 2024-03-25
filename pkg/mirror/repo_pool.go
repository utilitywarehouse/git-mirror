package mirror

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

var (
	ErrExist    = fmt.Errorf("repo already exist")
	ErrNotExist = fmt.Errorf("repo does not exist")
)

// RepoPool represents the collection of mirrored repositories
// it provides simple wrapper around Repository methods
type RepoPool struct {
	log   *slog.Logger
	repos []*Repository
}

// NewRepoPool will create mirror repositories based on given config and start loop
// RepoPool provides simple wrapper around Repository methods and used remote url to select repository
// remote repo will not be mirrored until either Mirror() or StartLoop() is called
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

// AddRepository will add given repository to repoPool
// remote repo will not be mirrored until either Mirror() or StartLoop() is called
func (rp *RepoPool) AddRepository(repo *Repository) error {
	if repo, _ := rp.Repo(repo.remote); repo != nil {
		return ErrExist
	}

	rp.repos = append(rp.repos, repo)

	return nil
}

// Mirror will trigger mirror on every repo in foreground with given timeout
// it will error out if any of the repository mirror errors
// ideally Mirror should be used for the first mirror to ensure repositories are
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

// StartLoop will start mirror loop if its not already started
func (rp *RepoPool) StartLoop() error {
	for _, repo := range rp.repos {
		if !repo.running {
			go repo.StartLoop(context.TODO())
			continue
		}
		rp.log.Info("start loop is already running", "repo", repo.gitURL.repo)
	}
	return nil
}

// Repo will return Repository object based on given remote URL
func (rp *RepoPool) Repo(remote string) (*Repository, error) {
	gitURL, err := ParseGitURL(remote)
	if err != nil {
		return nil, err
	}

	for _, repo := range rp.repos {
		if SameURL(repo.gitURL, gitURL) {
			return repo, nil
		}
	}
	return nil, ErrNotExist
}

// AddWorktreeLink is wrapper around repositories AddWorktreeLink method
func (rp *RepoPool) AddWorktreeLink(remote string, link, ref, pathspec string) error {
	repo, err := rp.Repo(remote)
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
					r.gitURL.repo, wl.link)
			}
		}
	}

	return nil
}

// Hash is wrapper around repositories hash method
func (rp *RepoPool) Hash(ctx context.Context, remote, ref, path string) (string, error) {
	repo, err := rp.Repo(remote)
	if err != nil {
		return "", err
	}
	return repo.Hash(ctx, ref, path)
}

// LogMsg is wrapper around repositories LogMsg method
func (rp *RepoPool) LogMsg(ctx context.Context, remote, ref, path string) (string, error) {
	repo, err := rp.Repo(remote)
	if err != nil {
		return "", err
	}
	return repo.LogMsg(ctx, ref, path)
}

// Clone is wrapper around repositories Clone method
func (rp *RepoPool) Clone(ctx context.Context, remote, dst, branch, pathspec string, rmGitDir bool) (string, error) {
	repo, err := rp.Repo(remote)
	if err != nil {
		return "", err
	}
	return repo.Clone(ctx, dst, branch, pathspec, rmGitDir)
}
