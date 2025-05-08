package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitAbs(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		expDir  string
		expBase string
	}{
		{name: "1", in: "", expDir: "", expBase: ""},
		{name: "2", in: "/", expDir: "/", expBase: ""},
		{name: "3", in: "//", expDir: "/", expBase: ""},
		{name: "4", in: "/one", expDir: "/", expBase: "one"},
		{name: "5", in: "/one/two", expDir: "/one", expBase: "two"},
		{name: "6", in: "/one/two/", expDir: "/one", expBase: "two"},
		{name: "7", in: "/one//two", expDir: "/one", expBase: "two"},
		{name: "8", in: "one/two", expDir: "one", expBase: "two"},
		{name: "8", in: "one", expDir: "/", expBase: "one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := SplitAbs(tt.in)
			if got != tt.expDir {
				t.Errorf("splitAbs() got = %v, want %v", got, tt.expDir)
			}
			if got1 != tt.expBase {
				t.Errorf("splitAbs() got1 = %v, want %v", got1, tt.expBase)
			}
		})
	}
}

func Test_reCreate(t *testing.T) {
	tempRoot := t.TempDir()

	// create files
	dir := filepath.Join(tempRoot, "files")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatalf("failed to make a temp subdir: %v", err)
	}
	for _, file := range []string{"a", "b", "c"} {
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, []byte{}, 0755); err != nil {
			t.Fatalf("failed to write a file: %v", err)
		}
	}

	if err := ReCreate(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// validate by making sure new dir is empty
	if empty, err := dirIsEmpty(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if !empty {
		t.Errorf("expected %q to be deemed empty", tempRoot)
	}
}

func dirIsEmpty(path string) (bool, error) {
	dirents, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(dirents) == 0, nil
}
