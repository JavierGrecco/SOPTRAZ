//go:build windows

package deviceprofile

import (
	"testing"
	"unsafe"
)

func TestWindowsNativeStructureLayouts(t *testing.T) {
	pointerSize := unsafe.Sizeof(uintptr(0))
	if pointerSize == 8 {
		assertPointerDeviceInfoLayout(t, 1080, 8, 16, 24, 32, 36, 38)
	} else if pointerSize == 4 {
		assertPointerDeviceInfoLayout(t, 1064, 4, 8, 12, 16, 20, 22)
	} else {
		t.Fatalf("unsupported pointer size %d", pointerSize)
	}

	var power systemPowerStatus
	if got := unsafe.Sizeof(power); got != 12 {
		t.Fatalf("SYSTEM_POWER_STATUS size = %d, want 12", got)
	}
	if got := unsafe.Offsetof(power.BatteryLifeTime); got != 4 {
		t.Fatalf("SYSTEM_POWER_STATUS.BatteryLifeTime offset = %d, want 4", got)
	}
	if got := unsafe.Offsetof(power.BatteryFullLifeTime); got != 8 {
		t.Fatalf("SYSTEM_POWER_STATUS.BatteryFullLifeTime offset = %d, want 8", got)
	}
}

func assertPointerDeviceInfoLayout(t *testing.T, size, device, deviceType, monitor, cursor, contacts, product uintptr) {
	t.Helper()

	var info pointerDeviceInfo
	if got := unsafe.Sizeof(info); got != size {
		t.Fatalf("POINTER_DEVICE_INFO size = %d, want %d", got, size)
	}
	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{name: "device", got: unsafe.Offsetof(info.Device), want: device},
		{name: "pointerDeviceType", got: unsafe.Offsetof(info.PointerDeviceType), want: deviceType},
		{name: "monitor", got: unsafe.Offsetof(info.Monitor), want: monitor},
		{name: "startingCursorId", got: unsafe.Offsetof(info.StartingCursorID), want: cursor},
		{name: "maxActiveContacts", got: unsafe.Offsetof(info.MaxActiveContacts), want: contacts},
		{name: "productString", got: unsafe.Offsetof(info.ProductString), want: product},
	}
	for _, offset := range offsets {
		if offset.got != offset.want {
			t.Errorf("POINTER_DEVICE_INFO.%s offset = %d, want %d", offset.name, offset.got, offset.want)
		}
	}
}
