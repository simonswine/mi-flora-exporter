package miflora

import (
	"testing"

	"github.com/go-ble/ble"
)

type fakeAddr string

func (f *fakeAddr) String() string {
	return string(*f)
}

type fakeAdvertisement struct {
	addr *fakeAddr
}

func (f *fakeAdvertisement) LocalName() string {
	panic("not implemented") // TODO: Implement
}

func (f *fakeAdvertisement) ManufacturerData() []byte {
	panic("not implemented") // TODO: Implement
}

func (f *fakeAdvertisement) ServiceData() []ble.ServiceData {
	panic("not implemented") // TODO: Implement
}

func (f *fakeAdvertisement) Services() []ble.UUID {
	panic("not implemented") // TODO: Implement
}

func (f *fakeAdvertisement) OverflowService() []ble.UUID {
	panic("not implemented") // TODO: Implement
}

func (f *fakeAdvertisement) TxPowerLevel() int {
	panic("not implemented") // TODO: Implement
}

func (f *fakeAdvertisement) Connectable() bool {
	panic("not implemented") // TODO: Implement
}

func (f *fakeAdvertisement) SolicitedService() []ble.UUID {
	panic("not implemented") // TODO: Implement
}

func (f *fakeAdvertisement) RSSI() int {
	return 66
}

func (f *fakeAdvertisement) Addr() ble.Addr {
	return f.addr
}

func newFakeAdvertisement(s string) *fakeAdvertisement {
	addr := fakeAddr(s)
	return &fakeAdvertisement{
		addr: &addr,
	}

}

func TestSensorSlice_InsertSorted(t *testing.T) {

	var s SensorSlice
	var existed bool

	// add initial element
	s, existed = s.insertSorted(&Sensor{advertisement: newFakeAdvertisement("b")})
	if exp, act := 1, len(s); exp != act {
		t.Errorf("unexpected length of slice: %v, act: %v", exp, act)
	}
	if exp, act := false, existed; exp != act {
		t.Errorf("unexpected existed exp: %v, act: %v", exp, act)
	}

	// add duplicate
	s, existed = s.insertSorted(&Sensor{advertisement: newFakeAdvertisement("b"), name: "replace"})
	if exp, act := 1, len(s); exp != act {
		t.Errorf("unexpected length of slice: %d, act: %d", exp, act)
	}
	if exp, act := true, existed; exp != act {
		t.Errorf("unexpected existed exp: %v, act: %v", exp, act)
	}
	if exp, act := "replace", s[0].name; exp != act {
		t.Errorf("unexpected existed exp: %v, act: %v", exp, act)
	}

	// add before
	s, existed = s.insertSorted(&Sensor{advertisement: newFakeAdvertisement("a")})
	if exp, act := 2, len(s); exp != act {
		t.Errorf("unexpected length of slice: %v, act: %v", exp, act)
	}
	if exp, act := false, existed; exp != act {
		t.Errorf("unexpected existed exp: %v, act: %v", exp, act)
	}

	// add after
	s, existed = s.insertSorted(&Sensor{advertisement: newFakeAdvertisement("c")})
	if exp, act := 3, len(s); exp != act {
		t.Errorf("unexpected length of slice: %v, act: %v", exp, act)
	}
	if exp, act := false, existed; exp != act {
		t.Errorf("unexpected existed exp: %v, act: %v", exp, act)
	}

	if exp, act := "a", s[0].advertisement.Addr().String(); exp != act {
		t.Errorf("unexpected element 0: %v, act: %v", exp, act)
	}
	if exp, act := "b", s[1].advertisement.Addr().String(); exp != act {
		t.Errorf("unexpected element 1: %v, act: %v", exp, act)
	}
	if exp, act := "c", s[2].advertisement.Addr().String(); exp != act {
		t.Errorf("unexpected element 2: %v, act: %v", exp, act)
	}

}
