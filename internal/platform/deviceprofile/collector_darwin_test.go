//go:build darwin

package deviceprofile

import (
	"errors"
	"testing"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
)

func TestClassifyDarwinHardwareModel(t *testing.T) {
	for _, test := range []struct {
		model string
		want  moduleprofile.FormFactor
	}{
		{model: "MacBookPro18,3", want: moduleprofile.FormFactorNotebook},
		{model: "MacBookAir10,1", want: moduleprofile.FormFactorNotebook},
		{model: "iMac21,2", want: moduleprofile.FormFactorAllInOne},
		{model: "Macmini9,1", want: moduleprofile.FormFactorDesktop},
		{model: "MacPro7,1", want: moduleprofile.FormFactorWorkstation},
		{model: "MacStudio", want: moduleprofile.FormFactorWorkstation},
		{model: "Mac15,3", want: moduleprofile.FormFactorUnknown},
		{model: "", want: moduleprofile.FormFactorUnknown},
	} {
		t.Run(test.model, func(t *testing.T) {
			got, _ := classifyDarwinHardwareModel(test.model)
			if got != test.want {
				t.Fatalf("classifyDarwinHardwareModel(%q) = %q, want %q", test.model, got, test.want)
			}
		})
	}
}

func TestCollectDarwinUsesOnlyNormalizedModelFamily(t *testing.T) {
	result := collectDarwin(func(name string) (string, error) {
		if name != "hw.model" {
			t.Fatalf("sysctl name = %q, want hw.model", name)
		}
		return "MacBookPro18,3", nil
	})
	if result.Observations.DeviceForm != moduleprofile.FormFactorNotebook {
		t.Fatalf("device form = %q, want notebook", result.Observations.DeviceForm)
	}
	if result.Observations.EvidenceSources.DeviceForm != "macos.sysctl.hw.model" {
		t.Fatalf("evidence source = %q", result.Observations.EvidenceSources.DeviceForm)
	}
	profile := moduleprofile.Classify(result.Observations)
	if len(profile.Evidence) != 1 || profile.Evidence[0].Source != "macos.sysctl.hw.model" {
		t.Fatalf("classification evidence = %#v", profile.Evidence)
	}
	if profile.Evidence[0].Value != string(moduleprofile.FormFactorNotebook) {
		t.Fatalf("evidence leaked raw model identifier: %#v", profile.Evidence[0])
	}
}

func TestCollectDarwinKeepsUnknownWhenSysctlFails(t *testing.T) {
	result := collectDarwin(func(string) (string, error) { return "", errors.New("sysctl unavailable") })
	if result.Observations.DeviceForm != "" || len(result.SourceErrors) != 1 {
		t.Fatalf("unexpected result for unavailable hw.model: %#v", result)
	}
}
