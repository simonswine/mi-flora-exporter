package miflora

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-ble/ble"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"github.com/simonswine/mi-flora-remote-write/miflora/model"
)

const (
	handleFirmwareBattery = uint16(0x38)
	handleDataRead        = uint16(0x35)
	handleModeChange      = uint16(0x33)
	handleDeviceTime      = uint16(0x41)
	handleHistoryControl  = uint16(0x3e)
	handleHistoryRead     = uint16(0x3c)
)

var (
	modeBlinkLED           = []byte{0xfd, 0xff}
	modeRealtimeReadInit   = []byte{0xa0, 0x1f}
	modeHistoryReadInit    = []byte{0xa0, 0x00, 0x00}
	modeHistoryReadSuccess = []byte{0xa2, 0x00, 0x00}
	modeHistoryReadFailed  = []byte{0xa3, 0x00, 0x00}
)

type MiFlora struct {
	logger  log.Logger
	device  ble.Device
	stopCh  chan struct{}
	sensors map[string]*Sensor
}

type Sensor struct {
	logger        log.Logger
	device        ble.Device
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
	for _, nameOverride := range SensorsNamesFromContext(ctx) {
		parts := strings.SplitN(nameOverride, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(addr, parts[0]) {
			return true, parts[1]
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

func New(device ble.Device) *MiFlora {
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
	resultCh := ResultChannelFromContext(ctx)

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

func (m *MiFlora) Realtime(ctx context.Context) error {
	resultCh := ResultChannelFromContext(ctx)

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

func (m *MiFlora) doScan(ctx context.Context) ([]*Sensor, error) {
	sensorsCh := make(chan *Sensor)

	ctx, cancel := context.WithTimeout(ctx, ScanTimeoutFromContext(ctx))
	defer cancel()

	var sensors SensorSlice
	expectedSensors := ExpectedSensorsFromContext(ctx)

	declaredSensorNames := len(SensorsNamesFromContext(ctx))

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

	// scan for devices
	if err := m.device.Scan(ctx, false, handler); err != nil &&
		!errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, context.Canceled) {
		return nil, fmt.Errorf("failed to scan for sensors: %w", err)
	}
	close(sensorsCh)

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
