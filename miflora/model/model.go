package model

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/kit/log"
)

type Result struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address,omitempty"`

	Timestamp   *time.Time   `json:"timestamp,omitempty"`
	Firmware    *Firmware    `json:"firmware,omitempty"`
	Measurement *Measurement `json:"measurement,omitempty"`
}

type Firmware struct {
	Version string `json:"version"`
	Battery uint8  `json:"battery"`
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

type Temperature int16

func (t Temperature) Value() float64 {
	return float64(t) * 0.1
}

func (t Temperature) String() string {
	return fmt.Sprintf("%.1f", t.Value())
}

func (t *Temperature) MarshalJSON() ([]byte, error) {
	return []byte(t.String()), nil
}

type Conductivity uint16

func (c Conductivity) Value() float64 {
	return float64(c) * 0.0001
}

func (c Conductivity) String() string {
	return fmt.Sprintf("%.4f", c.Value())
}

func (c Conductivity) MarshalJSON() ([]byte, error) {
	return []byte(c.String()), nil
}

type Measurement struct {
	Temperature  Temperature  `json:"temperature"`
	Moisture     uint8        `json:"moisture"`
	Brightness   uint16       `json:"brightness"`
	Conductivity Conductivity `json:"conductivity"`
}

func (m *Measurement) LogWith(l log.Logger) log.Logger {
	return log.With(
		l,
		"temperature", m.Temperature,
		"brightness", m.Brightness,
		"moisture", m.Moisture,
		"conductivity", m.Conductivity,
	)
}

func (m *Measurement) UnmarshalBinary(r io.Reader) error {
	// TT TT ?? LL LL ?? ?? MM CC CC ?? ?? ?? ?? ?? ??
	if err := binary.Read(r, binary.LittleEndian, &m.Temperature); err != nil {
		return fmt.Errorf("error reading data: %w", err)
	}

	// skip 1 byte
	if _, err := r.Read(make([]byte, 1)); err != nil {
		return fmt.Errorf("error skipping 1 byte: %w", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &m.Brightness); err != nil {
		return fmt.Errorf("error reading data: %w", err)
	}

	// skip 2 bytes
	if _, err := r.Read(make([]byte, 2)); err != nil {
		return fmt.Errorf("error skipping 2 byte: %w", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &m.Moisture); err != nil {
		return fmt.Errorf("error reading data: %w", err)
	}

	if err := binary.Read(r, binary.LittleEndian, &m.Conductivity); err != nil {
		return fmt.Errorf("error reading data: %w", err)
	}
	return nil
}
