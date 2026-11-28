//go:build darwin

package deviceprofile

import (
	"strings"
	"syscall"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
)

// Collect reads the hardware model identifier from the public hw.model sysctl.
// It does not invoke system_profiler or inspect private IOKit databases.
func Collect() Result {
	return collectDarwin(syscall.Sysctl)
}

func collectDarwin(readSysctl func(string) (string, error)) Result {
	result := newUnknownResult()
	model, err := readSysctl("hw.model")
	if err != nil {
		result.addSourceError("macos.sysctl.hw.model", err)
		return result
	}
	formFactor, _ := classifyDarwinHardwareModel(model)
	if formFactor == moduleprofile.FormFactorUnknown {
		return result
	}
	result.Observations.DeviceForm = formFactor
	result.Observations.EvidenceSources.DeviceForm = "macos.sysctl.hw.model"
	return result
}

func classifyDarwinHardwareModel(model string) (moduleprofile.FormFactor, string) {
	value := strings.TrimSpace(model)
	switch {
	case strings.HasPrefix(value, "MacBookPro"),
		strings.HasPrefix(value, "MacBookAir"),
		strings.HasPrefix(value, "MacBook"):
		return moduleprofile.FormFactorNotebook, "MacBook"
	case strings.HasPrefix(value, "iMac"):
		return moduleprofile.FormFactorAllInOne, "iMac"
	case strings.HasPrefix(value, "Macmini"):
		return moduleprofile.FormFactorDesktop, "Mac mini"
	case strings.HasPrefix(value, "MacPro"),
		strings.HasPrefix(value, "MacStudio"):
		return moduleprofile.FormFactorWorkstation, "Mac workstation"
	default:
		return moduleprofile.FormFactorUnknown, ""
	}
}
