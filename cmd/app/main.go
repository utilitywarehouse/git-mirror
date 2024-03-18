package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/urfave/cli/v3"
)

var (
	log = slog.Default()
)

func main() {
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Value:   "config",
				Usage:   "repositories configuration path",
				Sources: cli.EnvVars("GIT_MIRROR_CONFIG"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return fmt.Errorf("git-mirror cli is still WIP")
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Error("exiting", "err", err)
		os.Exit(1)
	}
}
