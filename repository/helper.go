package repository

import (
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/utilitywarehouse/git-mirror/internal/utils"
)

var (
	updatedRefRgx = regexp.MustCompile(`(?m)^[^=] \w+ \w+ (refs\/[^\s]+)`)

	// Objects can be named by their 40 hexadecimal digit SHA-1 name
	// or 64 hexadecimal digit SHA-256 name
	commitHashRgx            = regexp.MustCompile("^([0-9A-Fa-f]{40}|[0-9A-Fa-f]{64})$")
	abbreviatedCommitHashRgx = regexp.MustCompile("^[0-9A-Fa-f]{7,}$")
)

// IsCommitHash returns whether or not a string is a 40 char SHA-1
// or 64 char SHA-256 hash
func IsFullCommitHash(hash string) bool {
	return commitHashRgx.MatchString(hash)
}

// IsCommitHash returns whether or not a string is a abbreviated Hash or
// 40 char SHA-1 or 64 char SHA-256 hash
func IsCommitHash(hash string) bool {
	return abbreviatedCommitHashRgx.MatchString(hash)
}

func dirIsEmpty(path string) (bool, error) {
	dirents, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(dirents) == 0, nil
}

// publishSymlink atomically sets link to point at the specified target.
// both linkPath and targetPath must be absolute paths
func publishSymlink(linkPath string, targetPath string) error {
	linkDir, linkFile := utils.SplitAbs(linkPath)

	// Make sure the link directory exists.
	if err := os.MkdirAll(linkDir, defaultDirMode); err != nil {
		return fmt.Errorf("error making symlink dir: %w", err)
	}

	// linkDir is absolute, so we need to change it to a relative path.  This is
	// so it can be volume-mounted at another path and the symlink still works.
	targetRelative, err := filepath.Rel(linkDir, targetPath)
	if err != nil {
		return fmt.Errorf("error converting to relative path: %w", err)
	}

	// linkFile might exits and pointing to old worktree
	// hence we cant create symlink to it directly
	tmplink := linkFile + "-" + nextRandom()
	if err := os.Symlink(targetRelative, filepath.Join(linkDir, tmplink)); err != nil {
		return fmt.Errorf("error creating symlink: %w", err)
	}

	if err := os.Rename(filepath.Join(linkDir, tmplink), linkPath); err != nil {
		return fmt.Errorf("error replacing symlink: %w", err)
	}

	return nil
}

// readlinkAbs returns the destination of the named symbolic link.
// If the link destination is relative, it will resolve it to an absolute one.
func readlinkAbs(linkPath string) (string, error) {
	dst, err := os.Readlink(linkPath)
	if err != nil {
		return "", err
	}

	if filepath.IsAbs(dst) {
		return dst, nil
	} else {
		// Symlink targets are relative to the directory containing the link.
		return filepath.Join(filepath.Dir(linkPath), dst), nil
	}
}

// removeDirContents iterated the specified dir and removes all contents
func removeDirContents(dir string, log *slog.Logger) error {
	return removeDirContentsIf(dir, log, func(fi os.FileInfo) (bool, error) {
		return true, nil
	})
}

// removeDirContentsIf iterated the specified dir and removes entries
// if given function returns true for the given entry
func removeDirContentsIf(dir string, log *slog.Logger, fn func(fi os.FileInfo) (bool, error)) error {
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Save errors until the end.
	var errs []error
	for _, fi := range dirents {
		name := fi.Name()
		p := filepath.Join(dir, name)
		stat, err := os.Stat(p)
		if err != nil {
			log.Error("failed to stat path, skipping", "path", p, "err", err)
			continue
		}
		if shouldDelete, err := fn(stat); err != nil {
			log.Error("predicate function failed for path, skipping", "path", p, "err", err)
			continue
		} else if !shouldDelete {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("%s", errs)
	}
	return nil
}

// nextRandom will generate random number string
func nextRandom() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return strconv.Itoa(int(r.Uint32()))
}

func updatedRefs(output string) []string {
	var refs []string

	for _, match := range updatedRefRgx.FindAllStringSubmatch(output, -1) {
		refs = append(refs, match[1])
	}

	return refs
}

// jitter returns a time.Duration between duration and maxFactor * duration.
func jitter(duration time.Duration, maxFactor float64) time.Duration {
	return time.Duration(rand.Float64() * maxFactor * float64(duration))
}
