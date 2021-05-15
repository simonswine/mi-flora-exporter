package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/simonswine/mi-flora-exporter/miflora/model"
)

// source: https://github.com/xperimental/flowercare-exporter/blob/154cb55a20cc8e29aab04f82c187c54cce5ab3be/internal/collector/collector.go#L12-L58

const (
	// MetricPrefix contains the prefix used by all metrics emitted from this collector.
	Namespace = "flowercare"

	LabelAddress = "macaddress"
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
	MetricOptsRSSI = prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "signal_strength_rssi",
		Help:      "Signal strenght of the sensors as reported by the bluetooth adapter.",
		Buckets:   prometheus.LinearBuckets(-120, 10, 12),
	}
	MetricLastAdv = prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "last_adv_timestamp", // do not name this advertisement as that is blocked by adblockers
		Help:      "Contains the timestamp when the last advertisement from the sensor was received by the Bluetooth device.",
	}
)

type Metrics struct {
	Info         *prometheus.GaugeVec
	Battery      *prometheus.GaugeVec
	Conductivity *prometheus.GaugeVec
	Brightness   *prometheus.GaugeVec
	Moisture     *prometheus.GaugeVec
	Temperature  *prometheus.GaugeVec
	RSSI         *prometheus.HistogramVec
	LastAdv      *prometheus.GaugeVec
}

func (m *Metrics) ObserveRSSI(v float64, labelValues ...string) {
	m.RSSI.WithLabelValues(labelValues...).Observe(v)
	m.LastAdv.WithLabelValues(labelValues...).SetToCurrentTime()
}

func (m *Metrics) ObserveMeasurement(v *model.Measurement, labelValues ...string) {
	if v.Temperature != nil {
		m.Temperature.WithLabelValues(labelValues...).Set(v.Temperature.Value())
	}
	if v.Conductivity != nil {
		m.Conductivity.WithLabelValues(labelValues...).Set(v.Conductivity.Value())
	}
	if v.Brightness != nil {
		m.Brightness.WithLabelValues(labelValues...).Set(float64(*v.Brightness))
	}
	if v.Moisture != nil {
		m.Moisture.WithLabelValues(labelValues...).Set(float64(*v.Moisture))
	}
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		Info:         promauto.With(r).NewGaugeVec(MetricOptsInfo, append(defaultLabels, LabelVersion)),
		Battery:      promauto.With(r).NewGaugeVec(MetricOptsBattery, defaultLabels),
		Conductivity: promauto.With(r).NewGaugeVec(MetricOptsConductivity, defaultLabels),
		Brightness:   promauto.With(r).NewGaugeVec(MetricOptsBrightness, defaultLabels),
		Moisture:     promauto.With(r).NewGaugeVec(MetricOptsMoisture, defaultLabels),
		Temperature:  promauto.With(r).NewGaugeVec(MetricOptsTemperature, defaultLabels),
		RSSI:         promauto.With(r).NewHistogramVec(MetricOptsRSSI, defaultLabels),
		LastAdv:      promauto.With(r).NewGaugeVec(MetricLastAdv, defaultLabels),
	}
}
