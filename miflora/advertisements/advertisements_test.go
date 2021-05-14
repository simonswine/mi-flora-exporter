package advertisements

import (
	"encoding/hex"
	"testing"

	"github.com/simonswine/mi-flora-exporter/miflora/model"
	"github.com/stretchr/testify/assert"
)

func verifyFloraNormal(t *testing.T, d *XiaomiData) {
	assert.Equal(t, true, d.isNewFactory())
	assert.Equal(t, false, d.isConnected())
	assert.Equal(t, false, d.isCentral())
	assert.Equal(t, false, d.isEncrypted())
	assert.Equal(t, true, d.hasMacAddress())
	assert.Equal(t, true, d.hasCapabilities())
	assert.Equal(t, true, d.hasMeasurement())
	assert.Equal(t, false, d.isCustomData())
	assert.Equal(t, false, d.isSubtitle())
	assert.Equal(t, false, d.isBindingFrame())
}

func newFromHex(s string) (*XiaomiData, error) {
	d, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return New(d)
}

func TestParse(t *testing.T) {

	for _, tc := range []struct {
		name string
		data string

		macAddress  []byte
		measurement func(*testing.T, *model.Measurement)
	}{
		{
			name:       "mi-flora temperature",
			data:       "71209800da795d658d7cc40d0410021201",
			macAddress: []byte{0xc4, 0x7c, 0x8d, 0x65, 0x5d, 0x79},
			measurement: func(t *testing.T, m *model.Measurement) {
				assert.Equal(t, 27.4, m.Temperature.Value())
			},
		},
		{
			name:       "mi-flora negative temperature",
			data:       "71209800da795d658d7cc40d041002e7ff",
			macAddress: []byte{0xc4, 0x7c, 0x8d, 0x65, 0x5d, 0x79},
			measurement: func(t *testing.T, m *model.Measurement) {
				assert.Equal(t, -2.5, m.Temperature.Value())
			},
		},
		{
			name:       "mi-flora conductivitiy",
			data:       "71209800d9795d658d7cc40d0910022e00",
			macAddress: []byte{0xc4, 0x7c, 0x8d, 0x65, 0x5d, 0x79},
			measurement: func(t *testing.T, m *model.Measurement) {
				assert.Equal(t, 0.0046, m.Conductivity.Value())
			},
		},
		{
			name:       "mi-flora moisture",
			data:       "71209800d8795d658d7cc40d0810010d",
			macAddress: []byte{0xc4, 0x7c, 0x8d, 0x65, 0x5d, 0x79},
			measurement: func(t *testing.T, m *model.Measurement) {
				assert.Equal(t, uint8(13), *m.Moisture)
			},
		},
		{
			name:       "mi-flora brightness",
			data:       "71209800ef795d658d7cc40d071003fe4c00",
			macAddress: []byte{0xc4, 0x7c, 0x8d, 0x65, 0x5d, 0x79},
			measurement: func(t *testing.T, m *model.Measurement) {
				assert.Equal(t, uint16(0x4cfe), *m.Brightness)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := newFromHex(tc.data)
			assert.NoError(t, err)
			verifyFloraNormal(t, d)
			assert.Equal(t, uint8(2), d.Version())
			assert.Equal(t, uint16(0x0098), d.ProductID())
			assert.Equal(t, tc.macAddress, d.MacAddress())
			assert.Equal(t, byte(0x0d), d.Capabilities())
			if tc.measurement != nil {
				tc.measurement(t, d.Values())
			} else {
				assert.Nil(t, d.Values())
			}
		})
	}
}
