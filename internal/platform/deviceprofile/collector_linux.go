//go:build linux

package deviceprofile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
)

const (
	linuxDMIChassisTypePath = "/sys/class/dmi/id/chassis_type"
	linuxPowerSupplyPath    = "/sys/class/power_supply"
)

type linuxReadFile func(string) ([]byte, error)
type linuxReadSupplyNames func() ([]string, error)

// Collect reads public DMI and power-supply sysfs attributes. It does not
// invoke shell commands, read input-device event streams or modify the system.
func Collect() Result {
	return collectLinux(os.ReadFile, readLinuxPowerSupplyNames)
}

func collectLinux(readFile linuxReadFile, readSupplyNames linuxReadSupplyNames) Result {
	result := newUnknownResult()
	collectLinuxChassis(&result, readFile)
	collectLinuxPower(&result, readFile, readSupplyNames)
	return result
}

func collectLinuxChassis(result *Result, readFile linuxReadFile) {
	raw, err := readFile(linuxDMIChassisTypePath)
	if err != nil {
		result.addSourceError("linux.sysfs.dmi.chassis_type", fmt.Errorf("no se pudo leer el tipo de chasis DMI: %w", err))
		return
	}
	code, err := parseLinuxChassisType(raw)
	if err != nil {
		result.addSourceError("linux.sysfs.dmi.chassis_type", err)
		return
	}
	if code == 0 {
		return
	}
	result.Observations.SMBIOSChassisTypes = []uint16{code}
	result.Observations.EvidenceSources.SMBIOSChassisTypes = "linux.sysfs.dmi.chassis_type"
}

func parseLinuxChassisType(raw []byte) (uint16, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return 0, errors.New("chassis_type DMI está vacío")
	}
	parsed, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("chassis_type DMI no es un entero decimal válido: %w", err)
	}
	return uint16(parsed), nil
}

func readLinuxPowerSupplyNames() ([]string, error) {
	entries, err := os.ReadDir(linuxPowerSupplyPath)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

func collectLinuxPower(result *Result, readFile linuxReadFile, readSupplyNames linuxReadSupplyNames) {
	names, err := readSupplyNames()
	if err != nil {
		result.addSourceError("linux.sysfs.power_supply", fmt.Errorf("no se pudo enumerar las fuentes de alimentación: %w", err))
		return
	}

	batterySeen := false
	typeReadFailed := false
	mainsOnline := false
	batteryDischarging := false
	batteryCharging := false

	for _, name := range names {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			continue
		}
		base := filepath.Join(linuxPowerSupplyPath, name)
		typeRaw, readErr := readFile(filepath.Join(base, "type"))
		if readErr != nil {
			typeReadFailed = true
			result.addSourceError("linux.sysfs.power_supply", fmt.Errorf("no se pudo leer el tipo de %q: %w", name, readErr))
			continue
		}
		switch strings.TrimSpace(string(typeRaw)) {
		case "Battery":
			batterySeen = true
			statusRaw, statusErr := readFile(filepath.Join(base, "status"))
			if statusErr != nil {
				result.addSourceError("linux.sysfs.power_supply", fmt.Errorf("no se pudo leer el estado de batería de %q: %w", name, statusErr))
				continue
			}
			switch strings.TrimSpace(string(statusRaw)) {
			case "Discharging":
				batteryDischarging = true
			case "Charging", "Full":
				batteryCharging = true
			}
		case "Mains":
			onlineRaw, onlineErr := readFile(filepath.Join(base, "online"))
			if onlineErr != nil {
				result.addSourceError("linux.sysfs.power_supply", fmt.Errorf("no se pudo leer la conexión de red eléctrica de %q: %w", name, onlineErr))
				continue
			}
			if strings.TrimSpace(string(onlineRaw)) == "1" {
				mainsOnline = true
			}
		}
	}

	if !typeReadFailed {
		if batterySeen {
			result.Observations.Capabilities.BatteryPresent = moduleprofile.Yes
		} else {
			result.Observations.Capabilities.BatteryPresent = moduleprofile.No
		}
	}

	switch {
	case mainsOnline, batteryCharging:
		result.Observations.Capabilities.PowerSource = moduleprofile.PowerSourceAC
	case batteryDischarging:
		result.Observations.Capabilities.PowerSource = moduleprofile.PowerSourceBattery
	}
}
