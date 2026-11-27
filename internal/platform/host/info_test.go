package host

import (
	"runtime"
	"testing"
)

func TestCurrentReportsRuntimePlatform(t *testing.T) {
	got := Current()
	if got.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", got.OS, runtime.GOOS)
	}
	if got.Architecture != runtime.GOARCH {
		t.Errorf("Architecture = %q, want %q", got.Architecture, runtime.GOARCH)
	}
	if got.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", got.GoVersion, runtime.Version())
	}
}
