// Package mirror periodically mirrors (bare clones) remote repositories locally.
// The mirror is created with `--mirror=fetch` hence everything in `refs/*` on the remote
// will be directly mirrored into `refs/*` in the local repository.
// it can also maintain multiple mirrored checked out worktrees on different references.
//
// The implementation borrows heavily from [kubernetes/git-sync].
// If you want to sync single repository on one reference then you are probably better off
// with [kubernetes/git-sync], as it provides a lot more customisation.
// `git-mirror` should be used if multiple mirrored repositories with multiple
// checked out branches (worktrees) is required.
//
// # Usages
//
// please see examples below
//
// # Logging:
//
// package takes slog reference for logging and prints logs up to 'trace' level
//
// Example:
//
//	loggerLevel  = new(slog.LevelVar)
//	levelStrings = map[string]slog.Level{
//		"trace": slog.Level(-8),
//		"debug": slog.LevelDebug,
//		"info":  slog.LevelInfo,
//		"warn":  slog.LevelWarn,
//		"error": slog.LevelError,
//	}
//
//	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
//		Level: loggerLevel,
//	}))
//	loggerLevel.Set(levelStrings["trace"])
//
//	repos, err := NewRepoPool(conf, logger.With("logger", "git-mirror"), nil)
//	if err != nil {
//		panic(err)
//	}
//
// [kubernetes/git-sync]: https://github.com/kubernetes/git-sync
package mirror
