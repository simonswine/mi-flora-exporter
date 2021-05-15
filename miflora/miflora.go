package miflora

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/linux"
	"github.com/go-ble/ble/linux/hci/cmd"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/simonswine/mi-flora-exporter/miflora/advertisements"
	mcontext "github.com/simonswine/mi-flora-exporter/miflora/context"
	"github.com/simonswine/mi-flora-exporter/miflora/model"
)

const (
	handleFirmwareBattery = uint16(0x38)
	handleDataRead        = uint16(0x35)
	handleModeChange      = uint16(0x33)
	handleDeviceTime      = uint16(0x41)
	handleHistoryControl  = uint16(0x3e)
	handleHistoryRead     = uint16(0x3c)
)

//nolint:deadcode,varcheck,unused // keep the unimplemented modes
var (
	modeBlinkLED           = []byte{0xfd, 0xff}
	modeRealtimeReadInit   = []byte{0xa0, 0x1f}
	modeHistoryReadInit    = []byte{0xa0, 0x00, 0x00}
	modeHistoryReadSuccess = []byte{0xa2, 0x00, 0x00}
	modeHistoryReadFailed  = []byte{0xa3, 0x00, 0x00}
)

type MiFlora struct {
	logger  log.Logger
	device  *linux.Device
	stopCh  chan struct{}
	sensors map[string]*Sensor
}

type Sensor struct {
	logger        log.Logger
	device        *linux.Device
	advertisement ble.Advertisement

	name           string
	historyPointer *uint16
}

func (s *Sensor) finished() bool {
	if s.historyPointer == nil {
		return false
	}
	return *s.historyPointer == 0
}

type HistoricMeasurement struct {
	model.Measurement
	DeviceTime time.Time
}

func (m *HistoricMeasurement) UnmarshalBinary(r io.Reader) error {
	var t int32
	if err := binary.Read(r, binary.LittleEndian, &t); err != nil {
		return fmt.Errorf("error reading data: %w", err)
	}
	m.DeviceTime = time.Unix(int64(t), 0).UTC()
	return m.Measurement.UnmarshalBinary(r)
}

func (s *Sensor) client(ctx context.Context) (*client, error) {
	bleClient, err := s.device.Dial(ctx, s.advertisement.Addr())
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %w", err)
	}
	c := &client{
		client: bleClient,
	}

	// this handles disconnected clients
	go func() {
		<-c.client.Disconnected()
		_ = level.Debug(s.logger).Log("msg", "connection closed")
	}()

	p, err := c.client.DiscoverProfile(true)
	if err != nil {
		return nil, fmt.Errorf("failed to discover profile: %w", err)
	}
	var services []string
	for _, service := range p.Services {
		services = append(services, service.UUID.String())
	}
	_ = level.Debug(s.logger).Log("msg", "discovered profile", "services", strings.Join(services, ", "))
	c.profile = p

	if err := c.client.Subscribe(
		c.findCharacteristicByValueHandle(0x21),
		false,
		func(req []byte) {
			_ = level.Debug(s.logger).Log("msg", "received notification 0x21", "data", string(req))
		},
	); err != nil {
		_ = level.Warn(s.logger).Log("msg", "error subscribing to notification", "error", err)
	}

	return c, nil
}

