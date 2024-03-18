package mirror

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func dirIsEmpty(path string) (bool, error) {
	dirents, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(dirents) == 0, nil
}

// absLink will return absolute path for the given link
// if its not already abs. given root must be an absolute path
func absLink(root, link string) string {
	linkAbs := link
	if !filepath.IsAbs(linkAbs) {
		linkAbs = filepath.Join(root, link)
	}

	return linkAbs
}

// reCreate removes dir and any children it contains and creates new dir
// on the same path
func reCreate(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("can't delete unusable dir: %w", err)
	}
	if err := os.MkdirAll(path, defaultDirMode); err != nil {
		return fmt.Errorf("unable to create repo dir err:%w", err)
	}
	return nil
}

// publishSymlink atomically sets link to point at the specified target.
// both linkPath and targetPath must be absolute paths
func publishSymlink(linkPath string, targetPath string) error {
	linkDir, linkFile := SplitAbs(linkPath)

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

// readAbsLink returns the destination of the named symbolic link.
// return path will be absolute
func readAbsLink(link string) (string, error) {
	if !filepath.IsAbs(link) {
		return "", fmt.Errorf("given link path must be absolute")
	}
	target, err := os.Readlink(link)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if target == "" {
		return "", nil
	}
	if filepath.IsAbs(target) {
		return target, nil
	}
	linkDir, _ := SplitAbs(link)
	return filepath.Join(linkDir, target), nil
}

// removeDirContents iterated the specified dir and removes all contents
func removeDirContents(dir string, log *slog.Logger) error {
	return removeDirContentsIf(dir, log, func(fi os.FileInfo) (bool, error) {
		return true, nil
	})
}

// removeDirContents iterated the specified dir and removes entries
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

func SplitAbs(abs string) (string, string) {
	if abs == "" {
		return "", ""
	}

	// filepath.Split promises that dir+base == input, but trailing slashes on
	// the dir is confusing and ugly.
	pathSep := string(os.PathSeparator)
	dir, base := filepath.Split(strings.TrimRight(abs, pathSep))
	dir = strings.TrimRight(dir, pathSep)
	if len(dir) == 0 {
		dir = string(os.PathSeparator)
	}

	return dir, base
}

// nextRandom will generate random number string
func nextRandom() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return strconv.Itoa(int(r.Uint32()))
}

// runGitCommand runs git command with given arguments on given CWD
func runGitCommand(ctx context.Context, log *slog.Logger, envs []string, cwd string, args ...string) (string, error) {

	cmdStr := gitExecutablePath + " " + strings.Join(args, " ")
	log.Log(ctx, -8, "running command", "cwd", cwd, "cmd", cmdStr)

	cmd := exec.CommandContext(ctx, gitExecutablePath, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	outbuf := bytes.NewBuffer(nil)
	errbuf := bytes.NewBuffer(nil)
	cmd.Stdout = outbuf
	cmd.Stderr = errbuf

	if len(envs) > 0 {
		cmd.Env = append(cmd.Env, envs...)
	}

	start := time.Now()
	err := cmd.Run()
	runTime := time.Since(start)

	stdout := strings.TrimSpace(outbuf.String())
	stderr := strings.TrimSpace(errbuf.String())
	if ctx.Err() == context.DeadlineExceeded {
		err = ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("Run(%s): err:%w { stdout: %q, stderr: %q }", cmdStr, err, stdout, stderr)
	}
	log.Log(ctx, -8, "command result", "stdout", stdout, "stderr", stderr, "time", runTime)

	return stdout, nil
}
