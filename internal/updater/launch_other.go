//go:build !linux && !windows

package updater

import (
	"fmt"
	"runtime"
)

func LaunchInstaller(string, string) error {
	return fmt.Errorf("automatic updates do not support %s", runtime.GOOS)
}

func HandleUpdateHelper([]string) (bool, error) {
	return false, nil
}
