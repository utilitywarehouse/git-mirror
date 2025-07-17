package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/utilitywarehouse/git-mirror/internal/utils"
)

type WorkTreeLink struct {
	link      string   // link name as its specified in config, might not be unique only use it for logging
	linkAbs   string   // the path at which to create a symlink to the worktree dir
	dir       string   // the path of the dir where valid worktree is checked out
	ref       string   // the ref of the worktree
	pathspecs []string // pathspecs of the paths to checkout
	log       *slog.Logger
}

// AbsoluteLink returns worktree's absolute link path
func (wt *WorkTreeLink) AbsoluteLink() string {
	return wt.linkAbs
}

// Equals returns if given worktree and its config is equal
// they are considered equal only if link, ref and pathspecs are matching.
// order of pothspecs is ignored
func (wt *WorkTreeLink) Equals(wtc WorktreeConfig) bool {
	sortedConfigPaths := slices.Clone(wtc.Pathspecs)
	slices.Sort(sortedConfigPaths)

	return wt.link == wtc.Link &&
		wt.ref == wtc.Ref &&
		slices.Compare(wt.pathspecs, sortedConfigPaths) == 0
}

// worktreeDirName will generate worktree name for specific worktree link
// two worktree links can be on same ref but with diff pathspecs
// hence we cant just use tree hash as path
// 2 diff worktree links can have same basename hence also including hash of absolute link path
func (w *WorkTreeLink) worktreeDirName(hash string) string {
	linkHash := fmt.Sprintf("%x", sha256.Sum256([]byte(w.linkAbs)))
	base := filepath.Base(w.linkAbs)
	return base + "_" + linkHash[:7] + "-" + hash[:7]
}

// currentWorktree reads symlink path of the given worktree link
func (wl *WorkTreeLink) currentWorktree() (string, error) {
	return utils.ReadAbsLink(wl.linkAbs)
}

// workTreeHash returns the hash of the given revision and for the path if specified.
func (r *Repository) workTreeHash(ctx context.Context, wl *WorkTreeLink, wt string) (string, error) {
	// if worktree is not valid then command can return HEAD of the mirrored repo
	// instead of worktree
	if !r.isInsideWorkTree(ctx, wl, wt) {
		return "", fmt.Errorf("worktree is not a valid git worktree")
	}
	// git rev-parse HEAD
	return r.git(ctx, nil, wt, "rev-parse", "HEAD")
}

// isInsideWorkTree will make sure given worktree dir is inside worktree dir
// (.git file exists)
func (r *Repository) isInsideWorkTree(ctx context.Context, wl *WorkTreeLink, wt string) bool {
	// worktree path should not be empty and must be absolute
	if !filepath.IsAbs(wt) {
		return false
	}
	// git rev-parse --is-inside-work-tree
	if ok, err := r.git(ctx, nil, wt, "rev-parse", "--is-inside-work-tree"); err != nil {
		wl.log.Error("unable to verify if is-inside-work-tree", "path", wt, "err", err)
		return false
	} else if ok != "true" {
		wl.log.Error("given path is not inside the worktree", "path", wt)
		return false
	}
	return true
}

// sanityCheckWorktree tries to make sure that the dir is a valid worktree repository.
// Note that this does not guarantee that the worktree has all the
// files checked out - git could have died halfway through and the repo will
// still pass this check.
func (r *Repository) sanityCheckWorktree(ctx context.Context, wl *WorkTreeLink) bool {
	if wl.dir == "" {
		return false
	}

	// If it is empty, we are done.
	if empty, err := dirIsEmpty(wl.dir); err != nil {
		wl.log.Error("can't list worktree directory", "path", wl.dir, "err", err)
		return false
	} else if empty {
		wl.log.Info("worktree directory is empty", "path", wl.dir)
		return false
	}

	// makes sure path is inside the work tree of the repository
	if !r.isInsideWorkTree(ctx, wl, wl.dir) {
		return false
	}

	// Check that this is actually the root of the worktree.
	// git rev-parse --show-toplevel
	if root, err := r.git(ctx, nil, wl.dir, "rev-parse", "--show-toplevel"); err != nil {
		wl.log.Error("can't get worktree git dir", "path", wl.dir, "err", err)
		return false
	} else {
		if root != wl.dir {
			wl.log.Error("worktree directory is under another worktree", "path", wl.dir, "parent", root)
			return false
		}
	}

	// Consistency-check the repo.
	// git fsck --no-progress --connectivity-only
	if _, err := r.git(ctx, nil, wl.dir, "fsck", "--no-progress", "--connectivity-only"); err != nil {
		wl.log.Error("repo fsck failed", "path", wl.dir, "err", err)
		return false
	}

	return true
}
