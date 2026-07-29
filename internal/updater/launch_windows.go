//go:build windows

package updater

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const updateHelperArgument = "--entcoin-apply-update"

func LaunchInstaller(path, expectedVersion string) error {
	target, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find the running Entcoin executable: %w", err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve the running Entcoin executable: %w", err)
	}
	artifact, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve the downloaded Entcoin executable: %w", err)
	}
	if strings.EqualFold(artifact, target) {
		return errors.New("downloaded update is already the running executable")
	}
	command := exec.Command(artifact, updateHelperArgument, strconv.Itoa(os.Getpid()), target, expectedVersion)
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("schedule Entcoin update: %w", err)
	}
	return nil
}

func HandleUpdateHelper(arguments []string) (bool, error) {
	if len(arguments) == 0 || arguments[0] != updateHelperArgument {
		return false, nil
	}
	if len(arguments) != 4 {
		return true, errors.New("invalid update helper arguments")
	}
	oldPID, err := strconv.Atoi(arguments[1])
	if err != nil || oldPID <= 0 {
		return true, errors.New("invalid old process ID")
	}
	target, err := filepath.Abs(arguments[2])
	if err != nil || !strings.EqualFold(filepath.Ext(target), ".exe") {
		return true, errors.New("invalid update target")
	}
	if arguments[3] != CurrentVersion {
		return true, fmt.Errorf("update helper version is %s, expected %s", CurrentVersion, arguments[3])
	}
	source, err := os.Executable()
	if err != nil {
		return true, fmt.Errorf("find downloaded Entcoin executable: %w", err)
	}
	if err := waitForProcess(oldPID); err != nil {
		return true, recordUpdateError(target, err)
	}
	err = applyWindowsUpdate(source, target, startWindowsExecutable)
	if err != nil {
		return true, recordUpdateError(target, err)
	}
	_ = os.Remove(updateErrorPath(target))
	return true, nil
}

func waitForProcess(pid int) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open old Entcoin process: %w", err)
	}
	defer windows.CloseHandle(handle)
	result, err := windows.WaitForSingleObject(handle, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("wait for old Entcoin process: %w", err)
	}
	if result != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("wait for old Entcoin process returned %d", result)
	}
	return nil
}

func applyWindowsUpdate(source, target string, launch func(string) error) error {
	targetInfo, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("inspect current Entcoin executable: %w", err)
	}
	if !targetInfo.Mode().IsRegular() {
		return errors.New("current Entcoin executable is not a regular file")
	}
	directory := filepath.Dir(target)
	staged, err := os.CreateTemp(directory, ".entcoin-update-*.exe")
	if err != nil {
		return fmt.Errorf("create staged Entcoin executable: %w", err)
	}
	stagedPath := staged.Name()
	staged.Close()
	defer os.Remove(stagedPath)
	if err := copyExecutable(source, stagedPath, targetInfo.Mode()); err != nil {
		return err
	}
	sourceHash, err := fileSHA256(source)
	if err != nil {
		return fmt.Errorf("hash downloaded Entcoin executable: %w", err)
	}
	stagedHash, err := fileSHA256(stagedPath)
	if err != nil {
		return fmt.Errorf("hash staged Entcoin executable: %w", err)
	}
	if sourceHash != stagedHash {
		return errors.New("staged Entcoin executable does not match the verified download")
	}
	backup := target + ".update-backup"
	_ = os.Remove(backup)
	if err := copyExecutable(target, backup, targetInfo.Mode()); err != nil {
		_ = os.Remove(backup)
		return fmt.Errorf("back up current Entcoin executable: %w", err)
	}
	if err := replaceWindowsFile(stagedPath, target); err != nil {
		return rollbackWindowsUpdate(backup, target, fmt.Errorf("replace current Entcoin executable: %w", err))
	}
	targetHash, err := fileSHA256(target)
	if err != nil || targetHash != sourceHash {
		return rollbackWindowsUpdate(backup, target, errors.New("installed Entcoin executable failed verification"))
	}
	if err := launch(target); err != nil {
		return rollbackWindowsUpdate(backup, target, fmt.Errorf("restart updated Entcoin executable: %w", err))
	}
	_ = os.Remove(backup)
	return nil
}

func rollbackWindowsUpdate(backup, target string, updateErr error) error {
	if err := replaceWindowsFile(backup, target); err != nil {
		return fmt.Errorf("%w; restore failed, rollback preserved at %s: %v", updateErr, backup, err)
	}
	return updateErr
}

func copyExecutable(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(source), err)
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(target), err)
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		return fmt.Errorf("copy %s: %w", filepath.Base(target), copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("flush %s: %w", filepath.Base(target), syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(target), closeErr)
	}
	return nil
}

func fileSHA256(path string) ([sha256.Size]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [sha256.Size]byte{}, err
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func replaceWindowsFile(source, target string) error {
	sourcePointer, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(sourcePointer, targetPointer, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func startWindowsExecutable(path string) error {
	command := exec.Command(path)
	command.Dir = filepath.Dir(path)
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	return command.Start()
}

func updateErrorPath(target string) string {
	return target + ".update-error.log"
}

func recordUpdateError(target string, updateErr error) error {
	message := []byte(updateErr.Error() + "\r\n")
	if err := os.WriteFile(updateErrorPath(target), message, 0o600); err != nil {
		return fmt.Errorf("%w (also failed to write update log: %v)", updateErr, err)
	}
	return updateErr
}
