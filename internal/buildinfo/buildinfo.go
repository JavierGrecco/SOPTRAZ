package buildinfo

var Version = "0.1.0-dev"

func Info() map[string]string {
	return map[string]string{"name": "SOPTRAZ", "version": Version}
}
