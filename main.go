package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	"gopkg.in/yaml.v3"
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

	reposRootPath = path.Join(os.TempDir(), "git-mirror", "src")

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
	}
)

func init() {
	loggerLevel.Set(slog.LevelInfo)
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: loggerLevel,
	}))
}

func parseConfigFile(path string) (*mirror.RepoPoolConfig, error) {
	yamlFile, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	conf := &mirror.RepoPoolConfig{}
	err = yaml.Unmarshal(yamlFile, conf)
	if err != nil {
		return nil, err
	}
	return conf, nil
}

func applyGitDefaults(c *cli.Command, mirrorConf *mirror.RepoPoolConfig) *mirror.RepoPoolConfig {
	if mirrorConf.Defaults.Root == "" {
		mirrorConf.Defaults.Root = reposRootPath
	}

	if mirrorConf.Defaults.GitGC == "" {
		mirrorConf.Defaults.GitGC = "always"
	}

	if mirrorConf.Defaults.Interval == 0 {
		mirrorConf.Defaults.Interval = 30 * time.Second
	}

	if mirrorConf.Defaults.MirrorTimeout == 0 {
		mirrorConf.Defaults.MirrorTimeout = 2 * time.Minute
	}

	return mirrorConf
}

func main() {
	cmd := &cli.Command{
		Name:  "git-mirror",
		Usage: "git-mirror is a tool to periodically mirror remote repositories locally.",
		Flags: flags,
		Action: func(ctx context.Context, c *cli.Command) error {

			// set log level according to argument
			if v, ok := levelStrings[strings.ToLower(c.String("log-level"))]; ok {
				loggerLevel.Set(v)
			}

			conf, err := parseConfigFile(c.String("config"))
			if err != nil {
				logger.Error("unable to parse tf applier config file", "err", err)
				os.Exit(1)
			}

			// setup git-mirror
			conf = applyGitDefaults(c, conf)

			// path to resolve strongbox
			gitENV := []string{fmt.Sprintf("PATH=%s", os.Getenv("PATH"))}

			repos, err := mirror.NewRepoPool(*conf, logger.With("logger", "git-mirror"), gitENV)
			if err != nil {
				logger.Error("could not create git mirror pool", "err", err)
				os.Exit(1)
			}

			// start mirror Loop
			repos.StartLoop()

			//listenForShutdown
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
			<-stop
			logger.Info("Shutting down")

			return nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		logger.Error("failed to run app", "err", err)
		os.Exit(1)
	}
}
