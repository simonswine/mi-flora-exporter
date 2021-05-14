package advertisements

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/simonswine/mi-flora-exporter/miflora/model"
)

type frameControlFlags uint16

const (
	flagNewFactory frameControlFlags = 1 << iota
	flagConnected
	flagCentral
	flagEncrypted
	flagMacAddress
	flagCapabilities
	flagMeasurment
	flagCustomData
	flagSubtitle
	flagBinding
)

type measurementIDs uint16

//nolint:deadcode,varcheck // keep the unimplemented measurements
const (
	measurementTemperature            measurementIDs = 0x1004 // temp = value / 10
	measurementBrightness             measurementIDs = 0x1007
	measurementMoisture               measurementIDs = 0x1008
	measurementFertility              measurementIDs = 0x1009
	measurementBattery                measurementIDs = 0x100a
	measurementTemperatureAndHumidity measurementIDs = 0x100d // 2 byte temperature / 10, 2 byte humidity / 10
)

func New(d []byte) (*XiaomiData, error) {

	if len(d) < 5 {
		return nil, errors.New("A miflora advertisement frame must be at least 5 bytes long")
	}

	return &XiaomiData{
		data: d,
	}, nil
}

type XiaomiData struct {
	data []byte
}

func (x *XiaomiData) flags() frameControlFlags {
	return frameControlFlags(binary.LittleEndian.Uint16(x.data[0:2]) & 0x0fff)
}

func (x *XiaomiData) isNewFactory() bool {
	return (x.flags() & flagNewFactory) != 0
}

func (x *XiaomiData) isConnected() bool {
	return (x.flags() & flagConnected) != 0
}

func (x *XiaomiData) isCentral() bool {
	return (x.flags() & flagCentral) != 0
}

func (x *XiaomiData) isEncrypted() bool {
	return (x.flags() & flagEncrypted) != 0
}

func (x *XiaomiData) hasMacAddress() bool {
	return (x.flags() & flagMacAddress) != 0
}

func (x *XiaomiData) hasCapabilities() bool {
	return (x.flags() & flagCapabilities) != 0
}

func (x *XiaomiData) hasMeasurement() bool {
	return (x.flags() & flagMeasurment) != 0
}

func (x *XiaomiData) isCustomData() bool {
	return (x.flags() & flagCustomData) != 0
}
func (x *XiaomiData) isSubtitle() bool {
	return (x.flags() & flagSubtitle) != 0
}
func (x *XiaomiData) isBindingFrame() bool {
	return (x.flags() & flagBinding) != 0
}

func (x *XiaomiData) Version() uint8 {
	return uint8(binary.LittleEndian.Uint16(x.data[0:2]) >> 12)
}

func (x *XiaomiData) ProductID() uint16 {
	return binary.LittleEndian.Uint16(x.data[2:4])
}

func (x *XiaomiData) FrameCounter() uint8 {
	return x.data[4]
}

func (x *XiaomiData) macAddressOffset() int {
	return 5
}

func (x *XiaomiData) MacAddress() []byte {
	if !x.hasMacAddress() {
		return nil
	}
	offset := x.macAddressOffset()
	mac := make([]byte, 6)
	for pos := range mac {
		mac[pos] = x.data[offset+(5-pos)]
	}
	return mac
}

func (x *XiaomiData) capabiltiesOffset() int {
	offset := x.macAddressOffset()
	if x.hasMacAddress() {
		offset += 6
	}
	return offset
}

func (x *XiaomiData) Capabilities() byte {
	return x.data[x.capabiltiesOffset()]
}

func (x *XiaomiData) valuesOffset() int {
	offset := x.capabiltiesOffset()
	if x.hasCapabilities() {
		offset += 1
	}
	return offset
}

func (x *XiaomiData) Values() *model.Measurement {
	if !x.hasMeasurement() {
		return nil
	}
	offset := x.valuesOffset()
	id := measurementIDs(binary.LittleEndian.Uint16(x.data[offset : offset+2]))
	offset += 2

	length := x.data[offset]

	offset += 1
	data := x.data[offset : offset+int(length)]

	var measurement model.Measurement

	switch id {
	case measurementTemperature:
		val := model.Temperature(int16(binary.LittleEndian.Uint16(data)))
		measurement.Temperature = &val
	case measurementBrightness:
		val := binary.LittleEndian.Uint16(data)
		measurement.Brightness = &val
	case measurementMoisture:
		val := uint8(data[0])
		measurement.Moisture = &val
	case measurementFertility:
		val := model.Conductivity(binary.LittleEndian.Uint16(data))
		measurement.Conductivity = &val
	default:
		panic(fmt.Sprintf("unknown value: % x", id))
	}

	return &measurement
}
