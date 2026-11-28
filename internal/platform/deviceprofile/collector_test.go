package deviceprofile

import (
	"testing"

	moduleprofile "github.com/JavierGrecco/SOPTRAZ/internal/modules/deviceprofile"
)

func TestNewUnknownResultInitializesEveryCapabilityExplicitly(t *testing.T) {
	result := newUnknownResult()
	capabilities := result.Observations.Capabilities
	states := []moduleprofile.TriState{
		capabilities.BatteryPresent,
		capabilities.Touch,
		capabilities.Pen,
		capabilities.PhysicalKeyboard,
		capabilities.KeyboardAttached,
		capabilities.DedicatedGPU,
		capabilities.ThermalSensors,
		capabilities.DockPresent,
		capabilities.VirtualMachine,
	}
	for index, state := range states {
		if state != moduleprofile.Unknown {
			t.Errorf("capability state %d = %v, want unknown", index, state)
		}
	}
	if capabilities.PowerSource != moduleprofile.PowerSourceUnknown {
		t.Errorf("power source = %q, want unknown", capabilities.PowerSource)
	}
	if capabilities.ConvertibleSlate != moduleprofile.SlateModeUnknown {
		t.Errorf("convertible slate mode = %q, want unknown", capabilities.ConvertibleSlate)
	}
}
