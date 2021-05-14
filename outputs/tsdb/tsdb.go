package tsdb

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"

	"github.com/simonswine/mi-flora-exporter/miflora/model"
	promoutput "github.com/simonswine/mi-flora-exporter/outputs/prometheus"
)

type metric struct {
	l labels.Labels
	t int64
	v float64
}

func metricNameLabel(o prometheus.Opts) string {
	return prometheus.BuildFQName(o.Namespace, o.Subsystem, o.Name)
}

func resultToMetrics(r *model.Result) []*metric {
	var metrics []*metric

	var t = timestamp.FromTime(time.Now())
	if r.Timestamp != nil {
		t = timestamp.FromTime(*r.Timestamp)
	}

	defaultLabels := labels.New(
		labels.Label{
			Name:  promoutput.LabelName,
			Value: r.Name,
		},
		labels.Label{
			Name:  promoutput.LabelAddress,
			Value: r.Address,
		},
	)

	if r.Firmware != nil {
		// info
		metrics = append(metrics, &metric{
			l: labels.NewBuilder(defaultLabels).
				Set(promoutput.LabelVersion, r.Firmware.Version).
				Set(labels.MetricName, metricNameLabel(prometheus.Opts(promoutput.MetricOptsInfo))).
				Labels(),
			t: t,
			v: 1.0,
		})
		// battery
		metrics = append(metrics, &metric{
			l: labels.NewBuilder(defaultLabels).
				Set(labels.MetricName, metricNameLabel(prometheus.Opts(promoutput.MetricOptsBattery))).
				Labels(),
			t: t,
			v: float64(r.Firmware.Battery),
		})
	}

	if r.Measurement != nil {
		metrics = append(metrics, &metric{
			l: labels.NewBuilder(defaultLabels).
				Set(labels.MetricName, metricNameLabel(prometheus.Opts(promoutput.MetricOptsConductivity))).
				Labels(),
			t: t,
			v: r.Measurement.Conductivity.Value(),
		})
		metrics = append(metrics, &metric{
			l: labels.NewBuilder(defaultLabels).
				Set(labels.MetricName, metricNameLabel(prometheus.Opts(promoutput.MetricOptsBrightness))).
				Labels(),
			t: t,
			v: float64(*r.Measurement.Brightness),
		})
		metrics = append(metrics, &metric{
			l: labels.NewBuilder(defaultLabels).
				Set(labels.MetricName, metricNameLabel(prometheus.Opts(promoutput.MetricOptsMoisture))).
				Labels(),
			t: t,
			v: float64(*r.Measurement.Moisture),
		})
		metrics = append(metrics, &metric{
			l: labels.NewBuilder(defaultLabels).
				Set(labels.MetricName, metricNameLabel(prometheus.Opts(promoutput.MetricOptsTemperature))).
				Labels(),
			t: t,
			v: r.Measurement.Temperature.Value(),
		})
	}

	return metrics
}

type TSDB struct {
	logger log.Logger
}

func New(logger log.Logger) *TSDB {
	return &TSDB{
		logger: level.Debug(logger),
	}
}

func (t *TSDB) Run(ctx context.Context, dir string) (chan *model.Result, chan error, error) {
	resultsCh := make(chan *model.Result)
	head, err := tsdb.NewHead(
		nil,
		t.logger,
		nil,
		&tsdb.HeadOptions{
			ChunkRange: time.Duration(time.Hour * 24 * 365).Milliseconds(), // a year should be enough
		},
	)
	if err != nil {
		return nil, nil, err
	}

	if err := head.Init(math.MinInt64); err != nil {
		return nil, nil, err
	}

	errCh := make(chan error)

	go func() {
		defer close(errCh)

	results:
		for result := range resultsCh {
			a := head.Appender(ctx)
			for _, m := range resultToMetrics(result) {
				if _, err := a.Append(0, m.l, m.t, m.v); err != nil {
					errCh <- err
					break results
				}
			}
			if err := a.Commit(); err != nil {
				errCh <- err
				break results
			}

		}

		seriesCount := head.NumSeries()
		mint := head.MinTime()
		maxt := head.MaxTime() + 1

		_ = level.Info(t.logger).Log("msg", "flushing block", "series_count", seriesCount, "mint", timestamp.Time(mint), "maxt", timestamp.Time(maxt))

		// Flush head to disk as a block.
		compactor, err := tsdb.NewLeveledCompactor(
			ctx,
			nil,
			t.logger,
			[]int64{int64(1000 * (2 * time.Hour).Seconds())}, // Does not matter, used only for planning.
			chunkenc.NewPool())
		if err != nil {
			return
		}
		if _, err := compactor.Write(dir, head, mint, maxt, nil); err != nil {
			errCh <- fmt.Errorf("compactor write: %w", err)
			return
		}

		if err := head.Close(); err != nil {
			errCh <- err
			return
		}

	}()

	return resultsCh, errCh, nil
}
