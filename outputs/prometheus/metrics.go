package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// source: https://github.com/xperimental/flowercare-exporter/blob/154cb55a20cc8e29aab04f82c187c54cce5ab3be/internal/collector/collector.go#L12-L58

const (
	// MetricPrefix contains the prefix used by all metrics emitted from this collector.
	Namespace = "flowercare"

	LabelAddress = "address"
	LabelName    = "name"
	LabelVersion = "version"
)

var (
	// label every series contains
	defaultLabels = []string{LabelAddress, LabelName}

	MetricOptsInfo = prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "info",
		Help:      "Contains information about the Flower Care device.",
	}
	MetricOptsBattery = prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "battery",
		Help:      "Battery level in percent.",
	}
	MetricOptsConductivity = prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "conductivity_sm",
		Help:      "Soil conductivity in Siemens/meter.",
	}
	MetricOptsBrightness = prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "brightness_lux",
		Help:      "Ambient lighting in lux.",
	}
	MetricOptsMoisture = prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "moisture_percent",
		Help:      "Soil relative moisture in percent.",
	}
	MetricOptsTemperature = prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "temperature_celsius",
		Help:      "Ambient temperature in celsius.",
	}
)

type Metrics struct {
	Info         *prometheus.GaugeVec
	Battery      *prometheus.GaugeVec
	Conductivity *prometheus.GaugeVec
	Brightness   *prometheus.GaugeVec
	Moisture     *prometheus.GaugeVec
	Temperature  *prometheus.GaugeVec
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		Info:         promauto.With(r).NewGaugeVec(MetricOptsInfo, append(defaultLabels, LabelVersion)),
		Battery:      promauto.With(r).NewGaugeVec(MetricOptsBattery, defaultLabels),
		Conductivity: promauto.With(r).NewGaugeVec(MetricOptsConductivity, defaultLabels),
		Brightness:   promauto.With(r).NewGaugeVec(MetricOptsBrightness, defaultLabels),
		Moisture:     promauto.With(r).NewGaugeVec(MetricOptsMoisture, defaultLabels),
		Temperature:  promauto.With(r).NewGaugeVec(MetricOptsTemperature, defaultLabels),
	}
}
