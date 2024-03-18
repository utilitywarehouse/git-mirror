package mirror

import (
	"context"
	"fmt"
	"log/slog"
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

// AddRepository will add given repository to repoPool and
// start mirror loop if its not already started
func (rp *RepoPool) AddRepository(repo *Repository) error {
	if repo, _ := rp.Repo(repo.remote); repo != nil {
		return ErrExist
	}

	rp.repos = append(rp.repos, repo)

	if !repo.running {
		go repo.StartLoop(context.TODO())
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
