//go:build windows

// Windows default data directory mirrors install.go's
// `%ProgramData%\frpdeck` choice. We honour FRPDECK_DATA_DIR first so
// power users that install to a non-standard prefix do not need to
// pass --data-dir on every invocation.

package dbopen

import (
	"os"
	"path/filepath"
)

func defaultDataDir() string {
	if v := os.Getenv("FRPDECK_DATA_DIR"); v != "" {
		return v
	}
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "frpdeck")
}