func isDeclaredSensor(ctx context.Context, addr string) (bool, string) {
	for _, nameOverride := range mcontext.SensorsNamesFromContext(ctx) {
		parts := strings.SplitN(nameOverride, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(addr, parts[1]) {
			return true, parts[0]
		}
	}

	return false, ""
}

func (m *MiFlora) newSensor(ctx context.Context, adv ble.Advertisement) *Sensor {
	name := adv.LocalName()
	addr := adv.Addr().String()

	if declared, overrideName := isDeclaredSensor(ctx, addr); declared {
		name = overrideName
	}

	logger := log.With(m.logger, "address", adv.Addr().String())
	if len(name) > 0 {
		logger = log.With(logger, "name", name)
	}
	return &Sensor{
		logger:        logger,
		device:        m.device,
		advertisement: adv,
		name:          name,
	}
}

func New(device *linux.Device) *MiFlora {
	return &MiFlora{
		logger:  log.NewNopLogger(),
		device:  device,
		sensors: make(map[string]*Sensor),
		stopCh:  make(chan struct{}),
	}
}

func (m *MiFlora) WithLogger(l log.Logger) *MiFlora {
	m.logger = l
	return m
}

const (
	deviceName    = "Flower care"
	addressPrefix = "C4:7C:8D"
)

func (m *MiFlora) Scan(ctx context.Context) error {
	_, err := m.doScan(ctx)
	return err
}

func (m *MiFlora) HistoricValues(ctx context.Context) error {
	resultCh := mcontext.ResultChannelFromContext(ctx)

	sensors, err := m.doScan(ctx)
	if err != nil {
		return err
	}

	for {
		var nextSensors []*Sensor
		for _, s := range sensors {
			if err := func(s *Sensor) error {
				ctx, cancel := context.WithTimeout(ctx, time.Second*30)
				defer cancel()

				c, err := s.client(ctx)
				if err != nil {
					_ = level.Warn(s.logger).Log("msg", "error connecting to sensor", "error", err)
					return nil
				}
				defer func() {
					if err := c.client.CancelConnection(); err != nil {
						_ = level.Warn(s.logger).Log("msg", "error canceling connection", "error", err)
					}
				}()

				timeDiff, err := c.DeviceTimeDiff()
				if err != nil {
					_ = level.Warn(s.logger).Log("msg", "error reading device time", "error", err)
					return nil
				}

				historyLength, err := c.HistoryLength()
				if err != nil {
					_ = level.Warn(s.logger).Log("msg", "error querying history length", "error", err)
					return nil
				}
				_ = level.Debug(s.logger).Log("msg", "read length of history", "length", historyLength)

				// restore pointer
				if s.historyPointer != nil {
					historyLength = *s.historyPointer - 1
				}

				for i := int32(historyLength); i >= 0; i-- {
					pos := uint16(i)
					hm, err := c.HistoryMeasurement(pos)
					if err != nil {
						_ = level.Warn(s.logger).Log("msg", "error querying history measurement", "position", i, "error", err)
						return nil
					}

					timestamp := hm.DeviceTime.Add(timeDiff)
					if resultCh != nil {
						select {
						case <-ctx.Done():
							return ctx.Err()
						case resultCh <- &model.Result{
							Name:        s.name,
							Address:     s.advertisement.Addr().String(),
							Timestamp:   &timestamp,
							Measurement: &hm.Measurement,
						}:
						}
					}

					// store the position
					s.historyPointer = &pos

					_ = hm.LogWith(level.Debug(s.logger)).Log(
						"msg", "historic measurement successful",
						"pos", pos,
						"device_time", timestamp.Format(time.RFC3339),
					)

					// limit batch size at 50
					if historyLength-pos > 50 {
						return nil
					}
				}
				return nil

			}(s); err != nil {
				return err
			}
			if !s.finished() {
				nextSensors = append(nextSensors, s)
			}
		}
		if len(nextSensors) == 0 {
			break
		}
		sensors = nextSensors
	}
	return nil
}

type metrics struct {
	temperature  *prometheus.GaugeVec
	conductivity *prometheus.GaugeVec
	brightness   *prometheus.GaugeVec
	moisture     *prometheus.GaugeVec
	rssi         *prometheus.HistogramVec

	last_advertisement *prometheus.GaugeVec
	// TODO last_connection / battery / info
}

func newMetrics() *metrics {
	metricPrefix := "flowercare"
	sensorLabels := []string{
		"macaddress",
		"name",
	}
	return &metrics{
		temperature: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricPrefix,
				Name:      "temperature_celsius",
				Help:      "Ambient temperature in celsius.",
			},
			sensorLabels,
		),
		conductivity: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricPrefix,
				Name:      "conductivity_sm",
				Help:      "Soil conductivity in Siemens/meter.",
			},
			sensorLabels,
		),
		brightness: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricPrefix,
				Name:      "brightness_lux",
				Help:      "Ambient lighting in lux.",
			},
			sensorLabels,
		),
		moisture: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricPrefix,
				Name:      "moisture_percent",
				Help:      "Soil relative moisture in percent.",
			},
			sensorLabels,
		),
		rssi: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: metricPrefix,
				Name:      "signal_strength_rssi",
				Help:      "Signal strenght.",
				Buckets:   prometheus.LinearBuckets(-120, 10, 12),
			},
			sensorLabels,
		),
		last_advertisement: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: metricPrefix,
				Name:      "last_advertisement_timestamp",
				Help:      "Contains the timestamp when the last advertisement from the sensor was received by the Bluetooth device.",
			},
			sensorLabels,
		),
	}
}

