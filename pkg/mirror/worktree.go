package mirror

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
)

type WorkTreeLink struct {
	name     string // link file name might not be unique only use it for logging
	link     string // the path at which to create a symlink to the worktree dir
	ref      string // the ref of the worktree
	pathspec string // pathspec of the dirs to checkout
	log      *slog.Logger
}

// worktreeDirName will generate worktree name for specific worktree link
// two worktree links can be on same ref but with diff pathspecs
// hence we cant just use tree hash as path
func (w *WorkTreeLink) worktreeDirName(hash string) string {
	parts := strings.Split(strings.Trim(w.link, "/"), "/")
	return parts[len(parts)-1] + "-" + hash[:7]
}

// currentWorktree reads symlink path of the given worktree link
func (wl *WorkTreeLink) currentWorktree() (string, error) {
	return readAbsLink(wl.link)
}

// currentHash returns the hash of the given revision and for the path if specified.
func (wl *WorkTreeLink) workTreeHash(ctx context.Context, wt string) (string, error) {
	// if worktree is not valid then command can return HEAD of the mirrored repo
	// instead of worktree
	if !wl.isInsideWorkTree(ctx, wt) {
		return "", fmt.Errorf("worktree is not a valid git worktree")
	}
	// git rev-parse HEAD
	return runGitCommand(ctx, wl.log, nil, wt, "rev-parse", "HEAD")
}

// isInsideWorkTree will make sure given worktree dir is inside worktree dir
// (.git file exists)
func (wl *WorkTreeLink) isInsideWorkTree(ctx context.Context, wt string) bool {
	// worktree path should not be empty and must be absolute
	if !filepath.IsAbs(wt) {
		return false
	}
	// git rev-parse --is-inside-work-tree
	if ok, err := runGitCommand(ctx, wl.log, nil, wt, "rev-parse", "--is-inside-work-tree"); err != nil {
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
func (wl *WorkTreeLink) sanityCheckWorktree(ctx context.Context) bool {
	wt, err := wl.currentWorktree()
	if err != nil {
		wl.log.Error("can't get current worktree", "err", err)
		return false
	}
	if wt == "" {
		return false
	}

	// If it is empty, we are done.
	if empty, err := dirIsEmpty(wt); err != nil {
		wl.log.Error("can't list worktree directory", "path", wt, "err", err)
		return false
	} else if empty {
		wl.log.Info("worktree directory is empty", "path", wt)
		return false
	}

	// makes sure path is inside the work tree of the repository
	if !wl.isInsideWorkTree(ctx, wt) {
		return false
	}

	// Check that this is actually the root of the worktree.
	// git rev-parse --show-toplevel
	if root, err := runGitCommand(ctx, wl.log, nil, wt, "rev-parse", "--show-toplevel"); err != nil {
		wl.log.Error("can't get worktree git dir", "path", wt, "err", err)
		return false
	} else {
		if root != wt {
			wl.log.Error("worktree directory is under another worktree", "path", wt, "parent", root)
			return false
		}
	}

	// Consistency-check the repo.
	// git fsck --no-progress --connectivity-only
	if _, err := runGitCommand(ctx, wl.log, nil, wt, "fsck", "--no-progress", "--connectivity-only"); err != nil {
		wl.log.Error("repo fsck failed", "path", wt, "err", err)
		return false
	}

	return true
}
