package mirror

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func Test_dirIsEmpty(t *testing.T) {
	tempRoot := t.TempDir()

	// Brand new should be empty.
	if empty, err := dirIsEmpty(tempRoot); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if !empty {
		t.Errorf("expected %q to be deemed empty", tempRoot)
	}

	// Holding normal dir should not be empty.
	dir := filepath.Join(tempRoot, "files")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatalf("failed to make a temp subdir: %v", err)
	}
	for _, file := range []string{"a", "b", "c"} {
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, []byte{}, 0755); err != nil {
			t.Fatalf("failed to write a file: %v", err)
		}
		if empty, err := dirIsEmpty(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		} else if empty {
			t.Errorf("expected %q to be deemed not-empty", dir)
		}
	}

	// Holding dot-files should not be empty.
	dir = filepath.Join(tempRoot, "dot-files")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatalf("failed to make a temp subdir: %v", err)
	}
	for _, file := range []string{".a", ".b", ".c"} {
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, []byte{}, 0755); err != nil {
			t.Fatalf("failed to write a file: %v", err)
		}
		if empty, err := dirIsEmpty(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		} else if empty {
			t.Errorf("expected %q to be deemed not-empty", dir)
		}
	}

	// Holding dirs should not be empty.
	dir = filepath.Join(tempRoot, "dirs")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatalf("failed to make a temp subdir: %v", err)
	}
	for _, subdir := range []string{"a", "b", "c"} {
		path := filepath.Join(dir, subdir)
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatalf("failed to make a subdir: %v", err)
		}
		if empty, err := dirIsEmpty(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		} else if empty {
			t.Errorf("expected %q to be deemed not-empty", dir)
		}
	}

	// Test error path.
	if _, err := dirIsEmpty(filepath.Join(tempRoot, "does-not-exist")); err == nil {
		t.Errorf("unexpected success for non-existent dir")
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

	if err := reCreate(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// validate by making sure new dir is empty
	if empty, err := dirIsEmpty(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if !empty {
		t.Errorf("expected %q to be deemed empty", tempRoot)
	}
}

func Test_publishSymlink_readAbsLink(t *testing.T) {
	tempRoot := t.TempDir()

	link := filepath.Join(tempRoot, "link")

	// create target folder with some files
	target := filepath.Join(tempRoot, "target")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatalf("failed to make a temp subdir: %v", err)
	}
	for _, file := range []string{"a", "b", "c"} {
		path := filepath.Join(target, file)
		if err := os.WriteFile(path, []byte{}, 0755); err != nil {
			t.Fatalf("failed to write a file: %v", err)
		}
	}

	if err := publishSymlink(link, target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// read link to confirm target as destination
	if dest, err := readAbsLink(link); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if dest != target {
		t.Errorf("expected %q to be the destination of the link but its %q ",
			target, dest)
	}

	// Try symlinking to same destination again
	if err := publishSymlink(link, target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// read link to confirm target as destination
	if dest, err := readAbsLink(link); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if dest != target {
		t.Errorf("expected %q to be the destination of the link but its %q ",
			target, dest)
	}

	// swap link to new target2 while its still symlinked to old one
	target2 := filepath.Join(tempRoot, "target2")
	if err := os.Mkdir(target2, 0755); err != nil {
		t.Fatalf("failed to make a temp subdir: %v", err)
	}

	if err := publishSymlink(link, target2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// read link to confirm target2 as destination
	if dest, err := readAbsLink(link); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if dest != target2 {
		t.Errorf("expected %q to be the destination of the link but its %q ",
			target2, dest)
	}

	// Test non link absolute path.
	if _, err := readAbsLink("./link"); err == nil {
		t.Errorf("unexpected success for non absolute path dir")
	}
}

func Test_removeDirContentsIf(t *testing.T) {
	tempRoot := t.TempDir()

	// create target folder with some files
	target := filepath.Join(tempRoot, "target")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatalf("failed to make a temp subdir: %v", err)
	}
	for _, file := range []string{"a", "b", "c"} {
		path := filepath.Join(target, file)
		if err := os.WriteFile(path, []byte{}, 0755); err != nil {
			t.Fatalf("failed to write a file: %v", err)
		}
	}

	// should delete everything form the target dir
	if err := removeDirContents(target, slog.Default()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// target dir should be empty
	if empty, err := dirIsEmpty(target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if !empty {
		t.Errorf("expected %q to be deemed not-empty", target)
	}

	// add more files and dir
	for _, file := range []string{"a1", "b2", "c2", "d2"} {
		path := filepath.Join(target, file)
		if err := os.WriteFile(path, []byte{}, 0755); err != nil {
			t.Fatalf("failed to write a file: %v", err)
		}
	}
	if err := os.Mkdir(filepath.Join(target, "Dirs"), 0755); err != nil {
		t.Fatalf("failed to make a subdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(target, ".git"), 0755); err != nil {
		t.Fatalf("failed to make a subdir: %v", err)
	}

	// should delete everything except v2
	if err := removeDirContentsIf(
		target,
		slog.Default(),
		func(fi os.FileInfo) (bool, error) { return fi.Name() != "b2", nil },
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "b2")); err != nil {
		t.Fatalf("failed to read %q : %v", filepath.Join(target, "b2"), err)
	}

}

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
				t.Errorf("SplitAbs() got = %v, want %v", got, tt.expDir)
			}
			if got1 != tt.expBase {
				t.Errorf("SplitAbs() got1 = %v, want %v", got1, tt.expBase)
			}
		})
	}
}
