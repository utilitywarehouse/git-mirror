// Package repopool periodically mirrors (bare clones) remote repositories locally.
// It supports multiple mirrored checked out worktrees on different references and
// it can also mirror multiple repositories.
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
package repopool
