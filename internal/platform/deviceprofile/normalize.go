package deviceprofile

import moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"

const (
	batteryFlagHigh     = 0x01
	batteryFlagLow      = 0x02
	batteryFlagCritical = 0x04
	batteryFlagCharging = 0x08
	batteryFlagNoSystem = 0x80
	batteryFlagUnknown  = 0xff

	pointerDeviceIntegratedPen = 0x01
	pointerDeviceExternalPen   = 0x02
	pointerDeviceTouch         = 0x03
	pointerDeviceTouchpad      = 0x04
)

// normalizePowerStatus maps SYSTEM_POWER_STATUS values without treating an
// unknown battery flag as proof that the device has no battery.
func normalizePowerStatus(acLineStatus, batteryFlag byte) (moduleprofile.TriState, moduleprofile.PowerSource) {
	battery := moduleprofile.Unknown
	switch {
	case batteryFlag == batteryFlagNoSystem:
		battery = moduleprofile.No
	case batteryFlag == batteryFlagUnknown:
		battery = moduleprofile.Unknown
	case batteryFlag&(batteryFlagHigh|batteryFlagLow|batteryFlagCritical|batteryFlagCharging) != 0:
		battery = moduleprofile.Yes
	}

	power := moduleprofile.PowerSourceUnknown
	switch acLineStatus {
	case 0:
		power = moduleprofile.PowerSourceBattery
	case 1:
		power = moduleprofile.PowerSourceAC
	}
	return battery, power
}

// normalizePointerDeviceTypes maps POINTER_DEVICE_TYPE values. A successful
// empty enumeration is represented as No; callers must preserve Unknown when
// the native API itself failed.
func normalizePointerDeviceTypes(deviceTypes []uint32) (touch, pen moduleprofile.TriState) {
	touch = moduleprofile.No
	pen = moduleprofile.No
	for _, deviceType := range deviceTypes {
		switch deviceType {
		case pointerDeviceTouch:
			touch = moduleprofile.Yes
		case pointerDeviceIntegratedPen, pointerDeviceExternalPen:
			pen = moduleprofile.Yes
		}
	}
	return touch, pen
}

func normalizeConvertibleSlateMode(metric int32) moduleprofile.SlateMode {
	if metric == 0 {
		return moduleprofile.SlateModeSlate
	}
	return moduleprofile.SlateModeLaptop
}

func isConvertibleChassis(chassisTypes []uint16) bool {
	for _, chassisType := range chassisTypes {
		if chassisType == 31 || chassisType == 32 {
			return true
		}
	}
	return false
}
