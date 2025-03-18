package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
)

var (
	loggerLevel = new(slog.LevelVar)
	logger      *slog.Logger

	levelStrings = map[string]slog.Level{
		"trace": slog.Level(-8),
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}

	flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "config",
			Sources: cli.EnvVars("GIT_MIRROR_CONFIG"),
			Value:   "/etc/git-mirror/config.yaml",
			Usage:   "Absolute path to the config file.",
		},
		&cli.StringFlag{
			Name:    "log-level",
			Sources: cli.EnvVars("LOG_LEVEL"),
			Value:   "info",
			Usage:   "Log level",
		},
		&cli.BoolFlag{
			Name: "watch-config",
			Usage: "watch config for changes and reload when changes encountered.\n" +
				"Only changes related to add,remove repository or worktrees will be actioned.",
			Value: true,
		},
	}
)

func init() {
	loggerLevel.Set(slog.LevelInfo)
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: loggerLevel,
	}))
}

func main() {
	cmd := &cli.Command{
		Name:  "git-mirror",
		Usage: "git-mirror is a tool to periodically mirror remote repositories locally.",
		Flags: flags,
		Action: func(ctx context.Context, c *cli.Command) error {
			ctx, cancel := context.WithCancel(ctx)

			// set log level according to argument
			if v, ok := levelStrings[strings.ToLower(c.String("log-level"))]; ok {
				loggerLevel.Set(v)
			}

			// path to resolve git
			gitENV := []string{fmt.Sprintf("PATH=%s", os.Getenv("PATH"))}

			// create empty repo pool which will be populated by watchConfig
			repoPool, err := mirror.NewRepoPool(ctx, mirror.RepoPoolConfig{}, logger.With("logger", "git-mirror"), gitENV)
			if err != nil {
				logger.Error("could not create git mirror pool", "err", err)
				os.Exit(1)
			}

			onConfigChange := func(config *mirror.RepoPoolConfig) {
				ensureConfig(repoPool, config)
				// start mirror Loop on newly added repos
				repoPool.StartLoop()
			}

			// Start watching the config file
			go WatchConfig(ctx, c.String("config"), 10*time.Second, onConfigChange)

			//listenForShutdown
			stop := make(chan os.Signal, 2)
			signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

			<-stop

			logger.Info("shutting down...")
			cancel()

			select {
			case <-repoPool.Stopped:
				logger.Info("all repositories mirror loop is stopped")
				os.Exit(0)

			case <-stop:
				logger.Info("second signal received, terminating")
				os.Exit(1)
			}
			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		logger.Error("failed to run app", "err", err)
		os.Exit(1)
	}
}
