package utils

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
