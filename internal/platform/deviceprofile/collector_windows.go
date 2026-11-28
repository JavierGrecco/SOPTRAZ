//go:build windows

package deviceprofile

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
)

const (
	smbiosProviderRSMB             = 0x42534d52 // 'RSMB' in little-endian DWORD form.
	maxPointerDeviceCount          = 1024
	pointerDeviceProductMax        = 520
	smConvertibleSlateMode         = 0x2003
	smSystemDocked                 = 0x2004
	pointerDeviceTypeIntegratedPen = 0x00000001
	pointerDeviceTypeExternalPen   = 0x00000002
	pointerDeviceTypeTouch         = 0x00000003
	pointerDeviceTypeTouchpad      = 0x00000004
)

// pointerDeviceInfo mirrors POINTER_DEVICE_INFO from winuser.h. uintptr fields
// retain the native HANDLE/HMONITOR width and Go supplies the required padding.
type pointerDeviceInfo struct {
	DisplayOrientation uint32
	Device             uintptr
	PointerDeviceType  uint32
	Monitor            uintptr
	StartingCursorID   uint32
	MaxActiveContacts  uint16
	ProductString      [pointerDeviceProductMax]uint16
}

// systemPowerStatus mirrors SYSTEM_POWER_STATUS from winbase.h.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// Collect reads Windows hardware and operating-state signals using native,
// read-only APIs. A failure in one source is recorded and does not discard
// useful observations from other sources.
func Collect() Result {
	result := newUnknownResult()

	rawSMBIOS, err := readRawSMBIOS()
	if err != nil {
		result.addSourceError("windows.smbios", err)
	} else {
		chassisTypes, parseErr := ParseSMBIOSChassisTypes(rawSMBIOS)
		if parseErr != nil {
			result.addSourceError("windows.smbios", parseErr)
		} else {
			result.Observations.SMBIOSChassisTypes = chassisTypes
		}
	}

	if err := collectPowerStatus(&result); err != nil {
		result.addSourceError("windows.power_status", err)
	}
	if err := collectPointerCapabilities(&result); err != nil {
		result.addSourceError("windows.pointer_devices", err)
	}

	chassisTypes := result.Observations.SMBIOSChassisTypes
	if isConvertibleChassis(chassisTypes) {
		slateMode := getSystemMetric(smConvertibleSlateMode)
		result.Observations.Capabilities.ConvertibleSlate = normalizeConvertibleSlateMode(slateMode)
		if slateMode == 0 {
			result.Observations.Capabilities.KeyboardAttached = moduleprofile.No
		} else {
			result.Observations.Capabilities.KeyboardAttached = moduleprofile.Yes
		}
	}

	if hasPortableChassis(chassisTypes) {
		if getSystemMetric(smSystemDocked) == 0 {
			result.Observations.Capabilities.DockPresent = moduleprofile.No
		} else {
			result.Observations.Capabilities.DockPresent = moduleprofile.Yes
		}
	}
	return result
}

func readRawSMBIOS() ([]byte, error) {
	dll := syscall.NewLazyDLL("kernel32.dll")
	proc := dll.NewProc("GetSystemFirmwareTable")
	if err := proc.Find(); err != nil {
		return nil, fmt.Errorf("GetSystemFirmwareTable no está disponible: %w", err)
	}

	size, _, callErr := proc.Call(smbiosProviderRSMB, 0, 0, 0)
	if size == 0 {
		return nil, windowsCallError("GetSystemFirmwareTable(size)", callErr)
	}
	if size > maxRawSMBIOSBytes {
		return nil, fmt.Errorf("GetSystemFirmwareTable devolvió %d bytes; supera el límite de %d", size, maxRawSMBIOSBytes)
	}

	buffer := make([]byte, int(size))
	written, _, callErr := proc.Call(
		smbiosProviderRSMB,
		0,
		uintptr(unsafe.Pointer(&buffer[0])),
		size,
	)
	runtime.KeepAlive(buffer)
	if written == 0 {
		return nil, windowsCallError("GetSystemFirmwareTable(data)", callErr)
	}
	if written != size {
		return nil, fmt.Errorf("GetSystemFirmwareTable escribió %d bytes; se esperaban %d", written, size)
	}
	return buffer, nil
}

func collectPowerStatus(result *Result) error {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemPowerStatus")
	if err := proc.Find(); err != nil {
		return fmt.Errorf("GetSystemPowerStatus no está disponible: %w", err)
	}
	var status systemPowerStatus
	ok, _, callErr := proc.Call(uintptr(unsafe.Pointer(&status)))
	runtime.KeepAlive(&status)
	if ok == 0 {
		return windowsCallError("GetSystemPowerStatus", callErr)
	}

	batteryPresent, powerSource := normalizePowerStatus(status.ACLineStatus, status.BatteryFlag)
	result.Observations.Capabilities.BatteryPresent = batteryPresent
	result.Observations.Capabilities.PowerSource = powerSource
	return nil
}

func collectPointerCapabilities(result *Result) error {
	proc := syscall.NewLazyDLL("user32.dll").NewProc("GetPointerDevices")
	if err := proc.Find(); err != nil {
		return fmt.Errorf("GetPointerDevices no está disponible: %w", err)
	}

	var count uint32
	ok, _, callErr := proc.Call(uintptr(unsafe.Pointer(&count)), 0)
	runtime.KeepAlive(&count)
	if ok == 0 {
		return windowsCallError("GetPointerDevices(count)", callErr)
	}
	if count > maxPointerDeviceCount {
		return fmt.Errorf("GetPointerDevices devolvió %d dispositivos; supera el límite %d", count, maxPointerDeviceCount)
	}
	if count == 0 {
		result.Observations.Capabilities.Touch, result.Observations.Capabilities.Pen =
			normalizePointerDeviceTypes(nil)
		return nil
	}

	devices := make([]pointerDeviceInfo, int(count))
	capacity := count
	ok, _, callErr = proc.Call(uintptr(unsafe.Pointer(&capacity)), uintptr(unsafe.Pointer(&devices[0])))
	runtime.KeepAlive(devices)
	runtime.KeepAlive(&capacity)
	if ok == 0 {
		return windowsCallError("GetPointerDevices(data)", callErr)
	}
	if capacity > count {
		return fmt.Errorf("GetPointerDevices informó %d entradas para un búfer de %d", capacity, count)
	}

	deviceTypes := make([]uint32, 0, capacity)
	for _, device := range devices[:capacity] {
		deviceTypes = append(deviceTypes, device.PointerDeviceType)
	}
	touch, pen := normalizePointerDeviceTypes(deviceTypes)
	result.Observations.Capabilities.Touch = touch
	result.Observations.Capabilities.Pen = pen
	return nil
}

func getSystemMetric(metric int32) int32 {
	proc := syscall.NewLazyDLL("user32.dll").NewProc("GetSystemMetrics")
	result, _, _ := proc.Call(uintptr(uint32(metric)))
	return int32(result)
}

func hasPortableChassis(chassisTypes []uint16) bool {
	for _, chassisType := range chassisTypes {
		switch chassisType {
		case 8, 9, 10, 11, 14, 30, 31, 32:
			return true
		}
	}
	return false
}

func windowsCallError(api string, callErr error) error {
	if callErr == nil || callErr == syscall.Errno(0) {
		return fmt.Errorf("%s devolvió FALSE/0 sin código de error", api)
	}
	return fmt.Errorf("%s: %w", api, callErr)
}
