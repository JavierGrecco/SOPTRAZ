package host

import "runtime"

// Host information intentionally excludes serial numbers, usernames, hostnames, addresses and secrets.
type Info struct {
	OS           string
	Architecture string
	GoVersion    string
}

func Current() Info {
	return Info{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		GoVersion:    runtime.Version(),
	}
}
