package miflora

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	bluetooth "github.com/muka/go-bluetooth/api"
	"github.com/muka/go-bluetooth/bluez/profile/adapter"
	"github.com/muka/go-bluetooth/bluez/profile/device"
)

const (
	uuidFirmwareBattery = "00001a02-0000-1000-8000-00805f9b34fb" // handle 0x38
	uuidDataRead        = "00001a01-0000-1000-8000-00805f9b34fb" // handle 0x35
	uuidModeChange      = "00001a00-0000-1000-8000-00805f9b34fb" // handle 0x33
	uuidDeviceTime      = "00001a12-0000-1000-8000-00805f9b34fb" // handle 0x41
	uuidHistoryControl  = "00001a10-0000-1000-8000-00805f9b34fb" // handle 0x3e
	uuidHistoryRead     = "00001a11-0000-1000-8000-00805f9b34fb" // handle 0x3c
)

/*
handle = 0x0002, char properties = 0x02, char value handle = 0x0003, uuid = 00002a00-0000-1000-8000-00805f9b34fb
handle = 0x0004, char properties = 0x02, char value handle = 0x0005, uuid = 00002a01-0000-1000-8000-00805f9b34fb
handle = 0x0006, char properties = 0x0a, char value handle = 0x0007, uuid = 00002a02-0000-1000-8000-00805f9b34fb
handle = 0x0008, char properties = 0x02, char value handle = 0x0009, uuid = 00002a04-0000-1000-8000-00805f9b34fb
handle = 0x000d, char properties = 0x22, char value handle = 0x000e, uuid = 00002a05-0000-1000-8000-00805f9b34fb
handle = 0x0011, char properties = 0x18, char value handle = 0x0012, uuid = 00000001-0000-1000-8000-00805f9b34fb
handle = 0x0014, char properties = 0x02, char value handle = 0x0015, uuid = 00000002-0000-1000-8000-00805f9b34fb
handle = 0x0016, char properties = 0x12, char value handle = 0x0017, uuid = 00000004-0000-1000-8000-00805f9b34fb
handle = 0x0018, char properties = 0x08, char value handle = 0x0019, uuid = 00000007-0000-1000-8000-00805f9b34fb
handle = 0x001a, char properties = 0x08, char value handle = 0x001b, uuid = 00000010-0000-1000-8000-00805f9b34fb
handle = 0x001c, char properties = 0x0a, char value handle = 0x001d, uuid = 00000013-0000-1000-8000-00805f9b34fb
handle = 0x001e, char properties = 0x02, char value handle = 0x001f, uuid = 00000014-0000-1000-8000-00805f9b34fb
handle = 0x0020, char properties = 0x10, char value handle = 0x0021, uuid = 00001001-0000-1000-8000-00805f9b34fb
handle = 0x0024, char properties = 0x0a, char value handle = 0x0025, uuid = 8082caa8-41a6-4021-91c6-56f9b954cc34
handle = 0x0026, char properties = 0x0a, char value handle = 0x0027, uuid = 724249f0-5ec3-4b5f-8804-42345af08651
handle = 0x0028, char properties = 0x02, char value handle = 0x0029, uuid = 6c53db25-47a1-45fe-a022-7c92fb334fd4
handle = 0x002a, char properties = 0x0a, char value handle = 0x002b, uuid = 9d84b9a3-000c-49d8-9183-855b673fda31
handle = 0x002c, char properties = 0x0e, char value handle = 0x002d, uuid = 457871e8-d516-4ca1-9116-57d0b17b9cb2
handle = 0x002e, char properties = 0x12, char value handle = 0x002f, uuid = 5f78df94-798c-46f5-990a-b3eb6a065c88
handle = 0x0032, char properties = 0x0a, char value handle = 0x0033, uuid = 00001a00-0000-1000-8000-00805f9b34fb
handle = 0x0034, char properties = 0x1a, char value handle = 0x0035, uuid = 00001a01-0000-1000-8000-00805f9b34fb
handle = 0x0037, char properties = 0x02, char value handle = 0x0038, uuid = 00001a02-0000-1000-8000-00805f9b34fb
handle = 0x003b, char properties = 0x02, char value handle = 0x003c, uuid = 00001a11-0000-1000-8000-00805f9b34fb
handle = 0x003d, char properties = 0x1a, char value handle = 0x003e, uuid = 00001a10-0000-1000-8000-00805f9b34fb
handle = 0x0040, char properties = 0x02, char value handle = 0x0041, uuid = 00001a12-0000-1000-8000-00805f9b34fb
*/

