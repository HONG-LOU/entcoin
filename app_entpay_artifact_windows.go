//go:build windows

package main

import "os/exec"

func openEntPayArtifact(path string, reveal bool) error {
	if reveal {
		return exec.Command("explorer.exe", "/select,"+path).Start()
	}
	return exec.Command("explorer.exe", path).Start()
}
