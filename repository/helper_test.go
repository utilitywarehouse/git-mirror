package repository

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/git-mirror/internal/utils"
)

func TestIsFullCommitHash(t *testing.T) {
	type args struct {
		hash string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{"1", args{""}, false},
		{"2", args{"52e804596380380a9826bc12f891b7003350c518"}, true},
		{"3", args{"a555a3c852bd26bad24c80f693ca6855640fa5ed"}, true},
		{"4", args{"a555a3c852bd26bad24c80f693ca6855640fa5ez"}, false},
		{"5", args{"489118d558a6e402e078d7e279a9fe5d7d4fbf47400ad87209a8338524399cd8"}, true},
		{"6", args{"b996dcd2524489623d33f5ed49771b5211c3e42521445010610bb040884edeee"}, true},
		{"6", args{"z996dcd2524489623d33f5ed49771b5211c3e42521445010610bb040884edeee"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsFullCommitHash(tt.args.hash); got != tt.want {
				t.Errorf("IsFullCommitHash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCommitHash(t *testing.T) {
	type args struct {
		hash string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{"1", args{""}, false},
		{"2", args{"52e804596380380a9826bc12f891b7003350c518"}, true},
		{"3", args{"a555a3c852bd26bad24c80f693ca6855640fa5ed"}, true},
		{"4", args{"a555a3c852bd26bad24c80f693ca6855640fa5ez"}, false},
		{"5", args{"489118d558a6e402e078d7e279a9fe5d7d4fbf47400ad87209a8338524399cd8"}, true},
		{"6", args{"b996dcd2524489623d33f5ed49771b5211c3e42521445010610bb040884edeee"}, true},
		{"7", args{"z996dcd2524489623d33f5ed49771b5211c3e42521445010610bb040884edeee"}, false},
		{"8", args{"52e8045"}, true},
		{"9", args{"a555a3c"}, true},
		{"10", args{"489118d"}, true},
		{"11", args{"b996dcd"}, true},
		{"12", args{"b996dcz"}, false},
		{"13", args{"b996dc"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCommitHash(tt.args.hash); got != tt.want {
				t.Errorf("IsCommitHash() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
	if dest, err := utils.ReadAbsLink(link); err != nil {
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
	if dest, err := utils.ReadAbsLink(link); err != nil {
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
	if dest, err := utils.ReadAbsLink(link); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if dest != target2 {
		t.Errorf("expected %q to be the destination of the link but its %q ",
			target2, dest)
	}

	// Test non link absolute path.
	if _, err := utils.ReadAbsLink("./link"); err == nil {
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

func Test_updatedRefs(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			"1",
			`remote: Enumerating objects: 7, done.
remote: Counting objects: 100% (7/7), done.
remote: Compressing objects: 100% (1/1), done.
remote: Total 4 (delta 3), reused 4 (delta 3), pack-reused 0
Unpacking objects: 100% (4/4), 344 bytes | 172.00 KiB/s, done.
  da39a3ee5e6b4b0d3255bfef95601890afd80709 f109e33263250f9212b1ac6a2a96215c270a0232 refs/heads/branch1`,
			[]string{"refs/heads/branch1"},
		}, {
			"2",
			`remote: Enumerating objects: 124, done.
remote: Counting objects: 100% (98/98), done.
remote: Compressing objects: 100% (23/23), done.
remote: Total 26 (delta 20), reused 3 (delta 3), pack-reused 0
Unpacking objects: 100% (26/26), 6.51 KiB | 1.30 MiB/s, done.
/ f10e2821bbbea527ea02200352313bc059445190 ca46a771da19d175bc356a786aaae9c18c7eda50 refs/pull/1/merge asdfdsf
? 4452d71687b6bc2c9389c3349fdc17fbd73b833b e6c3d625ee5b1b4f36ac4f2c48579fd2c1cf0687 refs/pull/2/merge
  bb11b5672fefe86987e32960bd3a161b0d1717d9 44d11327a8be9107bade3b28a328ea261d7a482b refs/pull/3/merge
+ 79d6188de4447cb7cb204c6c610c8814b64460f8 90e42330a387dd7fba63d1c6ed02c965d8d10bd7 refs/pull/4/merge
= 1643d7874890dca5982facfba9c4f24da53876e9 5cbac6e18ac6079300f7d64bc9f38c5cd377f2aa refs/pull/5/merge
- 1643d7874890dca5982facfba9c4f24da53876e9 3da541559918a808c2402bba5012f6c60b27661c refs/pull/6/merge
* 1925b0b80b618dce7303cc3e7059da5032474967 180467973d800a01fece8e469dc40db11a1df206 refs/pull/7/merge
! 1925b0b80b618dce7303cc3e7059da5032474967 180467973d800a01fece8e469dc40db11a1df206 refs/pull/8/merge
t 1643d7874890dca5982facfba9c4f24da53876e9 4c286e182bc4d1832a8739b18c19ecaf9262c37a refs/pull/9/merge
t1643d7874890dca5982facfba9c4f24da53876e9 4c286e182bc4d1832a8739b18c19ecaf9262c37a refs/pull/10/merge`,
			[]string{
				"refs/pull/1/merge",
				"refs/pull/2/merge",
				"refs/pull/3/merge",
				"refs/pull/4/merge",
				"refs/pull/6/merge",
				"refs/pull/7/merge",
				"refs/pull/8/merge",
				"refs/pull/9/merge",
			},
		}, {
			"3",
			`
 e74db1326417c2faab522a0cdd3cb50a0e528a66 c257140b4e3202ba6ca34dca1234ac5a78700e5a refs/heads/branch1
			`,
			[]string{
				"refs/heads/branch1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updatedRefs(tt.output)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("updatedRefs() mismatch (-want +got):\n%s", diff)
			}

		})
	}
}

func TestJitter(t *testing.T) {
	type args struct {
		duration  time.Duration
		maxFactor float64
	}
	tests := []struct {
		name    string
		args    args
		minWant time.Duration
		maxWant time.Duration
	}{
		{"1", args{10 * time.Second, 0.1}, 10 * time.Second, 11 * time.Second},
		{"2", args{10 * time.Second, 0.5}, 10 * time.Second, 15 * time.Second},
		{"3", args{10 * time.Second, 0.0}, 10 * time.Second, 10 * time.Second},
		{"4", args{30 * time.Second, 0.1}, 30 * time.Second, 33 * time.Second},
		{"5", args{30 * time.Second, 0.5}, 30 * time.Second, 45 * time.Second},
		{"6", args{30 * time.Second, 0.0}, 30 * time.Second, 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// since we are using rand test values 10 times
			for i := 0; i < 10; i++ {
				got := jitter(tt.args.duration, tt.args.maxFactor)
				if got < tt.minWant {
					t.Errorf("jitter() = %v, min-want %v", got, tt.minWant)
				}
				if got > tt.maxWant {
					t.Errorf("jitter() = %v, max-want %v", got, tt.maxWant)
				}
			}
		})
	}
}