func (m *metrics) observeRSSI(v float64, labelValues ...string) {
	m.rssi.WithLabelValues(labelValues...).Observe(v)
	m.last_advertisement.WithLabelValues(labelValues...).SetToCurrentTime()
}

func (m *metrics) observeMeasurement(v *model.Measurement, labelValues ...string) {
	if v.Temperature != nil {
		m.temperature.WithLabelValues(labelValues...).Set(v.Temperature.Value())
	}
	if v.Conductivity != nil {
		m.conductivity.WithLabelValues(labelValues...).Set(v.Conductivity.Value())
	}
	if v.Brightness != nil {
		m.brightness.WithLabelValues(labelValues...).Set(float64(*v.Brightness))
	}
	if v.Moisture != nil {
		m.moisture.WithLabelValues(labelValues...).Set(float64(*v.Moisture))
	}
}

func (m *MiFlora) Exporter(ctx context.Context) error {
	sensorsCh := make(chan *Sensor)

	metrics := newMetrics()

	go func() {
		// Expose the registered metrics via HTTP.
		http.Handle("/metrics", promhttp.HandlerFor(
			prometheus.DefaultGatherer,
			promhttp.HandlerOpts{
				// Opt into OpenMetrics to support exemplars.
				EnableOpenMetrics: true,
			},
		))
		if err := http.ListenAndServe(":9294", nil); err != nil {
			_ = level.Error(m.logger).Log("err", err)
			os.Exit(1)
		}
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		for s := range sensorsCh {
			for _, serviceData := range s.advertisement.ServiceData() {
				data, err := advertisements.New(serviceData.Data)
				if err != nil {
					_ = level.Error(s.logger).Log("err", err)
					continue
				}
				measurement := data.Values()
				rssi := s.advertisement.RSSI()
				labelValues := []string{s.advertisement.Addr().String(), s.name}

				metrics.observeMeasurement(measurement, labelValues...)
				metrics.observeRSSI(float64(rssi), labelValues...)
				_ = level.Info(measurement.LogWith(s.logger)).Log("msg", "sensor advertisement received", "rssi", rssi)
			}
		}
	}()

	if err := m.doScanReal(ctx, sensorsCh); err != nil {
		return err
	}

	return nil
}

func (m *MiFlora) Realtime(ctx context.Context) error {
	resultCh := mcontext.ResultChannelFromContext(ctx)

	sensors, err := m.doScan(ctx)
	if err != nil {
		return err
	}

	for _, s := range sensors {
		if err := func(s *Sensor) error {
			ctx, cancel := context.WithTimeout(ctx, time.Second*30)
			defer cancel()

			c, err := s.client(ctx)
			if err != nil {
				_ = level.Warn(s.logger).Log("msg", "error connecting to sensor", "error", err)
				return nil
			}

			f, err := c.Firmware()
			if err != nil {
				_ = level.Warn(s.logger).Log("msg", "error querying firmware", "error", err)
				return nil
			}
			_ = level.Info(s.logger).Log("msg", "connected", "version", f.Version, "battery", f.Battery)

			m, err := c.Measurement()
			if err != nil {
				_ = level.Warn(s.logger).Log("msg", "error querying measurement", "error", err)
				return nil
			}
			if resultCh != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case resultCh <- &model.Result{
					Name:        s.name,
					Address:     s.advertisement.Addr().String(),
					Firmware:    f,
					Measurement: m,
				}:
				}
			}
			_ = m.LogWith(level.Info(s.logger)).Log(
				"msg", "measurement successful",
			)

			return nil
		}(s); err != nil {
			return err
		}
	}
	return nil
}

