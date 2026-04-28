//go:build !windows

// Linux/macOS default data directory mirrors install.go's FHS-strict
// choice. Keeping the fallback in a per-OS file means the CLI does
// not need a runtime.GOOS switch and the value is one grep away.

package dbopen

import "os"

func defaultDataDir() string {
	if v := os.Getenv("FRPDECK_DATA_DIR"); v != "" {
		return v
	}
	return "/var/lib/frpdeck"
}
