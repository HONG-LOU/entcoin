//go:build linux

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func LaunchInstaller(path, _ string) error {
	if err := exec.Command("pkexec", "/usr/bin/apt-get", "install", "-y", path).Run(); err != nil {
		return fmt.Errorf("install Entcoin update: %w", err)
	}
	const restartScript = `
pid=$1
while kill -0 "$pid" 2>/dev/null; do sleep 0.2; done
exec /usr/bin/entcoin
`
	command := exec.Command("/bin/sh", "-c", restartScript, "entcoin-restart", strconv.Itoa(os.Getpid()))
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("schedule Entcoin restart: %w", err)
	}
	return nil
}
