package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/utilitywarehouse/git-mirror/repopool"
	"github.com/utilitywarehouse/git-mirror/repository"
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

	gitExecutablePath = exec.Command("git").String()
)

func init() {
	loggerLevel.Set(slog.LevelInfo)
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: loggerLevel,
	}))
}

func envString(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if ok {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value, ok := os.LookupEnv(key)
	if ok {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
		return fallback
	}
	return fallback
}

func usage() {
	fmt.Fprintf(os.Stderr, "NAME:\n")
	fmt.Fprintf(os.Stderr, "\tgit-mirror - git-mirror is a tool to periodically mirror remote repositories locally.\n")
	fmt.Fprintf(os.Stderr, "\nUsage:\n")
	fmt.Fprintf(os.Stderr, "\tgit-mirror [global options]\n")
	fmt.Fprintf(os.Stderr, "\nGLOBAL OPTIONS:\n")
	fmt.Fprintf(os.Stderr, "\t-log-level value            (default: 'info') Log level [$LOG_LEVEL]\n")
	fmt.Fprintf(os.Stderr, "\t-config value               (default: '/etc/git-mirror/config.yaml') Absolute path to the config file. [$GIT_MIRROR_CONFIG]\n")
	fmt.Fprintf(os.Stderr, "\t-watch-config value         (default: true) watch config for changes and reload when changes encountered. [$GIT_MIRROR_WATCH_CONFIG]\n")
	fmt.Fprintf(os.Stderr, "\t-http-bind-address value    (default: ':9001') The address the web server binds to. [$GIT_MIRROR_HTTP_BIND]\n")
	fmt.Fprintf(os.Stderr, "\t-one-time                   (default: 'false') Exit after first mirror. [$GIT_MIRROR_ONE_TIME]\n")
	fmt.Fprintf(os.Stderr, "\t-github-webhook-secret      (default: '') The Github webhook secret used to validate payload [$GITHUB_WEBHOOK_SECRET]\n")
	fmt.Fprintf(os.Stderr, "\t-github-skip-sig-validation (default: false) If set github webhook signature validation will be skipped [$GITHUB_SKIP_SIG_VALIDATION]\n")
	fmt.Fprintf(os.Stderr, "\t-github-webhook-path        (default: '/github-webhook') The path on which webserver will receive github webhook events [$GITHUB_WEBHOOK_PATH]\n")

	os.Exit(2)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	flagLogLevel := flag.String("log-level", envString("LOG_LEVEL", "info"), "Log level")
	flagConfig := flag.String("config", envString("GIT_MIRROR_CONFIG", "/etc/git-mirror/config.yaml"), "Absolute path to the config file")
	flagWatchConfig := flag.Bool("watch-config", envBool("GIT_MIRROR_WATCH_CONFIG", true), "watch config for changes and reload when changes encountered")
	flagHttpBind := flag.String("http-bind-address", envString("GIT_MIRROR_HTTP_BIND", ":9001"), "The address the web server binds to")
	flagGithubWhSecret := flag.String("github-webhook-secret", envString("GITHUB_WEBHOOK_SECRET", ""), "The Github webhook secret used to validate payload")
	flagGithubWhSkipValidation := flag.Bool("github-skip-sig-validation", envBool("GITHUB_SKIP_SIG_VALIDATION", false), "If set github webhook signature validation will be skipped")
	flagGithubWhPath := flag.String("github-webhook-path", envString("GITHUB_WEBHOOK_PATH", "/github-webhook"), "The path on which webserver will receive github webhook events")

	flagOneTime := flag.Bool("one-time", envBool("GIT_MIRROR_ONE_TIME", false), "Exit after first mirror")
	flagVersion := flag.Bool("version", false, "git-mirror version")

	flag.Usage = usage
	flag.Parse()

	info, _ := debug.ReadBuildInfo()

	if *flagVersion || (flag.NArg() == 1 && flag.Arg(0) == "version") {
		fmt.Printf("version=%s go=%s\n", info.Main.Version, info.GoVersion)
		return
	}

	// set log level according to argument
	if v, ok := levelStrings[strings.ToLower(*flagLogLevel)]; ok {
		loggerLevel.Set(v)
	}

	logger.Info("version", "app", info.Main.Version, "go", info.GoVersion)
	logger.Info("config", "path", *flagConfig, "watch", *flagWatchConfig)

	repository.EnableMetrics("", prometheus.NewRegistry())
	prometheus.MustRegister(configSuccess, configSuccessTime)

	conf, err := parseConfigFile(*flagConfig)
	if err != nil {
		logger.Error("unable to parse git-mirror config file", "err", err)
		os.Exit(1)
	}

	applyGitDefaults(conf)

	repoPool, err := repopool.New(ctx, *conf, logger.With("logger", "git-mirror"), gitExecutablePath, nil)
	if err != nil {
		logger.Error("could not create git mirror pool", "err", err)
		os.Exit(1)
	}

	allSucceed := true
	// perform 1st mirror to ensure all repositories syncs to indicate readiness
	// also initial mirror might take longer
	timeout := 2 * conf.Defaults.MirrorTimeout
	for _, repo := range conf.Repositories {
		mCtx, cancel := context.WithTimeout(ctx, timeout)
		err = repoPool.Mirror(mCtx, repo.Remote)
		cancel()
		if err != nil {
			allSucceed = false
			logger.Error("initial mirror failed", "repo", repo.Remote, "err", err)
		}
	}

	if *flagOneTime {
		logger.Info("existing after first mirror")
		if !allSucceed {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// start mirror Loop
	repoPool.StartLoop()

	cleanupOrphanedRepos(conf, repoPool)

	onConfigChange := func(config *repopool.Config) bool {
		return ensureConfig(repoPool, config)
	}

	// Start watching the config file
	go WatchConfig(ctx, *flagConfig, *flagWatchConfig, 10*time.Second, onConfigChange)

	// setup webhook and metrics server
	wh := &GithubWebhookHandler{
		repoPool:          repoPool,
		log:               logger.With("logger", "github-webhook"),
		secret:            *flagGithubWhSecret,
		skipSigValidation: *flagGithubWhSkipValidation,
	}

	server := &http.Server{
		Addr:              *flagHttpBind,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       5 * time.Second,
		ReadHeaderTimeout: 1 * time.Second,
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// register handler if skip validation flag is set or secret is set
	if *flagGithubWhSkipValidation || *flagGithubWhSecret != "" {
		logger.Info("registering github webhook", "path", *flagGithubWhPath)
		mux.Handle(*flagGithubWhPath, wh)
	}

	server.Handler = mux

	go func() {
		logger.Info("starting web server", "add", *flagHttpBind)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server terminated", "err", err)
		}
	}()

	//listenForShutdown
	stop := make(chan os.Signal, 2)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop

	logger.Info("shutting down...")
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("failed to shutdown http server", "err", err)
	}
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