var (
	modeBlinkLED           = []byte{0xfd, 0xff}
	modeRealtimeReadInit   = []byte{0xa0, 0x1f}
	modeHistoryReadInit    = []byte{0xa0, 0x00, 0x00}
	modeHistoryReadSuccess = []byte{0xa2, 0x00, 0x00}
	modeHistoryReadFailed  = []byte{0xa3, 0x00, 0x00}
)

type MiFlora struct {
	logger log.Logger
	wg     sync.WaitGroup
	stopCh chan struct{}
}

type Sensor struct {
	logger  log.Logger
	device  *device.Device1
	name    string
	address string
}

type Measurement struct {
	Temperature  float64
	Moisture     byte
	Light        uint16
	Conductivity uint16
}

func (s *Measurement) UnmarshalBinary(data []byte) error {
	// TT TT ?? LL LL ?? ?? MM CC CC ?? ?? ?? ?? ?? ??
	if len(data) != 16 {
		return fmt.Errorf("invalid data length: %d != 10", len(data))
	}

	p := bytes.NewBuffer(data)
	var t int16

	if err := binary.Read(p, binary.LittleEndian, &t); err != nil {
		return fmt.Errorf("error reading data: %s", err)
	}

	p.Next(1)
	if err := binary.Read(p, binary.LittleEndian, &s.Light); err != nil {
		return fmt.Errorf("error reading data: %s", err)
	}

	p.Next(2)
	if err := binary.Read(p, binary.LittleEndian, &s.Moisture); err != nil {
		return fmt.Errorf("error reading data: %s", err)
	}
	if err := binary.Read(p, binary.LittleEndian, &s.Conductivity); err != nil {
		return fmt.Errorf("error reading data: %s", err)
	}

	s.Temperature = float64(t) / 10
	return nil
}

type Firmware struct {
	Version string
	Battery uint8
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler.
func (f *Firmware) UnmarshalBinary(data []byte) error {
	if len(data) < 3 {
		return fmt.Errorf("data not long enough: %d < 3", len(data))
	}

	f.Battery = data[0]
	f.Version = string(data[2:])
	return nil
}

func (s *Sensor) Start(stopCh chan struct{}) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

query:
	for {
		select {
		case <-stopCh:
			break query
		case <-ticker.C:
			if err := s.device.Connect(); err != nil {
				s.logger.Log("msg", "connect failed", "err", err)
				continue
			}

			_, err := s.History()
			if err != nil {
				s.logger.Log("msg", "getting history failed", "err", err)
				continue
			}

			/*

				f, err := s.Firmware()
				if err != nil {
					s.logger.Log("msg", "getting firmware failed", "err", err)
					continue
				}
				s.logger.Log(
					"msg", "firmware detected",
					"version", f.Version,
					"battery", f.Battery,
				)

				m, err := s.Measurement()
				if err != nil {
					s.logger.Log("msg", "getting measurement failed", "err", err)
					continue
				}
				s.logger.Log(
					"msg", "measurement successful",
					"temperature", m.Temperature,
					"light", m.Light,
					"moisture", m.Moisture,
					"conductivity", m.Conductivity,
				)
			*/

			if err := s.device.Disconnect(); err != nil {
				s.logger.Log("msg", "disconnect failed", "err", err)
				continue
			}

		}
	}

	return nil
}

