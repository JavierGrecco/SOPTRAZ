package deviceprofile

import (
	"reflect"
	"testing"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
)

func TestNormalizePowerStatus(t *testing.T) {
	tests := []struct {
		name        string
		ac          byte
		batteryFlag byte
		wantBattery moduleprofile.TriState
		wantPower   moduleprofile.PowerSource
	}{
		{name: "AC and battery present", ac: 1, batteryFlag: batteryFlagHigh, wantBattery: moduleprofile.Yes, wantPower: moduleprofile.PowerSourceAC},
		{name: "running on battery", ac: 0, batteryFlag: batteryFlagCharging, wantBattery: moduleprofile.Yes, wantPower: moduleprofile.PowerSourceBattery},
		{name: "no system battery", ac: 1, batteryFlag: batteryFlagNoSystem, wantBattery: moduleprofile.No, wantPower: moduleprofile.PowerSourceAC},
		{name: "unknown battery flag", ac: 255, batteryFlag: batteryFlagUnknown, wantBattery: moduleprofile.Unknown, wantPower: moduleprofile.PowerSourceUnknown},
		{name: "zero battery flag remains unknown", ac: 1, batteryFlag: 0, wantBattery: moduleprofile.Unknown, wantPower: moduleprofile.PowerSourceAC},
		{name: "unknown AC status", ac: 255, batteryFlag: batteryFlagLow, wantBattery: moduleprofile.Yes, wantPower: moduleprofile.PowerSourceUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotBattery, gotPower := normalizePowerStatus(test.ac, test.batteryFlag)
			if gotBattery != test.wantBattery || gotPower != test.wantPower {
				t.Fatalf("normalizePowerStatus(%d, %d) = %v/%s, want %v/%s",
					test.ac, test.batteryFlag, gotBattery, gotPower, test.wantBattery, test.wantPower)
			}
		})
	}
}

func TestNormalizePointerDeviceTypes(t *testing.T) {
	tests := []struct {
		name        string
		deviceTypes []uint32
		wantTouch   moduleprofile.TriState
		wantPen     moduleprofile.TriState
	}{
		{name: "empty successful enumeration", wantTouch: moduleprofile.No, wantPen: moduleprofile.No},
		{name: "touch device", deviceTypes: []uint32{pointerDeviceTouch}, wantTouch: moduleprofile.Yes, wantPen: moduleprofile.No},
		{name: "integrated pen", deviceTypes: []uint32{pointerDeviceIntegratedPen}, wantTouch: moduleprofile.No, wantPen: moduleprofile.Yes},
		{name: "external pen", deviceTypes: []uint32{pointerDeviceExternalPen}, wantTouch: moduleprofile.No, wantPen: moduleprofile.Yes},
		{name: "touchpad is not a touchscreen", deviceTypes: []uint32{pointerDeviceTouchpad}, wantTouch: moduleprofile.No, wantPen: moduleprofile.No},
		{name: "touch and pen", deviceTypes: []uint32{pointerDeviceTouch, pointerDeviceIntegratedPen}, wantTouch: moduleprofile.Yes, wantPen: moduleprofile.Yes},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			touch, pen := normalizePointerDeviceTypes(test.deviceTypes)
			if touch != test.wantTouch || pen != test.wantPen {
				t.Fatalf("normalizePointerDeviceTypes(%v) = %v/%v, want %v/%v",
					test.deviceTypes, touch, pen, test.wantTouch, test.wantPen)
			}
		})
	}
}

func TestNormalizeConvertibleSlateMode(t *testing.T) {
	if got := normalizeConvertibleSlateMode(0); got != moduleprofile.SlateModeSlate {
		t.Fatalf("slate mode = %q, want slate", got)
	}
	if got := normalizeConvertibleSlateMode(1); got != moduleprofile.SlateModeLaptop {
		t.Fatalf("laptop mode = %q, want laptop", got)
	}
}

func TestIsConvertibleChassis(t *testing.T) {
	tests := []struct {
		name   string
		values []uint16
		want   bool
	}{
		{name: "convertible", values: []uint16{31}, want: true},
		{name: "detachable", values: []uint16{32}, want: true},
		{name: "notebook", values: []uint16{10}, want: false},
		{name: "desktop plus convertible", values: []uint16{3, 31}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isConvertibleChassis(test.values); got != test.want {
				t.Fatalf("isConvertibleChassis(%v) = %t, want %t", test.values, got, test.want)
			}
		})
	}
}

func TestSMBIOSParserDoesNotModifyInput(t *testing.T) {
	table := appendSMBIOSStructure(nil, 3, []byte{1, 30})
	table = appendSMBIOSStructure(table, smbiosEndOfTableType, nil)
	raw := wrapRawSMBIOS(table)
	original := append([]byte(nil), raw...)

	if _, err := ParseSMBIOSChassisTypes(raw); err != nil {
		t.Fatalf("ParseSMBIOSChassisTypes() error = %v", err)
	}
	if !reflect.DeepEqual(raw, original) {
		t.Fatal("SMBIOS parser modified its input buffer")
	}
}
