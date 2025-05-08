package repopool_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/utilitywarehouse/git-mirror/repopool"
	"gopkg.in/yaml.v3"
)

func Test_Example_withOutWorktree(t *testing.T) {
	Example_withOutWorktree()
}

func Example_withOutWorktree() {
	tmpRoot, err := os.MkdirTemp("", "git-mirror-without-worktree-example-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpRoot)

	config := `
defaults:
  root:
  interval: 30s
  mirror_timeout: 2m
  git_gc: always
repositories:
  - remote: https://github.com/utilitywarehouse/git-mirror.git
`
	ctx := context.Background()

	conf := repopool.Config{}
	err = yaml.Unmarshal([]byte(config), &conf)
	if err != nil {
		panic(err)
	}
	conf.Defaults.Root = tmpRoot

	repos, err := repopool.New(ctx, conf, slog.Default(), "", nil)
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
}