func (s *Sensor) Firmware() (*Firmware, error) {
	charFirmware, err := s.device.GetCharByUUID(uuidFirmwareBattery)
	if err != nil {
		return nil, err
	}

	dataFirmware, err := charFirmware.ReadValue(map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	firmware := &Firmware{}
	if err := firmware.UnmarshalBinary(dataFirmware); err != nil {
		return nil, err
	}

	return firmware, nil
}

func (s *Sensor) History() (*string, error) {
	charMode, err := s.device.GetCharByUUID(uuidHistoryControl)
	if err != nil {
		return nil, err
	}

	if err := charMode.WriteValue(modeHistoryReadInit, map[string]interface{}{}); err != nil {
		return nil, err
	}

	charData, err := s.device.GetCharByUUID(uuidHistoryRead)
	if err != nil {
		return nil, err
	}

	dataHistory, err := charData.ReadValue(map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var historyLength int16
	if err := binary.Read(bytes.NewReader(dataHistory), binary.LittleEndian, &historyLength); err != nil {
		return nil, fmt.Errorf("error reading history length: %s", err)
	}
	level.Info(s.logger).Log("msg", "read history header", "length", historyLength, "header", fmt.Sprintf("%x", dataHistory[2:]))

	return nil, nil //fmt.Errorf("not implemented: %s", "history")
}

func (s *Sensor) Measurement() (*Measurement, error) {
	charMode, err := s.device.GetCharByUUID(uuidModeChange)
	if err != nil {
		return nil, err
	}

	if err := charMode.WriteValue(modeRealtimeReadInit, map[string]interface{}{}); err != nil {
		return nil, err
	}

	charData, err := s.device.GetCharByUUID(uuidDataRead)
	if err != nil {
		return nil, err
	}

	dataMeasurement, err := charData.ReadValue(map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	measurement := &Measurement{}
	if err := measurement.UnmarshalBinary(dataMeasurement); err != nil {
		return nil, err
	}

	return measurement, nil
}

func (m *MiFlora) newSensor(device *device.Device1) (*Sensor, error) {
	name, err := device.GetName()
	if err != nil {
		return nil, err
	}
	address, err := device.GetAddress()
	if err != nil {
		return nil, err
	}

	return &Sensor{
		logger:  log.With(m.logger, "address", address, "name", name),
		name:    name,
		address: address,
		device:  device,
	}, nil
}

func (s *Sensor) isDevice() bool {
	if s.name == deviceName {
		return true
	}

	if strings.HasPrefix(strings.ToUpper(s.address), addressPrefix) {
		return true
	}

	return false
}

func New() *MiFlora {
	return &MiFlora{
		logger: log.NewNopLogger(),
		stopCh: make(chan struct{}, 0),
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

func (m *MiFlora) Scan(timeout time.Duration) error {
	action := func(device *device.Device1) error {
		s, err := m.newSensor(device)
		if err != nil {
			return err
		}

		if !s.isDevice() {
			return nil
		}

		s.logger.Log("msg", "sensor found")
		return nil
	}

	return m.doScan(timeout, action)

}

func (m *MiFlora) Realtime() error {
	action := func(device *device.Device1) error {
		s, err := m.newSensor(device)
		if err != nil {
			return err
		}

		if !s.isDevice() {
			return nil
		}

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			err := s.Start(m.stopCh)
			if err != nil {
				s.logger.Log("msg", "query failed", "err", err)
			}
		}()
		return nil
	}

	return m.doScan(time.Hour*24*365*100, action)
}

func (m *MiFlora) doScan(timeout time.Duration, action func(*device.Device1) error) error {
	a, err := bluetooth.GetDefaultAdapter()
	if err != nil {
		return fmt.Errorf("failed to get default adapter: %w", err)
	}

	if err := a.StartDiscovery(); err != nil {
		return fmt.Errorf("failed to set discovering: %w", err)
	}

	// list already discovered devices
	devices, err := a.GetDevices()
	if err != nil {
		return fmt.Errorf("failed to set discovering: %w", err)
	}
	for _, device := range devices {
		action(device)
	}

	// wait for later discovered devices
	discoveredDevices, _, err := a.OnDeviceDiscovered()
discovery:
	for {
		select {
		case discoveredDevice := <-discoveredDevices:
			if discoveredDevice.Type != adapter.DeviceAdded {
				continue
			}

			device, err := device.NewDevice1(discoveredDevice.Path)
			if err != nil {
				return err
			}

			action(device)
		case <-time.After(timeout):
			break discovery
		}

	}

	return nil
}
