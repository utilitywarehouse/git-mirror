package mirror_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	"gopkg.in/yaml.v3"
)

func Test_Example_worktree(t *testing.T) {
	Example_worktree()
}

func Example_worktree() {
	tmpRoot, err := os.MkdirTemp("", "git-mirror-worktree-example-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpRoot)

	config := `
defaults:
  root: /tmp/git-mirror
  interval: 30s
  mirror_timeout: 2m
  git_gc: always
repositories:
  - remote: https://github.com/utilitywarehouse/git-mirror.git
    worktrees:
    - link: main
`
	ctx := context.Background()

	conf := mirror.RepoPoolConfig{}
	err = yaml.Unmarshal([]byte(config), &conf)
	if err != nil {
		panic(err)
	}

	conf.Defaults.Root = tmpRoot

	repos, err := mirror.NewRepoPool(conf, slog.Default(), nil)
	if err != nil {
		panic(err)
	}

	// perform 1st mirror to ensure all repositories
	// initial mirror might take longer
	if err := repos.MirrorAll(ctx, 5*time.Minute); err != nil {
		panic(err)
	}

	// start mirror Loop
	repos.StartLoop()

	hash, err := repos.Hash(ctx, "https://github.com/utilitywarehouse/git-mirror.git", "main", "")
	if err != nil {
		panic(err)
	}
	fmt.Println("last commit hash at main", "hash", hash)

	msg, err := repos.Subject(ctx, "https://github.com/utilitywarehouse/git-mirror.git", "main")
	if err != nil {
		panic(err)
	}
	fmt.Println("last commit msg at main", "msg", msg)

	// make sure file exists in the tree
	_, err = os.Stat(tmpRoot + "/main/pkg/mirror/repository.go")
	if err != nil {
		panic(err)
	}
}