func (m *MiFlora) doScanReal(ctx context.Context, sensorsCh chan *Sensor) error {
	declaredSensorNames := len(mcontext.SensorsNamesFromContext(ctx))

	handler := func(a ble.Advertisement) {
		if !isMiraFloraDevice(a) {
			return
		}
		if declaredSensorNames > 0 {
			if ok, _ := isDeclaredSensor(ctx, a.Addr().String()); !ok {
				return
			}
		}
		sensorsCh <- m.newSensor(ctx, a)
	}

	// set passive mode if required
	if mcontext.ScanPassiveFromContext(ctx) {
		if err := m.device.HCI.Send(&cmd.LESetScanParameters{
			LEScanType:           0x00,   // 0x00: passive
			LEScanInterval:       0x4000, // 0x0004 - 0x4000; N * 0.625msec
			LEScanWindow:         0x4000, // 0x0004 - 0x4000; N * 0.625msec
			OwnAddressType:       0x00,   // 0x00: public
			ScanningFilterPolicy: 0x00,   // 0x00: accept all
		}, nil); err != nil {
			return err
		}
	}

	// scan for devices
	if err := m.device.Scan(ctx, true, handler); err != nil &&
		!errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, context.Canceled) {
		return fmt.Errorf("failed to scan for sensors: %w", err)
	}
	close(sensorsCh)

	return nil
}

func (m *MiFlora) doScan(ctx context.Context) ([]*Sensor, error) {
	sensorsCh := make(chan *Sensor)

	ctx, cancel := context.WithTimeout(ctx, mcontext.ScanTimeoutFromContext(ctx))
	defer cancel()

	var sensors SensorSlice
	expectedSensors := mcontext.ExpectedSensorsFromContext(ctx)

	declaredSensorNames := len(mcontext.SensorsNamesFromContext(ctx))
	if declaredSensorNames > 0 {
		expectedSensors = int64(declaredSensorNames)
	}
	var expectedSensorsOnce sync.Once
	go func() {
		for s := range sensorsCh {
			var existed bool
			sensors, existed = sensors.insertSorted(s)
			if !existed {
				_ = level.Info(s.logger).Log("msg", "sensor found", "rssi", s.advertisement.RSSI())
			}
			if expectedSensors > 0 && int64(len(sensors)) >= expectedSensors {
				expectedSensorsOnce.Do(func() {
					_ = level.Info(m.logger).Log("msg", "all expected sensors found", "expected_sensors", expectedSensors)
					cancel()
				})
			}
		}
	}()

	if err := m.doScanReal(ctx, sensorsCh); err != nil {
		return nil, err
	}

	return sensors, nil
}

func isMiraFloraDevice(a ble.Advertisement) bool {
	if !a.Connectable() {
		return false
	}

	if a.LocalName() == deviceName {
		return true
	}

	if strings.HasPrefix(strings.ToUpper(a.Addr().String()), addressPrefix) {
		return true
	}

	return false
}

type SensorSlice []*Sensor

func (s SensorSlice) insertSorted(e *Sensor) (SensorSlice, bool) {
	v := func(s *Sensor) string {
		return s.advertisement.Addr().String()
	}
	if len(s) == 0 {
		return []*Sensor{e}, false
	}
	i := sort.Search(len(s), func(i int) bool { return v(s[i]) >= v(e) })

	if len(s) > i && v(s[i]) == v(e) {
		s[i] = e
		return s, true
	}
	s = append(s, nil)
	copy(s[i+1:], s[i:])
	s[i] = e
	return s, false
}
