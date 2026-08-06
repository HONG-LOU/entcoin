//go:build !windows

package entpay

import "os"

func unsafeArtifactPathComponent(path string) (bool, error) {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0, err
}
