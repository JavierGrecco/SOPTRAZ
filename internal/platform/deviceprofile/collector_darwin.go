//go:build darwin

package deviceprofile

import (
	"strings"
	"syscall"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
)

var darwinModelFormFactors = map[string]moduleprofile.FormFactor{
	"Mac14,2":  moduleprofile.FormFactorNotebook,
	"Mac14,15": moduleprofile.FormFactorNotebook,
	"Mac15,12": moduleprofile.FormFactorNotebook,
	"Mac15,13": moduleprofile.FormFactorNotebook,
	"Mac16,12": moduleprofile.FormFactorNotebook,
	"Mac16,13": moduleprofile.FormFactorNotebook,
	"Mac17,3":  moduleprofile.FormFactorNotebook,
	"Mac17,4":  moduleprofile.FormFactorNotebook,
	"Mac14,5":  moduleprofile.FormFactorNotebook,
	"Mac14,6":  moduleprofile.FormFactorNotebook,
	"Mac14,7":  moduleprofile.FormFactorNotebook,
	"Mac14,9":  moduleprofile.FormFactorNotebook,
	"Mac14,10": moduleprofile.FormFactorNotebook,
	"Mac15,3":  moduleprofile.FormFactorNotebook,
	"Mac16,1":  moduleprofile.FormFactorNotebook,
	"Mac16,5":  moduleprofile.FormFactorNotebook,
	"Mac16,6":  moduleprofile.FormFactorNotebook,
	"Mac16,7":  moduleprofile.FormFactorNotebook,
	"Mac16,8":  moduleprofile.FormFactorNotebook,
	"Mac17,2":  moduleprofile.FormFactorNotebook,
	"Mac17,6":  moduleprofile.FormFactorNotebook,
	"Mac17,7":  moduleprofile.FormFactorNotebook,
	"Mac17,8":  moduleprofile.FormFactorNotebook,
	"Mac17,9":  moduleprofile.FormFactorNotebook,
	"Mac15,4":  moduleprofile.FormFactorAllInOne,
	"Mac15,5":  moduleprofile.FormFactorAllInOne,
	"Mac16,2":  moduleprofile.FormFactorAllInOne,
	"Mac16,3":  moduleprofile.FormFactorAllInOne,
	"Mac18,2":  moduleprofile.FormFactorAllInOne,
	"Mac14,3":  moduleprofile.FormFactorDesktop,
	"Mac14,12": moduleprofile.FormFactorDesktop,
	"Mac16,10": moduleprofile.FormFactorDesktop,
	"Mac16,11": moduleprofile.FormFactorDesktop,
	"Mac17,16": moduleprofile.FormFactorDesktop,
	"Mac18,5":  moduleprofile.FormFactorDesktop,
	"Mac13,1":  moduleprofile.FormFactorWorkstation,
	"Mac13,2":  moduleprofile.FormFactorWorkstation,
	"Mac14,8":  moduleprofile.FormFactorWorkstation,
	"Mac14,13": moduleprofile.FormFactorWorkstation,
	"Mac14,14": moduleprofile.FormFactorWorkstation,
	"Mac15,14": moduleprofile.FormFactorWorkstation,
	"Mac16,9":  moduleprofile.FormFactorWorkstation,
	"Mac17,14": moduleprofile.FormFactorWorkstation,
	"Mac17,15": moduleprofile.FormFactorWorkstation,
}

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
	formFactor := classifyDarwinHardwareModel(model)
	if formFactor == moduleprofile.FormFactorUnknown {
		return result
	}
	result.Observations.DeviceForm = formFactor
	result.Observations.EvidenceSources.DeviceForm = "macos.sysctl.hw.model"
	return result
}

func classifyDarwinHardwareModel(model string) moduleprofile.FormFactor {
	value := strings.TrimSpace(model)
	switch {
	case strings.HasPrefix(value, "MacBookPro"),
		strings.HasPrefix(value, "MacBookAir"),
		strings.HasPrefix(value, "MacBook"):
		return moduleprofile.FormFactorNotebook
	case strings.HasPrefix(value, "iMac"):
		return moduleprofile.FormFactorAllInOne
	case strings.HasPrefix(value, "Macmini"):
		return moduleprofile.FormFactorDesktop
	case strings.HasPrefix(value, "MacPro"),
		strings.HasPrefix(value, "MacStudio"):
		return moduleprofile.FormFactorWorkstation
	default:
		if formFactor, ok := darwinModelFormFactors[value]; ok {
			return formFactor
		}
		return moduleprofile.FormFactorUnknown
	}
}
