//go:build !windows

package deviceprofile

// Collect returns no Windows hardware data on non-Windows platforms. The
// shared classifier and SMBIOS parser remain portable for deterministic tests.
func Collect() Result {
	result := Result{}
	result.addSourceError("platform", errWindowsCollectorUnsupported)
	return result
}
