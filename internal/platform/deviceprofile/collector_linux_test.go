//go:build linux

package deviceprofile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
)

func TestCollectLinuxReadsDMIAndBatteryPower(t *testing.T) {
	files := map[string]string{
		linuxDMIChassisTypePath: "10\n",
		filepath.Join(linuxPowerSupplyPath, "BAT0", "type"): "Battery\n",
		filepath.Join(linuxPowerSupplyPath, "BAT0", "status"): "Discharging\n",
		filepath.Join(linuxPowerSupplyPath, "AC", "type"): "Mains\n",
		filepath.Join(linuxPowerSupplyPath, "AC", "online"): "0\n",
	}
	readFile := func(path string) ([]byte, error) {
		value, ok := files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(value), nil
	}
	readNames := func() ([]string, error) { return []string{"BAT0", "AC"}, nil }

	result := collectLinux(readFile, readNames)
	if len(result.SourceErrors) != 0 {
		t.Fatalf("unexpected source errors: %#v", result.SourceErrors)
	}
	if len(result.Observations.SMBIOSChassisTypes) != 1 || result.Observations.SMBIOSChassisTypes[0] != 10 {
		t.Fatalf("DMI chassis = %v, want [10]", result.Observations.SMBIOSChassisTypes)
	}
	if result.Observations.EvidenceSources.SMBIOSChassisTypes != "linux.sysfs.dmi.chassis_type" {
		t.Fatalf("DMI evidence source = %q", result.Observations.EvidenceSources.SMBIOSChassisTypes)
	}
	if result.Observations.Capabilities.BatteryPresent != moduleprofile.Yes {
		t.Fatalf("battery present = %v, want yes", result.Observations.Capabilities.BatteryPresent)
	}
	if result.Observations.Capabilities.PowerSource != moduleprofile.PowerSourceBattery {
		t.Fatalf("power source = %q, want battery", result.Observations.Capabilities.PowerSource)
	}
	profile := moduleprofile.Classify(result.Observations)
	if profile.FormFactor != moduleprofile.FormFactorNotebook {
		t.Fatalf("form factor = %q, want notebook", profile.FormFactor)
	}
}

func TestCollectLinuxPrefersOnlineMainsOverBatteryStatus(t *testing.T) {
	files := map[string]string{
		filepath.Join(linuxPowerSupplyPath, "BAT0", "type"): "Battery",
		filepath.Join(linuxPowerSupplyPath, "BAT0", "status"): "Discharging",
		filepath.Join(linuxPowerSupplyPath, "AC", "type"): "Mains",
		filepath.Join(linuxPowerSupplyPath, "AC", "online"): "1",
	}
	readFile := func(path string) ([]byte, error) {
		value, ok := files[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(value), nil
	}
	result := collectLinux(readFile, func() ([]string, error) { return []string{"BAT0", "AC"}, nil })
	if result.Observations.Capabilities.PowerSource != moduleprofile.PowerSourceAC {
		t.Fatalf("power source = %q, want AC", result.Observations.Capabilities.PowerSource)
	}
}

func TestCollectLinuxKeepsUnknownWhenPowerSupplyEnumerationFails(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == linuxDMIChassisTypePath {
			return []byte("9"), nil
		}
		return nil, os.ErrNotExist
	}
	result := collectLinux(readFile, func() ([]string, error) { return nil, errors.New("permission denied") })
	if result.Observations.Capabilities.BatteryPresent != moduleprofile.Unknown ||
		result.Observations.Capabilities.PowerSource != moduleprofile.PowerSourceUnknown {
		t.Fatalf("unavailable power source was treated as a negative: %#v", result.Observations.Capabilities)
	}
	if len(result.Observations.SMBIOSChassisTypes) != 1 || result.Observations.SMBIOSChassisTypes[0] != 9 {
		t.Fatalf("successful DMI evidence was discarded: %v", result.Observations.SMBIOSChassisTypes)
	}
	if len(result.SourceErrors) != 1 {
		t.Fatalf("source errors = %d, want 1", len(result.SourceErrors))
	}
}

func TestParseLinuxChassisType(t *testing.T) {
	for _, test := range []struct {
		name string
		in   string
		want uint16
		bad  bool
	}{
		{name: "notebook", in: "10\n", want: 10},
		{name: "tablet", in: "30", want: 30},
		{name: "unknown", in: "0", want: 0},
		{name: "empty", in: "", bad: true},
		{name: "negative", in: "-1", bad: true},
		{name: "overflow", in: "65536", bad: true},
		{name: "non decimal", in: "laptop", bad: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseLinuxChassisType([]byte(test.in))
			if (err != nil) != test.bad || (!test.bad && got != test.want) {
				t.Fatalf("parseLinuxChassisType(%q) = %d, %v", test.in, got, err)
			}
		})
	}
}
