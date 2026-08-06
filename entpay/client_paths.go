package entpay

import (
	"os"
	"path/filepath"
)

func DefaultArtifactDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads", "EntPay"), nil
}
