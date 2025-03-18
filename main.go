package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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

	flagLogLevel    = flag.String("log-level", "info", "Log level")
	flagConfig      = flag.String("config", "/etc/git-mirror/config.yaml", "Absolute path to the config file")
	flagWatchConfig = flag.Bool("watch-config", true, "watch config for changes and reload when changes encountered")
)

func init() {
	loggerLevel.Set(slog.LevelInfo)
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: loggerLevel,
	}))
}

func usage() {
	fmt.Fprintf(os.Stderr, "NAME:\n")
	fmt.Fprintf(os.Stderr, "\tgit-mirror - git-mirror is a tool to periodically mirror remote repositories locally.\n")
	fmt.Fprintf(os.Stderr, "\nUsage:\n")
	fmt.Fprintf(os.Stderr, "\tgit-mirror [global options]\n")
	fmt.Fprintf(os.Stderr, "\nGLOBAL OPTIONS:\n")
	fmt.Fprintf(os.Stderr, "\t-log-level value    (default: 'info') Log level [$LOG_LEVEL]\n")
	fmt.Fprintf(os.Stderr, "\t-config value       (default: '/etc/git-mirror/config.yaml') Absolute path to the config file. [$GIT_MIRROR_CONFIG]\n")
	fmt.Fprintf(os.Stderr, "\t-watch-config value (default: true) watch config for changes and reload when changes encountered. [$GIT_MIRROR_WATCH_CONFIG]\n")

	os.Exit(2)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	flag.Usage = usage
	flag.Parse()

	if v, ok := os.LookupEnv("LOG_LEVEL"); ok {
		*flagLogLevel = v
	}
	if v, ok := os.LookupEnv("GIT_MIRROR_CONFIG"); ok {
		*flagConfig = v
	}
	if v, ok := os.LookupEnv("GIT_MIRROR_WATCH_CONFIG"); ok {
		if strings.EqualFold(v, "true") {
			*flagWatchConfig = true
		}
		if strings.EqualFold(v, "false") {
			*flagWatchConfig = false
		}
	}

	// set log level according to argument
	if v, ok := levelStrings[strings.ToLower(*flagLogLevel)]; ok {
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
	go WatchConfig(ctx, *flagConfig, *flagWatchConfig, 10*time.Second, onConfigChange)

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
}
