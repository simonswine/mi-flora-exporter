package main

import (
	"context"
	"errors"
	"fmt"
	stdlog "log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/go-ble/ble/linux"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/urfave/cli/v2"

	"github.com/simonswine/mi-flora-exporter/miflora"
	mcontext "github.com/simonswine/mi-flora-exporter/miflora/context"
	"github.com/simonswine/mi-flora-exporter/miflora/model"
	"github.com/simonswine/mi-flora-exporter/outputs/json"
	"github.com/simonswine/mi-flora-exporter/outputs/tsdb"
)

func scanFlags(scanPassiveDefault bool) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "adapter",
			Value: "default",
			Usage: "Bluetooth adapter to use.",
		},
		&cli.DurationFlag{
			Name:  "scan-timeout",
			Value: mcontext.ScanTimeoutFromContext(context.Background()),
			Usage: "Timeout after which scanning for sensor devices is stopped.",
		},
		&cli.BoolFlag{
			Name:  "scan-passive",
			Value: scanPassiveDefault,
			Usage: "A passive scan is taking longer, but more energy efficient (and hence less battery draining for the sensors).",
		},
		&cli.Int64Flag{
			Name:  "expected-sensors",
			Value: mcontext.ExpectedSensorsFromContext(context.Background()),
			Usage: "If set to a value > 0 sensor scanning will stop after this number of sensors are detected.",
		},
		&cli.StringSliceFlag{
			Name:  "sensor-name",
			Usage: "This flag can be used to define customized names for certain adapters. Can be repeated. (Example: 'my-bedroom-plant=c4:7c:8d:aa:bb:cc')",
		},
	}
}

var outputFlags = []cli.Flag{
	&cli.StringFlag{
		Name:  "output",
		Value: "json",
		Usage: "Output plugin to use (json|tsdb).",
	},
	&cli.StringFlag{
		Name:  "tsdb.path",
		Value: "./tsdb",
		Usage: "Path to the TSDB database.",
	},
}

func scanContext(c *cli.Context, ctx context.Context) context.Context {
	ctx = mcontext.ContextWithExpectedSensors(ctx, c.Int64("expected-sensors"))
	ctx = mcontext.ContextWithScanTimeout(ctx, c.Duration("scan-timeout"))
	ctx = mcontext.ContextWithScanPassive(ctx, c.Bool("scan-passive"))
	ctx = mcontext.ContextWithSensorNames(ctx, c.StringSlice("sensor-name"))
	return ctx
}

func filterContextErr(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func main() {

	go func() {
		if err := http.ListenAndServe(":7070", nil); err != nil {
			panic(err)
		}
	}()

	var logger log.Logger
	logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)
	stdlog.SetOutput(log.NewStdlibAdapter(level.Debug(logger)))

	newMiraFlora := func(c *cli.Context) (context.Context, *miflora.MiFlora) {
		device := c.String("adapter")
		d, err := linux.NewDevice()
		if err != nil {
			_ = level.Error(logger).Log("msg", fmt.Sprintf("failed to get %s device", device), "error", err)
			os.Exit(1)
		}
		ctx := scanContext(c, context.Background())
		return ctx, miflora.New(d).WithLogger(logger)
	}

	setupOutput := func(ctx context.Context, c *cli.Context) (context.Context, func() error, error) {
		var resultCh chan *model.Result
		var errCh chan error
		var err error

		switch outputType := c.String("output"); outputType {
		case "json":
			resultCh, errCh, err = json.New(logger).Run(ctx, os.Stdout)
		case "tsdb":
			resultCh, errCh, err = tsdb.New(logger).Run(ctx, c.String("tsdb.path"))
		default:
			return nil, nil, fmt.Errorf("unknown output '%s", outputType)
		}

		if err != nil {
			return nil, nil, err
		}

		ctx = mcontext.ContextWithResultChannel(ctx, resultCh)

		ctx, cancel := context.WithCancel(ctx)

		errResult := make(chan error)

		go func() {

			// wait for error in output
			// TODO: support consecutive errors
			err = <-errCh

			if err != nil {
				_ = level.Error(logger).Log("msg", "cancel operation due to error in output", "error", err)
				cancel()
			}

			errResult <- err
		}()

		return ctx,
			func() error {
				//close(resultCh)
				return <-errResult
			}, nil
	}

	app := &cli.App{
		Commands: []*cli.Command{
			{
				Name:    "scan",
				Aliases: []string{"s"},
				Flags:   scanFlags(false),
				Usage:   "scan for sensors reachable by bluetooth",
				Action: func(c *cli.Context) error {
					_ = logger.Log("msg", "scanning for available bluetooth sensors")
					ctx, m := newMiraFlora(c)
					if err := m.Scan(ctx); err != nil {
						return err
					}
					return nil
				},
			},
			{
				Name:    "exporter",
				Aliases: []string{"e"},
				Flags:   scanFlags(true),
				Usage:   "run prometheus exporter",
				Action: func(c *cli.Context) error {
					_ = logger.Log("msg", "starting exporter")
					ctx, m := newMiraFlora(c)
					if err := m.Exporter(ctx); err != nil {
						return err
					}
					return nil
				},
			},
			{
				Name:    "realtime",
				Aliases: []string{"r"},
				Flags:   append(scanFlags(false), outputFlags...),
				Usage:   "receive realtime values from sensors",
				Action: func(c *cli.Context) error {
					ctx, m := newMiraFlora(c)

					ctx, finish, err := setupOutput(ctx, c)
					if err != nil {
						return err
					}

					if err := filterContextErr(m.Realtime(ctx)); err != nil {
						return err
					}

					return finish()

				},
			},
			{
				Name:    "history",
				Aliases: []string{"H"},
				Flags:   append(scanFlags(false), outputFlags...),
				Usage:   "receive historic values from sensors",
				Action: func(c *cli.Context) error {
					ctx, m := newMiraFlora(c)

					ctx, finish, err := setupOutput(ctx, c)
					if err != nil {
						return err
					}

					if err := filterContextErr(m.HistoricValues(ctx)); err != nil {
						return err
					}

					return finish()
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		_ = level.Error(logger).Log("msg", err)
		os.Exit(1)
	}
}
