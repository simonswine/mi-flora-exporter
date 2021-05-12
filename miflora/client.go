package miflora

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/go-ble/ble"
	"github.com/simonswine/mi-flora-remote-write/miflora/model"
)

type client struct {
	client  ble.Client
	profile *ble.Profile
}

func (c *client) findCharacteristicByValueHandle(handle uint16) *ble.Characteristic {
	for _, s := range c.profile.Services {
		for _, c := range s.Characteristics {
			if c.ValueHandle == handle {
				return c
			}
		}
	}
	return nil
}

func (c *client) read(handle uint16) ([]byte, error) {
	char := c.findCharacteristicByValueHandle(handle)
	if char == nil {
		return nil, fmt.Errorf("error couldn't find characteristic with ValueHandle 0x%x", handle)
	}

	return c.client.ReadCharacteristic(char)
}

func (c *client) write(handle uint16, data []byte) error {
	char := c.findCharacteristicByValueHandle(handle)
	if char == nil {
		return fmt.Errorf("error couldn't find characteristic with ValueHandle 0x%x", handle)
	}

	return c.client.WriteCharacteristic(char, data, false)
}

func (c *client) DeviceTimeDiff() (time.Duration, error) {
	start := time.Now().UTC()
	data, err := c.read(handleDeviceTime)
	if err != nil {
		return 0, err
	}
	duration := time.Now().UTC().Sub(start)

	var t int32
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &t); err != nil {
		return 0, fmt.Errorf("error reading timestamp: %w", err)
	}

	return start.Add(duration / 2).Sub(time.Unix(int64(t), 0)), nil
}

func (c *client) Firmware() (*model.Firmware, error) {
	data, err := c.read(handleFirmwareBattery)
	if err != nil {
		return nil, err
	}

	firmware := &model.Firmware{}
	if err := firmware.UnmarshalBinary(data); err != nil {
		return nil, err
	}

	return firmware, nil
}

func (c *client) Measurement() (*model.Measurement, error) {
	if err := c.write(handleModeChange, modeRealtimeReadInit); err != nil {
		return nil, err
	}

	data, err := c.read(handleDataRead)
	if err != nil {
		return nil, err
	}

	measurement := &model.Measurement{}
	if err := measurement.UnmarshalBinary(bytes.NewReader(data)); err != nil {
		return nil, err
	}

	return measurement, nil
}

func (c *client) HistoryLength() (uint16, error) {
	if err := c.write(handleHistoryControl, modeHistoryReadInit); err != nil {
		return 0, err
	}

	data, err := c.read(handleHistoryRead)
	if err != nil {
		return 0, err
	}

	var historyLength uint16
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &historyLength); err != nil {
		return 0, fmt.Errorf("error can't read history length: %w", err)
	}

	return historyLength, nil
}

func historyAddress(pos uint16) []byte {
	b := make([]byte, 3)
	b[0] = 0xa1
	binary.LittleEndian.PutUint16(b[1:], pos)
	return b
}
func (c *client) HistoryMeasurement(pos uint16) (*HistoricMeasurement, error) {
	if err := c.write(handleHistoryControl, historyAddress(pos)); err != nil {
		return nil, err
	}

	data, err := c.read(handleHistoryRead)
	if err != nil {
		return nil, err
	}

	measurement := &HistoricMeasurement{}
	if err := measurement.UnmarshalBinary(bytes.NewReader(data)); err != nil {
		return nil, err
	}

	return measurement, nil
}
