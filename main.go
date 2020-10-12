package main

import (
	"os"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/urfave/cli"

	"github.com/simonswine/mi-flora-remote-write/miflora"
)

func main() {
	var logger log.Logger
	logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)

	m := miflora.New().WithLogger(logger)

	app := &cli.App{
		Commands: []cli.Command{
			{
				Name:    "scan",
				Aliases: []string{"s"},
				Flags: []cli.Flag{
					cli.DurationFlag{Name: "timeout", Value: time.Second * 2},
				},
				Usage: "scan for sensors reachable by bluetooth",
				Action: func(c *cli.Context) error {
					logger.Log("msg", "scanning for available bluetooth sensors")
					if err := m.Scan(c.Duration("timeout")); err != nil {
						return err
					}
					return nil
				},
			},
			{
				Name:    "realtime",
				Aliases: []string{"r"},
				Usage:   "receive realtime values from sensors",
				Action: func(c *cli.Context) error {
					if err := m.Realtime(); err != nil {
						return err
					}
					return nil
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		level.Error(logger).Log("msg", err)
	}
}
