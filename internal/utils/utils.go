package utils

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultDirMode fs.FileMode = os.FileMode(0755) // 'rwxr-xr-x'

// ReadAbsLink returns the destination of the named symbolic link.
// return path will be absolute
func ReadAbsLink(link string) (string, error) {
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

// ReCreate removes dir and any children it contains and creates new dir
// on the same path
func ReCreate(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("can't delete unusable dir: %w", err)
	}
	if err := os.MkdirAll(path, defaultDirMode); err != nil {
		return fmt.Errorf("unable to create repo dir err:%w", err)
	}
	return nil
}

// AbsLink will return absolute path for the given link
// if its not already abs. given root must be an absolute path
func AbsLink(root, link string) string {
	linkAbs := link
	if !filepath.IsAbs(linkAbs) {
		linkAbs = filepath.Join(root, link)
	}

	return linkAbs
}

// RunCommand runs given command with given arguments on given CWD
func RunCommand(ctx context.Context, log *slog.Logger, envs []string, cwd string, command string, args ...string) (string, error) {

	cmdStr := command + " " + strings.Join(args, " ")
	log.Log(ctx, -8, "running command", "cwd", cwd, "cmd", cmdStr)

	cmd := exec.CommandContext(ctx, command, args...)
	// force kill git & child process 5 seconds after sending it sigterm (when ctx is cancelled/timed out)
	cmd.WaitDelay = 5 * time.Second
	if cwd != "" {
		cmd.Dir = cwd
	}
	outbuf := bytes.NewBuffer(nil)
	errbuf := bytes.NewBuffer(nil)
	cmd.Stdout = outbuf
	cmd.Stderr = errbuf

	// If Env is nil, the new process uses the current process's environment.
	cmd.Env = []string{}

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
